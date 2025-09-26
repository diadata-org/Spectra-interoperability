package scanner

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/mock"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// MockEthClient is a mock Ethereum client for testing
type MockEthClient struct {
	mock.Mock
}

func (m *MockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockEthClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, query)
	return args.Get(0).([]types.Log), args.Error(1)
}

func (m *MockEthClient) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	args := m.Called(ctx, query, ch)
	return args.Get(0).(ethereum.Subscription), args.Error(1)
}

func (m *MockEthClient) Close() {
	m.Called()
}

// MockDatabase is a mock database for testing
type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) InitializeChainState(chainID int64, name string, startBlock uint64) error {
	args := m.Called(chainID, name, startBlock)
	return args.Error(0)
}

func (m *MockDatabase) GetChainState(chainID int64) (*database.ChainState, error) {
	args := m.Called(chainID)
	return args.Get(0).(*database.ChainState), args.Error(1)
}

func (m *MockDatabase) UpdateLastScanBlock(chainID int64, blockNumber uint64) error {
	args := m.Called(chainID, blockNumber)
	return args.Error(0)
}

func (m *MockDatabase) IsEventProcessed(intentHash string) (bool, error) {
	args := m.Called(intentHash)
	return args.Bool(0), args.Error(1)
}

func (m *MockDatabase) GetProcessedEventsByBlockRange(startBlock, endBlock uint64) ([]*bridgeTypes.EventData, error) {
	args := m.Called(startBlock, endBlock)
	return args.Get(0).([]*bridgeTypes.EventData), args.Error(1)
}

func (m *MockDatabase) SaveProcessedEvent(event *bridgeTypes.EventData) error {
	args := m.Called(event)
	return args.Error(0)
}

// MockSubscription is a mock Ethereum subscription for testing
type MockSubscription struct {
	mock.Mock
	errChan chan error
}

func NewMockSubscription() *MockSubscription {
	return &MockSubscription{
		errChan: make(chan error, 1),
	}
}

func (m *MockSubscription) Unsubscribe() {
	m.Called()
}

func (m *MockSubscription) Err() <-chan error {
	m.Called()
	return m.errChan
}

func (m *MockSubscription) SendError(err error) {
	m.errChan <- err
}

// Test data generators

// CreateTestConfig creates a test configuration for scanners
func CreateTestConfig() (*config.BlockScannerConfig, *config.SourceConfig, map[string]*config.EventDefinition) {
	scannerConfig := &config.BlockScannerConfig{
		Enabled:       true,
		ScanInterval:  config.Duration(5 * time.Second),
		BlockRange:    100,
		MaxBlockGap:   1000,
		BackwardSync:  false,
	}

	sourceConfig := &config.SourceConfig{
		ChainID:    11155420, // Optimism Sepolia
		Name:       "optimism-sepolia",
		StartBlock: 1000,
	}

	eventDefinitions := map[string]*config.EventDefinition{
		"IntentRegistered": {
			Contract: "0x1234567890abcdef1234567890abcdef12345678",
			ABI:      `{"name":"IntentRegistered","type":"event","inputs":[{"name":"intentHash","type":"bytes32","indexed":true},{"name":"symbol","type":"string","indexed":true},{"name":"price","type":"uint256","indexed":false},{"name":"timestamp","type":"uint256","indexed":false},{"name":"signer","type":"address","indexed":false}]}`,
		},
		"IntArraySet": {
			Contract: "0xabcdef1234567890abcdef1234567890abcdef12",
			ABI:      `{"name":"IntArraySet","type":"event","inputs":[{"name":"requestId","type":"uint256","indexed":false},{"name":"round","type":"int256","indexed":true},{"name":"seed","type":"string","indexed":false},{"name":"signature","type":"string","indexed":false}]}`,
		},
	}

	return scannerConfig, sourceConfig, eventDefinitions
}

// CreateTestLog creates a test Ethereum log for testing
func CreateTestLog(eventName string, blockNumber uint64, logIndex uint) types.Log {
	var topics []common.Hash
	var data []byte

	switch eventName {
	case "IntentRegistered":
		// Event signature: IntentRegistered(bytes32,string,uint256,uint256,address)
		topics = []common.Hash{
			common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"), // intentHash
			common.HexToHash("0x" + common.Bytes2Hex([]byte("BTC"))),                                    // symbol hash
		}
		// Non-indexed data: price (32 bytes) + timestamp (32 bytes) + signer (32 bytes, padded)
		price := new(big.Int)
		price.SetString("50000000000000000000000", 10) // 50000 * 10^18
		timestamp := big.NewInt(time.Now().Unix())
		signer := common.HexToAddress("0x742d35cc6641c31b0c23b8e53d8cf3d21b1e4b7b")

		data = append(data, common.LeftPadBytes(price.Bytes(), 32)...)
		data = append(data, common.LeftPadBytes(timestamp.Bytes(), 32)...)
		data = append(data, common.LeftPadBytes(signer.Bytes(), 32)...)

	case "IntArraySet":
		// Event signature: IntArraySet(uint256,int256,string,string)
		topics = []common.Hash{
			common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
			common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"), // round = 1
		}
		// Non-indexed data: requestId + dynamic string data
		requestId := big.NewInt(12345)
		data = append(data, common.LeftPadBytes(requestId.Bytes(), 32)...)
		// Add dynamic string data (simplified for testing)
		data = append(data, make([]byte, 64)...) // placeholder for dynamic data

	default:
		// Generic event
		topics = []common.Hash{
			common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		}
		data = make([]byte, 32)
	}

	return types.Log{
		Address:     common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		Topics:      topics,
		Data:        data,
		BlockNumber: blockNumber,
		TxHash:      common.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"),
		TxIndex:     0,
		BlockHash:   common.HexToHash("0xeeffaabbeeffaabbeeffaabbeeffaabbeeffaabbeeffaabbeeffaabbeeffaabb"),
		Index:       logIndex,
		Removed:     false,
	}
}

// CreateTestEventData creates test event data
func CreateTestEventData(eventName string, blockNumber uint64) *bridgeTypes.EventData {
	intentHash := [32]byte{}
	copy(intentHash[:], common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890").Bytes())

	price := new(big.Int)
	price.SetString("50000000000000000000000", 10) // 50000 * 10^18

	event := &bridgeTypes.EventData{
		EventName:       eventName,
		ContractAddress: common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		BlockNumber:     blockNumber,
		TxHash:          common.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"),
		LogIndex:        0,
		IntentHash:      intentHash,
		Symbol:          "BTC",
		Price:           price,
		Timestamp:       big.NewInt(time.Now().Unix()),
		Signer:          common.HexToAddress("0x742d35cc6641c31b0c23b8e53d8cf3d21b1e4b7b"),
		Priority:        1,
	}

	if eventName == "IntArraySet" {
		event.RequestId = big.NewInt(12345)
		event.Round = big.NewInt(1)
		event.Seed = "test-seed"
		event.Signature = "test-signature"
	}

	return event
}

// TestChannels holds channels for testing
type TestChannels struct {
	EventChan chan *bridgeTypes.EventData
	ErrorChan chan error
}

// NewTestChannels creates test channels with buffers
func NewTestChannels() *TestChannels {
	return &TestChannels{
		EventChan: make(chan *bridgeTypes.EventData, 100),
		ErrorChan: make(chan error, 100),
	}
}

// DrainChannels drains all channels to prevent blocking
func (tc *TestChannels) DrainChannels() {
	for {
		select {
		case <-tc.EventChan:
		case <-tc.ErrorChan:
		default:
			return
		}
	}
}

// MockChainState creates a mock chain state for testing
func MockChainState(chainID int64, lastScanBlock uint64) *database.ChainState {
	return &database.ChainState{
		ChainID:       chainID,
		ChainName:     "test-chain",
		LastScanBlock: lastScanBlock,
		UpdatedAt:     time.Now(),
	}
}

// WaitForEvents waits for a specific number of events with timeout
func WaitForEvents(eventChan <-chan *bridgeTypes.EventData, expectedCount int, timeout time.Duration) ([]*bridgeTypes.EventData, error) {
	var events []*bridgeTypes.EventData
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for len(events) < expectedCount {
		select {
		case event := <-eventChan:
			events = append(events, event)
		case <-timeoutTimer.C:
			return events, context.DeadlineExceeded
		}
	}

	return events, nil
}

// WaitForErrors waits for a specific number of errors with timeout
func WaitForErrors(errorChan <-chan error, expectedCount int, timeout time.Duration) ([]error, error) {
	var errors []error
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for len(errors) < expectedCount {
		select {
		case err := <-errorChan:
			errors = append(errors, err)
		case <-timeoutTimer.C:
			return errors, context.DeadlineExceeded
		}
	}

	return errors, nil
}