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
	workerPool := worker.NewWorkerPool(cfgService.GetInfrastructure().WorkerPool.MaxWorkers)

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
	// Validate inputs
	if chainConfig == nil {
		return nil, fmt.Errorf("chain config cannot be nil")
	}
	if contractConfigs == nil {
		return nil, fmt.Errorf("contract configs cannot be nil")
	}
	if privateKey == "" {
		return nil, fmt.Errorf("private key cannot be empty")
	}

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
						// The enricher now stores *types.OracleIntent directly, no conversion needed
						if intent, ok := value.(*bridgetypes.OracleIntent); ok {
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
						return nil, fmt.Errorf("fullIntent has unexpected type %T", value)
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
		apiServer := api.NewServer(legacyConfig, b.db, metricsCollector, b.routerRegistry)

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
