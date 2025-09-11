package monitor

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// EventMonitor monitors blockchain events via WebSocket
type EventMonitor struct {
	config           *config.EventMonitorConfig
	sourceConfig     *config.SourceConfig
	httpClient       *ethclient.Client
	wsClient         *ethclient.Client
	rpcClient        *rpc.Client
	contractABIs     map[string]abi.ABI
	eventSignatures  map[string]common.Hash
	eventChan        chan<- *bridgeTypes.EventData
	errorChan        chan<- error
	
	mu               sync.RWMutex
	connected        bool
	lastBlockNumber  uint64
	reconnectCount   int
	subscription     ethereum.Subscription
	
	stopChan         chan struct{}
	stoppedChan      chan struct{}
}

// NewEventMonitor creates a new event monitor
func NewEventMonitor(
	cfg *config.EventMonitorConfig,
	sourceConfig *config.SourceConfig,
	httpClient *ethclient.Client,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (*EventMonitor, error) {
	// Connect to WebSocket
	wsClient, rpcClient, err := connectWebSocket(sourceConfig.WsURL)
	if err != nil {
		logger.Warnf("Failed to connect to WebSocket, will retry: %v", err)
		// Don't fail here, we'll retry in the monitor loop
	}

	monitor := &EventMonitor{
		config:          cfg,
		sourceConfig:    sourceConfig,
		httpClient:      httpClient,
		wsClient:        wsClient,
		rpcClient:       rpcClient,
		contractABIs:    make(map[string]abi.ABI),
		eventSignatures: make(map[string]common.Hash),
		eventChan:       eventChan,
		errorChan:       errorChan,
		stopChan:        make(chan struct{}),
		stoppedChan:     make(chan struct{}),
	}

	// Parse event ABIs and calculate signatures
	if err := monitor.parseEventConfigs(); err != nil {
		return nil, fmt.Errorf("failed to parse event configs: %w", err)
	}

	return monitor, nil
}

// Start begins monitoring events
func (em *EventMonitor) Start(ctx context.Context) error {
	if !em.config.Enabled {
		logger.Info("Event monitor disabled")
		return nil
	}

	logger.Info("Starting event monitor")

	// Start WebSocket subscription in a goroutine
	go em.monitorLoop(ctx)

	// Start connection health monitor
	go em.connectionMonitor(ctx)

	return nil
}

// Stop gracefully stops the event monitor
func (em *EventMonitor) Stop() error {
	logger.Info("Stopping event monitor")
	
	close(em.stopChan)
	
	// Wait for monitor to stop with timeout
	select {
	case <-em.stoppedChan:
		logger.Info("Event monitor stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Event monitor stop timeout")
	}

	// Close WebSocket connection
	if em.wsClient != nil {
		em.wsClient.Close()
	}
	if em.rpcClient != nil {
		em.rpcClient.Close()
	}

	return nil
}

// monitorLoop is the main event monitoring loop
func (em *EventMonitor) monitorLoop(ctx context.Context) {
	defer close(em.stoppedChan)

	for {
		select {
		case <-ctx.Done():
			return
		case <-em.stopChan:
			return
		default:
			if err := em.subscribeToEvents(ctx); err != nil {
				logger.Errorf("Event subscription error: %v", err)
				em.handleReconnect()
			}
		}
	}
}

// subscribeToEvents creates and maintains event subscriptions
func (em *EventMonitor) subscribeToEvents(ctx context.Context) error {
	// Ensure we have a WebSocket connection
	if em.wsClient == nil {
		if err := em.reconnectWebSocket(); err != nil {
			return fmt.Errorf("failed to connect to WebSocket: %w", err)
		}
	}

	// Build filter query for all enabled events
	addresses := []common.Address{}
	topics := [][]common.Hash{{}} // First topic is event signature

	// TODO: This code needs to be updated to work with the new configuration structure
	// For now, we'll use empty addresses and topics to allow compilation
	_ = addresses
	_ = topics

	// Create filter query
	query := ethereum.FilterQuery{
		Addresses: addresses,
		Topics:    topics,
	}

	// Subscribe to logs
	logs := make(chan types.Log, 100)
	sub, err := em.wsClient.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("failed to subscribe to logs: %w", err)
	}

	em.mu.Lock()
	em.subscription = sub
	em.connected = true
	em.mu.Unlock()

	logger.Info("Successfully subscribed to events")

	// Process incoming logs
	for {
		select {
		case <-ctx.Done():
			sub.Unsubscribe()
			return ctx.Err()
			
		case <-em.stopChan:
			sub.Unsubscribe()
			return nil
			
		case err := <-sub.Err():
			em.mu.Lock()
			em.connected = false
			em.mu.Unlock()
			return fmt.Errorf("subscription error: %w", err)
			
		case log := <-logs:
			if err := em.processLog(log); err != nil {
				logger.Errorf("Failed to process log: %v", err)
				em.errorChan <- err
			}
		}
	}
}

// processLog processes a single event log
func (em *EventMonitor) processLog(log types.Log) error {
	// Update last block number
	em.mu.Lock()
	if log.BlockNumber > em.lastBlockNumber {
		em.lastBlockNumber = log.BlockNumber
	}
	em.mu.Unlock()

	// Find matching event configuration
	eventName, eventConfig := em.findEventConfig(log)
	if eventName == "" {
		return fmt.Errorf("unknown event signature: %s", log.Topics[0].Hex())
	}

	// Parse event data
	eventData, err := em.parseEventData(eventName, eventConfig, log)
	if err != nil {
		return fmt.Errorf("failed to parse event data: %w", err)
	}

	// Apply event filters
	if !em.shouldProcessEvent(eventData) {
		logger.Debugf("Event filtered out: %s", eventName)
		return nil
	}

	// Send to event channel
	select {
	case em.eventChan <- eventData:
		logger.Infof("Event detected: %s, symbol: %s, block: %d", 
			eventName, eventData.Symbol, log.BlockNumber)
	default:
		logger.Warn("Event channel full, dropping event")
	}

	return nil
}

// parseEventData parses raw log data into EventData
func (em *EventMonitor) parseEventData(eventName string, eventConfig map[string]interface{}, log types.Log) (*bridgeTypes.EventData, error) {
	// Get ABI for this event
	eventABI, exists := em.contractABIs[eventName]
	if !exists {
		return nil, fmt.Errorf("no ABI found for event: %s", eventName)
	}

	// Find the event definition
	event, err := eventABI.EventByID(log.Topics[0])
	if err != nil {
		return nil, fmt.Errorf("event not found in ABI: %w", err)
	}

	// Parse indexed and non-indexed data
	eventMap := make(map[string]interface{})
	
	// Parse indexed parameters
	indexedArgs := make([]abi.Argument, 0)
	for _, input := range event.Inputs {
		if input.Indexed {
			indexedArgs = append(indexedArgs, input)
		}
	}
	
	if len(indexedArgs) > 0 && len(log.Topics) > 1 {
		err := abi.ParseTopicsIntoMap(eventMap, indexedArgs, log.Topics[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse topics: %w", err)
		}
	}

	// Parse non-indexed data
	if len(log.Data) > 0 {
		err := eventABI.UnpackIntoMap(eventMap, event.Name, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to unpack data: %w", err)
		}
	}

	// Extract common fields
	eventData := &bridgeTypes.EventData{
		EventName:       eventName,
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Raw:             log,
		Data:            eventMap,
	}

	// Extract specific fields based on event type
	if intentHash, ok := getFieldValue(eventMap, "intentHash", "hash"); ok {
		eventData.IntentHash = intentHash.([32]byte)
	}
	
	if symbol, ok := getFieldValue(eventMap, "symbol", ""); ok {
		eventData.Symbol = symbol.(string)
	}
	
	if price, ok := getFieldValue(eventMap, "price", ""); ok {
		eventData.Price = price.(*big.Int)
	}
	
	if timestamp, ok := getFieldValue(eventMap, "timestamp", ""); ok {
		eventData.Timestamp = timestamp.(*big.Int)
	}
	
	if signer, ok := getFieldValue(eventMap, "signer", ""); ok {
		eventData.Signer = signer.(common.Address)
	}

	return eventData, nil
}

// shouldProcessEvent checks if an event should be processed based on filters
func (em *EventMonitor) shouldProcessEvent(event *bridgeTypes.EventData) bool {
	// TODO: This code needs to be updated to work with the new configuration structure
	// For now, we'll accept all events to allow compilation
	_ = event
	return true
}

// connectionMonitor monitors WebSocket connection health
func (em *EventMonitor) connectionMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-em.stopChan:
			return
		case <-ticker.C:
			em.checkConnection()
		}
	}
}

// checkConnection verifies WebSocket connection is healthy
func (em *EventMonitor) checkConnection() {
	em.mu.RLock()
	connected := em.connected
	em.mu.RUnlock()

	if !connected {
		logger.Warn("WebSocket disconnected, attempting reconnect")
		if err := em.reconnectWebSocket(); err != nil {
			logger.Errorf("Failed to reconnect WebSocket: %v", err)
		}
	} else {
		// Verify connection with a simple call
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if _, err := em.wsClient.BlockNumber(ctx); err != nil {
			logger.Warnf("WebSocket health check failed: %v", err)
			em.mu.Lock()
			em.connected = false
			em.mu.Unlock()
		}
	}
}

// handleReconnect implements exponential backoff for reconnection
func (em *EventMonitor) handleReconnect() {
	em.mu.Lock()
	em.reconnectCount++
	count := em.reconnectCount
	em.mu.Unlock()

	if count > em.config.MaxReconnectAttempts {
		logger.Error("Max reconnection attempts reached")
		em.errorChan <- fmt.Errorf("max reconnection attempts reached")
		return
	}

	// Exponential backoff
	backoff := time.Duration(count) * em.config.ReconnectInterval.Duration()
	if backoff > time.Minute {
		backoff = time.Minute
	}

	logger.Infof("Waiting %s before reconnect attempt %d", backoff, count)
	time.Sleep(backoff)
}

// reconnectWebSocket attempts to reconnect the WebSocket client
func (em *EventMonitor) reconnectWebSocket() error {
	// Close existing connections
	if em.wsClient != nil {
		em.wsClient.Close()
	}
	if em.rpcClient != nil {
		em.rpcClient.Close()
	}

	// Connect to WebSocket
	wsClient, rpcClient, err := connectWebSocket(em.sourceConfig.WsURL)
	if err != nil {
		return err
	}

	em.mu.Lock()
	em.wsClient = wsClient
	em.rpcClient = rpcClient
	em.reconnectCount = 0
	em.mu.Unlock()

	logger.Info("WebSocket reconnected successfully")
	return nil
}

// parseEventConfigs parses event configurations and prepares ABIs
func (em *EventMonitor) parseEventConfigs() error {
	// TODO: This code needs to be updated to work with the new configuration structure
	// For now, we'll skip parsing to allow compilation
	return nil
}

// findEventConfig finds the matching event configuration for a log
func (em *EventMonitor) findEventConfig(log types.Log) (string, map[string]interface{}) {
	// TODO: This code needs to be updated to work with the new configuration structure
	// For now, we'll return empty to allow compilation
	_ = log
	return "", nil
}

// Helper functions

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

func parseEventABI(abiStr string) (abi.ABI, error) {
	// Create a minimal ABI with just the event
	fullABI := fmt.Sprintf(`[%s]`, abiStr)
	return abi.JSON(strings.NewReader(fullABI))
}

func calculateEventSignature(eventABI string) common.Hash {
	// Extract event signature from ABI string
	// Format: "event EventName(type1,type2,...)"
	start := strings.Index(eventABI, "event ") + 6
	end := strings.Index(eventABI[start:], "(") + start
	eventName := eventABI[start:end]
	
	// Extract parameters
	paramsStart := strings.Index(eventABI, "(")
	paramsEnd := strings.LastIndex(eventABI, ")")
	params := eventABI[paramsStart:paramsEnd+1]
	
	// Remove parameter names, keep only types
	// This is a simplified version - in production, use proper ABI parsing
	signature := eventName + params
	
	return common.BytesToHash(common.Hex2Bytes(common.Bytes2Hex([]byte(signature))[:8]))
}

func getFieldValue(data map[string]interface{}, fieldNames ...string) (interface{}, bool) {
	for _, name := range fieldNames {
		if value, exists := data[name]; exists {
			return value, true
		}
	}
	return nil, false
}