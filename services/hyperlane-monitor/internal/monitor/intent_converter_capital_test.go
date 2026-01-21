package monitor

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertJSONToOracleIntent_CapitalizedFields(t *testing.T) {
	// Test with the exact data structure from the database
	dbData := map[string]interface{}{
		"IntentType": "OracleUpdate",
		"Version":    "1.0",
		"ChainId":    json.Number("100640"),
		"Nonce":      json.Number("1754238283565961711"),
		"Expiry":     json.Number("1754241883"),
		"Symbol":     "BTC/USD",
		"Price":      json.Number("11383460505068"),
		"Timestamp":  json.Number("1754238283"),
		"Source":     "DIA Oracle",
		"Signature":  "+TYZJZ25rA0MW6kQ05Sv8T9OU4SsS3TC5GmN8soJmbNWf5DMvVkJhU9t3mkLHIwmLAvGw/Zoap7fYd9a7mTKDxs=",
		"Signer":     "0x914baf368d65d4ed5bf8b174eb72cd3912281b9d",
	}

	result, err := ConvertJSONToOracleIntent(dbData)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, "OracleUpdate", result.IntentType)
	assert.Equal(t, "1.0", result.Version)
	assert.Equal(t, "BTC/USD", result.Symbol)
	assert.Equal(t, "DIA Oracle", result.Source)

	// Verify numeric fields
	assert.Equal(t, int64(100640), result.ChainID.Int64())
	assert.Equal(t, "1754238283565961711", result.Nonce.String())
	assert.Equal(t, int64(1754241883), result.Expiry.Int64())
	assert.Equal(t, "11383460505068", result.Price.String())
	assert.Equal(t, int64(1754238283), result.Timestamp.Int64())

	// Verify signature was decoded from base64
	assert.NotNil(t, result.Signature)
	assert.Greater(t, len(result.Signature), 0)

	// Verify signer
	assert.Equal(t, common.HexToAddress("0x914baf368d65d4ed5bf8b174eb72cd3912281b9d"), result.Signer)
}

func TestConvertJSONToOracleIntent_MixedCase(t *testing.T) {
	// Test with mixed case field names
	input := map[string]interface{}{
		"intentType": "PriceUpdate",  // lowercase
		"Version":    "1.0",           // capitalized
		"CHAINID":    json.Number("1"), // uppercase
		"symbol":     "ETH/USD",       // lowercase
		"Price":      json.Number("2000000000000000000000"),
		"TIMESTAMP":  json.Number("1234567890"),
		"source":     "test",
	}

	result, err := ConvertJSONToOracleIntent(input)
	require.NoError(t, err)

	assert.Equal(t, "PriceUpdate", result.IntentType)
	assert.Equal(t, "1.0", result.Version)
	assert.Equal(t, "ETH/USD", result.Symbol)
	assert.Equal(t, "test", result.Source)
	assert.Equal(t, int64(1), result.ChainID.Int64())
	assert.Equal(t, "2000000000000000000000", result.Price.String())
	assert.Equal(t, int64(1234567890), result.Timestamp.Int64())
}