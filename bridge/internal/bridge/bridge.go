package bridge

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/grpc"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/processor"
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
	routerRegistry *router.GenericRegistry

	// Block scanner
	blockScanner BlockScanner
	
	// Event processor
	eventProcessor *processor.GenericEventProcessor

	// Metrics tracking
	metricsTracker *MetricsTracker
	// API components
	apiServer       *api.Server
	metrics         *metrics.Metrics
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
	// For new config, registry address would come from event definitions
	// Extract registry address from event definitions
	registryAddress := ""
	for _, eventDef := range cfg.EventDefinitions {
		if eventDef.Contract != "" {
			// Use the contract address from the IntentRegistered event
			registryAddress = eventDef.Contract
			break
		}
	}
	
	if registryAddress == "" {
		return nil, fmt.Errorf("no registry contract address found in event definitions")
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
	routerRegistry := router.NewGenericRegistry()
	if err := routerRegistry.LoadRouters(cfg.Routers); err != nil {
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
	
	// Get destination eth clients for event processor
	destEthClients := make(map[int64]*ethclient.Client)
	for chainID, destClient := range destClients {
		ethClient, err := destClient.client.GetClient()
		if err != nil {
			return nil, fmt.Errorf("failed to get eth client for chain %d: %w", chainID, err)
		}
		destEthClients[chainID] = ethClient
	}
	
	// Create generic event processor
	eventProcessor, err := processor.NewGenericEventProcessor(
		&cfg.EventProcessor,
		cfg.EventDefinitions,
		cfg.Destinations,
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

		// Start generic event processor
		if b.eventProcessor != nil {
			if err := b.eventProcessor.Start(ctx); err != nil {
				return fmt.Errorf("failed to start event processor: %w", err)
			}
			logger.Info("Generic event processor started")
		}

		// Start error handler
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.handleErrors(ctx)
		}()
	}


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



// processIntentEvent processes a single IntentRegistered event
func (b *Bridge) processIntentEvent(ctx context.Context, event *bridgetypes.IntentRegisteredEvent) {
	logger.Debugf("Legacy processIntentEvent called for intent: %s - routing now handled by GenericEventProcessor", event.IntentHash.Hex())
	// This function is now deprecated as routing is handled by the GenericEventProcessor
	// using the new router system directly from event processing
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
				taskID = fmt.Sprintf("%s-%d", updateReq.Intent.Symbol, updateReq.DestinationChain.ChainID)
			} else if updateReq.Event != nil {
				// For events like IntArraySet that don't have Intent
				taskID = fmt.Sprintf("%s-%d-%d", updateReq.Event.EventName, updateReq.DestinationChain.ChainID, time.Now().Unix())
			} else {
				taskID = fmt.Sprintf("unknown-%d-%d", updateReq.DestinationChain.ChainID, time.Now().Unix())
			}
			
			b.workerPool.Submit(&WorkerTask{
				ID:      taskID,
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

	// Get identifier for logging based on request type
	var identifier string
	if updateReq.Intent != nil {
		identifier = updateReq.Intent.Symbol
	} else if updateReq.Event != nil {
		identifier = fmt.Sprintf("%s(requestId:%s)", updateReq.Event.EventName, updateReq.Event.RequestId.String())
	} else {
		identifier = "unknown"
	}
	
	logger.Infof("Processing update for %s on chain %d", identifier, updateReq.DestinationChain.ChainID)

	// Check if intent has expired before processing (only for Intent-based requests)
	if updateReq.Intent != nil {
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
	}

	// Check if signer is authorized (only for Intent-based requests with actual signers)
	if updateReq.Intent != nil && updateReq.Intent.Signer != (common.Address{}) {
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
	} else {
		logger.Infof("Skipping authorization check for non-signer request: %s", identifier)
	}

	// Get gas price for transaction
	gasPrice, err := b.getGasPrice(ctx, destClient)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	// Send transaction using router method configuration or fallback to legacy
	var tx *types.Transaction
	if updateReq.DestinationMethodConfig != nil {
		// Use router-specified method (e.g., fulfillRandomInt for randomness)
		gasLimit := uint64(300000) // Default
		if updateReq.DestinationMethodConfig.GasLimit > 0 {
			gasLimit = uint64(updateReq.DestinationMethodConfig.GasLimit)
		}
		
		logger.Infof("Sending transaction for %s on chain %d using method %s with gas limit %d", 
			identifier, updateReq.DestinationChain.ChainID, updateReq.DestinationMethodConfig.Name, gasLimit)
		
		tx, err = b.callRouterMethod(ctx, destClient, updateReq, gasPrice, gasLimit)
	} else {
		// Fallback to legacy HandleIntentUpdate for oracle intents
		logger.Infof("Sending transaction for %s on chain %d with gas limit 300000 (legacy)", identifier, updateReq.DestinationChain.ChainID)
		tx, err = destClient.receiverClient.HandleIntentUpdate(
			ctx,
			updateReq.Intent,
			300000, // Default gas limit (would need to be per-contract in production)
			gasPrice,
		)
	}
	if err != nil {
		// Log intent details for debugging
		if strings.Contains(err.Error(), "simulation failed") && updateReq.Intent != nil {
			logger.Errorf("Intent details that failed simulation: symbol=%s, price=%s, timestamp=%s, nonce=%s, expiry=%s, signer=%s",
				updateReq.Intent.Symbol,
				updateReq.Intent.Price.String(),
				updateReq.Intent.Timestamp.String(),
				updateReq.Intent.Nonce.String(),
				updateReq.Intent.Expiry.String(),
				updateReq.Intent.Signer.Hex())
		}
		if b.metricsTracker != nil && updateReq.Intent != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "submission", "transaction_failed")
		}
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	logger.Infof("Transaction sent: %s for %s on chain %d", tx.Hash().Hex(), identifier, updateReq.DestinationChain.ChainID)

	// Add a defer to catch any panics
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("PANIC in handleUpdateRequest after sending tx %s: %v", tx.Hash().Hex(), r)
		}
	}()

	// Track submission
	if b.metricsTracker != nil && updateReq.Intent != nil {
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
		if b.metricsTracker != nil && updateReq.Intent != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "confirmation", "receipt_timeout")
		}
		return fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt.Status == 0 {
		if b.metricsTracker != nil && updateReq.Intent != nil {
			b.metricsTracker.RecordIntentFailed(updateReq.Intent, "confirmation", "transaction_reverted")
		}
		return fmt.Errorf("transaction failed: %s", tx.Hash().Hex())
	}

	// Track confirmation
	if b.metricsTracker != nil && updateReq.Intent != nil {
		b.metricsTracker.RecordIntentConfirmed(updateReq.Intent, tx.Hash().Hex(), receipt.GasUsed)
	}

	// Update last update time
	if updateReq.Intent != nil {
		destClient.updateLastUpdate(updateReq.Intent.Symbol)
	}

	logger.Infof("Successfully updated %s on chain %d, gas used: %d",
		identifier, updateReq.DestinationChain.ChainID, receipt.GasUsed)

	return nil
}

// callRouterMethod calls a contract method using router configuration
func (b *Bridge) callRouterMethod(ctx context.Context, destClient *DestinationClient, updateReq *bridgetypes.UpdateRequest, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, error) {
	methodConfig := updateReq.DestinationMethodConfig
	
	// Build method parameters from router configuration
	params, err := b.buildMethodParams(methodConfig, updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to build method params: %w", err)
	}
	
	// Call the contract method
	return b.callContractMethod(ctx, destClient, methodConfig.Name, methodConfig.ABI, params, gasPrice, gasLimit)
}

// buildMethodParams builds method parameters from router configuration
func (b *Bridge) buildMethodParams(methodConfig *config.DestinationMethodConfig, updateReq *bridgetypes.UpdateRequest) ([]interface{}, error) {
	var params []interface{}
	
	// For fulfillRandomInt method, we need requestId and randomInts
	if methodConfig.Name == "fulfillRandomInt" {
		// Get requestId from event
		if updateReq.Event != nil && updateReq.Event.RequestId != nil {
			params = append(params, updateReq.Event.RequestId)
		} else {
			return nil, fmt.Errorf("requestId not found in event data")
		}
		
		// Get randomInts from enrichment data
		if updateReq.ExtractedData != nil && updateReq.ExtractedData.Enrichment != nil {
			if randomInts, exists := updateReq.ExtractedData.Enrichment["randomInts"]; exists {
				params = append(params, randomInts)
			} else {
				return nil, fmt.Errorf("randomInts not found in enrichment data")
			}
		} else {
			return nil, fmt.Errorf("enrichment data not available")
		}
	} else {
		return nil, fmt.Errorf("unsupported method: %s", methodConfig.Name)
	}
	
	return params, nil
}

// callContractMethod calls a generic contract method
func (b *Bridge) callContractMethod(ctx context.Context, destClient *DestinationClient, methodName, abiJSON string, params []interface{}, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, error) {
	// Parse the method ABI
	parsedABI, err := abi.JSON(strings.NewReader(fmt.Sprintf(`[%s]`, abiJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse method ABI: %w", err)
	}
	
	// Verify the method exists
	if _, exists := parsedABI.Methods[methodName]; !exists {
		return nil, fmt.Errorf("method %s not found in ABI", methodName)
	}
	
	// Get auth transactor
	auth := destClient.receiverClient.GetAuth()
	auth.GasLimit = gasLimit
	auth.GasPrice = gasPrice
	auth.Context = ctx
	
	// Create transaction
	contractAddress := destClient.receiverClient.GetAddress()
	tx, err := bind.NewBoundContract(contractAddress, parsedABI, destClient.client, destClient.client, destClient.client).Transact(auth, methodName, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}
	
	return tx, nil
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

// GetRouterRegistry returns the router registry
func (b *Bridge) GetRouterRegistry() *router.GenericRegistry {
	return b.routerRegistry
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

	// Start API server if configured
	if b.config.API.ListenAddr != "" {
		// Create metrics collector for API
		var metricsCollector *metrics.Collector
		if b.metrics != nil {
			// Use the singleton metrics collector which includes IntentMetrics
			metricsCollector = metrics.NewCollector()
			// Override the FailoverMetrics with the bridge's instance
			metricsCollector.FailoverMetrics = b.metrics
		}
		
		// API server needs nil health monitor and router registry for now
		apiServer := api.NewServer(b.config, b.db, nil, metricsCollector, b.routerRegistry)
		
		go func() {
			if err := apiServer.Start(ctx); err != nil {
				logger.Errorf("API server error: %v", err)
			}
		}()
		
		b.apiServer = apiServer
		
		// Start gRPC server if failover handler is available
		if apiServer.GetFailoverHandler() != nil {
			grpcServer := grpc.NewServer(apiServer.GetFailoverHandler())
			go func() {
				grpcPort := 8082 // Use port 8082 for gRPC
				logger.Infof("Starting gRPC server on port %d", grpcPort)
				if err := grpcServer.Start(grpcPort); err != nil {
					logger.Errorf("Failed to start gRPC server: %v", err)
				}
			}()
		}
	}
}

// processScannerEvents processes events from the block scanner
// DEPRECATED: This is replaced by GenericEventProcessor
/*
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
*/

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
