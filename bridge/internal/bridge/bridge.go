package bridge

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/utils"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/router"
)

// Bridge represents the main bridge service
type Bridge struct {
	config             *config.Config
	db                 *database.DB
	sourceClient       rpc.EthClient
	registryClient     *contracts.RegistryClient
	destinationClients map[int64]*DestinationClient

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

	// Worker management
	workerPool *WorkerPool

	// Router system
	routerRegistry *router.Registry

	// Block scanner
	blockScanner BlockScanner

	// Metrics tracking
	metricsTracker *MetricsTracker
}

// DestinationClient represents a client for a destination chain
type DestinationClient struct {
	config         *config.DestinationConfig
	client         rpc.EthClient
	receiverClient *contracts.ReceiverClient
	lastUpdate     map[string]time.Time // symbol -> last update time
	mu             sync.RWMutex
}

// NewBridge creates a new bridge instance
func NewBridge(cfg *config.Config, db *database.DB, metricsCollector *metrics.Collector) (*Bridge, error) {
	// Connect to source chain with multiple RPC support
	sourceClient, err := rpc.NewMultiClient(cfg.Source.RPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source chain: %w", err)
	}
	logger.Infof("Connected to source chain %s via %s", cfg.Source.Name, sourceClient.GetCurrentRPCURL())

	// Create registry client
	// Note: RegistryAddress should be extracted from Source.Contracts if needed
	var registryAddress string
	if registryContract, ok := cfg.Source.Contracts["registry"]; ok {
		if addr, ok := registryContract["address"].(string); ok {
			registryAddress = addr
		}
	}
	if registryAddress == "" {
		return nil, fmt.Errorf("registry address not found in source contracts")
	}

	// Get the underlying ethclient for contracts
	ethClient, err := sourceClient.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	registryClient, err := contracts.NewRegistryClient(
		ethClient,
		common.HexToAddress(registryAddress),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create destination clients
	destClients := make(map[int64]*DestinationClient)
	for _, destConfig := range cfg.GetEnabledDestinations() {
		destClient, err := NewDestinationClient(destConfig, cfg.PrivateKey)
		if err != nil {
			logger.Errorf("Failed to create destination client for chain %d: %v", destConfig.ChainID, err)
			continue
		}
		destClients[destConfig.ChainID] = destClient
	}

	if len(destClients) == 0 {
		return nil, fmt.Errorf("no destination clients available")
	}

	// Create worker pool with configured size
	workerPool := NewWorkerPool(cfg.WorkerPool.MaxWorkers)

	// Create router registry and load routers
	routerRegistry := router.NewRegistry()
	if err := routerRegistry.LoadFromConfig(cfg.Routers); err != nil {
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

	bridge := &Bridge{
		config:             cfg,
		db:                 db,
		sourceClient:       sourceClient,
		registryClient:     registryClient,
		destinationClients: destClients,
		updateChan:         make(chan *bridgetypes.UpdateRequest, 1000),
		eventChan:          eventChan,
		errorChan:          errorChan,
		shutdownChan:       make(chan struct{}),
		stats: &bridgetypes.BridgeStats{
			ChainStats: make(map[int64]*bridgetypes.ChainStatus),
			StartTime:  time.Now(),
		},
		lastProcessedBlock: cfg.Source.StartBlock,
		workerPool:         workerPool,
		routerRegistry:     routerRegistry,
		metricsTracker:     metricsTracker,
	}

	// Create block scanner if enabled
	if cfg.BlockScanner.Enabled {
		scanner, err := CreateBlockScanner(cfg, sourceClient, db, eventChan, errorChan)
		if err != nil {
			return nil, fmt.Errorf("failed to create block scanner: %w", err)
		}
		bridge.blockScanner = scanner
	}

	// Initialize chain stats
	bridge.initializeChainStats()

	logger.Infof("Bridge initialized with %d routers", len(routerRegistry.GetAll()))

	return bridge, nil
}

// NewDestinationClient creates a new destination client
func NewDestinationClient(cfg *config.DestinationConfig, privateKey string) (*DestinationClient, error) {
	// Connect to destination chain with multiple RPC support
	client, err := rpc.NewMultiClient(cfg.RPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to destination chain: %w", err)
	}
	logger.Infof("Connected to destination chain %s via %s", cfg.Name, client.GetCurrentRPCURL())

	// Create receiver client
	// Note: ReceiverAddress should be extracted from destination contracts
	var receiverAddress string
	for _, contract := range cfg.Contracts {
		if (contract.Type == "receiver" || contract.Type == "pushoracle") && contract.Enabled {
			receiverAddress = contract.Address
			break
		}
	}
	if receiverAddress == "" {
		return nil, fmt.Errorf("no enabled receiver contract found")
	}

	// Get the underlying ethclient for contracts
	ethClient, err := client.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	receiverClient, err := contracts.NewReceiverClient(
		ethClient,
		common.HexToAddress(receiverAddress),
		privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver client: %w", err)
	}

	return &DestinationClient{
		config:         cfg,
		client:         client,
		receiverClient: receiverClient,
		lastUpdate:     make(map[string]time.Time),
	}, nil
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

	// Start worker pool
	b.workerPool.Start(ctx)

	// Start monitoring goroutines
	var wg sync.WaitGroup

	// Start block scanner if enabled
	if b.blockScanner != nil {
		if err := b.blockScanner.Start(ctx); err != nil {
			return fmt.Errorf("failed to start block scanner: %w", err)
		}
		logger.Info("Block scanner started")

		// Start scanner event processor
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.processScannerEvents(ctx)
		}()

		// Start error handler
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.handleErrors(ctx)
		}()
	}

	// Start event monitoring
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.monitorEvents(ctx)
	}()

	// Start update processor
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.processUpdates(ctx)
	}()

	// Start health checker
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.healthCheck(ctx)
	}()

	// Start metrics server
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.startMetricsServer(ctx)
	}()

	// Wait for all goroutines to finish
	wg.Wait()

	return nil
}

// Stop stops the bridge service
func (b *Bridge) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("bridge is not running")
	}

	logger.Info("Stopping bridge service")

	// Signal shutdown
	close(b.shutdownChan)

	// Stop block scanner if running
	if b.blockScanner != nil {
		if err := b.blockScanner.Stop(); err != nil {
			logger.Errorf("Failed to stop block scanner: %v", err)
		}
	}

	// Stop worker pool
	b.workerPool.Stop(ctx)

	// Close connections
	b.sourceClient.Close()
	for _, destClient := range b.destinationClients {
		destClient.client.Close()
	}

	b.running = false
	return nil
}

// monitorEvents monitors for new IntentRegistered events
func (b *Bridge) monitorEvents(ctx context.Context) {
	logger.Info("Starting event monitoring")

	// Use BlockScanner scan interval for polling
	ticker := time.NewTicker(b.config.BlockScanner.ScanInterval.Duration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case <-ticker.C:
			b.processNewEvents(ctx)
		}
	}
}

// processNewEvents processes new IntentRegistered events
func (b *Bridge) processNewEvents(ctx context.Context) {
	// Get current block
	currentBlock, err := b.sourceClient.BlockNumber(ctx)
	if err != nil {
		logger.Errorf("Failed to get current block: %v", err)
		return
	}

	// Process events from last processed block to current block
	if currentBlock > b.lastProcessedBlock {
		fromBlock := b.lastProcessedBlock + 1
		toBlock := currentBlock

		// Limit batch size using EventProcessor batch size
		if toBlock-fromBlock > uint64(b.config.EventProcessor.BatchSize) {
			toBlock = fromBlock + uint64(b.config.EventProcessor.BatchSize)
		}

		logger.Debugf("Processing events from block %d to %d", fromBlock, toBlock)

		events, err := b.registryClient.GetIntentRegisteredEvents(ctx, fromBlock, toBlock)
		if err != nil {
			logger.Errorf("Failed to get intent registered events: %v", err)
			return
		}

		for _, event := range events {
			b.processIntentEvent(ctx, event)
		}

		b.lastProcessedBlock = toBlock
		b.updateStats()
	}
}

// processIntentEvent processes a single IntentRegistered event
func (b *Bridge) processIntentEvent(ctx context.Context, event *bridgetypes.IntentRegisteredEvent) {
	logger.Debugf("Processing intent event: %s for symbol %s", event.IntentHash.Hex(), event.Symbol)

	// Get the full intent data
	intent, err := b.registryClient.GetIntent(ctx, event.IntentHash)
	if err != nil {
		logger.Errorf("Failed to get intent %s: %v", event.IntentHash.Hex(), err)
		return
	}

	// Check if intent has expired before routing
	// currentTime := time.Now().Unix()
	// if intent.Expiry.Int64() < currentTime {
	// 	expiryTime := time.Unix(intent.Expiry.Int64(), 0)
	// 	logger.Warnf("Skipping expired intent %s for %s: expired at %s (current: %s)",
	// 		event.IntentHash.Hex(),
	// 		intent.Symbol,
	// 		expiryTime.Format(time.RFC3339),
	// 		time.Unix(currentTime, 0).Format(time.RFC3339))

	// 	if b.metricsTracker != nil {
	// 		b.metricsTracker.RecordIntentFailed(intent, "routing", "intent_expired")
	// 	}
	// 	return // Skip expired intent
	// }

	// Track intent lifecycle start
	if b.metricsTracker != nil {
		b.metricsTracker.StartIntentLifecycle(intent, fmt.Sprintf("%d", b.config.Source.ChainID))
	}

	// Use routers to determine routing
	routers := b.routerRegistry.GetActiveRouters()
	if len(routers) == 0 {
		logger.Warn("No active routers configured")
		return
	}

	routedCount := 0
	for _, r := range routers {
		routerStart := time.Now()
		shouldRoute, reason := r.ShouldRoute(intent)

		// Track router decision
		if b.metricsTracker != nil {
			b.metricsTracker.RecordRouterDecision(r.ID(), intent, shouldRoute, reason, routerStart)
		}

		if !shouldRoute {
			logger.Debugf("Router %s skipped: %s", r.ID(), reason)
			continue
		}

		logger.Infof("Router %s approved for %s: %s", r.ID(), intent.Symbol, reason)

		// Track processing start
		if b.metricsTracker != nil {
			b.metricsTracker.RecordIntentProcessing(intent, r.ID())
		}

		// Create update requests for router's destinations
		for _, dest := range r.GetDestinations() {
			destClient := b.destinationClients[dest.ChainID]
			if destClient == nil {
				logger.Warnf("Destination client not found for chain %d", dest.ChainID)
				continue
			}

			// Create update request for each contract
			for _, contractAddr := range dest.Contracts {
				// Find contract config
				var contractConfig *config.ContractConfig
				for i := range destClient.config.Contracts {
					if destClient.config.Contracts[i].Address == contractAddr {
						contractConfig = &destClient.config.Contracts[i]
						break
					}
				}

				if contractConfig == nil {
					logger.Warnf("Contract config not found for %s", contractAddr)
					continue
				}

				updateReq := &bridgetypes.UpdateRequest{
					Intent:           intent,
					DestinationChain: destClient.config,
					Contract:         contractConfig,
					Priority:         1,
					Retries:          0,
					CreatedAt:        time.Now(),
				}

				select {
				case b.updateChan <- updateReq:
					routedCount++
					logger.Debugf("Router %s: Queued update for %s on chain %d contract %s",
						r.ID(), intent.Symbol, dest.ChainID, contractAddr)
				default:
					logger.Warnf("Update channel full, dropping request")
				}
			}
		}

		// Notify router that intent was processed
		r.OnRouted(intent)
	}

	if routedCount > 0 {
		logger.Infof("Routed intent %s to %d destinations", event.IntentHash.Hex(), routedCount)
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
			b.workerPool.Submit(&WorkerTask{
				ID:      fmt.Sprintf("%s-%d", updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID),
				Request: updateReq,
				Handler: b.handleUpdateRequest,
			})
		}
	}
}

// handleUpdateRequest handles a single update request
func (b *Bridge) handleUpdateRequest(ctx context.Context, task *WorkerTask) error {
	updateReq := task.Request
	destClient := b.destinationClients[updateReq.DestinationChain.ChainID]
	if destClient == nil {
		return fmt.Errorf("destination client not found for chain %d", updateReq.DestinationChain.ChainID)
	}

	logger.Infof("Processing update for %s on chain %d", updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)

	// Check if intent has expired before processing
	currentTime := time.Now().Unix()
	if updateReq.Intent.Expiry.Int64() < currentTime {
		expiryTime := time.Unix(updateReq.Intent.Expiry.Int64(), 0)
		logger.Warnf("Skipping expired intent for %s: expired at %s (current: %s)",
			updateReq.Intent.Symbol,
			expiryTime.Format(time.RFC3339),
			time.Unix(currentTime, 0).Format(time.RFC3339))

		if b.metricsTracker != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "validation", "intent_expired")
		}
		return nil // Skip expired intent
	}

	// Check if signer is authorized
	logger.Infof("Checking signer authorization for %s on chain %d, contract %s",
		updateReq.Intent.Signer.Hex(), updateReq.DestinationChain.ChainID,
		destClient.receiverClient.GetAddress().Hex())
	isAuthorized, err := destClient.receiverClient.IsAuthorizedSigner(ctx, updateReq.Intent.Signer)
	if err != nil {
		logger.Errorf("Failed to check signer authorization: %v", err)
		return fmt.Errorf("failed to check signer authorization: %w", err)
	}
	if !isAuthorized {
		logger.Errorf("Signer %s is not authorized on contract %s",
			updateReq.Intent.Signer.Hex(), destClient.receiverClient.GetAddress().Hex())
		return fmt.Errorf("signer %s is not authorized", updateReq.Intent.Signer.Hex())
	}
	logger.Infof("Signer %s is authorized", updateReq.Intent.Signer.Hex())

	// Get gas price
	gasPrice, err := b.getGasPrice(ctx, destClient)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	// Update auth
	logger.Infof("Updating auth for symbol %s on chain %d", updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)
	if err := destClient.receiverClient.UpdateAuth(ctx, gasPrice); err != nil {
		return fmt.Errorf("failed to update auth: %w", err)
	}

	// Send transaction
	logger.Infof("Sending transaction for symbol %s on chain %d with gas limit 300000",
		updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)
	tx, err := destClient.receiverClient.HandleIntentUpdate(
		ctx,
		updateReq.Intent,
		300000, // Default gas limit (would need to be per-contract in production)
		gasPrice,
	)
	if err != nil {
		// Log intent details for debugging
		if strings.Contains(err.Error(), "simulation failed") {
			logger.Errorf("Intent details that failed simulation: symbol=%s, price=%s, timestamp=%s, nonce=%s, expiry=%s, signer=%s",
				updateReq.Intent.Symbol,
				updateReq.Intent.Price.String(),
				updateReq.Intent.Timestamp.String(),
				updateReq.Intent.Nonce.String(),
				updateReq.Intent.Expiry.String(),
				updateReq.Intent.Signer.Hex())
		}
		if b.metricsTracker != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "submission", "transaction_failed")
		}
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	logger.Infof("Transaction sent: %s for %s on chain %d", tx.Hash().Hex(), updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)

	// Add a defer to catch any panics
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("PANIC in handleUpdateRequest after sending tx %s: %v", tx.Hash().Hex(), r)
		}
	}()

	// Track submission
	if b.metricsTracker != nil {
		logger.Debugf("Recording intent submission for tx %s", tx.Hash().Hex())
		b.metricsTracker.RecordIntentSubmitted(
			updateReq.Intent,
			fmt.Sprintf("%d", updateReq.DestinationChain.ChainID),
			tx.Hash().Hex(),
			gasPrice,
		)
		logger.Debugf("Intent submission recorded for tx %s", tx.Hash().Hex())
	}

	logger.Infof("About to wait for receipt for tx %s", tx.Hash().Hex())
	
	// Wait for transaction receipt
	receipt, err := b.waitForReceipt(ctx, destClient.client, tx.Hash())
	if err != nil {
		if b.metricsTracker != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "confirmation", "receipt_timeout")
		}
		return fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt.Status == 0 {
		if b.metricsTracker != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "confirmation", "transaction_reverted")
		}
		return fmt.Errorf("transaction failed: %s", tx.Hash().Hex())
	}

	// Track confirmation
	if b.metricsTracker != nil {
		b.metricsTracker.RecordIntentConfirmed(updateReq.Intent, tx.Hash().Hex(), receipt.GasUsed)
	}

	// Update last update time
	destClient.updateLastUpdate(updateReq.Intent.Symbol)

	logger.Infof("Successfully updated %s on chain %d, gas used: %d",
		updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID, receipt.GasUsed)

	return nil
}

// updateLastUpdate updates the last update time for a symbol
func (dc *DestinationClient) updateLastUpdate(symbol string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.lastUpdate[symbol] = time.Now()
}

// getGasPrice gets the current gas price for a destination chain
func (b *Bridge) getGasPrice(ctx context.Context, destClient *DestinationClient) (*big.Int, error) {
	gasPrice, err := destClient.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	// Increase suggested gas price by 20% to ensure timely inclusion
	// This helps avoid stuck transactions
	gasPrice.Mul(gasPrice, big.NewInt(120))
	gasPrice.Div(gasPrice, big.NewInt(100))

	// Apply gas price cap from contract config if available
	// Note: This would need to be per-contract in production
	for _, contract := range destClient.config.Contracts {
		if contract.MaxGasPrice != "" {
			maxGasPrice := new(big.Int)
			maxGasPrice, ok := maxGasPrice.SetString(contract.MaxGasPrice, 10)
			if ok && gasPrice.Cmp(maxGasPrice) > 0 {
				logger.Warnf("Gas price %s exceeds max %s, using max", gasPrice.String(), maxGasPrice.String())
				gasPrice = maxGasPrice
			}
			break
		}
	}

	logger.Infof("Using gas price: %s wei (%s gwei)", gasPrice.String(), 
		new(big.Int).Div(gasPrice, big.NewInt(1e9)).String())

	return gasPrice, nil
}

// waitForReceipt waits for a transaction receipt
func (b *Bridge) waitForReceipt(ctx context.Context, client rpc.EthClient, txHash common.Hash) (*types.Receipt, error) {
	logger.Infof("Waiting for transaction receipt: %s", txHash.Hex())
	
	// Maximum wait time: 5 minutes
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

// initializeChainStats initializes chain statistics
func (b *Bridge) initializeChainStats() {
	// Source chain stats
	b.stats.ChainStats[b.config.Source.ChainID] = &bridgetypes.ChainStatus{
		ChainID:   b.config.Source.ChainID,
		Name:      b.config.Source.Name,
		Connected: true,
	}

	// Destination chain stats
	for _, destClient := range b.destinationClients {
		b.stats.ChainStats[destClient.config.ChainID] = &bridgetypes.ChainStatus{
			ChainID:   destClient.config.ChainID,
			Name:      destClient.config.Name,
			Connected: true,
		}
	}
}

// updateStats updates bridge statistics
func (b *Bridge) updateStats() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stats.LastProcessedBlock = b.lastProcessedBlock
	b.stats.Uptime = time.Since(b.stats.StartTime)
	b.stats.UptimeFormatted = utils.FormatDuration(b.stats.Uptime)
}

// healthCheck performs periodic health checks
func (b *Bridge) healthCheck(ctx context.Context) {
	// Use HealthCheck interval from config
	ticker := time.NewTicker(b.config.HealthCheck.CheckInterval.Duration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case <-ticker.C:
			b.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck performs health checks on all chains
func (b *Bridge) performHealthCheck(ctx context.Context) {
	// Check source chain
	if err := b.checkChainHealth(ctx, b.sourceClient, b.config.Source.ChainID); err != nil {
		logger.Errorf("Source chain health check failed: %v", err)
	}

	// Check destination chains
	for _, destClient := range b.destinationClients {
		if err := b.checkChainHealth(ctx, destClient.client, destClient.config.ChainID); err != nil {
			logger.Errorf("Destination chain %d health check failed: %v", destClient.config.ChainID, err)
		}
	}
}

// checkChainHealth checks the health of a single chain
func (b *Bridge) checkChainHealth(ctx context.Context, client rpc.EthClient, chainID int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	chainStats := b.stats.ChainStats[chainID]
	if chainStats == nil {
		return fmt.Errorf("chain stats not found for chain %d", chainID)
	}

	// Get latest block
	latestBlock, err := client.BlockNumber(ctx)
	if err != nil {
		chainStats.Connected = false
		chainStats.LastError = err.Error()
		return err
	}

	chainStats.Connected = true
	chainStats.LatestBlock = latestBlock
	chainStats.LastHealthCheck = time.Now()
	chainStats.LastError = ""

	return nil
}

// startMetricsServer starts the metrics server
func (b *Bridge) startMetricsServer(ctx context.Context) {
	// Import the api package when we implement this
	// For now, just log that it would start
	// Log that metrics would be available if enabled
	if b.config.Metrics.Enabled {
		logger.Info("Metrics collection is enabled")
	}

	// TODO: Implement HTTP server with health, stats, and metrics endpoints
	// Example implementation:
	// apiServer := api.NewAPIServer(b, b.config.Bridge.MetricsPort)
	// go apiServer.Start()
}

// processScannerEvents processes events from the block scanner
func (b *Bridge) processScannerEvents(ctx context.Context) {
	logger.Info("Starting scanner event processor")

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case event := <-b.eventChan:
			// Determine discovery method based on priority and flags
			discoveryMethod := "FORWARD"
			if event.Priority == 3 {
				discoveryMethod = "WEBSOCKET"
			} else if event.IsBackwardScan {
				discoveryMethod = "BACKFILL"
			}
			
			logger.Infof("[%s] Received event: %s at block %d, intent: %s", 
				discoveryMethod, event.EventName, event.BlockNumber, 
				common.BytesToHash(event.IntentHash[:]).Hex())
			
			// Track scanner detection
			scannerType := "forward"
			if event.IsBackwardScan {
				scannerType = "backward"
			}
			if b.metricsTracker != nil {
				b.metricsTracker.RecordIntentScanned(event, scannerType)
			}

			// Convert scanner event to intent event
			intentEvent := &bridgetypes.IntentRegisteredEvent{
				IntentHash:  common.BytesToHash(event.IntentHash[:]),
				Symbol:      event.Symbol,
				Price:       event.Price,
				Timestamp:   event.Timestamp,
				Signer:      event.Signer,
				BlockNumber: event.BlockNumber,
				TxHash:      event.TxHash,
			}

			// Track registration
			if b.metricsTracker != nil {
				b.metricsTracker.RecordIntentRegistered(intentEvent, fmt.Sprintf("%d", b.config.Source.ChainID))
			}

			// Get the full intent data to enrich the event
			intent, err := b.registryClient.GetIntent(ctx, intentEvent.IntentHash)
			if err != nil {
				logger.Errorf("Failed to get intent %s: %v", intentEvent.IntentHash.Hex(), err)
				// Still save the event with empty symbol to avoid reprocessing
			} else {
				// Use enriched data from intent
				intentEvent.Symbol = intent.Symbol
				intentEvent.Price = intent.Price
				intentEvent.Timestamp = intent.Timestamp
				intentEvent.Signer = intent.Signer
			}

			// Process the event (this will fetch intent again, but it's ok for now)
			b.processIntentEvent(ctx, intentEvent)

			// Mark event as processed in database with enriched data
			processedEvent := &database.ProcessedEvent{
				IntentHash:      intentEvent.IntentHash.Hex(),
				BlockNumber:     intentEvent.BlockNumber,
				TransactionHash: intentEvent.TxHash.Hex(),
				LogIndex:        0, // TODO: Get from event
				Symbol:          intentEvent.Symbol,
				Price:           intentEvent.Price.String(),
				Timestamp:       uint64(intentEvent.Timestamp.Int64()),
				Signer:          intentEvent.Signer,
				ProcessedAt:     time.Now(),
			}
			if err := b.db.SaveProcessedEvent(processedEvent); err != nil {
				logger.Errorf("Failed to save processed event: %v", err)
			}
		}
	}
}

// handleErrors handles errors from various components
func (b *Bridge) handleErrors(ctx context.Context) {
	logger.Info("Starting error handler")

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case err := <-b.errorChan:
			logger.Errorf("Bridge error: %v", err)
			// TODO: Implement error handling logic (alerts, retries, etc.)
		}
	}
}

// GetStats returns current bridge statistics
func (b *Bridge) GetStats() *bridgetypes.BridgeStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Create a copy of stats and update uptime
	stats := *b.stats
	stats.Uptime = time.Since(b.stats.StartTime)
	stats.UptimeFormatted = utils.FormatDuration(stats.Uptime)

	// Add scanner stats if available
	if b.blockScanner != nil {
		stats.ScannerStats = b.blockScanner.GetStats()
	}

	return &stats
}
