package bridge

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/leader"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/transaction"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/worker"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// Bridge represents the main bridge service
type Bridge struct {
	modularConfig *config.ModularConfig
	configService *config.ConfigService
	db            *database.DB
	readClient    rpc.EthClient
	writeClients  map[int64]Destination

	// Channels for communication
	updateChan   chan *bridgetypes.UpdateRequest
	eventChan    chan *bridgetypes.EventData
	errorChan    chan error
	shutdownChan chan struct{}

	// State management
	mu                 sync.RWMutex
	running            bool
	stats              *bridgetypes.BridgeStats
	lastProcessedBlock uint64

	// Goroutine coordination
	wg sync.WaitGroup

	// Worker management
	workerPool *worker.WorkerPool

	// Router system
	routerRegistry *router.GenericRegistry

	// On-chain monitor for replica failover
	onChainMonitor *leader.OnChainMonitor

	// Block scanner
	blockScanner BlockScanner

	// Event processor
	eventProcessor *processor.GenericEventProcessor

	// Metrics tracking
	metricsManager *MetricsManager

	// API components
	apiServer *api.Server

	// Transaction queue manager
	queueManager *transaction.QueueManager
}

// NewBridge creates a new bridge instance
func NewBridge(modularCfg *config.ModularConfig, cfgService *config.ConfigService, db *database.DB, metricsCollector *metrics.Collector) (*Bridge, error) {
	// Connect to source chain with multiple RPC support
	sourceConfig := cfgService.GetInfrastructure().Source
	readClient, err := rpc.NewMultiClient(sourceConfig.RPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source chain: %w", err)
	}
	logger.Infof("Connected to source chain %s via %s", sourceConfig.Name, readClient.GetCurrentRPCURL())

	ethClient, err := readClient.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	// Create transaction queue manager
	queueManager := transaction.NewQueueManager(1000, metricsCollector)

	routerRegistry := router.NewGenericRegistry()
	enabledRouterPointers := cfgService.GetEnabledRouters()

	var enabledRouters []config.RouterConfig
	for _, routerPtr := range enabledRouterPointers {
		routerCfg := *routerPtr

		for i := range routerCfg.Destinations {
			dest := &routerCfg.Destinations[i]

			if dest.ContractRef != "" {
				contract := cfgService.GetContractConfig(dest.ContractRef)
				if contract != nil {
					dest.ChainID = contract.ChainID
					dest.Contract = contract.Address
					logger.Debugf("Resolved contract_ref %s to chain %d contract %s",
						dest.ContractRef, dest.ChainID, dest.Contract)
				} else {
					logger.Warnf("Contract reference %s not found for router %s",
						dest.ContractRef, routerCfg.ID)
				}
			}
		}

		enabledRouters = append(enabledRouters, routerCfg)
	}
	if err := routerRegistry.LoadRouters(enabledRouters); err != nil {
		logger.Errorf("Failed to load routers: %v", err)
	}

	destClients := make(map[int64]Destination)
	for _, chainConfig := range cfgService.GetEnabledChains() {
		contracts := cfgService.GetContractsForChain(chainConfig.ChainID)
		if len(contracts) == 0 {
			continue
		}

		var destClient Destination
		if chainConfig.Kind == "arch" {
			// Arch Network: use the first enabled contract as the receiver program.
			archContract := contracts[0]
			var buildErr error
			destClient, buildErr = buildDestination(*chainConfig, *archContract, cfgService.GetInfrastructure().PrivateKey)
			if buildErr != nil {
				logger.Errorf("Failed to create Arch destination client for chain %d: %v", chainConfig.ChainID, buildErr)
				continue
			}
		} else {
			// EVM path: pass all contracts (existing behaviour).
			// For NonceManager
			oracleCount := countDestinationsForChain(routerRegistry, chainConfig.ChainID)
			maxSafeGap := calculateMaxSafeGap(oracleCount)
			logger.Infof("Chain %d (%s): %d oracles configured, maxSafeGap=%d",
				chainConfig.ChainID, chainConfig.Name, oracleCount, maxSafeGap)

			var evmErr error
			destClient, evmErr = NewWriteClient(chainConfig, contracts, cfgService.GetInfrastructure().PrivateKey, queueManager, maxSafeGap)
			if evmErr != nil {
				logger.Errorf("Failed to create destination client for chain %d: %v", chainConfig.ChainID, evmErr)
				continue
			}
		}
		destClients[chainConfig.ChainID] = destClient
	}

	if len(destClients) == 0 {
		return nil, fmt.Errorf("no destination clients available")
	}

	workerPool := worker.NewWorkerPool(
		cfgService.GetInfrastructure().WorkerPool.MaxWorkers,
		cfgService.GetInfrastructure().WorkerPool.TaskQueueSize,
	)
	if metricsCollector != nil {
		workerPool.SetMetricsCollector(metricsCollector)
	}

	eventChan := make(chan *bridgetypes.EventData, 100)
	errorChan := make(chan error, 10)

	// Create metrics manager
	metricsManager := NewMetricsManager(metricsCollector)

	ethClients := make(map[int64]rpc.EthClient)
	for chainID, dest := range destClients {
		if wc, ok := dest.(*WriteClient); ok {
			ethClients[chainID] = wc.GetEthClient()
		}
	}

	// Create bridge instance now that we have all dependencies
	bridge := &Bridge{
		modularConfig: modularCfg,
		configService: cfgService,
		db:            db,
		readClient:    readClient,
		writeClients:  destClients,
		updateChan:    make(chan *bridgetypes.UpdateRequest, 1000),
		eventChan:     eventChan,
		errorChan:     errorChan,
		shutdownChan:  make(chan struct{}),
		stats: &bridgetypes.BridgeStats{
			ChainStats: make(map[int64]*bridgetypes.ChainStatus),
			StartTime:  time.Now(),
		},
		lastProcessedBlock: cfgService.GetInfrastructure().Source.StartBlock,
		workerPool:         workerPool,
		routerRegistry:     routerRegistry,
		metricsManager:     metricsManager,
		queueManager:       queueManager,
	}

	// Create block scanner if enabled
	if cfgService.GetInfrastructure().BlockScanner.Enabled {
		scanner, err := CreateBlockScanner(cfgService, readClient, db, eventChan, errorChan)
		if err != nil {
			return nil, fmt.Errorf("failed to create block scanner: %w", err)
		}
		bridge.blockScanner = scanner
	}

	// Create generic event processor
	// Create callback function to report queue size after enqueue
	reportQueueSize := func() {
		if bridge.metricsManager != nil {
			bridge.metricsManager.ReportUpdateQueueSize(len(bridge.updateChan))
		}
	}

	eventProcessor, err := processor.NewGenericEventProcessor(
		&cfgService.GetInfrastructure().EventProcessor,
		cfgService.GetEventDefinitions(),
		cfgService,
		db,
		routerRegistry,
		ethClient,
		ethClients,
		eventChan,
		errorChan,
		bridge.updateChan,
		metricsCollector,
		reportQueueSize,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create event processor: %w", err)
	}
	bridge.eventProcessor = eventProcessor

	// Initialize chain stats
	bridge.initializeChainStats()

	replicaConfig := cfgService.GetInfrastructure().Replica
	monitorConfig := leader.DefaultMonitorConfig()

	if replicaConfig != nil {
		replicaConfig.ApplyEnvOverrides()

		monitorConfig.Enabled = replicaConfig.Enabled

		if replicaConfig.TimeThresholdOffset > 0 {
			monitorConfig.TimeThresholdOffset = replicaConfig.TimeThresholdOffset.Duration()
		}
		if replicaConfig.CheckInterval > 0 {
			monitorConfig.CheckInterval = replicaConfig.CheckInterval.Duration()
		}
		if replicaConfig.PriceDeviationOffset != "" {
			if priceDevOffset := leader.ParsePriceDeviation(replicaConfig.PriceDeviationOffset); priceDevOffset != nil {
				monitorConfig.PriceDeviationOffset = priceDevOffset
			}
		}
		logMonitorConfig(monitorConfig, "initialized")
	} else {
		logMonitorConfig(monitorConfig, "using defaults (no replica config)")
	}

	bridge.onChainMonitor = leader.NewOnChainMonitor(routerRegistry, ethClients, monitorConfig)

	logger.Infof("Bridge initialized with %d routers", routerRegistry.Count())

	return bridge, nil
}

// logMonitorConfig logs monitoring configuration values
func logMonitorConfig(config leader.MonitorConfig, status string) {
	priceDevPercent := "0%"
	if config.PriceDeviationOffset != nil {
		percent := new(big.Float).Mul(config.PriceDeviationOffset, big.NewFloat(100))
		priceDevPercent = percent.Text('f', 2) + "%"
	}
	logger.Infof("Replica monitoring config %s: enabled=%v, time_threshold_offset=%v, price_deviation_offset=%s, check_interval=%v",
		status, config.Enabled, config.TimeThresholdOffset, priceDevPercent, config.CheckInterval)
}

// countDestinationsForChain counts all destinations (oracles) configured for a specific chain
func countDestinationsForChain(routerRegistry *router.GenericRegistry, chainID int64) int {
	if routerRegistry == nil {
		logger.Warnf("Router registry is nil for chain %d, cannot count destinations", chainID)
		return 0
	}

	activeRouters := routerRegistry.GetActiveRouters()
	count := 0

	for _, router := range activeRouters {
		destinations := router.GetConfigDestinations()

		for _, dest := range destinations {
			if dest.ChainID == chainID {
				count++
				logger.Debugf("Found destination for chain %d: router=%s, contract=%s",
					chainID, router.ID(), dest.Contract)
			}
		}
	}

	return count
}

// calculateMaxSafeGap calculates dynamic maxSafeGap based on oracle count
func calculateMaxSafeGap(oracleCount int) uint64 {
	const (
		baseValue  = 5
		multiplier = 10
		minValue   = 5
		maxValue   = 500
	)

	if oracleCount < 0 {
		oracleCount = 0
	}

	calculated := baseValue + (oracleCount * multiplier)

	if calculated < minValue {
		calculated = minValue
	}
	if calculated > maxValue {
		calculated = maxValue
	}

	return uint64(calculated)
}

// Start starts the bridge service
func (b *Bridge) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("bridge is already running")
	}
	b.running = true
	b.mu.Unlock()

	logger.Info("Starting bridge service")

	// Start transaction queue manager
	b.queueManager.Start()

	// Start on-chain monitor
	if b.onChainMonitor != nil {
		b.onChainMonitor.Start()
		time.Sleep(2 * time.Second) // Wait for initial check
	}

	// Start worker pool
	b.workerPool.Start(ctx)

	// Start block scanner if enabled
	if b.blockScanner != nil {
		if err := b.blockScanner.Start(ctx); err != nil {
			return fmt.Errorf("failed to start block scanner: %w", err)
		}
		logger.Info("Block scanner started")

		// Start generic event processor
		if b.eventProcessor != nil {
			if err := b.eventProcessor.Start(ctx); err != nil {
				return fmt.Errorf("failed to start event processor: %w", err)
			}
			logger.Info("Generic event processor started")
		}

		// Start error handler
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.handleErrors(ctx)
		}()
	}

	// Start update processor
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.processUpdates(ctx)
	}()

	// Initialize update channel metric to 0 immediately
	if b.metricsManager != nil {
		b.metricsManager.ReportUpdateQueueSize(0)
		logger.Debugf("Initialized update channel metric to 0")
	}

	// Start update channel metrics reporter
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.reportUpdateChanMetrics(ctx)
	}()

	// Start health checker
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.healthCheck(ctx)
	}()

	// Start metrics server
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.startMetricsServer(ctx)
	}()

	logger.Info("All bridge components started successfully")
	return nil
}

// Stop stops the bridge service
func (b *Bridge) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return fmt.Errorf("bridge is not running")
	}
	b.mu.Unlock()

	logger.Info("Stopping bridge service")

	// Signal shutdown
	close(b.shutdownChan)

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All bridge goroutines stopped gracefully")
	case <-ctx.Done():
		logger.Warn("Bridge shutdown timeout reached, some goroutines may still be running")
	}

	if b.onChainMonitor != nil {
		b.onChainMonitor.Stop()
	}

	// Stop block scanner if running
	if b.blockScanner != nil {
		if err := b.blockScanner.Stop(); err != nil {
			logger.Errorf("Failed to stop block scanner: %v", err)
		}
	}

	// Stop worker pool
	b.workerPool.Stop(ctx)

	// Stop transaction queue manager
	b.queueManager.Stop()

	// Close connections
	b.readClient.Close()
	for _, destClient := range b.writeClients {
		if wc, ok := destClient.(*WriteClient); ok {
			wc.client.Close()
		}
	}

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	return nil
}

// Wait waits for all bridge goroutines to finish
func (b *Bridge) Wait() {
	b.wg.Wait()
}

// reportUpdateChanMetrics periodically reports the update channel queue size
func (b *Bridge) reportUpdateChanMetrics(ctx context.Context) {
	logger.Info("Starting update channel metrics reporter")
	ticker := time.NewTicker(100 * time.Millisecond) // Report every 100ms to catch items quickly
	defer ticker.Stop()

	// Report initial size immediately (even if 0, to ensure metric is exposed)
	if b.metricsManager != nil {
		size := len(b.updateChan)
		b.metricsManager.ReportUpdateQueueSize(size)
		logger.Debugf("Initial update channel size: %d", size)
	} else {
		logger.Warn("Metrics collector is nil, cannot report update channel size")
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Update channel metrics reporter stopped (context cancelled)")
			return
		case <-b.shutdownChan:
			logger.Info("Update channel metrics reporter stopped (shutdown)")
			return
		case <-ticker.C:
			// Periodically report updateChan size (more frequently to catch items)
			if b.metricsManager != nil {
				size := len(b.updateChan)
				b.metricsManager.ReportUpdateQueueSize(size)
				// Log when queue has items
				if size > 0 {
					logger.Debugf("Update channel size: %d/%d", size, cap(b.updateChan))
				}
			}
		}
	}
}

// processUpdates processes update requests
func (b *Bridge) processUpdates(ctx context.Context) {
	logger.Info("Starting update processor")

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case updateReq := <-b.updateChan:
			// Report metric: when we successfully dequeue, we know there was at least 1 item
			// Report current size + 1 to show the size BEFORE we dequeued this item
			// This gives a more accurate picture of queue depth
			if b.metricsManager != nil {
				queueSize := len(b.updateChan)
				b.metricsManager.ReportUpdateQueueSize(queueSize)
			}

			// Check if update is stale based on last update in cache
			if updateReq.Intent != nil && !updateReq.CreatedAt.IsZero() && updateReq.Contract != nil {
				destClient := b.writeClients[updateReq.DestinationChain.ChainID]
				if wc, ok := destClient.(*WriteClient); ok {
					lastUpdateTime := wc.getLastUpdate(updateReq.Intent.Symbol, updateReq.Contract.Address)
					if !lastUpdateTime.IsZero() && updateReq.CreatedAt.Before(lastUpdateTime) {
						logger.Debugf("Skipping stale update: symbol=%s, chain=%d, contract=%s, updateTime=%v, lastUpdateTime=%v, age=%v",
							updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID, updateReq.Contract.Address,
							updateReq.CreatedAt, lastUpdateTime, lastUpdateTime.Sub(updateReq.CreatedAt))
						continue
					}
				}
			}

			if b.onChainMonitor != nil {
				symbol := "unknown"
				if updateReq.Intent != nil && updateReq.Intent.Symbol != "" {
					symbol = updateReq.Intent.Symbol
				} else if updateReq.ExtractedData != nil && updateReq.RouterID != "" {
					if routerInstance := b.routerRegistry.GetRouterByID(updateReq.RouterID); routerInstance != nil {
						if s := routerInstance.GetSymbolFromData(updateReq.ExtractedData); s != "" && s != "unknown" {
							symbol = s
						}
					}
				}

				var incomingPrice *big.Int
				if updateReq.Intent != nil && updateReq.Intent.Price != nil {
					incomingPrice = updateReq.Intent.Price
				}

				shouldProcess := b.onChainMonitor.ShouldProcess(
					updateReq.DestinationChain.ChainID,
					common.HexToAddress(updateReq.Contract.Address),
					symbol,
					incomingPrice)

				if !shouldProcess {
					logger.Infof("Skipping update - primary active for chain %d contract %s symbol %s",
						updateReq.DestinationChain.ChainID,
						updateReq.Contract.Address,
						symbol)
					continue
				}

				updateReq.TriggeredByMonitoring = true
				logger.Infof("Processing update: monitoring check passed for chain=%d contract=%s symbol=%s",
					updateReq.DestinationChain.ChainID,
					updateReq.Contract.Address,
					symbol)
			}

			// Create task ID based on available data
			var taskID string
			if updateReq.Intent != nil {
				taskID = fmt.Sprintf("Process Updates %s-%d", updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)
			} else if updateReq.Event != nil {
				// For events like IntArraySet that don't have Intent
				taskID = fmt.Sprintf("Process Updates %s-%d-%d", updateReq.Event.EventName, updateReq.DestinationChain.ChainID, time.Now().Unix())
			} else {
				taskID = fmt.Sprintf("Process Updates unknown-%d-%d", updateReq.DestinationChain.ChainID, time.Now().Unix())
			}

			b.workerPool.Submit(&worker.WorkerTask{
				ID:      taskID,
				Request: updateReq,
				Handler: b.handleUpdateRequest,
			})
		}
	}
}

// handleUpdateRequest processes an update request using the TransactionHandler
func (b *Bridge) handleUpdateRequest(ctx context.Context, task *worker.WorkerTask) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in handleUpdateRequest: %v", r)
			logger.Errorf("PANIC in handleUpdateRequest: %v", r)
		}
	}()

	var rawDB *sql.DB
	if b.db != nil {
		rawDB = b.db.DB
	}
	handler := NewTransactionHandler(b.writeClients, b.routerRegistry, b.metricsManager.GetTracker(), rawDB)
	return handler.Process(ctx, task.Request)
}

// buildDestination is the factory that picks the right backend based on
// chain.Kind. An empty Kind (or "evm") selects the existing EVM WriteClient.
// "arch" selects ArchWriteClient. For EVM chains the caller must use
// NewWriteClient directly (passing the full contract slice); this function
// handles the single-contract Arch path.
func buildDestination(chain config.ChainConfig, contract config.ContractConfig, signerSecretHex string) (Destination, error) {
	switch chain.Kind {
	case "arch":
		return newArchDestinationFromConfig(chain, contract, signerSecretHex)
	default: // "evm" or ""
		// Single-contract shim: wrap in a slice for NewWriteClient.
		// In production, NewBridge calls NewWriteClient directly with the full
		// contract slice; this path is used by tests and tooling that construct
		// an EVM destination from a single ContractConfig.
		return NewWriteClient(&chain, []*config.ContractConfig{&contract}, signerSecretHex, nil, 5)
	}
}

// newArchDestinationFromConfig validates config fields and constructs an
// ArchWriteClient from a chain/contract config pair.
func newArchDestinationFromConfig(chain config.ChainConfig, contract config.ContractConfig, signerSecretHex string) (Destination, error) {
	if signerSecretHex == "" {
		return nil, fmt.Errorf("arch destination %d: missing signer key", chain.ChainID)
	}
	signer, err := arch.NewSignerFromHex(signerSecretHex)
	if err != nil {
		return nil, fmt.Errorf("arch destination %d: %w", chain.ChainID, err)
	}
	receiverPK, err := decodePubkeyHex(contract.Address)
	if err != nil {
		return nil, fmt.Errorf("arch destination %d: receiver address: %w", chain.ChainID, err)
	}
	if contract.FeeHookProgramID == "" {
		return nil, fmt.Errorf("arch destination %d: missing fee_hook_program_id", chain.ChainID)
	}
	feeHookPK, err := decodePubkeyHex(contract.FeeHookProgramID)
	if err != nil {
		return nil, fmt.Errorf("arch destination %d: fee hook id: %w", chain.ChainID, err)
	}
	if len(chain.RPCURLs) == 0 {
		return nil, fmt.Errorf("arch destination %d: no rpc_urls", chain.ChainID)
	}
	rpc := arch.NewRPC(chain.RPCURLs[0])
	return NewArchWriteClient(chain.ChainID, receiverPK, feeHookPK, rpc, signer, 30*time.Second), nil
}

// decodePubkeyHex decodes a 64-character hex string into an arch.Pubkey.
func decodePubkeyHex(s string) (arch.Pubkey, error) {
	if len(s) != 64 {
		return arch.Pubkey{}, fmt.Errorf("pubkey hex must be 64 chars, got %d", len(s))
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return arch.Pubkey{}, err
	}
	var out arch.Pubkey
	copy(out[:], raw)
	return out, nil
}

// BuildDestinationForTest is an exported thin wrapper around buildDestination
// that allows external test packages (e.g. archtest) to exercise the factory
// without exposing it as part of the public production API.
func BuildDestinationForTest(chain config.ChainConfig, contract config.ContractConfig, signerSecretHex string) (Destination, error) {
	return buildDestination(chain, contract, signerSecretHex)
}

// callRouterMethod calls a contract method using router configuration
