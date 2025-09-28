package scanner

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// ChainSimulator simulates blockchain events and RPC responses
type ChainSimulator struct {
	mu            sync.RWMutex
	currentBlock  uint64
	blocks        map[uint64]*SimulatedBlock
	eventsByBlock map[uint64][]types.Log
	rpcDelay      time.Duration
	rpcErrorRate  float64 // 0.0 to 1.0
	rpcErrors     map[uint64]error
}

type SimulatedBlock struct {
	Number    uint64
	Timestamp uint64
	Events    []types.Log
}

// NewChainSimulator creates a new chain simulator
func NewChainSimulator(startBlock uint64) *ChainSimulator {
	return &ChainSimulator{
		currentBlock:  startBlock,
		blocks:        make(map[uint64]*SimulatedBlock),
		eventsByBlock: make(map[uint64][]types.Log),
		rpcDelay:      10 * time.Millisecond, // Realistic RPC delay
		rpcErrors:     make(map[uint64]error),
	}
}

// SimulateNewBlocks generates new blocks with events
func (cs *ChainSimulator) SimulateNewBlocks(count uint64, eventsPerBlock int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := uint64(0); i < count; i++ {
		blockNum := cs.currentBlock + i + 1
		block := &SimulatedBlock{
			Number:    blockNum,
			Timestamp: uint64(time.Now().Unix() + int64(i)),
			Events:    make([]types.Log, 0),
		}

		// Generate events for this block
		for j := 0; j < eventsPerBlock; j++ {
			event := cs.createTestEvent(blockNum, uint(j))
			block.Events = append(block.Events, event)
		}

		cs.blocks[blockNum] = block
		cs.eventsByBlock[blockNum] = block.Events
	}

	cs.currentBlock += count
}

// SimulateMissedBlocks simulates a scenario where some blocks are missed
func (cs *ChainSimulator) SimulateMissedBlocks(startBlock, endBlock uint64, eventsPerBlock int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		block := &SimulatedBlock{
			Number:    blockNum,
			Timestamp: uint64(time.Now().Unix()),
			Events:    make([]types.Log, 0),
		}

		// Generate events for missed blocks
		for j := 0; j < eventsPerBlock; j++ {
			event := cs.createTestEvent(blockNum, uint(j))
			block.Events = append(block.Events, event)
		}

		cs.blocks[blockNum] = block
		cs.eventsByBlock[blockNum] = block.Events
	}
}

// SetRPCError simulates RPC errors for specific operations
func (cs *ChainSimulator) SetRPCError(blockNum uint64, err error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.rpcErrors[blockNum] = err
}

// createTestEvent creates a realistic test event
func (cs *ChainSimulator) createTestEvent(blockNum uint64, logIndex uint) types.Log {
	// Alternate between IntentRegistered and IntArraySet events
	if blockNum%2 == 0 {
		return cs.createIntentRegisteredEvent(blockNum, logIndex)
	} else {
		return cs.createIntArraySetEvent(blockNum, logIndex)
	}
}

func (cs *ChainSimulator) createIntentRegisteredEvent(blockNum uint64, logIndex uint) types.Log {
	// IntentRegistered(bytes32,string,uint256,uint256,address)
	eventSig := crypto.Keccak256Hash([]byte("IntentRegistered(bytes32,string,uint256,uint256,address)"))
	intentHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("intent_%d_%d", blockNum, logIndex)))
	symbolHash := crypto.Keccak256Hash([]byte("BTC"))

	// Event data: price, timestamp, signer
	price := big.NewInt(50000 + int64(blockNum))
	timestamp := big.NewInt(int64(time.Now().Unix()))
	signer := common.HexToAddress("0x742d35cc6641c31b0c23b8e53d8cf3d21b1e4b7b")

	data := make([]byte, 96)
	copy(data[0:32], price.FillBytes(make([]byte, 32)))
	copy(data[32:64], timestamp.FillBytes(make([]byte, 32)))
	copy(data[64:96], common.LeftPadBytes(signer.Bytes(), 32))

	return types.Log{
		Address:     common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Topics:      []common.Hash{eventSig, intentHash, symbolHash},
		Data:        data,
		BlockNumber: blockNum,
		TxHash:      crypto.Keccak256Hash([]byte(fmt.Sprintf("tx_%d_%d", blockNum, logIndex))),
		TxIndex:     uint(logIndex),
		BlockHash:   crypto.Keccak256Hash([]byte(fmt.Sprintf("block_%d", blockNum))),
		Index:       logIndex,
		Removed:     false,
	}
}

func (cs *ChainSimulator) createIntArraySetEvent(blockNum uint64, logIndex uint) types.Log {
	// IntArraySet(uint256,int256,string,string)
	eventSig := crypto.Keccak256Hash([]byte("IntArraySet(uint256,int256,string,string)"))
	round := big.NewInt(int64(blockNum))

	// Event data: requestId + dynamic data
	requestId := big.NewInt(int64(blockNum*1000 + uint64(logIndex)))
	data := make([]byte, 32)
	copy(data[0:32], requestId.FillBytes(make([]byte, 32)))

	return types.Log{
		Address:     common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12"),
		Topics:      []common.Hash{eventSig, common.BigToHash(round)},
		Data:        data,
		BlockNumber: blockNum,
		TxHash:      crypto.Keccak256Hash([]byte(fmt.Sprintf("tx_%d_%d", blockNum, logIndex))),
		TxIndex:     uint(logIndex),
		BlockHash:   crypto.Keccak256Hash([]byte(fmt.Sprintf("block_%d", blockNum))),
		Index:       logIndex,
		Removed:     false,
	}
}

// MockClientWithSimulator implements ethclient.Client interface with simulation
type MockClientWithSimulator struct {
	mock.Mock
	simulator *ChainSimulator
}

func (m *MockClientWithSimulator) BlockNumber(ctx context.Context) (uint64, error) {
	time.Sleep(m.simulator.rpcDelay) // Simulate network delay

	m.simulator.mu.RLock()
	currentBlock := m.simulator.currentBlock
	m.simulator.mu.RUnlock()

	return currentBlock, nil
}

func (m *MockClientWithSimulator) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	time.Sleep(m.simulator.rpcDelay) // Simulate network delay

	startBlock := query.FromBlock.Uint64()
	endBlock := query.ToBlock.Uint64()

	// Check for simulated RPC errors
	m.simulator.mu.RLock()
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		if err, exists := m.simulator.rpcErrors[blockNum]; exists {
			m.simulator.mu.RUnlock()
			return nil, err
		}
	}
	m.simulator.mu.RUnlock()

	var allLogs []types.Log
	m.simulator.mu.RLock()
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		if events, exists := m.simulator.eventsByBlock[blockNum]; exists {
			// Filter events by addresses and topics
			for _, event := range events {
				if m.matchesQuery(event, query) {
					allLogs = append(allLogs, event)
				}
			}
		}
	}
	m.simulator.mu.RUnlock()

	return allLogs, nil
}

func (m *MockClientWithSimulator) matchesQuery(log types.Log, query ethereum.FilterQuery) bool {
	// Check addresses
	if len(query.Addresses) > 0 {
		addressMatch := false
		for _, addr := range query.Addresses {
			if log.Address == addr {
				addressMatch = true
				break
			}
		}
		if !addressMatch {
			return false
		}
	}

	// Enhanced topic filtering to support multi-level topic matching
	for topicLevel, topicOptions := range query.Topics {
		if len(topicOptions) > 0 && topicLevel < len(log.Topics) {
			topicMatch := false
			for _, topic := range topicOptions {
				if log.Topics[topicLevel] == topic {
					topicMatch = true
					break
				}
			}
			if !topicMatch {
				return false
			}
		}
	}

	return true
}

func (m *MockClientWithSimulator) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	sub := NewMockSubscription()
	// Configure the mock subscription to expect calls to Err() and Unsubscribe()
	sub.On("Err").Return(sub.errChan)
	sub.On("Unsubscribe").Return()
	return sub, nil
}

func (m *MockClientWithSimulator) Close() {}

// MockDatabaseWithState provides a database with realistic state management
type MockDatabaseWithState struct {
	mock.Mock
	chainStates     map[int64]*database.ChainState
	processedEvents map[string]*bridgeTypes.EventData
	mu              sync.RWMutex
}

func NewMockDatabaseWithState() *MockDatabaseWithState {
	return &MockDatabaseWithState{
		chainStates:     make(map[int64]*database.ChainState),
		processedEvents: make(map[string]*bridgeTypes.EventData),
	}
}

func (m *MockDatabaseWithState) InitializeChainState(chainID int64, name string, startBlock uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chainStates[chainID] = &database.ChainState{
		ChainID:       chainID,
		ChainName:     name,
		LastScanBlock: startBlock,
		UpdatedAt:     time.Now(),
	}
	return nil
}

func (m *MockDatabaseWithState) GetChainState(chainID int64) (*database.ChainState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if state, exists := m.chainStates[chainID]; exists {
		stateCopy := *state
		return &stateCopy, nil
	}
	return nil, fmt.Errorf("chain state not found for chain %d", chainID)
}

func (m *MockDatabaseWithState) UpdateLastScanBlock(chainID int64, blockNumber uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, exists := m.chainStates[chainID]; exists {
		if blockNumber > state.LastScanBlock {
			state.LastScanBlock = blockNumber
			state.UpdatedAt = time.Now()
		}
	}
	return nil
}

func (m *MockDatabaseWithState) IsEventProcessed(intentHash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.processedEvents[intentHash]
	return exists, nil
}

// MarkEventProcessed now stores the full event, which is more realistic
func (m *MockDatabaseWithState) MarkEventProcessed(event *bridgeTypes.EventData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hash := common.BytesToHash(event.IntentHash[:]).Hex()
	m.processedEvents[hash] = event
}

// GetProcessedEventsByBlockRange is now fully implemented for gap detection testing
func (m *MockDatabaseWithState) GetProcessedEventsByBlockRange(startBlock, endBlock uint64) ([]*database.ProcessedEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var events []*database.ProcessedEvent
	for hash, event := range m.processedEvents {
		if event.BlockNumber >= startBlock && event.BlockNumber <= endBlock {
			processedEvent := &database.ProcessedEvent{
				EventName:   event.EventName,
				IntentHash:  hash,
				BlockNumber: event.BlockNumber,
				TransactionHash: func() string {
					if event.TxHash == (common.Hash{}) {
						return ""
					}
					return event.TxHash.Hex()
				}(),
				LogIndex: event.LogIndex,
				Symbol:   event.Symbol,
				Price: func() string {
					if event.Price != nil {
						return event.Price.String()
					}
					return "0"
				}(),
				Timestamp: func() uint64 {
					if event.Timestamp != nil {
						return event.Timestamp.Uint64()
					}
					return 0
				}(),
				Signer:      event.Signer,
				ProcessedAt: time.Now(),
			}
			events = append(events, processedEvent)
		}
	}
	return events, nil
}

// True Integration Tests for EnhancedBlockScanner

// testHarness sets up a full test environment for the EnhancedBlockScanner
type testHarness struct {
	t                *testing.T
	scanner          *EnhancedBlockScanner
	simulator        *ChainSimulator
	mockClient       *MockClientWithSimulator
	mockDB           *MockDatabaseWithState
	eventChan        chan *bridgeTypes.EventData
	errorChan        chan error
	collectedEvents  []*bridgeTypes.EventData
	collectedErrors  []error
	collectionWg     sync.WaitGroup
	stopConsumerOnce sync.Once
	consumer         func(event *bridgeTypes.EventData)
}

func setupScannerTest(t *testing.T) *testHarness {
	eventChan := make(chan *bridgeTypes.EventData, 100)
	errorChan := make(chan error, 10)

	h := &testHarness{
		t:               t,
		simulator:       NewChainSimulator(1000),
		mockDB:          NewMockDatabaseWithState(),
		eventChan:       eventChan,
		errorChan:       errorChan,
		collectedEvents: make([]*bridgeTypes.EventData, 0),
	}
	h.mockClient = &MockClientWithSimulator{simulator: h.simulator}

	scfg, sourceConfig, eventDefs := CreateTestConfig()

	var err error
	h.scanner, err = NewEnhancedBlockScanner(scfg, sourceConfig, eventDefs, h.mockClient, h.mockDB, eventChan, errorChan)
	assert.NoError(t, err)

	// Set a default consumer that just collects events.
	h.consumer = func(event *bridgeTypes.EventData) {
		h.mockDB.mu.Lock()
		h.collectedEvents = append(h.collectedEvents, event)
		h.mockDB.mu.Unlock()
	}

	return h
}

func (h *testHarness) start(ctx context.Context) {
	h.collectionWg.Add(2)
	// Start collecting events and errors
	go func() {
		defer h.collectionWg.Done()
		for event := range h.eventChan {
			if h.consumer != nil {
				h.consumer(event)
			}
		}
	}()
	go func() {
		defer h.collectionWg.Done()
		for err := range h.errorChan {
			h.mockDB.mu.Lock()
			h.collectedErrors = append(h.collectedErrors, err)
			h.mockDB.mu.Unlock()
		}
	}()

	// Start the scanner
	err := h.scanner.Start(ctx)
	assert.NoError(h.t, err)
}

func (h *testHarness) stop() {
	err := h.scanner.Stop()
	assert.NoError(h.t, err)

	h.stopConsumerOnce.Do(func() {
		close(h.eventChan)
		close(h.errorChan)
	})

	h.collectionWg.Wait()
}

// TestEnhancedScanner_NormalForwardScan tests the scanner under normal conditions where it only needs to scan forward.
func TestEnhancedScanner_NormalForwardScan(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup: DB is at block 1000. Scanner will start, then new blocks will appear.
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.HeadTrackerInterval = config.Duration(100 * time.Millisecond) // Speed up for test

	// Action: Start scanner, then simulate new blocks appearing
	h.start(ctx)
	time.Sleep(200 * time.Millisecond)   // Allow workers to start
	h.simulator.SimulateNewBlocks(10, 2) // Blocks 1001-1010, 20 events total

	// Assertions
	assert.Eventually(t, func() bool {
		h.mockDB.mu.RLock()
		defer h.mockDB.mu.RUnlock()
		return len(h.collectedEvents) == 20
	}, 5*time.Second, 100*time.Millisecond, "Should have collected all 20 events")

	h.stop()

	assert.Empty(t, h.collectedErrors, "Should be no errors during scan")
	finalState, err := h.mockDB.GetChainState(11155420)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, finalState.LastScanBlock, uint64(1010), "Scanner should have scanned up to the latest block")
}

// TestEnhancedScanner_DualScanConvergence tests the scanner's ability to handle a large gap
// by running forward and backward scanners concurrently and converging them.
func TestEnhancedScanner_DualScanConvergence(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Setup: DB is at block 1000, chain is at 1100. A 100-block gap will trigger dual-scan.
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.MaxBlockGap = 50 // Ensure dual-scan is triggered
	h.scanner.config.HeadTrackerInterval = config.Duration(200 * time.Millisecond)
	h.scanner.config.ScanInterval = config.Duration(500 * time.Millisecond)

	// Action: Simulate blocks first, then start scanner to trigger the large gap logic on startup.
	h.simulator.SimulateNewBlocks(100, 1) // Blocks 1001-1100, 100 events total
	h.start(ctx)

	// Assertions
	assert.Eventually(t, func() bool {
		h.mockDB.mu.RLock()
		defer h.mockDB.mu.RUnlock()
		state, _ := h.mockDB.GetChainState(11155420)
		// Wait for both event collection and the final DB update on convergence.
		return len(h.collectedEvents) == 100 && state.LastScanBlock >= 1100
	}, 10*time.Second, 200*time.Millisecond, "Scanner should find all 100 events and update DB")

	h.stop()

	assert.Len(t, h.collectedEvents, 100, "Should have collected all 100 events from the gap")
	assert.Empty(t, h.collectedErrors, "Should be no errors during scan")

	stats := h.scanner.GetStats()
	assert.True(t, stats.Converged, "Scanners should have converged")

	finalState, err := h.mockDB.GetChainState(11155420)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, finalState.LastScanBlock, uint64(1100), "Scanner should have scanned up to the latest block")
}

// TestEnhancedScanner_GapDetectionAndFill tests the scanner's ability to find and fill a gap
// of missed events after its initial sync is complete.
func TestEnhancedScanner_GapDetectionAndFill(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Setup: Simulate a full scan, but manually create a gap by not marking some events as processed.
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.HeadTrackerInterval = config.Duration(100 * time.Millisecond)
	h.scanner.config.ScanInterval = config.Duration(200 * time.Millisecond)
	if h.scanner.config.GapDetectionInterval > 0 {
		h.scanner.config.GapDetectionInterval = 0 // Disable gap detection loop for manual trigger
	}

	var initialScanWg sync.WaitGroup
	initialScanWg.Add(20) // Expect 20 events for the initial scan

	// Action 1: Override the default consumer with one that intentionally creates a processing gap.
	h.consumer = func(event *bridgeTypes.EventData) {
		// Create a gap: don't mark events from blocks 1005-1010 as processed
		if event.BlockNumber >= 1005 && event.BlockNumber <= 1010 {
			// Skip marking these events as processed to create a gap
		} else {
			h.mockDB.MarkEventProcessed(event)
		}
		h.mockDB.mu.Lock()
		h.collectedEvents = append(h.collectedEvents, event)
		h.mockDB.mu.Unlock()
		// Only call Done() for initial scan events (not gap fill events)
		if !event.IsGapFill {
			initialScanWg.Done()
		}
	}

	// Start the scanner, then simulate the blocks appearing
	h.start(ctx)
	time.Sleep(200 * time.Millisecond)   // Allow workers to start
	h.simulator.SimulateNewBlocks(20, 1) // Blocks 1001-1020, 20 events

	// Wait for the consumer to process all 20 events from the initial scan.
	initialScanWg.Wait()

	// Now that the consumer has finished, we can safely assert the DB state.
	assert.Len(t, h.mockDB.processedEvents, 14, "Should have marked 14 events as processed")

	// Action 2: Manually trigger gap detection to find the 6 unprocessed events.
	h.collectedEvents = make([]*bridgeTypes.EventData, 0) // Clear collected events
	h.scanner.mu.Lock()
	h.scanner.converged = true // Force scanner into converged state for gap detection
	h.scanner.backwardScanning = false
	// Set forwardBlock to ensure gap detection has a range to check
	h.scanner.forwardBlock = 1020 // Should cover the range where we have gaps
	h.scanner.mu.Unlock()

	// Debug: Check what events are actually marked as processed
	t.Logf("DEBUG: Events marked as processed: %d", len(h.mockDB.processedEvents))
	for hash, event := range h.mockDB.processedEvents {
		t.Logf("DEBUG: Processed event at block %d, hash %s", event.BlockNumber, hash)
	}

	err := h.scanner.detectAndFillGaps(ctx)
	assert.NoError(t, err)

	// Let the event channel process any new events from the gap fill
	time.Sleep(200 * time.Millisecond)
	h.stop()

	// Assertions
	assert.Len(t, h.collectedEvents, 6, "Gap detection should have found the 6 missing events")
	for _, event := range h.collectedEvents {
		assert.GreaterOrEqual(t, event.BlockNumber, uint64(1005))
		assert.LessOrEqual(t, event.BlockNumber, uint64(1010))
		assert.True(t, event.IsGapFill, "Events found during gap fill should be marked as such")
	}
}

// TestEnhancedScanner_WebSocketReconnection tests the scanner's ability to handle WebSocket subscription errors
func TestEnhancedScanner_WebSocketReconnection(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.HeadTrackerInterval = config.Duration(100 * time.Millisecond)

	// Mock WebSocket subscription that fails
	mockSub := NewMockSubscription()
	h.mockClient.On("SubscribeFilterLogs", mock.Anything, mock.Anything, mock.Anything).Return(mockSub, nil)

	// Start scanner
	h.start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Simulate WebSocket error
	mockSub.SendError(fmt.Errorf("websocket connection lost"))

	// Scanner should continue working despite WebSocket issues
	h.simulator.SimulateNewBlocks(5, 1)

	assert.Eventually(t, func() bool {
		h.mockDB.mu.RLock()
		defer h.mockDB.mu.RUnlock()
		return len(h.collectedEvents) >= 5
	}, 5*time.Second, 100*time.Millisecond, "Scanner should continue working after WebSocket error")

	h.stop()
}

// TestEnhancedScanner_RPCTimeouts tests the scanner's behavior when RPC calls timeout
func TestEnhancedScanner_RPCTimeouts(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Setup with longer RPC delays to simulate timeouts
	h.simulator.rpcDelay = 500 * time.Millisecond
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.ScanInterval = config.Duration(1 * time.Second)

	// Add some RPC errors to simulate intermittent failures
	h.simulator.SetRPCError(1005, fmt.Errorf("RPC timeout"))
	h.simulator.SetRPCError(1008, fmt.Errorf("RPC connection refused"))

	h.start(ctx)
	h.simulator.SimulateNewBlocks(10, 1)

	// Scanner should eventually process events despite some RPC failures
	assert.Eventually(t, func() bool {
		h.mockDB.mu.RLock()
		defer h.mockDB.mu.RUnlock()
		return len(h.collectedEvents) >= 8 // Should get most events despite 2 RPC errors
	}, 12*time.Second, 500*time.Millisecond, "Scanner should handle RPC timeouts gracefully")

	h.stop()

	// Should have some errors logged but not complete failure
	assert.True(t, len(h.collectedErrors) > 0, "Should have logged RPC errors")
	assert.True(t, len(h.collectedEvents) > 0, "Should have processed some events despite errors")
}

// TestEnhancedScanner_DatabaseConstraintViolation tests the scanner's behavior when database constraints are violated
func TestEnhancedScanner_DatabaseConstraintViolation(t *testing.T) {
	h := setupScannerTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup
	h.mockDB.InitializeChainState(11155420, "test-chain", 1000)
	h.scanner.config.HeadTrackerInterval = config.Duration(100 * time.Millisecond)

	// Override the consumer to simulate database constraint violations
	h.consumer = func(event *bridgeTypes.EventData) {
		// Simulate constraint violation for every 3rd event
		if event.BlockNumber%3 == 0 {
			// Don't mark as processed to simulate DB constraint failure
		} else {
			h.mockDB.MarkEventProcessed(event)
		}
		h.mockDB.mu.Lock()
		h.collectedEvents = append(h.collectedEvents, event)
		h.mockDB.mu.Unlock()
	}

	h.start(ctx)
	time.Sleep(200 * time.Millisecond)
	h.simulator.SimulateNewBlocks(9, 1) // Blocks 1001-1009

	// Wait for processing
	assert.Eventually(t, func() bool {
		h.mockDB.mu.RLock()
		defer h.mockDB.mu.RUnlock()
		return len(h.collectedEvents) == 9
	}, 5*time.Second, 100*time.Millisecond, "Should collect all events")

	h.stop()

	// Verify that only some events were marked as processed due to constraint violations
	h.mockDB.mu.RLock()
	processedCount := len(h.mockDB.processedEvents)
	h.mockDB.mu.RUnlock()

	assert.Equal(t, 9, len(h.collectedEvents), "Should have collected all 9 events")
	assert.Equal(t, 6, processedCount, "Should have marked 6 events as processed (blocks 1001,1002,1004,1005,1007,1008)")
}
