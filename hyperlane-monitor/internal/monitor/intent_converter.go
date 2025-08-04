package monitor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// ConvertJSONToOracleIntent converts JSON data to OracleIntent struct
func ConvertJSONToOracleIntent(data interface{}) (*types.OracleIntent, error) {
	// Convert to map if needed
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected map[string]interface{}, got %T", data)
	}

	// Create intent with string fields - handle both camelCase and PascalCase
	intent := &types.OracleIntent{
		IntentType: getStringFieldCaseInsensitive(dataMap, "intentType"),
		Version:    getStringFieldCaseInsensitive(dataMap, "version"),
		Symbol:     getStringFieldCaseInsensitive(dataMap, "symbol"),
		Source:     getStringFieldCaseInsensitive(dataMap, "source"),
	}

	// Convert numeric fields - handle both camelCase and PascalCase
	if v := getFieldCaseInsensitive(dataMap, "chainId"); v != nil {
		intent.ChainID = toBigInt(v)
	}

	if v := getFieldCaseInsensitive(dataMap, "nonce"); v != nil {
		intent.Nonce = toBigInt(v)
	}

	if v := getFieldCaseInsensitive(dataMap, "expiry"); v != nil {
		intent.Expiry = toBigInt(v)
	}

	if v := getFieldCaseInsensitive(dataMap, "price"); v != nil {
		intent.Price = toBigInt(v)
	}

	if v := getFieldCaseInsensitive(dataMap, "timestamp"); v != nil {
		intent.Timestamp = toBigInt(v)
	}

	// Convert signature (handle both hex and base64)
	if sigRaw := getFieldCaseInsensitive(dataMap, "signature"); sigRaw != nil {
		if sig := fmt.Sprintf("%v", sigRaw); sig != "" && sig != "0x" {
			// Try hex first
			if common.IsHexAddress(sig) || (len(sig) > 2 && sig[:2] == "0x") {
				intent.Signature = common.FromHex(sig)
			} else {
				// Try base64
				decoded, err := base64.StdEncoding.DecodeString(sig)
				if err == nil {
					intent.Signature = decoded
				}
			}
		}
	}

	// Convert signer
	if signer := getStringFieldCaseInsensitive(dataMap, "signer"); common.IsHexAddress(signer) {
		intent.Signer = common.HexToAddress(signer)
	}

	return intent, nil
}

// getStringField safely gets a string field from a map
func getStringField(m map[string]interface{}, key string) string {
	if v, exists := m[key]; exists {
		if str, ok := v.(string); ok {
			return str
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// getFieldCaseInsensitive gets a field from map with case-insensitive key matching
func getFieldCaseInsensitive(m map[string]interface{}, key string) interface{} {
	// Try exact match first
	if v, exists := m[key]; exists {
		return v
	}
	
	// Try case-insensitive match
	keyLower := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == keyLower {
			return v
		}
	}
	
	return nil
}

// getStringFieldCaseInsensitive gets a string field with case-insensitive key matching
func getStringFieldCaseInsensitive(m map[string]interface{}, key string) string {
	v := getFieldCaseInsensitive(m, key)
	if v == nil {
		return ""
	}
	
	if str, ok := v.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", v)
}

// toBigInt converts various types to *big.Int
func toBigInt(value interface{}) *big.Int {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		// Try to parse as integer
		if bigInt, ok := new(big.Int).SetString(v, 10); ok {
			return bigInt
		}
		return nil

	case json.Number:
		str := string(v)
		if str == "" {
			return nil
		}
		// Always use string parsing to handle arbitrarily large numbers
		if bigInt, ok := new(big.Int).SetString(str, 10); ok {
			return bigInt
		}
		return nil

	case float64:
		// For float64, we need to be careful with large numbers
		// If the number is too large for int64, it might have lost precision
		// Try to convert to string first to see if it's in scientific notation
		str := fmt.Sprintf("%.0f", v)
		if bigInt, ok := new(big.Int).SetString(str, 10); ok {
			return bigInt
		}
		// Fallback to int64 conversion
		return big.NewInt(int64(v))

	case int:
		return big.NewInt(int64(v))

	case int64:
		return big.NewInt(v)

	case *big.Int:
		return v

	default:
		// Try to convert to string and parse
		str := fmt.Sprintf("%v", v)
		if str == "" || str == "<nil>" {
			return nil
		}
		if bigInt, ok := new(big.Int).SetString(str, 10); ok {
			return bigInt
		}
		return nil
	}
}