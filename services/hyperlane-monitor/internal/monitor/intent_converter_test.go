package monitor

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/pkg/types"
)

func TestConvertJSONToOracleIntent(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected *types.OracleIntent
		wantErr  bool
	}{
		{
			name: "valid intent with all fields",
			input: map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"chainId":    "11155420",
				"nonce":      "12345",
				"expiry":     "1234567890",
				"symbol":     "BTC/USD",
				"price":      "50000000000000000000000",
				"timestamp":  "1234567890",
				"source":     "diadata",
				"signature":  "0x1234567890abcdef",
				"signer":     "0x742d35Cc6634C0532925a3b844Bc9e7595f62A40",
			},
			expected: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(11155420),
				Nonce:      big.NewInt(12345),
				Expiry:     big.NewInt(1234567890),
				Symbol:     "BTC/USD",
				Price:      mustParseBigInt("50000000000000000000000"),
				Timestamp:  big.NewInt(1234567890),
				Source:     "diadata",
				Signature:  common.FromHex("0x1234567890abcdef"),
				Signer:     common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f62A40"),
			},
			wantErr: false,
		},
		{
			name: "intent with json.Number fields (from database JSONB)",
			input: map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"chainId":    json.Number("11155420"),
				"nonce":      json.Number("12345"),
				"expiry":     json.Number("1234567890"),
				"symbol":     "ETH/USD",
				"price":      json.Number("2000000000000000000000"),
				"timestamp":  json.Number("1234567890"),
				"source":     "diadata",
				"signature":  "0xabcdef",
				"signer":     "0x742d35Cc6634C0532925a3b844Bc9e7595f62A40",
			},
			expected: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(11155420),
				Nonce:      big.NewInt(12345),
				Expiry:     big.NewInt(1234567890),
				Symbol:     "ETH/USD",
				Price:      mustParseBigInt("2000000000000000000000"),
				Timestamp:  big.NewInt(1234567890),
				Source:     "diadata",
				Signature:  common.FromHex("0xabcdef"),
				Signer:     common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f62A40"),
			},
			wantErr: false,
		},
		{
			name: "intent with missing optional fields",
			input: map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"symbol":     "BTC/USD",
				"source":     "diadata",
			},
			expected: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				Symbol:     "BTC/USD",
				Source:     "diadata",
			},
			wantErr: false,
		},
		{
			name: "intent with empty strings for numeric fields",
			input: map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"chainId":    "",
				"nonce":      "",
				"expiry":     "",
				"symbol":     "BTC/USD",
				"price":      "",
				"timestamp":  "",
				"source":     "diadata",
				"signature":  "",
				"signer":     "",
			},
			expected: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				Symbol:     "BTC/USD",
				Source:     "diadata",
			},
			wantErr: false,
		},
		{
			name: "intent with very large price value",
			input: map[string]interface{}{
				"intentType": "PriceUpdate",
				"version":    "1.0",
				"chainId":    "1",
				"symbol":     "BTC/USD",
				"price":      "999999999999999999999999999999999999999999",
				"timestamp":  "1234567890",
				"source":     "diadata",
			},
			expected: &types.OracleIntent{
				IntentType: "PriceUpdate",
				Version:    "1.0",
				ChainID:    big.NewInt(1),
				Symbol:     "BTC/USD",
				Price:      mustParseBigInt("999999999999999999999999999999999999999999"),
				Timestamp:  big.NewInt(1234567890),
				Source:     "diadata",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertJSONToOracleIntent(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.IntentType, result.IntentType)
			assert.Equal(t, tt.expected.Version, result.Version)
			assert.Equal(t, tt.expected.Symbol, result.Symbol)
			assert.Equal(t, tt.expected.Source, result.Source)

			// Compare big.Int values
			assertBigIntEqual(t, tt.expected.ChainID, result.ChainID, "ChainID")
			assertBigIntEqual(t, tt.expected.Nonce, result.Nonce, "Nonce")
			assertBigIntEqual(t, tt.expected.Expiry, result.Expiry, "Expiry")
			assertBigIntEqual(t, tt.expected.Price, result.Price, "Price")
			assertBigIntEqual(t, tt.expected.Timestamp, result.Timestamp, "Timestamp")

			// Compare bytes
			assert.Equal(t, tt.expected.Signature, result.Signature, "Signature")

			// Compare addresses
			assert.Equal(t, tt.expected.Signer, result.Signer, "Signer")
		})
	}
}

func TestConvertJSONToOracleIntent_DatabaseJSONB(t *testing.T) {
	// This test simulates the exact scenario where data comes from database JSONB
	// The database stores the intent inside an "intent" key
	dbData := map[string]interface{}{
		"intent": map[string]interface{}{
			"intentType": "PriceUpdate",
			"version":    "1.0",
			"chainId":    json.Number("11155420"),
			"nonce":      json.Number("0"),
			"expiry":     json.Number("0"),
			"symbol":     "BTC/USD",
			"price":      json.Number("50000000000000000000000"),
			"timestamp":  json.Number("1734567890"),
			"source":     "diadata",
			"signature":  "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12",
			"signer":     "0x742d35Cc6634C0532925a3b844Bc9e7595f62A40",
		},
	}

	// Extract the intent data
	intentRaw, exists := dbData["intent"]
	require.True(t, exists, "intent key should exist")

	// Convert to OracleIntent
	result, err := ConvertJSONToOracleIntent(intentRaw)
	require.NoError(t, err)

	// Verify all fields are properly converted
	assert.Equal(t, "PriceUpdate", result.IntentType)
	assert.Equal(t, "1.0", result.Version)
	assert.Equal(t, "BTC/USD", result.Symbol)
	assert.Equal(t, "diadata", result.Source)

	// Verify big.Int fields are not nil
	require.NotNil(t, result.ChainID, "ChainID should not be nil")
	require.NotNil(t, result.Nonce, "Nonce should not be nil")
	require.NotNil(t, result.Expiry, "Expiry should not be nil")
	require.NotNil(t, result.Price, "Price should not be nil")
	require.NotNil(t, result.Timestamp, "Timestamp should not be nil")

	// Verify values
	assert.Equal(t, int64(11155420), result.ChainID.Int64())
	assert.Equal(t, int64(0), result.Nonce.Int64())
	assert.Equal(t, int64(0), result.Expiry.Int64())
	assert.Equal(t, "50000000000000000000000", result.Price.String())
	assert.Equal(t, int64(1734567890), result.Timestamp.Int64())

	// Verify signature
	assert.Equal(t, 65, len(result.Signature)) // 130 hex chars / 2 = 65 bytes

	// Verify signer
	assert.Equal(t, common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f62A40"), result.Signer)
}

func TestConvertJSONToOracleIntent_NilHandling(t *testing.T) {
	// Test with nil/null values
	input := map[string]interface{}{
		"intentType": "PriceUpdate",
		"version":    "1.0",
		"chainId":    nil,
		"nonce":      nil,
		"expiry":     nil,
		"symbol":     "BTC/USD",
		"price":      nil,
		"timestamp":  nil,
		"source":     "diadata",
		"signature":  nil,
		"signer":     nil,
	}

	result, err := ConvertJSONToOracleIntent(input)
	require.NoError(t, err)

	// Basic fields should be set
	assert.Equal(t, "PriceUpdate", result.IntentType)
	assert.Equal(t, "1.0", result.Version)
	assert.Equal(t, "BTC/USD", result.Symbol)
	assert.Equal(t, "diadata", result.Source)

	// Numeric fields should be nil
	assert.Nil(t, result.ChainID)
	assert.Nil(t, result.Nonce)
	assert.Nil(t, result.Expiry)
	assert.Nil(t, result.Price)
	assert.Nil(t, result.Timestamp)

	// Other fields should be zero values
	assert.Nil(t, result.Signature)
	assert.Equal(t, common.Address{}, result.Signer)
}

// Helper functions
func mustParseBigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("failed to parse big int: " + s)
	}
	return n
}

func assertBigIntEqual(t *testing.T, expected, actual *big.Int, fieldName string) {
	if expected == nil && actual == nil {
		return
	}
	if expected == nil || actual == nil {
		t.Errorf("%s mismatch: expected %v, got %v", fieldName, expected, actual)
		return
	}
	if expected.Cmp(actual) != 0 {
		t.Errorf("%s mismatch: expected %s, got %s", fieldName, expected.String(), actual.String())
	}
}