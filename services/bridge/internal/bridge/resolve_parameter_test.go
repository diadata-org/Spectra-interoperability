package bridge

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

func TestBridge_resolveParameterValue(t *testing.T) {
	// Create a bridge instance for testing
	bridge := &Bridge{}

	t.Run("EnrichmentValues", func(t *testing.T) {
		// Create test data with enrichment
		updateReq := &bridgetypes.UpdateRequest{
			ExtractedData: &config.ExtractedData{
				Enrichment: map[string]interface{}{
					"fullIntent": &bridgetypes.OracleIntent{
						Symbol:    "ETH",
						Price:     big.NewInt(2000),
						Timestamp: big.NewInt(1234567890),
						Signer:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
					},
					"randomInts": []int{42, 24, 99, 1337},
					"stringValue": "test_string",
					"numberValue": 42,
					"boolValue":   true,
				},
			},
		}

		testCases := []struct {
			name         string
			source       string
			expectedType string
			expectedOk   bool
		}{
			{
				name:         "FullIntent",
				source:       "${enrichment.fullIntent}",
				expectedType: "*types.OracleIntent",
				expectedOk:   true,
			},
			{
				name:         "RandomInts",
				source:       "${enrichment.randomInts}",
				expectedType: "[]int",
				expectedOk:   true,
			},
			{
				name:         "StringValue",
				source:       "${enrichment.stringValue}",
				expectedType: "string",
				expectedOk:   true,
			},
			{
				name:         "NumberValue",
				source:       "${enrichment.numberValue}",
				expectedType: "int",
				expectedOk:   true,
			},
			{
				name:         "BoolValue",
				source:       "${enrichment.boolValue}",
				expectedType: "bool",
				expectedOk:   true,
			},
			{
				name:       "NonExistentKey",
				source:     "${enrichment.nonExistent}",
				expectedOk: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				value, err := bridge.resolveParameterValue(tc.source, updateReq)

				if tc.expectedOk {
					assert.NoError(t, err)
					assert.NotNil(t, value)

					// Verify specific values for known cases
					switch tc.name {
					case "FullIntent":
						intent, ok := value.(*bridgetypes.OracleIntent)
						assert.True(t, ok)
						assert.Equal(t, "ETH", intent.Symbol)
						assert.Equal(t, big.NewInt(2000), intent.Price)

					case "RandomInts":
						ints, ok := value.([]int)
						assert.True(t, ok)
						assert.Equal(t, []int{42, 24, 99, 1337}, ints)

					case "StringValue":
						str, ok := value.(string)
						assert.True(t, ok)
						assert.Equal(t, "test_string", str)

					case "NumberValue":
						num, ok := value.(int)
						assert.True(t, ok)
						assert.Equal(t, 42, num)

					case "BoolValue":
						b, ok := value.(bool)
						assert.True(t, ok)
						assert.Equal(t, true, b)
					}
				} else {
					assert.Error(t, err)
					assert.Contains(t, err.Error(), "enrichment key")
				}
			})
		}
	})

	t.Run("NoEnrichmentData", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{
			ExtractedData: nil,
		}

		value, err := bridge.resolveParameterValue("${enrichment.fullIntent}", updateReq)
		assert.Error(t, err)
		assert.Nil(t, value)
		assert.Contains(t, err.Error(), "enrichment data not available")
	})

	t.Run("EmptyEnrichmentData", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{
			ExtractedData: &config.ExtractedData{
				Enrichment: nil,
			},
		}

		value, err := bridge.resolveParameterValue("${enrichment.fullIntent}", updateReq)
		assert.Error(t, err)
		assert.Nil(t, value)
		assert.Contains(t, err.Error(), "enrichment data not available")
	})

	t.Run("EventValues", func(t *testing.T) {
		// Create test data with event
		requestId := big.NewInt(123456)
		updateReq := &bridgetypes.UpdateRequest{
			Event: &bridgetypes.EventData{
				EventName: "IntArraySet",
				RequestId: requestId,
			},
		}

		testCases := []struct {
			name       string
			source     string
			expectedOk bool
		}{
			{
				name:       "EventRequestId",
				source:     "${event.requestId}",
				expectedOk: true,
			},
			{
				name:       "UnsupportedEventField",
				source:     "${event.unsupportedField}",
				expectedOk: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				value, err := bridge.resolveParameterValue(tc.source, updateReq)

				if tc.expectedOk {
					assert.NoError(t, err)
					if tc.name == "EventRequestId" {
						assert.Equal(t, requestId, value)
					}
				} else {
					assert.Error(t, err)
				}
			})
		}
	})

	t.Run("IntentValues", func(t *testing.T) {
		// Create test data with intent
		intent := &bridgetypes.OracleIntent{
			Symbol:    "BTC",
			Price:     big.NewInt(50000),
			Timestamp: big.NewInt(9876543210),
			Signer:    common.HexToAddress("0x9876543210987654321098765432109876543210"),
		}

		updateReq := &bridgetypes.UpdateRequest{
			Intent: intent,
		}

		// Test intent parameter resolution
		value, err := bridge.resolveParameterValue("${intent.full}", updateReq)
		assert.NoError(t, err)
		assert.Equal(t, intent, value)
	})

	t.Run("LiteralValues", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{}

		testCases := []struct {
			name     string
			source   string
			expected interface{}
		}{
			{
				name:     "StringLiteral",
				source:   "literal_string",
				expected: "literal_string",
			},
			{
				name:     "NumberStringLiteral",
				source:   "42",
				expected: "42",
			},
			{
				name:     "EmptyString",
				source:   "",
				expected: "",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				value, err := bridge.resolveParameterValue(tc.source, updateReq)
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, value)
			})
		}
	})

	t.Run("InvalidTemplateVariables", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{}

		testCases := []struct {
			name          string
			source        string
			expectsLiteral bool
		}{
			{
				name:   "UnsupportedPrefix",
				source: "${unknown.field}",
			},
			{
				name:           "MalformedTemplate",
				source:         "${enrichment.field",
				expectsLiteral: true, // Missing closing brace, treated as literal
			},
			{
				name:   "EmptyTemplate",
				source: "${}",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				value, err := bridge.resolveParameterValue(tc.source, updateReq)
				
				if tc.expectsLiteral {
					// Malformed templates are treated as literal values
					assert.NoError(t, err)
					assert.Equal(t, tc.source, value)
				} else {
					assert.Error(t, err)
					assert.Nil(t, value)
				}
			})
		}
	})
}

func TestBridge_buildMethodParams(t *testing.T) {
	bridge := &Bridge{}

	t.Run("MultipleParams", func(t *testing.T) {
		// Create comprehensive test data
		intent := &bridgetypes.OracleIntent{
			Symbol:    "ETH",
			Price:     big.NewInt(2000),
			Timestamp: big.NewInt(1234567890),
			Signer:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		}

		updateReq := &bridgetypes.UpdateRequest{
			Intent: intent,
			ExtractedData: &config.ExtractedData{
				Enrichment: map[string]interface{}{
					"fullIntent": intent,
					"randomInts": []int{1, 2, 3, 4, 5},
				},
			},
		}

		// Test method config with multiple parameters
		methodConfig := &config.DestinationMethodConfig{
			Name: "testMethod",
			Params: map[string]string{
				"intent":     "${enrichment.fullIntent}",
				"randomData": "${enrichment.randomInts}",
				"literal":    "test_value",
			},
		}

		params, err := bridge.buildMethodParams(methodConfig, updateReq)
		assert.NoError(t, err)
		assert.Len(t, params, 3)

		// Verify parameter values (order might vary due to map iteration)
		paramValues := make(map[string]interface{})
		for i, param := range params {
			switch i {
			case 0:
				// First param could be any of the three
				if intentParam, ok := param.(*bridgetypes.OracleIntent); ok {
					paramValues["intent"] = intentParam
				} else if randomParam, ok := param.([]int); ok {
					paramValues["randomData"] = randomParam
				} else if literalParam, ok := param.(string); ok {
					paramValues["literal"] = literalParam
				}
			case 1:
				// Second param
				if intentParam, ok := param.(*bridgetypes.OracleIntent); ok {
					paramValues["intent"] = intentParam
				} else if randomParam, ok := param.([]int); ok {
					paramValues["randomData"] = randomParam
				} else if literalParam, ok := param.(string); ok {
					paramValues["literal"] = literalParam
				}
			case 2:
				// Third param
				if intentParam, ok := param.(*bridgetypes.OracleIntent); ok {
					paramValues["intent"] = intentParam
				} else if randomParam, ok := param.([]int); ok {
					paramValues["randomData"] = randomParam
				} else if literalParam, ok := param.(string); ok {
					paramValues["literal"] = literalParam
				}
			}
		}

		// Verify all expected parameters are present
		assert.Contains(t, paramValues, "intent")
		assert.Contains(t, paramValues, "randomData")
		assert.Contains(t, paramValues, "literal")

		// Verify parameter values
		assert.Equal(t, intent, paramValues["intent"])
		assert.Equal(t, []int{1, 2, 3, 4, 5}, paramValues["randomData"])
		assert.Equal(t, "test_value", paramValues["literal"])
	})

	t.Run("EmptyParams", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{}
		methodConfig := &config.DestinationMethodConfig{
			Name:   "emptyMethod",
			Params: map[string]string{},
		}

		params, err := bridge.buildMethodParams(methodConfig, updateReq)
		assert.NoError(t, err)
		assert.Len(t, params, 0)
	})

	t.Run("InvalidParam", func(t *testing.T) {
		updateReq := &bridgetypes.UpdateRequest{}
		methodConfig := &config.DestinationMethodConfig{
			Name: "invalidMethod",
			Params: map[string]string{
				"invalid": "${enrichment.nonExistent}",
			},
		}

		params, err := bridge.buildMethodParams(methodConfig, updateReq)
		assert.Error(t, err)
		assert.Nil(t, params)
		assert.Contains(t, err.Error(), "failed to resolve parameter invalid")
	})
}

// TestEnrichmentDataTypes tests various data types in enrichment
func TestEnrichmentDataTypes(t *testing.T) {
	bridge := &Bridge{}

	// Create a complex intent for testing
	complexIntent := &bridgetypes.OracleIntent{
		Symbol:     "COMPLEX",
		Price:      big.NewInt(999999),
		Timestamp:  big.NewInt(1699999999),
		Nonce:      big.NewInt(12345),
		Expiry:     big.NewInt(1799999999),
		Signer:     common.HexToAddress("0xComplexAddress123456789012345678901234567890"),
		Signature:  []byte("complex_signature_data"),
		IntentType: "oracle",
		Version:    "v2",
		Source:     "complex_source",
	}

	enrichmentData := map[string]interface{}{
		// Different data types
		"intent":      complexIntent,
		"strings":     []string{"a", "b", "c"},
		"numbers":     []int{10, 20, 30},
		"bigNumbers":  []*big.Int{big.NewInt(100), big.NewInt(200)},
		"addresses":   []common.Address{common.HexToAddress("0x1111"), common.HexToAddress("0x2222")},
		"nested": map[string]interface{}{
			"level2": map[string]interface{}{
				"value": "deep_value",
			},
		},
		"mixed": []interface{}{"string", 42, true},
	}

	updateReq := &bridgetypes.UpdateRequest{
		ExtractedData: &config.ExtractedData{
			Enrichment: enrichmentData,
		},
	}

	testCases := []struct {
		name         string
		source       string
		expectedType string
	}{
		{"ComplexIntent", "${enrichment.intent}", "*types.OracleIntent"},
		{"StringArray", "${enrichment.strings}", "[]string"},
		{"NumberArray", "${enrichment.numbers}", "[]int"},
		{"BigIntArray", "${enrichment.bigNumbers}", "[]*big.Int"},
		{"AddressArray", "${enrichment.addresses}", "[]common.Address"},
		{"NestedMap", "${enrichment.nested}", "map[string]interface {}"},
		{"MixedArray", "${enrichment.mixed}", "[]interface {}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := bridge.resolveParameterValue(tc.source, updateReq)
			assert.NoError(t, err)
			assert.NotNil(t, value)

			// Verify specific complex intent case
			if tc.name == "ComplexIntent" {
				resolvedIntent, ok := value.(*bridgetypes.OracleIntent)
				assert.True(t, ok)
				assert.Equal(t, complexIntent.Symbol, resolvedIntent.Symbol)
				assert.Equal(t, complexIntent.Price, resolvedIntent.Price)
				assert.Equal(t, complexIntent.Signer, resolvedIntent.Signer)
			}
		})
	}
}