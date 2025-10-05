package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/grpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/utils"
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
	apiServer *api.Server
	metrics   *metrics.Metrics
}

// WriteClient represents a client for write operations to a destination chain
type WriteClient struct {
	chainConfig    *config.ChainConfig
	contracts      []*config.ContractConfig
	client         rpc.EthClient
	receiverClient *contracts.ReceiverClient
	lastUpdate     map[string]time.Time // symbol -> last update time
	mu             sync.RWMutex
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

	// Create destination clients
	destClients := make(map[int64]*WriteClient)
	for _, chainConfig := range cfgService.GetEnabledChains() {
		contracts := cfgService.GetContractsForChain(chainConfig.ChainID)
		if len(contracts) == 0 {
			continue // Skip chains with no contracts
		}

		destClient, err := NewWriteClient(chainConfig, contracts, cfgService.GetInfrastructure().PrivateKey)
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
	workerPool := NewWorkerPool(cfgService.GetInfrastructure().WorkerPool.MaxWorkers)

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

	// Create legacy destinations for processor (temporary)
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

// NewWriteClient creates a new write client for destination operations
func NewWriteClient(chainConfig *config.ChainConfig, contractConfigs []*config.ContractConfig, privateKey string) (*WriteClient, error) {
	// Connect to destination chain with multiple RPC support
	client, err := rpc.NewMultiClient(chainConfig.RPCURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to destination chain: %w", err)
	}
	logger.Infof("Connected to destination chain %s via %s", chainConfig.Name, client.GetCurrentRPCURL())

	// Create receiver client
	// Note: ReceiverAddress should be extracted from destination contracts
	var receiverAddress string
	for _, contract := range contractConfigs {
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

	return &WriteClient{
		chainConfig:    chainConfig,
		contracts:      contractConfigs,
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
	destClient := b.writeClients[updateReq.DestinationChain.ChainID]
	if destClient == nil {
		return fmt.Errorf("destination client not found for chain %d", updateReq.DestinationChain.ChainID)
	}

	// Get identifier for logging based on request type
	var identifier string
	if updateReq.Intent != nil {
		identifier = updateReq.Intent.Symbol
		// Debug log the received intent
		logger.Debugf("Received intent: symbol=%s price=%s timestamp=%s nonce=%s expiry=%s signer=%s source=%s",
			updateReq.Intent.Symbol,
			updateReq.Intent.Price.String(),
			updateReq.Intent.Timestamp.String(),
			updateReq.Intent.Nonce.String(),
			updateReq.Intent.Expiry.String(),
			updateReq.Intent.Signer.Hex(),
			updateReq.Intent.Source)
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
	// Extract symbol once for use in all logs - try Intent first, then enrichment data
	symbol := "unknown"
	if updateReq.Intent != nil && updateReq.Intent.Symbol != "" {
		symbol = updateReq.Intent.Symbol
	} else if updateReq.ExtractedData != nil && updateReq.RouterID != "" {
		// Try to extract from enrichment data via router
		if router := b.routerRegistry.GetRouterByID(updateReq.RouterID); router != nil {
			extracted := router.GetSymbolFromData(updateReq.ExtractedData)
			if extracted != "" && extracted != "unknown" {
				symbol = extracted
			}
		}
	}

	var tx *types.Transaction
	if updateReq.DestinationMethodConfig != nil {
		// Use router-specified method (e.g., fulfillRandomInt for randomness)
		gasLimit := uint64(300000) // Default
		if updateReq.DestinationMethodConfig.GasLimit > 0 {
			gasLimit = uint64(updateReq.DestinationMethodConfig.GasLimit)
		}

		logger.Infof("Sending transaction for %s on chain %d using method %s with gas limit %d, router=%s, symbol=%s",
			identifier, updateReq.DestinationChain.ChainID, updateReq.DestinationMethodConfig.Name, gasLimit, updateReq.RouterID, symbol)

		tx, err = b.callRouterMethod(ctx, destClient, updateReq, gasPrice, gasLimit)
	} else {
		// Fallback to legacy HandleIntentUpdate for oracle intents
		logger.Infof("Sending transaction for %s on chain %d with gas limit 300000 (legacy), router=%s, symbol=%s",
			identifier, updateReq.DestinationChain.ChainID, updateReq.RouterID, symbol)
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

	logger.Infof("Transaction sent: %s for %s on chain %d, router=%s, symbol=%s",
		tx.Hash().Hex(), identifier, updateReq.DestinationChain.ChainID, updateReq.RouterID, symbol)

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

	logger.Infof("About to wait for receipt for tx %s, router=%s, symbol=%s", tx.Hash().Hex(), updateReq.RouterID, symbol)

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

		// Enhanced error logging for failed transactions
		symbol := "unknown"
		if updateReq.Intent != nil {
			symbol = updateReq.Intent.Symbol
		}
		logger.Errorf("Transaction REVERTED: hash=%s, symbol=%s, gas_used=%d, chain=%d - Likely causes: InvalidSignature, insufficient gas, or contract revert",
			tx.Hash().Hex(), symbol, receipt.GasUsed, updateReq.DestinationChain.ChainID)
		logger.Debugf("Failed transaction details: router=%s, method=%s, contract=%s",
			updateReq.RouterID, updateReq.Contract.Address, updateReq.Contract.Address)

		return fmt.Errorf("transaction reverted (status: 0): hash=%s, symbol=%s, gas=%d - check contract logs or simulate with cast",
			tx.Hash().Hex(), symbol, receipt.GasUsed)
	}

	// Track confirmation
	if b.metricsTracker != nil && updateReq.Intent != nil {
		b.metricsTracker.RecordIntentConfirmed(updateReq.Intent, tx.Hash().Hex(), receipt.GasUsed)
	}

	// Update last update time
	if updateReq.Intent != nil {
		destClient.updateLastUpdate(updateReq.Intent.Symbol)
	}

	logger.Infof("Transaction receipt received: %s, status: %d, gas used: %d, router=%s, symbol=%s",
		tx.Hash().Hex(), receipt.Status, receipt.GasUsed, updateReq.RouterID, symbol)

	// Call router's OnRouted callback to update time tracking
	if updateReq.RouterID != "" {
		if router := b.routerRegistry.GetRouterByID(updateReq.RouterID); router != nil {
			eventName := ""

			// Use the original extracted data that was used for routing decisions
			extractedData := updateReq.ExtractedData

			if updateReq.Event != nil {
				eventName = updateReq.Event.EventName
			}

			// Call OnRouted with original data to ensure proper time tracking
			router.OnRouted(eventName, extractedData)
		}
	}

	return nil
}

// callRouterMethod calls a contract method using router configuration
func (b *Bridge) callRouterMethod(ctx context.Context, destClient *WriteClient, updateReq *bridgetypes.UpdateRequest, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, error) {
	methodConfig := updateReq.DestinationMethodConfig

	// Build method parameters from router configuration
	params, err := b.buildMethodParams(methodConfig, updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to build method params: %w", err)
	}

	contractAddress := common.HexToAddress(updateReq.Contract.Address)
	return b.callContractMethod(ctx, destClient, contractAddress, methodConfig.Name, methodConfig.ABI, params, gasPrice, gasLimit)
}

// buildMethodParams builds method parameters from router configuration using generic param mapping
func (b *Bridge) buildMethodParams(methodConfig *config.DestinationMethodConfig, updateReq *bridgetypes.UpdateRequest) ([]interface{}, error) {
	var params []interface{}

	// Build parameters based on config mapping
	for paramName, paramSource := range methodConfig.Params {
		value, err := b.resolveParameterValue(paramSource, updateReq)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %s: %w", paramName, err)
		}

		// Convert OracleIntent into tuple struct for ABI packing
		if paramName == "intent" && paramSource == "${enrichment.fullIntent}" {
			if intent, ok := value.(*bridgetypes.OracleIntent); ok {
				tuple := struct {
					IntentType string         `abi:"intentType"`
					Version    string         `abi:"version"`
					ChainId    *big.Int       `abi:"chainId"`
					Nonce      *big.Int       `abi:"nonce"`
					Expiry     *big.Int       `abi:"expiry"`
					Symbol     string         `abi:"symbol"`
					Price      *big.Int       `abi:"price"`
					Timestamp  *big.Int       `abi:"timestamp"`
					Source     string         `abi:"source"`
					Signature  []byte         `abi:"signature"`
					Signer     common.Address `abi:"signer"`
				}{
					IntentType: intent.IntentType,
					Version:    intent.Version,
					ChainId:    intent.ChainID,
					Nonce:      intent.Nonce,
					Expiry:     intent.Expiry,
					Symbol:     intent.Symbol,
					Price:      intent.Price,
					Timestamp:  intent.Timestamp,
					Source:     intent.Source,
					Signature:  []byte(intent.Signature),
					Signer:     intent.Signer,
				}
				params = append(params, tuple)
				continue
			}
		}
		// Default append
		params = append(params, value)
	}

	return params, nil
}

// resolveParameterValue resolves a parameter value from the configuration source
func (b *Bridge) resolveParameterValue(source string, updateReq *bridgetypes.UpdateRequest) (interface{}, error) {
	// Handle template variables like ${enrichment.fullIntent}
	if strings.HasPrefix(source, "${") && strings.HasSuffix(source, "}") {
		templateVar := strings.TrimSuffix(strings.TrimPrefix(source, "${"), "}")

		switch {
		case strings.HasPrefix(templateVar, "enrichment."):
			enrichmentKey := strings.TrimPrefix(templateVar, "enrichment.")
			if updateReq.ExtractedData != nil && updateReq.ExtractedData.Enrichment != nil {
				if value, exists := updateReq.ExtractedData.Enrichment[enrichmentKey]; exists {
					// Debug log for fullIntent specifically
					if enrichmentKey == "fullIntent" {
						intent, err := convertToOracleIntent(value)
						if err != nil {
							return nil, fmt.Errorf("failed to convert fullIntent enrichment: %w", err)
						}
						logger.Debugf("Retrieved fullIntent from enrichment: symbol=%s price=%s timestamp=%s nonce=%s expiry=%s signer=%s source=%s",
							intent.Symbol,
							intent.Price.String(),
							intent.Timestamp.String(),
							intent.Nonce.String(),
							intent.Expiry.String(),
							intent.Signer.Hex(),
							intent.Source)
						return intent, nil
					}
					return value, nil
				}
				return nil, fmt.Errorf("enrichment key %s not found", enrichmentKey)
			}
			return nil, fmt.Errorf("enrichment data not available")

		case strings.HasPrefix(templateVar, "event."):
			eventField := strings.TrimPrefix(templateVar, "event.")
			if updateReq.Event == nil {
				return nil, fmt.Errorf("event data not available")
			}

			// Handle common event fields
			switch eventField {
			case "requestId":
				if updateReq.Event.RequestId != nil {
					return updateReq.Event.RequestId, nil
				}
				return nil, fmt.Errorf("event requestId not found")
			default:
				return nil, fmt.Errorf("unsupported event field: %s", eventField)
			}

		case strings.HasPrefix(templateVar, "intent."):
			if updateReq.Intent == nil {
				return nil, fmt.Errorf("intent data not available")
			}
			// Return the entire intent for handleIntentUpdate
			return updateReq.Intent, nil

		default:
			return nil, fmt.Errorf("unsupported template variable: %s", templateVar)
		}
	}

	// Handle literal values
	return source, nil
}

func convertToOracleIntent(value interface{}) (*bridgetypes.OracleIntent, error) {
	if value == nil {
		return nil, fmt.Errorf("fullIntent enrichment is nil")
	}

	switch v := value.(type) {
	case *bridgetypes.OracleIntent:
		return v, nil
	case bridgetypes.OracleIntent:
		intent := v
		return &intent, nil
	case map[string]interface{}:
		return mapToOracleIntent(v)
	default:
		val := reflect.ValueOf(value)
		if !val.IsValid() {
			return nil, fmt.Errorf("invalid fullIntent value")
		}

		if val.Kind() == reflect.Pointer {
			if val.IsNil() {
				return nil, fmt.Errorf("fullIntent pointer is nil")
			}
			val = val.Elem()
		}

		if val.Kind() == reflect.Struct {
			return structToOracleIntent(val)
		}
	}

	return nil, fmt.Errorf("unsupported fullIntent type %T", value)
}

func mapToOracleIntent(data map[string]interface{}) (*bridgetypes.OracleIntent, error) {
	if data == nil {
		return nil, fmt.Errorf("fullIntent map is nil")
	}

	intent := &bridgetypes.OracleIntent{}

	intent.IntentType = getStringCaseInsensitive(data, "intentType")
	intent.Version = getStringCaseInsensitive(data, "version")
	intent.Symbol = getStringCaseInsensitive(data, "symbol")
	intent.Source = getStringCaseInsensitive(data, "source")

	if v := getValueCaseInsensitive(data, "chainId"); v != nil {
		if bi, err := toBigIntValue(v); err == nil {
			intent.ChainID = bi
		} else {
			return nil, fmt.Errorf("invalid chainId in fullIntent: %w", err)
		}
	}

	if v := getValueCaseInsensitive(data, "nonce"); v != nil {
		if bi, err := toBigIntValue(v); err == nil {
			intent.Nonce = bi
		} else {
			return nil, fmt.Errorf("invalid nonce in fullIntent: %w", err)
		}
	}

	if v := getValueCaseInsensitive(data, "expiry"); v != nil {
		if bi, err := toBigIntValue(v); err == nil {
			intent.Expiry = bi
		} else {
			return nil, fmt.Errorf("invalid expiry in fullIntent: %w", err)
		}
	}

	if v := getValueCaseInsensitive(data, "price"); v != nil {
		if bi, err := toBigIntValue(v); err == nil {
			intent.Price = bi
		} else {
			return nil, fmt.Errorf("invalid price in fullIntent: %w", err)
		}
	}

	if v := getValueCaseInsensitive(data, "timestamp"); v != nil {
		if bi, err := toBigIntValue(v); err == nil {
			intent.Timestamp = bi
		} else {
			return nil, fmt.Errorf("invalid timestamp in fullIntent: %w", err)
		}
	}

	if sigVal := getValueCaseInsensitive(data, "signature"); sigVal != nil {
		bytes, err := toByteSlice(sigVal)
		if err != nil {
			return nil, fmt.Errorf("invalid signature in fullIntent: %w", err)
		}
		intent.Signature = bytes
	}

	if signerVal := getValueCaseInsensitive(data, "signer"); signerVal != nil {
		addr, err := toAddressValue(signerVal)
		if err != nil {
			return nil, fmt.Errorf("invalid signer in fullIntent: %w", err)
		}
		intent.Signer = addr
	}

	return intent, nil
}

func structToOracleIntent(val reflect.Value) (*bridgetypes.OracleIntent, error) {
	intent := &bridgetypes.OracleIntent{}

	getField := func(name string) reflect.Value {
		if field := val.FieldByName(name); field.IsValid() {
			return field
		}
		lowerName := strings.ToLower(name)
		typeOfVal := val.Type()
		for i := 0; i < typeOfVal.NumField(); i++ {
			if strings.ToLower(typeOfVal.Field(i).Name) == lowerName {
				return val.Field(i)
			}
		}
		return reflect.Value{}
	}

	if field := getField("IntentType"); field.IsValid() {
		intent.IntentType = fmt.Sprintf("%v", field.Interface())
	}
	if field := getField("Version"); field.IsValid() {
		intent.Version = fmt.Sprintf("%v", field.Interface())
	}
	if field := getField("Symbol"); field.IsValid() {
		intent.Symbol = fmt.Sprintf("%v", field.Interface())
	}
	if field := getField("Source"); field.IsValid() {
		intent.Source = fmt.Sprintf("%v", field.Interface())
	}

	if field := getField("ChainId"); field.IsValid() {
		if bi, err := toBigIntValue(field.Interface()); err == nil {
			intent.ChainID = bi
		} else {
			return nil, fmt.Errorf("invalid struct ChainId: %w", err)
		}
	}

	if field := getField("Nonce"); field.IsValid() {
		if bi, err := toBigIntValue(field.Interface()); err == nil {
			intent.Nonce = bi
		} else {
			return nil, fmt.Errorf("invalid struct Nonce: %w", err)
		}
	}

	if field := getField("Expiry"); field.IsValid() {
		if bi, err := toBigIntValue(field.Interface()); err == nil {
			intent.Expiry = bi
		} else {
			return nil, fmt.Errorf("invalid struct Expiry: %w", err)
		}
	}

	if field := getField("Price"); field.IsValid() {
		if bi, err := toBigIntValue(field.Interface()); err == nil {
			intent.Price = bi
		} else {
			return nil, fmt.Errorf("invalid struct Price: %w", err)
		}
	}

	if field := getField("Timestamp"); field.IsValid() {
		if bi, err := toBigIntValue(field.Interface()); err == nil {
			intent.Timestamp = bi
		} else {
			return nil, fmt.Errorf("invalid struct Timestamp: %w", err)
		}
	}

	if field := getField("Signature"); field.IsValid() {
		bytes, err := toByteSlice(field.Interface())
		if err != nil {
			return nil, fmt.Errorf("invalid struct Signature: %w", err)
		}
		intent.Signature = bytes
	}

	if field := getField("Signer"); field.IsValid() {
		addr, err := toAddressValue(field.Interface())
		if err != nil {
			return nil, fmt.Errorf("invalid struct Signer: %w", err)
		}
		intent.Signer = addr
	}

	return intent, nil
}

func getValueCaseInsensitive(m map[string]interface{}, key string) interface{} {
	if v, ok := m[key]; ok {
		return v
	}
	lowerKey := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == lowerKey {
			return v
		}
	}
	return nil
}

func getStringCaseInsensitive(m map[string]interface{}, key string) string {
	if v := getValueCaseInsensitive(m, key); v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func toBigIntValue(value interface{}) (*big.Int, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case *big.Int:
		return v, nil
	case big.Int:
		copy := new(big.Int).Set(&v)
		return copy, nil
	case string:
		if v == "" {
			return nil, nil
		}
		if strings.HasPrefix(v, "0x") {
			bi, ok := new(big.Int).SetString(v[2:], 16)
			if !ok {
				return nil, fmt.Errorf("invalid hex integer %s", v)
			}
			return bi, nil
		}
		bi, ok := new(big.Int).SetString(v, 10)
		if !ok {
			return nil, fmt.Errorf("invalid integer %s", v)
		}
		return bi, nil
	case json.Number:
		return toBigIntValue(string(v))
	case int, int8, int16, int32, int64:
		return big.NewInt(reflect.ValueOf(v).Int()), nil
	case uint, uint8, uint16, uint32, uint64:
		return new(big.Int).SetUint64(reflect.ValueOf(v).Uint()), nil
	case float32, float64:
		f := reflect.ValueOf(v).Float()
		return big.NewInt(int64(f)), nil
	case []byte:
		if len(v) == 0 {
			return nil, nil
		}
		return new(big.Int).SetBytes(v), nil
	default:
		return toBigIntValue(fmt.Sprintf("%v", v))
	}
}

func toByteSlice(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case bridgetypes.HexBytes:
		return []byte(v), nil
	case string:
		if v == "" || v == "0x" {
			return nil, nil
		}
		if strings.HasPrefix(v, "0x") {
			return common.FromHex(v), nil
		}
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, err
		}
		return decoded, nil
	case json.Number:
		return toByteSlice(string(v))
	default:
		return nil, fmt.Errorf("unsupported signature type %T", value)
	}
}

func toAddressValue(value interface{}) (common.Address, error) {
	switch v := value.(type) {
	case common.Address:
		return v, nil
	case *common.Address:
		if v == nil {
			return common.Address{}, fmt.Errorf("nil address pointer")
		}
		return *v, nil
	case string:
		if !common.IsHexAddress(v) {
			return common.Address{}, fmt.Errorf("invalid hex address %s", v)
		}
		return common.HexToAddress(v), nil
	case []byte:
		if len(v) == 0 {
			return common.Address{}, nil
		}
		if len(v) != common.AddressLength {
			return common.Address{}, fmt.Errorf("unexpected address byte length %d", len(v))
		}
		return common.BytesToAddress(v), nil
	default:
		val := reflect.ValueOf(value)
		if val.Kind() == reflect.Array && val.Len() == common.AddressLength {
			bytes := make([]byte, common.AddressLength)
			for i := 0; i < common.AddressLength; i++ {
				bytes[i] = byte(val.Index(i).Uint())
			}
			return common.BytesToAddress(bytes), nil
		}
	}
	return common.Address{}, fmt.Errorf("unsupported address type %T", value)
}

// callContractMethod calls a generic contract method
func (b *Bridge) callContractMethod(ctx context.Context, destClient *WriteClient, contractAddress common.Address, methodName, abiJSON string, params []interface{}, gasPrice *big.Int, gasLimit uint64) (*types.Transaction, error) {
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

	// Create transaction using the provided contract address
	tx, err := bind.NewBoundContract(contractAddress, parsedABI, destClient.client, destClient.client, destClient.client).Transact(auth, methodName, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return tx, nil
}

// updateLastUpdate updates the last update time for a symbol
func (wc *WriteClient) updateLastUpdate(symbol string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.lastUpdate[symbol] = time.Now()
}

// getGasPrice gets the current gas price for a destination chain
func (b *Bridge) getGasPrice(ctx context.Context, destClient *WriteClient) (*big.Int, error) {
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
	for _, contract := range destClient.contracts {
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
	sourceConfig := b.configService.GetInfrastructure().Source
	b.stats.ChainStats[sourceConfig.ChainID] = &bridgetypes.ChainStatus{
		ChainID:   sourceConfig.ChainID,
		Name:      sourceConfig.Name,
		Connected: true,
	}

	// Destination chain stats
	for _, destClient := range b.writeClients {
		b.stats.ChainStats[destClient.chainConfig.ChainID] = &bridgetypes.ChainStatus{
			ChainID:   destClient.chainConfig.ChainID,
			Name:      destClient.chainConfig.Name,
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
	ticker := time.NewTicker(b.configService.GetInfrastructure().HealthCheck.CheckInterval.Duration())
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
	sourceConfig := b.configService.GetInfrastructure().Source
	if err := b.checkChainHealth(ctx, b.readClient, sourceConfig.ChainID); err != nil {
		logger.Errorf("Source chain health check failed: %v", err)
	}

	// Check destination chains
	for _, destClient := range b.writeClients {
		if err := b.checkChainHealth(ctx, destClient.client, destClient.chainConfig.ChainID); err != nil {
			logger.Errorf("Destination chain %d health check failed: %v", destClient.chainConfig.ChainID, err)
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
	if b.configService.GetInfrastructure().Metrics.Enabled {
		logger.Info("Metrics collection is enabled")
	}

	// Start API server if configured
	if b.configService.GetInfrastructure().API.ListenAddr != "" {
		// Create metrics collector for API
		var metricsCollector *metrics.Collector
		if b.metrics != nil {
			// Use the singleton metrics collector which includes IntentMetrics
			metricsCollector = metrics.NewCollector()
			// Override the FailoverMetrics with the bridge's instance
			metricsCollector.FailoverMetrics = b.metrics
		}

		// API server needs legacy config format (temporary)
		// Create minimal legacy config for API server
		legacyConfig := &config.Config{
			Database:         b.configService.GetInfrastructure().Database,
			Source:           b.configService.GetInfrastructure().Source,
			PrivateKey:       b.configService.GetInfrastructure().PrivateKey,
			EventDefinitions: b.configService.GetEventDefinitions(),
			API:              b.configService.GetInfrastructure().API,
			Destinations:     b.legacyDestinations,
		}
		apiServer := api.NewServer(legacyConfig, b.db, nil, metricsCollector, b.routerRegistry)

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

	select {
	case <-ctx.Done():
		logger.Info("Metrics server stopping due to context cancellation")
		return
	case <-b.shutdownChan:
		logger.Info("Metrics server stopping due to shutdown signal")
		return
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

			// Record error metrics if available
			if b.metricsTracker != nil {
				// Count errors for monitoring/alerting
				// This enables external alerting systems to detect issues
			}

			// Log error details for troubleshooting
			logger.Errorf("Error reported by bridge component: %v", err)
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
