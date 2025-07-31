package api

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailoverRequestJSONMarshaling(t *testing.T) {
	// Test that JSON marshaling/unmarshaling works correctly
	// Create price as big.Int
	price := new(big.Int)
	price.SetString("50000000000000000000000", 10) // 50000 * 10^18
	
	intent := &bridgetypes.OracleIntent{
		IntentType: "PriceUpdate",
		Version:    "1.0",
		Symbol:     "BTC/USD",
		Price:      price,
		Timestamp:  big.NewInt(1234567890),
		ChainID:    big.NewInt(1),
		Nonce:      big.NewInt(0),
		Expiry:     big.NewInt(0),
		Source:     "hyperlane-failover",
		Signature:  []byte{0x01, 0x02, 0x03},
		Signer:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}

	req := FailoverRequest{
		MessageID:          "test-message-id",
		IntentHash:         "0xabcdef",
		PairID:             "test-pair",
		SourceChainID:      1,
		DestinationChainID: 11155420,
		ReceiverAddress:    "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65",
		IntentData:         intent,
		Reason:             "test reason",
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(req)
	require.NoError(t, err)

	t.Logf("Marshaled JSON: %s", string(jsonBytes))

	// Unmarshal back
	var decoded FailoverRequest
	err = json.Unmarshal(jsonBytes, &decoded)
	require.NoError(t, err)

	// Verify fields
	assert.Equal(t, req.MessageID, decoded.MessageID)
	assert.Equal(t, req.IntentHash, decoded.IntentHash)
	assert.Equal(t, req.PairID, decoded.PairID)
	assert.Equal(t, req.SourceChainID, decoded.SourceChainID)
	assert.Equal(t, req.DestinationChainID, decoded.DestinationChainID)
	assert.Equal(t, req.ReceiverAddress, decoded.ReceiverAddress)
	assert.Equal(t, req.Reason, decoded.Reason)

	// Verify intent data
	require.NotNil(t, decoded.IntentData)
	assert.Equal(t, intent.IntentType, decoded.IntentData.IntentType)
	assert.Equal(t, intent.Version, decoded.IntentData.Version)
	assert.Equal(t, intent.Symbol, decoded.IntentData.Symbol)
	assert.Equal(t, 0, intent.Price.Cmp(decoded.IntentData.Price))
	assert.Equal(t, 0, intent.Timestamp.Cmp(decoded.IntentData.Timestamp))
	assert.Equal(t, 0, intent.ChainID.Cmp(decoded.IntentData.ChainID))
	assert.Equal(t, intent.Source, decoded.IntentData.Source)
	assert.Equal(t, intent.Signature, decoded.IntentData.Signature)
	assert.Equal(t, intent.Signer, decoded.IntentData.Signer)
}

func TestFailoverHandlerJSONParsing(t *testing.T) {
	// Create a mock handler
	cfg := &config.Config{
		PrivateKey: "0x1234567890123456789012345678901234567890123456789012345678901234",
		Destinations: map[int64]*config.DestinationConfig{
			11155420: {
				ChainID: 11155420,
				Enabled: true,
				Contracts: []config.ContractConfig{
					{
						Type:    "pushoracle",
						Address: "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65",
						Enabled: true,
					},
				},
			},
		},
	}

	db := &database.DB{} // Mock DB
	handler, err := NewFailoverHandler(cfg, db)
	require.NoError(t, err)

	// Create request JSON that mimics what hyperlane-monitor sends
	jsonRequest := `{
		"message_id": "test-message-id",
		"intent_hash": "0xabcdef",
		"pair_id": "test-pair",
		"source_chain_id": 1,
		"destination_chain_id": 11155420,
		"receiver_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65",
		"intent_data": {
			"intentType": "PriceUpdate",
			"version": "1.0",
			"symbol": "BTC/USD",
			"price": "50000000000000000000000",
			"timestamp": "1234567890",
			"chainId": "1",
			"nonce": "0",
			"expiry": "0",
			"source": "hyperlane-failover",
			"signature": "0x010203",
			"signer": "0x1234567890123456789012345678901234567890"
		},
		"reason": "test reason"
	}`

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/api/v1/failover/trigger", bytes.NewBufferString(jsonRequest))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call handler (it will fail due to missing client, but we can check if JSON parsing worked)
	handler.TriggerFailover(rr, req)

	// The request should fail with "No client available" not "Intent data is required"
	// This would indicate that the JSON was parsed correctly
	body := rr.Body.String()
	t.Logf("Response: %s", body)
	
	// Should not fail due to missing intent data
	assert.NotContains(t, body, "Intent data is required")
	// Should fail due to missing client (which comes after JSON parsing)
	assert.Contains(t, body, "No client available")
}

func TestHyperlaneMonitorJSONFormat(t *testing.T) {
	// Test the exact JSON format that hyperlane-monitor sends
	hyperlaneJSON := `{
		"message_id": "0x1234",
		"intent_hash": "0x5678",
		"pair_id": "lasernet-opsepolia",
		"source_chain_id": 1,
		"destination_chain_id": 11155420,
		"receiver_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65",
		"intent_data": {
			"intentType": "PriceUpdate",
			"version": "1.0",
			"chainId": 1,
			"nonce": 0,
			"expiry": 0,
			"symbol": "ETH/USD",
			"price": 3500000000000000000000,
			"timestamp": 1234567890,
			"source": "hyperlane-failover",
			"signature": "",
			"signer": "0x0000000000000000000000000000000000000000"
		},
		"reason": "Hyperlane delivery timeout after 10s"
	}`

	var req FailoverRequest
	err := json.Unmarshal([]byte(hyperlaneJSON), &req)
	require.NoError(t, err)

	// Verify all fields parsed correctly
	assert.Equal(t, "0x1234", req.MessageID)
	assert.Equal(t, "0x5678", req.IntentHash)
	assert.Equal(t, "lasernet-opsepolia", req.PairID)
	assert.Equal(t, 1, req.SourceChainID)
	assert.Equal(t, 11155420, req.DestinationChainID)
	assert.Equal(t, "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65", req.ReceiverAddress)
	
	// Verify intent data
	require.NotNil(t, req.IntentData)
	assert.Equal(t, "PriceUpdate", req.IntentData.IntentType)
	assert.Equal(t, "1.0", req.IntentData.Version)
	assert.Equal(t, "ETH/USD", req.IntentData.Symbol)
	assert.Equal(t, "3500000000000000000000", req.IntentData.Price.String())
	assert.Equal(t, "1234567890", req.IntentData.Timestamp.String())
	assert.Equal(t, "1", req.IntentData.ChainID.String())
	assert.Equal(t, "hyperlane-failover", req.IntentData.Source)
}