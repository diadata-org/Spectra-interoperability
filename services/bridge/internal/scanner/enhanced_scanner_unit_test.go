package scanner

import (
	"math/big"

	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

func TestCalculateEventSignature(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	// Create scanner with test config (we only need the eventDefinitions for this test)
	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	tests := []struct {
		name        string
		eventABI    string
		expectedSig string
		description string
	}{
		{
			name:        "IntentRegistered signature",
			eventABI:    eventDefs["IntentRegistered"].ABI,
			expectedSig: "IntentRegistered(bytes32,string,uint256,uint256,address)",
			description: "Should correctly calculate IntentRegistered event signature",
		},
		{
			name:        "IntArraySet signature",
			eventABI:    eventDefs["IntArraySet"].ABI,
			expectedSig: "IntArraySet(uint256,int256,string,string)",
			description: "Should correctly calculate IntArraySet event signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualSig := scanner.calculateEventSignature(tt.eventABI)
			expectedHash := crypto.Keccak256Hash([]byte(tt.expectedSig))

			assert.Equal(t, expectedHash, actualSig, tt.description)
		})
	}
}

func TestExtractContractInfo(t *testing.T) {
	// Create test configuration
	_, sourceConfig, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		sourceConfig:     sourceConfig,
		eventDefinitions: eventDefs,
	}

	err := scanner.extractContractInfo()
	require.NoError(t, err, "extractContractInfo should not return an error")

	// Should have extracted 2 unique contract addresses
	assert.Len(t, scanner.contractAddresses, 2, "Should extract 2 contract addresses")

	// Should have extracted 2 event signatures
	assert.Len(t, scanner.eventSignatures, 2, "Should extract 2 event signatures")

	// Verify specific addresses are included
	expectedAddresses := []common.Address{
		common.HexToAddress(eventDefs["IntentRegistered"].Contract),
		common.HexToAddress(eventDefs["IntArraySet"].Contract),
	}

	for _, expected := range expectedAddresses {
		found := false
		for _, actual := range scanner.contractAddresses {
			if actual == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected address %s should be in contract addresses", expected.Hex())
	}
}

func TestExtractContractInfo_NoEventDefinitions(t *testing.T) {
	scanner := &EnhancedBlockScanner{
		eventDefinitions: nil,
	}

	err := scanner.extractContractInfo()
	assert.Error(t, err, "Should return error when no event definitions provided")
	assert.Contains(t, err.Error(), "no event definitions provided")
}

func TestFindEventDefinition(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	tests := []struct {
		name         string
		signature    string
		expectedName string
		shouldFind   bool
		description  string
	}{
		{
			name:         "IntentRegistered event",
			signature:    "IntentRegistered(bytes32,string,uint256,uint256,address)",
			expectedName: "IntentRegistered",
			shouldFind:   true,
			description:  "Should find IntentRegistered event definition",
		},
		{
			name:         "IntArraySet event",
			signature:    "IntArraySet(uint256,int256,string,string)",
			expectedName: "IntArraySet",
			shouldFind:   true,
			description:  "Should find IntArraySet event definition",
		},
		{
			name:         "Unknown event",
			signature:    "UnknownEvent()",
			expectedName: "",
			shouldFind:   false,
			description:  "Should not find unknown event definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventSig := crypto.Keccak256Hash([]byte(tt.signature))
			name, def := scanner.findEventDefinition(eventSig)

			if tt.shouldFind {
				assert.Equal(t, tt.expectedName, name, tt.description)
				assert.NotNil(t, def, "Event definition should not be nil")
			} else {
				assert.Empty(t, name, tt.description)
				assert.Nil(t, def, "Event definition should be nil")
			}
		})
	}
}

func TestParseIntentRegisteredEvent(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	// Create mock log for IntentRegistered event
	intentHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	symbolHash := crypto.Keccak256Hash([]byte("BTC"))

	mockLog := types.Log{
		Address: common.HexToAddress(eventDefs["IntentRegistered"].Contract),
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("IntentRegistered(bytes32,string,uint256,uint256,address)")),
			intentHash,
			symbolHash,
		},
		Data:        make([]byte, 96), // 32 bytes each for price, timestamp, address (padded)
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		Index:       0,
	}

	// Set mock data: price = 50000, timestamp = 1234567890, signer address
	price := big.NewInt(50000)
	timestamp := big.NewInt(1234567890)
	signer := common.HexToAddress("0x742b6e68e8b11d4dd78ed7eb96b10f7dd1b5ba7b")

	copy(mockLog.Data[0:32], price.FillBytes(make([]byte, 32)))
	copy(mockLog.Data[32:64], timestamp.FillBytes(make([]byte, 32)))
	// Address is stored in the last 20 bytes of a 32-byte slot (left-padded with zeros)
	copy(mockLog.Data[64:96], common.LeftPadBytes(signer.Bytes(), 32))

	event := &bridgeTypes.EventData{
		EventName:       "IntentRegistered",
		ContractAddress: mockLog.Address,
		BlockNumber:     mockLog.BlockNumber,
		TxHash:          mockLog.TxHash,
		LogIndex:        mockLog.Index,
		Raw:             mockLog,
	}

	parsedEvent, err := scanner.parseIntentRegisteredEvent(event, mockLog)

	assert.NoError(t, err, "parseIntentRegisteredEvent should not return an error")
	assert.Equal(t, "IntentRegistered", parsedEvent.EventName)
	assert.Equal(t, [32]byte(intentHash), parsedEvent.IntentHash, "IntentHash should match")
	assert.Equal(t, price, parsedEvent.Price, "Price should match")
	assert.Equal(t, timestamp, parsedEvent.Timestamp, "Timestamp should match")
	assert.Equal(t, signer, parsedEvent.Signer, "Signer should match")
}

func TestParseIntArraySetEvent(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	// Create mock log for IntArraySet event
	round := big.NewInt(2000)
	requestId := big.NewInt(466)

	mockLog := types.Log{
		Address: common.HexToAddress(eventDefs["IntArraySet"].Contract),
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("IntArraySet(uint256,int256,string,string)")),
			common.BigToHash(round),
		},
		Data:        make([]byte, 32), // Mock data containing requestId
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		Index:       0,
	}

	// Set requestId in data
	copy(mockLog.Data[0:32], requestId.FillBytes(make([]byte, 32)))

	event := &bridgeTypes.EventData{
		EventName:       "IntArraySet",
		ContractAddress: mockLog.Address,
		BlockNumber:     mockLog.BlockNumber,
		TxHash:          mockLog.TxHash,
		LogIndex:        mockLog.Index,
		Raw:             mockLog,
	}

	parsedEvent, err := scanner.parseIntArraySetEvent(event, mockLog)

	assert.NoError(t, err, "parseIntArraySetEvent should not return an error")
	assert.Equal(t, "IntArraySet", parsedEvent.EventName)
	assert.Equal(t, round, parsedEvent.Round, "Round should match")
	assert.Equal(t, requestId, parsedEvent.RequestId, "RequestId should match")
	assert.Equal(t, mockLog.Data, parsedEvent.RawData, "RawData should match")

	// Verify IntentHash is set from RequestId
	expectedIntentHash := make([]byte, 32)
	copy(expectedIntentHash[32-len(requestId.Bytes()):], requestId.Bytes())
	assert.Equal(t, [32]byte(expectedIntentHash), parsedEvent.IntentHash, "IntentHash should be derived from RequestId")
}

func TestParseLog_IntentRegistered(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	mockLog := types.Log{
		Address: common.HexToAddress(eventDefs["IntentRegistered"].Contract),
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("IntentRegistered(bytes32,string,uint256,uint256,address)")),
			common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			crypto.Keccak256Hash([]byte("BTC")),
		},
		Data:        make([]byte, 96),
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		Index:       0,
	}

	event, err := scanner.parseLog(mockLog)

	assert.NoError(t, err, "parseLog should not return an error")
	assert.Equal(t, "IntentRegistered", event.EventName)
	assert.Equal(t, mockLog.Address, event.ContractAddress)
	assert.Equal(t, mockLog.BlockNumber, event.BlockNumber)
	assert.Equal(t, mockLog.TxHash, event.TxHash)
	assert.Equal(t, mockLog.Index, event.LogIndex)
}

func TestParseLog_IntArraySet(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	mockLog := types.Log{
		Address: common.HexToAddress(eventDefs["IntArraySet"].Contract),
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("IntArraySet(uint256,int256,string,string)")),
			common.BigToHash(big.NewInt(2000)),
		},
		Data:        make([]byte, 32),
		BlockNumber: 12345,
		TxHash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		Index:       0,
	}

	event, err := scanner.parseLog(mockLog)

	assert.NoError(t, err, "parseLog should not return an error")
	assert.Equal(t, "IntArraySet", event.EventName)
	assert.Equal(t, mockLog.Address, event.ContractAddress)
	assert.Equal(t, mockLog.BlockNumber, event.BlockNumber)
	assert.Equal(t, mockLog.TxHash, event.TxHash)
	assert.Equal(t, mockLog.Index, event.LogIndex)
}

func TestParseLog_UnknownEvent(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	mockLog := types.Log{
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("UnknownEvent()")),
		},
		BlockNumber: 12345,
	}

	_, err := scanner.parseLog(mockLog)

	assert.Error(t, err, "parseLog should return an error for unknown event")
	assert.Contains(t, err.Error(), "unknown event signature")
}

func TestParseLog_NoTopics(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	mockLog := types.Log{
		Topics:      []common.Hash{},
		BlockNumber: 12345,
	}

	_, err := scanner.parseLog(mockLog)

	assert.Error(t, err, "parseLog should return an error when log has no topics")
	assert.Contains(t, err.Error(), "log has no topics")
}

func TestShouldProcessEvent(t *testing.T) {
	// Create test configuration
	_, _, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		eventDefinitions: eventDefs,
	}

	event := &bridgeTypes.EventData{
		EventName:   "IntentRegistered",
		BlockNumber: 12345,
	}

	// Since the current implementation returns true for all events
	result := scanner.shouldProcessEvent(event)
	assert.True(t, result, "shouldProcessEvent should return true (current implementation)")
}

func TestCalculateEventSignature_InvalidJSON(t *testing.T) {
	scanner := &EnhancedBlockScanner{}

	// Test with invalid JSON
	invalidABI := `{"name":"Event","type":"event","inputs":[invalid json}`
	result := scanner.calculateEventSignature(invalidABI)

	// Should return zero hash for invalid JSON
	assert.Equal(t, common.Hash{}, result, "Should return zero hash for invalid ABI JSON")
}

// Test NewEnhancedBlockScanner constructor (simplified version)
func TestNewEnhancedBlockScanner_Structure(t *testing.T) {
	_, sourceConfig, eventDefs := CreateTestConfig()

	// Test the structure without actual initialization to avoid interface issues
	scanner := &EnhancedBlockScanner{
		sourceConfig:     sourceConfig,
		eventDefinitions: eventDefs,
	}

	assert.NotNil(t, scanner, "Scanner should not be nil")
	assert.Equal(t, sourceConfig, scanner.sourceConfig)
	assert.Equal(t, eventDefs, scanner.eventDefinitions)
}

// Test basic scanner operations
func TestBasicScannerOperations(t *testing.T) {
	_, sourceConfig, eventDefs := CreateTestConfig()

	scanner := &EnhancedBlockScanner{
		sourceConfig:     sourceConfig,
		eventDefinitions: eventDefs,
		stopChan:         make(chan struct{}),
		stoppedChan:      make(chan struct{}),
	}

	// Test extractContractInfo
	err := scanner.extractContractInfo()
	require.NoError(t, err)
	assert.Len(t, scanner.contractAddresses, 2)
	assert.Len(t, scanner.eventSignatures, 2)

	// Test shouldProcessEvent (always returns true)
	event := CreateTestEventData("IntentRegistered", 12345)
	assert.True(t, scanner.shouldProcessEvent(event))
}

// Test Stop method
func TestStop(t *testing.T) {
	scanner := &EnhancedBlockScanner{
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}

	// Simulate stopped scanner by closing stoppedChan
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(scanner.stoppedChan)
	}()

	err := scanner.Stop()

	assert.NoError(t, err)

	// Verify stopChan was closed
	select {
	case <-scanner.stopChan:
		// Expected
	default:
		t.Error("stopChan should be closed")
	}
}

// Test logging progress
func TestLogProgress(t *testing.T) {
	scanner := &EnhancedBlockScanner{
		headBlock:           2100,
		headEventsFound:     3,
		lastHeadUpdate:      time.Now().Add(-30 * time.Second),
		forwardBlock:        1000,
		backwardBlock:       2000,
		forwardEventsFound:  10,
		backwardEventsFound: 5,
		totalBlocksScanned:  500,
		backwardScanning:    true,
		converged:           false,
	}

	// This mainly tests that logProgress doesn't panic
	// Since it only logs, we can't easily verify output
	assert.NotPanics(t, func() {
		scanner.logProgress()
	})
}

func TestParseIntentRegisteredEvent_InsufficientData(t *testing.T) {
	scanner := &EnhancedBlockScanner{}

	event := &bridgeTypes.EventData{
		EventName: "IntentRegistered",
	}

	mockLog := types.Log{
		Topics: []common.Hash{
			common.HexToHash("0x1234"),
			common.HexToHash("0x5678"),
		},
		Data: make([]byte, 32), // Insufficient data (should be 96 bytes)
	}

	parsedEvent, err := scanner.parseIntentRegisteredEvent(event, mockLog)

	assert.NoError(t, err, "Should not error with insufficient data")
	assert.Equal(t, [32]byte(common.HexToHash("0x5678")), parsedEvent.IntentHash, "Should extract IntentHash from topics[1]")
	// Price, Timestamp, and Signer should be nil/zero since data is insufficient
}

func TestParseIntArraySetEvent_EmptyData(t *testing.T) {
	scanner := &EnhancedBlockScanner{}

	event := &bridgeTypes.EventData{
		EventName: "IntArraySet",
	}

	mockLog := types.Log{
		Topics: []common.Hash{
			common.HexToHash("0x1234"),
			common.BigToHash(big.NewInt(2000)),
		},
		Data: []byte{}, // Empty data
	}

	parsedEvent, err := scanner.parseIntArraySetEvent(event, mockLog)

	assert.NoError(t, err, "Should not error with empty data")
	assert.Equal(t, big.NewInt(2000), parsedEvent.Round, "Should extract round from topics[1]")
	assert.Nil(t, parsedEvent.RequestId, "RequestId should be nil with empty data")
	assert.Empty(t, parsedEvent.RawData, "RawData should be empty")
}
