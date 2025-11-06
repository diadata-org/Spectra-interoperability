package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/transaction"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/worker"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// Bridge represents the main bridge service
type Bridge struct {
	modularConfig      *config.ModularConfig
	configService      *config.ConfigService
	legacyDestinations map[int64]*config.DestinationConfig // temporary for API compatibility
	db                 *database.DB
	readClient         rpc.EthClient
	writeClients       map[int64]*WriteClient

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

	// Block scanner
	blockScanner BlockScanner

	// Event processor
	eventProcessor *processor.GenericEventProcessor

	// Metrics tracking
	metricsTracker *MetricsTracker
	// API components
	apiServer *api.Server
	metrics   *metrics.Metrics

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
	queueManager := transaction.NewQueueManager(1000)

	// Create destination clients
	destClients := make(map[int64]*WriteClient)
	for _, chainConfig := range cfgService.GetEnabledChains() {
		contracts := cfgService.GetContractsForChain(chainConfig.ChainID)
		if len(contracts) == 0 {
			continue // Skip chains with no contracts
		}

		destClient, err := NewWriteClient(chainConfig, contracts, cfgService.GetInfrastructure().PrivateKey, queueManager)
		if err != nil {
			logger.Errorf("Failed to create destination client for chain %d: %v", chainConfig.ChainID, err)
			continue
		}
		destClients[chainConfig.ChainID] = destClient
	}

	if len(destClients) == 0 {
		return nil, fmt.Errorf("no destination clients available")
	}

	// Create worker pool with configured size
	workerPool := worker.NewWorkerPool(
		cfgService.GetInfrastructure().WorkerPool.MaxWorkers,
		cfgService.GetInfrastructure().WorkerPool.TaskQueueSize,
	)

	// Create router registry and load routers
	routerRegistry := router.NewGenericRegistry()
	// Load routers directly (convert pointers to values for registry)
	enabledRouterPointers := cfgService.GetEnabledRouters()

	var enabledRouters []config.RouterConfig
	for _, routerPtr := range enabledRouterPointers {
		// Create a copy of the router config
		routerCfg := *routerPtr

		// Resolve contract references in destinations
		for i := range routerCfg.Destinations {
			dest := &routerCfg.Destinations[i]

			// If using contract_ref, resolve it to get ChainID and Contract address
			if dest.ContractRef != "" {
				contract := cfgService.GetContractConfig(dest.ContractRef)
				if contract != nil {
					// Populate the legacy fields from the contract config
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

	// Create channels
	eventChan := make(chan *bridgetypes.EventData, 100)
	errorChan := make(chan error, 10)

	// Create metrics tracker
	var metricsTracker *MetricsTracker
	if metricsCollector != nil {
		metricsTracker = NewMetricsTracker(metricsCollector)
	}

	// Get destination eth clients for event processor
	destEthClients := make(map[int64]*ethclient.Client)
	for chainID, destClient := range destClients {
		ethClient, err := destClient.client.GetClient()
		if err != nil {
			return nil, fmt.Errorf("failed to get eth client for chain %d: %w", chainID, err)
		}
		destEthClients[chainID] = ethClient
	}

	legacyDestinations := make(map[int64]*config.DestinationConfig)
	for _, chain := range cfgService.GetEnabledChains() {
		contracts := cfgService.GetContractsForChain(chain.ChainID)
		var legacyContracts []config.LegacyContractConfig
		for _, contract := range contracts {
			legacyContract := config.LegacyContractConfig{
				Name:          contract.Address, // Use address as name for compatibility
				Address:       contract.Address,
				Type:          contract.Type,
				Enabled:       contract.Enabled,
				ABI:           contract.ABI,
				GasLimit:      contract.GasLimit,
				GasMultiplier: contract.GasMultiplier,
				MaxGasPrice:   contract.MaxGasPrice,
				Methods:       contract.Methods,
			}
			legacyContracts = append(legacyContracts, legacyContract)
		}

		legacyDestinations[chain.ChainID] = &config.DestinationConfig{
			ChainID:   chain.ChainID,
			Name:      chain.Name,
			RPCURLs:   chain.RPCURLs,
			Enabled:   chain.Enabled,
			Contracts: legacyContracts,
		}
	}

	// Create bridge instance now that we have all dependencies
	bridge := &Bridge{
		modularConfig:      modularCfg,
		configService:      cfgService,
		legacyDestinations: legacyDestinations,
		db:                 db,
		readClient:         readClient,
		writeClients:       destClients,
		updateChan:         make(chan *bridgetypes.UpdateRequest, 1000),
		eventChan:          eventChan,
		errorChan:          errorChan,
		shutdownChan:       make(chan struct{}),
		stats: &bridgetypes.BridgeStats{
			ChainStats: make(map[int64]*bridgetypes.ChainStatus),
			StartTime:  time.Now(),
		},
		lastProcessedBlock: cfgService.GetInfrastructure().Source.StartBlock,
		workerPool:         workerPool,
		routerRegistry:     routerRegistry,
		metricsTracker:     metricsTracker,
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
	eventProcessor, err := processor.NewGenericEventProcessor(
		&cfgService.GetInfrastructure().EventProcessor,
		cfgService.GetEventDefinitions(),
		legacyDestinations,
		db,
		routerRegistry,
		ethClient,
		destEthClients,
		eventChan,
		errorChan,
		bridge.updateChan,
		metricsCollector,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create event processor: %w", err)
	}
	bridge.eventProcessor = eventProcessor

	// Store metrics collector for API server
	if metricsCollector != nil {
		bridge.metrics = metricsCollector.FailoverMetrics
	}

	// Initialize chain stats
	bridge.initializeChainStats()

	logger.Infof("Bridge initialized with %d routers", routerRegistry.Count())

	return bridge, nil
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
		destClient.client.Close()
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

	handler := NewTransactionHandler(b)
	return handler.Process(ctx, task.Request)
}

// callRouterMethod calls a contract method using router configuration

// waitForReceipt waits for a transaction receipt
func (b *Bridge) waitForReceipt(ctx context.Context, client rpc.EthClient, txHash common.Hash) (*types.Receipt, error) {
	logger.Infof("Waiting for transaction receipt: %s", txHash.Hex())

	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	attempts := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for transaction receipt after 5 minutes")
		case <-ticker.C:
			attempts++
			receipt, err := client.TransactionReceipt(ctx, txHash)
			if err != nil {
				if attempts%12 == 0 { // Log every minute
					logger.Debugf("Still waiting for receipt %s (attempt %d): %v", txHash.Hex(), attempts, err)
				}
				continue
			}
			logger.Infof("Transaction receipt received: %s, status: %d, gas used: %d",
				txHash.Hex(), receipt.Status, receipt.GasUsed)
			return receipt, nil
		}
	}
}
