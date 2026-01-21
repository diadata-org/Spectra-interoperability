package failover

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailoverRequestJSONMarshaling(t *testing.T) {
	// Create an intent like delivery_checker.go does
	price := new(big.Int)
	price.SetString("3500000000000000000000", 10) // 3500 * 10^18
	
	intent := &types.OracleIntent{
		IntentType: "PriceUpdate",
		Version:    "1.0",
		Symbol:     "ETH/USD",
		Price:      price,
		Timestamp:  big.NewInt(1234567890),
		ChainID:    big.NewInt(1),
		Nonce:      big.NewInt(0),
		Expiry:     big.NewInt(0),
		Source:     "hyperlane-failover",
		Signature:  []byte{},
		Signer:     common.Address{},
	}

	// Create failover request
	request := &types.FailoverRequest{
		MessageID:          "0x1234",
		IntentHash:         "0x5678",
		PairID:             "lasernet-opsepolia",
		SourceChainID:      1,
		DestinationChainID: 11155420,
		ReceiverAddress:    "0x742d35Cc6634C0532925a3b844Bc9e7595f8fA65",
		IntentData:         intent,
		Reason:             "Hyperlane delivery timeout after 10s",
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(request)
	require.NoError(t, err)

	t.Logf("Marshaled JSON from hyperlane-monitor: %s", string(jsonBytes))

	// Parse the JSON to verify structure
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err)

	// Check that intent_data exists
	intentDataRaw, exists := jsonMap["intent_data"]
	assert.True(t, exists, "intent_data should exist in JSON")
	
	// Check that it's not null
	assert.NotNil(t, intentDataRaw, "intent_data should not be null")

	// Check intent data fields
	intentData := intentDataRaw.(map[string]interface{})
	assert.Equal(t, "PriceUpdate", intentData["intentType"])
	assert.Equal(t, "1.0", intentData["version"])
	assert.Equal(t, "ETH/USD", intentData["symbol"])
	
	// Verify price and other big.Int fields are strings
	assert.Equal(t, "3500000000000000000000", intentData["price"])
	assert.Equal(t, "1234567890", intentData["timestamp"])
	assert.Equal(t, "1", intentData["chainId"])
}

func TestOracleIntentJSONTags(t *testing.T) {
	// Verify that the JSON tags use camelCase
	intent := types.OracleIntent{
		IntentType: "test",
		ChainID:    big.NewInt(1),
	}

	jsonBytes, err := json.Marshal(intent)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	t.Logf("OracleIntent JSON: %s", jsonStr)

	// Should use camelCase
	assert.Contains(t, jsonStr, `"intentType"`)
	assert.Contains(t, jsonStr, `"chainId"`)
	
	// Should NOT use snake_case
	assert.NotContains(t, jsonStr, `"intent_type"`)
	assert.NotContains(t, jsonStr, `"chain_id"`)
}