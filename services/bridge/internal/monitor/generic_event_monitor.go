package monitor

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/pipeline"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// GenericEventMonitor monitors blockchain events using generic event definitions
type GenericEventMonitor struct {
	config        *config.EventMonitorConfig
	sourceConfig  *config.SourceConfig
	eventDefs     map[string]*config.EventDefinition
	httpClient    *ethclient.Client
	wsClient      *ethclient.Client
	rpcClient     *rpc.Client
	dataExtractor *pipeline.DataExtractor
	eventChan     chan<- *bridgeTypes.EventData
	errorChan     chan<- error

	mu              sync.RWMutex
	connected       bool
	lastBlockNumber uint64
	reconnectCount  int
	subscription    ethereum.Subscription

	stopChan    chan struct{}
	stoppedChan chan struct{}
}

// NewGenericEventMonitor creates a new generic event monitor
func NewGenericEventMonitor(
	cfg *config.EventMonitorConfig,
	sourceConfig *config.SourceConfig,
	eventDefs map[string]*config.EventDefinition,
	httpClient *ethclient.Client,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (*GenericEventMonitor, error) {
	// Create data extractor
	dataExtractor, err := pipeline.NewDataExtractor(eventDefs)
	if err != nil {
		return nil, fmt.Errorf("failed to create data extractor: %w", err)
	}

	// Connect to WebSocket if URL provided
	var wsClient *ethclient.Client
	var rpcClient *rpc.Client
	if sourceConfig.WsURL != "" {
		wsClient, rpcClient, err = connectWebSocket(sourceConfig.WsURL)
		if err != nil {
			logger.Warnf("Failed to connect to WebSocket, will retry: %v", err)
		}
	}

	monitor := &GenericEventMonitor{
		config:        cfg,
		sourceConfig:  sourceConfig,
		eventDefs:     eventDefs,
		httpClient:    httpClient,
		wsClient:      wsClient,
		rpcClient:     rpcClient,
		dataExtractor: dataExtractor,
		eventChan:     eventChan,
		errorChan:     errorChan,
		stopChan:      make(chan struct{}),
		stoppedChan:   make(chan struct{}),
	}

	return monitor, nil
}

// Start begins monitoring events
func (gem *GenericEventMonitor) Start(ctx context.Context) error {
	if !gem.config.Enabled {
		logger.Info("Generic event monitor disabled")
		return nil
	}

	logger.Info("Starting generic event monitor")

	// Start WebSocket subscription if available
	if gem.sourceConfig.WsURL != "" {
		go gem.monitorLoop(ctx)
		go gem.connectionMonitor(ctx)
	} else {
		// Fall back to polling mode
		go gem.pollingLoop(ctx)
	}

	return nil
}

// Stop gracefully stops the event monitor
func (gem *GenericEventMonitor) Stop() error {
	logger.Info("Stopping generic event monitor")

	close(gem.stopChan)

	// Wait for monitor to stop with timeout
	select {
	case <-gem.stoppedChan:
		logger.Info("Generic event monitor stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Generic event monitor stop timeout")
	}

	// Close WebSocket connection
	if gem.wsClient != nil {
		gem.wsClient.Close()
	}
	if gem.rpcClient != nil {
		gem.rpcClient.Close()
	}

	return nil
}

// monitorLoop is the main event monitoring loop for WebSocket
func (gem *GenericEventMonitor) monitorLoop(ctx context.Context) {
	defer close(gem.stoppedChan)

	for {
		select {
		case <-ctx.Done():
			return
		case <-gem.stopChan:
			return
		default:
			if err := gem.subscribeToEvents(ctx); err != nil {
				logger.Errorf("Event subscription error: %v", err)
				gem.handleReconnect()
			}
		}
	}
}

// subscribeToEvents creates and maintains event subscriptions
func (gem *GenericEventMonitor) subscribeToEvents(ctx context.Context) error {
	// Ensure we have a WebSocket connection
	if gem.wsClient == nil {
		if err := gem.reconnectWebSocket(); err != nil {
			return fmt.Errorf("failed to connect to WebSocket: %w", err)
		}
	}

	// Build filter query for all configured events
	addresses := make([]common.Address, 0)
	topics := [][]common.Hash{{}} // First topic is event signature

	// Collect all contract addresses and event signatures
	addressMap := make(map[common.Address]bool)
	for _, eventDef := range gem.eventDefs {
		// Add contract address
		contractAddr := common.HexToAddress(eventDef.Contract)
		if !addressMap[contractAddr] {
			addresses = append(addresses, contractAddr)
			addressMap[contractAddr] = true
		}

		// TODO: Calculate event signature - need to expose abiCache from dataExtractor
		// eventABI, exists := gem.dataExtractor.abiCache[eventName]
		// if exists {
		// 	topics[0] = append(topics[0], eventABI.ID)
		// }
	}

	// Create filter query
	query := ethereum.FilterQuery{
		Addresses: addresses,
		Topics:    topics,
	}

	// Subscribe to logs
	logs := make(chan types.Log, 100)
	sub, err := gem.wsClient.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("failed to subscribe to logs: %w", err)
	}

	gem.mu.Lock()
	gem.subscription = sub
	gem.connected = true
	gem.mu.Unlock()

	logger.Infof("Successfully subscribed to %d events from %d contracts",
		len(gem.eventDefs), len(addresses))

	// Process incoming logs
	for {
		select {
		case <-ctx.Done():
			sub.Unsubscribe()
			return ctx.Err()

		case <-gem.stopChan:
			sub.Unsubscribe()
			return nil

		case err := <-sub.Err():
			gem.mu.Lock()
			gem.connected = false
			gem.mu.Unlock()
			return fmt.Errorf("subscription error: %w", err)

		case log := <-logs:
			if err := gem.processLog(log); err != nil {
				logger.Errorf("Failed to process log: %v", err)
				gem.errorChan <- err
			}
		}
	}
}

// processLog processes a single event log
func (gem *GenericEventMonitor) processLog(log types.Log) error {
	// Update last block number
	gem.mu.Lock()
	if log.BlockNumber > gem.lastBlockNumber {
		gem.lastBlockNumber = log.BlockNumber
	}
	gem.mu.Unlock()

	// Match log to event definition
	eventName, _, err := gem.dataExtractor.MatchEventDefinition(log)
	if err != nil {
		return fmt.Errorf("failed to match event: %w", err)
	}

	// Extract event data
	extractedData, err := gem.dataExtractor.ExtractEventData(eventName, log)
	if err != nil {
		return fmt.Errorf("failed to extract event data: %w", err)
	}

	// Convert to bridge event data
	eventData := gem.dataExtractor.ConvertToEventData(eventName, extractedData, log)

	// Send to event channel
	select {
	case gem.eventChan <- eventData:
		logger.Infof("Generic event detected: %s from contract %s at block %d",
			eventName, log.Address.Hex(), log.BlockNumber)
	default:
		logger.Warn("Event channel full, dropping event")
	}

	return nil
}

// pollingLoop polls for events when WebSocket is not available
func (gem *GenericEventMonitor) pollingLoop(ctx context.Context) {
	defer close(gem.stoppedChan)

	ticker := time.NewTicker(10 * time.Second) // Poll every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gem.stopChan:
			return
		case <-ticker.C:
			if err := gem.pollEvents(ctx); err != nil {
				logger.Errorf("Failed to poll events: %v", err)
			}
		}
	}
}

// pollEvents polls for new events
func (gem *GenericEventMonitor) pollEvents(ctx context.Context) error {
	// Get current block number
	currentBlock, err := gem.httpClient.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}

	// Determine block range to scan
	fromBlock := gem.lastBlockNumber + 1
	if fromBlock == 1 {
		fromBlock = gem.sourceConfig.StartBlock
	}

	if fromBlock > currentBlock {
		return nil // No new blocks
	}

	toBlock := fromBlock + 1000 // Scan up to 1000 blocks at a time
	if toBlock > currentBlock {
		toBlock = currentBlock
	}

	// Build filter query
	addresses := make([]common.Address, 0)
	for _, eventDef := range gem.eventDefs {
		addresses = append(addresses, common.HexToAddress(eventDef.Contract))
	}

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: addresses,
	}

	// Get logs
	logs, err := gem.httpClient.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to filter logs: %w", err)
	}

	// Process logs
	for _, log := range logs {
		if err := gem.processLog(log); err != nil {
			logger.Errorf("Failed to process log: %v", err)
		}
	}

	// Update last block number
	gem.mu.Lock()
	gem.lastBlockNumber = toBlock
	gem.mu.Unlock()

	return nil
}

// connectionMonitor monitors WebSocket connection health
func (gem *GenericEventMonitor) connectionMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gem.stopChan:
			return
		case <-ticker.C:
			gem.checkConnection()
		}
	}
}

// checkConnection verifies WebSocket connection is healthy
func (gem *GenericEventMonitor) checkConnection() {
	gem.mu.RLock()
	connected := gem.connected
	gem.mu.RUnlock()

	if !connected {
		logger.Warn("WebSocket disconnected, attempting reconnect")
		if err := gem.reconnectWebSocket(); err != nil {
			logger.Errorf("Failed to reconnect WebSocket: %v", err)
		}
	} else if gem.wsClient != nil {
		// Verify connection with a simple call
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := gem.wsClient.BlockNumber(ctx); err != nil {
			logger.Warnf("WebSocket health check failed: %v", err)
			gem.mu.Lock()
			gem.connected = false
			gem.mu.Unlock()
		}
	}
}

// handleReconnect implements exponential backoff for reconnection
func (gem *GenericEventMonitor) handleReconnect() {
	gem.mu.Lock()
	gem.reconnectCount++
	count := gem.reconnectCount
	gem.mu.Unlock()

	if count > gem.config.MaxReconnectAttempts {
		logger.Error("Max reconnection attempts reached")
		gem.errorChan <- fmt.Errorf("max reconnection attempts reached")
		return
	}

	// Exponential backoff
	backoff := time.Duration(count) * gem.config.ReconnectInterval.Duration()
	if backoff > time.Minute {
		backoff = time.Minute
	}

	logger.Infof("Waiting %s before reconnect attempt %d", backoff, count)
	time.Sleep(backoff)
}

// reconnectWebSocket attempts to reconnect the WebSocket client
func (gem *GenericEventMonitor) reconnectWebSocket() error {
	// Close existing connections
	if gem.wsClient != nil {
		gem.wsClient.Close()
	}
	if gem.rpcClient != nil {
		gem.rpcClient.Close()
	}

	// Connect to WebSocket
	wsClient, rpcClient, err := connectWebSocket(gem.sourceConfig.WsURL)
	if err != nil {
		return err
	}

	gem.mu.Lock()
	gem.wsClient = wsClient
	gem.rpcClient = rpcClient
	gem.reconnectCount = 0
	gem.mu.Unlock()

	logger.Info("WebSocket reconnected successfully")
	return nil
}

// GetStats returns monitor statistics
func (gem *GenericEventMonitor) GetStats() EventMonitorStats {
	gem.mu.RLock()
	defer gem.mu.RUnlock()

	return EventMonitorStats{
		Connected:       gem.connected,
		LastBlockNumber: gem.lastBlockNumber,
		ReconnectCount:  gem.reconnectCount,
		EventCount:      len(gem.eventDefs),
	}
}

// EventMonitorStats represents event monitor statistics
type EventMonitorStats struct {
	Connected       bool
	LastBlockNumber uint64
	ReconnectCount  int
	EventCount      int
}

func connectWebSocket(wsURL string) (*ethclient.Client, *rpc.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rpcClient, err := rpc.DialContext(ctx, wsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	wsClient := ethclient.NewClient(rpcClient)

	// Test connection
	if _, err := wsClient.BlockNumber(ctx); err != nil {
		rpcClient.Close()
		return nil, nil, fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	return wsClient, rpcClient, nil
}
