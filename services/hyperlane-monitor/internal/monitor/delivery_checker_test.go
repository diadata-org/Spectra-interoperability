package monitor

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/pkg/types"
)

// mockBridgeClient is a mock implementation of BridgeClientInterface for testing
type mockBridgeClient struct {
	triggerFailoverCalled bool
	lastRequest          *types.FailoverRequest
	returnError          error
}

func (m *mockBridgeClient) CheckHealth(ctx context.Context) error {
	return nil
}

func (m *mockBridgeClient) TriggerFailover(ctx context.Context, req *types.FailoverRequest) (*types.FailoverResponse, error) {
	m.triggerFailoverCalled = true
	m.lastRequest = req
	if m.returnError != nil {
		return nil, m.returnError
	}
	return &types.FailoverResponse{
		RequestID: "test-request-123",
		Status:    "accepted",
		Timestamp: time.Now(),
	}, nil
}


func TestDeliveryChecker_ExtractIntentFromJSONB(t *testing.T) {
	tests := []struct {
		name              string
		message           database.HyperlaneMessage
		expectedIntent    *types.OracleIntent
		expectFallback    bool
	}{
		{
			name: "valid intent in JSONB with json.Number fields",
			message: database.HyperlaneMessage{
				MessageID:          "msg-1",
				IntentHash:         "0x123",
				SourceChainID:      11155420,
				DestinationChainID: 1,
				Symbol:             "BTC/USD",
				Price:              "50000000000000000000000",
				Timestamp:          1734567890,
				IntentData: map[string]interface{}{
					"intent": map[string]interface{}{
						"intentType": "PriceUpdate",
						"version":    "1.0",
						"chainId":    json.Number("11155420"),
						"nonce":      json.Number("12345"),
						"expiry":     json.Number("1234567890"),
						"symbol":     "BTC/USD",
						"price":      json.Number("50000000000000000000000"),
						"timestamp":  json.Number("1734567890"),
						"source":     "diadata",
						"signature":  "0x1234567890abcdef",
						"signer":     "0x742d35Cc6634C0532925a3b844Bc9e7595f62A40",
					},
				},
			},
			expectedIntent: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(11155420),
				Nonce:      big.NewInt(12345),
				Expiry:     big.NewInt(1234567890),
				Symbol:     "BTC/USD",
				Price:      mustParseBigInt("50000000000000000000000"),
				Timestamp:  big.NewInt(1734567890),
				Source:     "diadata",
				Signature:  common.FromHex("0x1234567890abcdef"),
				Signer:     common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f62A40"),
			},
			expectFallback: false,
		},
		{
			name: "intent with very large price value",
			message: database.HyperlaneMessage{
				MessageID:          "msg-2",
				IntentHash:         "0x456",
				SourceChainID:      1,
				DestinationChainID: 11155420,
				Symbol:             "ETH/USD",
				Price:              "999999999999999999999999999999999999999999",
				Timestamp:          1734567890,
				IntentData: map[string]interface{}{
					"intent": map[string]interface{}{
						"intentType": "PriceUpdate",
						"version":    "1.0",
						"chainId":    json.Number("1"),
						"symbol":     "ETH/USD",
						"price":      json.Number("999999999999999999999999999999999999999999"),
						"timestamp":  json.Number("1734567890"),
						"source":     "diadata",
					},
				},
			},
			expectedIntent: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(1),
				Symbol:     "ETH/USD",
				Price:      mustParseBigInt("999999999999999999999999999999999999999999"),
				Timestamp:  big.NewInt(1734567890),
				Source:     "diadata",
			},
			expectFallback: false,
		},
		{
			name: "no intent key in JSONB - should use fallback",
			message: database.HyperlaneMessage{
				MessageID:          "msg-3",
				IntentHash:         "0x789",
				SourceChainID:      11155420,
				DestinationChainID: 1,
				Symbol:             "LINK/USD",
				Price:              "15000000000000000000",
				Timestamp:          1734567890,
				IntentData: map[string]interface{}{
					"other_data": "some_value",
				},
			},
			expectedIntent: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(11155420),
				Symbol:     "LINK/USD",
				Price:      mustParseBigInt("15000000000000000000"),
				Timestamp:  big.NewInt(1734567890),
				Nonce:      big.NewInt(0),
				Expiry:     big.NewInt(0),
				Source:     "hyperlane-failover",
				Signature:  []byte{},
				Signer:     common.Address{},
			},
			expectFallback: true,
		},
		{
			name: "nil intent data - should use fallback",
			message: database.HyperlaneMessage{
				MessageID:          "msg-4",
				IntentHash:         "0xabc",
				SourceChainID:      1,
				DestinationChainID: 11155420,
				Symbol:             "UNI/USD",
				Price:              "5000000000000000000",
				Timestamp:          1734567890,
				IntentData:         nil,
			},
			expectedIntent: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(1),
				Symbol:     "UNI/USD",
				Price:      mustParseBigInt("5000000000000000000"),
				Timestamp:  big.NewInt(1734567890),
				Nonce:      big.NewInt(0),
				Expiry:     big.NewInt(0),
				Source:     "hyperlane-failover",
				Signature:  []byte{},
				Signer:     common.Address{},
			},
			expectFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a delivery checker with mock dependencies
			mockBridge := &mockBridgeClient{}
			checker := &DeliveryChecker{
				bridgeClient:  mockBridge,
				pairReceivers: make(map[string]map[string]*types.ReceiverConfig),
				metrics:       nil, // Not needed for this test
			}

			// Create a copy of the message
			msg := tt.message
			msg.CreatedAt = time.Now().Add(-1 * time.Hour)
			msg.ReceiverAddress = "0x1234567890123456789012345678901234567890"
			msg.PairID = "test-pair"

			// Add receiver config
			checker.AddPairReceivers("test-pair", []types.ReceiverConfig{
				{
					Address:         msg.ReceiverAddress,
					Name:            "Test Receiver",
					MaxDeliveryWait: 30 * time.Minute,
				},
			})

			// Trigger failover (which includes intent extraction)
			err := checker.triggerFailover(context.Background(), &msg, &types.ReceiverConfig{
				MaxDeliveryWait: 30 * time.Minute,
			})

			require.NoError(t, err)
			assert.True(t, mockBridge.triggerFailoverCalled)
			
			// Check the extracted intent
			require.NotNil(t, mockBridge.lastRequest)
			require.NotNil(t, mockBridge.lastRequest.IntentData)

			actualIntent := mockBridge.lastRequest.IntentData
			
			// Compare basic fields
			assert.Equal(t, tt.expectedIntent.IntentType, actualIntent.IntentType)
			assert.Equal(t, tt.expectedIntent.Version, actualIntent.Version)
			assert.Equal(t, tt.expectedIntent.Symbol, actualIntent.Symbol)
			assert.Equal(t, tt.expectedIntent.Source, actualIntent.Source)

			// Compare big.Int fields
			assertBigIntEqual(t, tt.expectedIntent.ChainID, actualIntent.ChainID, "ChainID")
			assertBigIntEqual(t, tt.expectedIntent.Nonce, actualIntent.Nonce, "Nonce")
			assertBigIntEqual(t, tt.expectedIntent.Expiry, actualIntent.Expiry, "Expiry")
			assertBigIntEqual(t, tt.expectedIntent.Price, actualIntent.Price, "Price")
			assertBigIntEqual(t, tt.expectedIntent.Timestamp, actualIntent.Timestamp, "Timestamp")

			// Compare signature and signer
			assert.Equal(t, tt.expectedIntent.Signature, actualIntent.Signature)
			assert.Equal(t, tt.expectedIntent.Signer, actualIntent.Signer)
		})
	}
}

func TestDeliveryChecker_FailoverRequestValidation(t *testing.T) {
	// Test that the failover request is properly constructed
	mockBridge := &mockBridgeClient{}
	checker := &DeliveryChecker{
		bridgeClient:  mockBridge,
		pairReceivers: make(map[string]map[string]*types.ReceiverConfig),
		metrics:       nil, // Not needed for this test
	}

	msg := database.HyperlaneMessage{
		MessageID:          "msg-123",
		IntentHash:         "0xdeadbeef",
		PairID:             "11155420_1_0xOracleTrigger",
		SourceChainID:      11155420,
		DestinationChainID: 1,
		ReceiverAddress:    "0x742d35Cc6634C0532925a3b844Bc9e7595f62A40",
		Symbol:             "BTC/USD",
		Price:              "50000000000000000000000",
		Timestamp:          1734567890,
		CreatedAt:          time.Now().Add(-2 * time.Hour),
		IntentData: map[string]interface{}{
			"intent": map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"chainId":    json.Number("11155420"),
				"symbol":     "BTC/USD",
				"price":      json.Number("50000000000000000000000"),
				"timestamp":  json.Number("1734567890"),
				"source":     "diadata",
			},
		},
	}

	// Add receiver config
	checker.AddPairReceivers(msg.PairID, []types.ReceiverConfig{
		{
			Address:         msg.ReceiverAddress,
			Name:            "Test Receiver",
			MaxDeliveryWait: 30 * time.Minute,
		},
	})

	// Trigger failover
	err := checker.triggerFailover(context.Background(), &msg, &types.ReceiverConfig{
		MaxDeliveryWait: 30 * time.Minute,
	})

	require.NoError(t, err)
	assert.True(t, mockBridge.triggerFailoverCalled)

	// Validate the request
	req := mockBridge.lastRequest
	assert.Equal(t, msg.MessageID, req.MessageID)
	assert.Equal(t, msg.IntentHash, req.IntentHash)
	assert.Equal(t, msg.PairID, req.PairID)
	assert.Equal(t, msg.SourceChainID, req.SourceChainID)
	assert.Equal(t, msg.DestinationChainID, req.DestinationChainID)
	assert.Equal(t, msg.ReceiverAddress, req.ReceiverAddress)
	assert.Contains(t, req.Reason, "Hyperlane delivery timeout")
	
	// Validate intent data
	require.NotNil(t, req.IntentData)
	assert.Equal(t, "PriceUpdate", req.IntentData.IntentType)
	assert.Equal(t, "BTC/USD", req.IntentData.Symbol)
	assert.Equal(t, "50000000000000000000000", req.IntentData.Price.String())
}