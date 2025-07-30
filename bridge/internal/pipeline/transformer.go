package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	
	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// DataTransformer applies transformations to extracted data
type DataTransformer struct {
}

// NewDataTransformer creates a new data transformer
func NewDataTransformer() *DataTransformer {
	return &DataTransformer{}
}

// ApplyTransformations applies all configured transformations
func (dt *DataTransformer) ApplyTransformations(data *config.ExtractedData, transformations []config.Transformation) error {
	if len(transformations) == 0 {
		return nil
	}
	
	if data.Processed == nil {
		data.Processed = make(map[string]interface{})
	}
	
	for _, transform := range transformations {
		inputValue, err := dt.resolveValue(transform.Input, data)
		if err != nil {
			return fmt.Errorf("failed to resolve input for transformation %s: %w", transform.Field, err)
		}
		
		result, err := dt.transform(transform.Operation, inputValue, transform.Params)
		if err != nil {
			return fmt.Errorf("transformation %s failed: %w", transform.Field, err)
		}
		
		data.Processed[transform.Field] = result
	}
	
	return nil
}

// resolveValue resolves a value from data using template syntax
func (dt *DataTransformer) resolveValue(template string, data *config.ExtractedData) (interface{}, error) {
	if !strings.HasPrefix(template, "${") || !strings.HasSuffix(template, "}") {
		return template, nil
	}
	
	path := template[2 : len(template)-1]
	
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid template path: %s", path)
	}
	
	var source map[string]interface{}
	switch parts[0] {
	case "event":
		source = data.Event
	case "enrichment":
		if data.Enrichment == nil {
			return nil, fmt.Errorf("no enrichment data available")
		}
		source = data.Enrichment
	case "processed":
		if data.Processed == nil {
			return nil, fmt.Errorf("no processed data available")
		}
		source = data.Processed
	default:
		return nil, fmt.Errorf("unknown template source: %s", parts[0])
	}
	
	return dt.navigatePath(source, parts[1:])
}

// navigatePath navigates through a nested structure
func (dt *DataTransformer) navigatePath(data interface{}, path []string) (interface{}, error) {
	current := data
	
	for _, part := range path {
		switch v := current.(type) {
		case map[string]interface{}:
			var exists bool
			current, exists = v[part]
			if !exists {
				return nil, fmt.Errorf("field not found: %s", part)
			}
		case []interface{}:
			var idx int
			if _, err := fmt.Sscanf(part, "[%d]", &idx); err == nil {
				if idx >= len(v) {
					return nil, fmt.Errorf("array index out of bounds: %d", idx)
				}
				current = v[idx]
			} else {
				return nil, fmt.Errorf("invalid array access: %s", part)
			}
		default:
			return nil, fmt.Errorf("cannot navigate through %T at %s", v, part)
		}
	}
	
	return current, nil
}

// transform applies a transformation operation
func (dt *DataTransformer) transform(operation string, input interface{}, params map[string]interface{}) (interface{}, error) {
	switch operation {
	case "slice":
		return dt.transformSlice(input, params)
	case "concat":
		return dt.transformConcat(input, params)
	case "hash":
		return dt.transformHash(input, params)
	case "encode":
		return dt.transformEncode(input, params)
	case "toBigInt":
		return dt.transformToBigInt(input)
	case "toAddress":
		return dt.transformToAddress(input)
	case "toHex":
		return dt.transformToHex(input)
	case "toString":
		return dt.transformToString(input)
	default:
		return nil, fmt.Errorf("unsupported transformation: %s", operation)
	}
}

// transformSlice slices an array
func (dt *DataTransformer) transformSlice(input interface{}, params map[string]interface{}) (interface{}, error) {
	slice := reflect.ValueOf(input)
	if slice.Kind() != reflect.Slice {
		return nil, fmt.Errorf("slice operation requires array input, got %T", input)
	}
	
	start := 0
	if s, ok := params["start"].(float64); ok {
		start = int(s)
	}
	
	length := slice.Len()
	if l, ok := params["length"].(float64); ok {
		length = int(l)
	}
	
	if start < 0 || start >= slice.Len() {
		return nil, fmt.Errorf("slice start index out of bounds: %d", start)
	}
	
	end := start + length
	if end > slice.Len() {
		end = slice.Len()
	}
	
	result := reflect.MakeSlice(slice.Type(), end-start, end-start)
	for i := start; i < end; i++ {
		result.Index(i - start).Set(slice.Index(i))
	}
	
	return result.Interface(), nil
}

// transformConcat concatenates values
func (dt *DataTransformer) transformConcat(input interface{}, params map[string]interface{}) (interface{}, error) {
	separator := ""
	if sep, ok := params["separator"].(string); ok {
		separator = sep
	}
	
	var values []string
	
	values = append(values, fmt.Sprintf("%v", input))
	
	if additional, ok := params["values"].([]interface{}); ok {
		for _, v := range additional {
			values = append(values, fmt.Sprintf("%v", v))
		}
	}
	
	return strings.Join(values, separator), nil
}

// transformHash hashes a value
func (dt *DataTransformer) transformHash(input interface{}, params map[string]interface{}) (interface{}, error) {
	hashType := "keccak256"
	if ht, ok := params["type"].(string); ok {
		hashType = ht
	}
	
	var data []byte
	switch v := input.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	case common.Hash:
		data = v[:]
	case common.Address:
		data = v[:]
	default:
		data = []byte(fmt.Sprintf("%v", v))
	}
	
	switch hashType {
	case "keccak256":
		hash := crypto.Keccak256Hash(data)
		return hash, nil
	case "sha256":
		hash := sha256.Sum256(data)
		return common.BytesToHash(hash[:]), nil
	default:
		return nil, fmt.Errorf("unsupported hash type: %s", hashType)
	}
}

// transformEncode encodes data
func (dt *DataTransformer) transformEncode(input interface{}, params map[string]interface{}) (interface{}, error) {
	encodeType := "abi"
	if et, ok := params["type"].(string); ok {
		encodeType = et
	}
	
	switch encodeType {
	case "abi":
		return dt.encodeABI(input, params)
	case "hex":
		return dt.encodeHex(input)
	case "packed":
		return dt.encodePacked(input, params)
	default:
		return nil, fmt.Errorf("unsupported encoding type: %s", encodeType)
	}
}

// encodeABI performs ABI encoding
func (dt *DataTransformer) encodeABI(input interface{}, params map[string]interface{}) (interface{}, error) {
	types, ok := params["types"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("ABI encoding requires types parameter")
	}
	
	var typeStrings []string
	for _, t := range types {
		typeStrings = append(typeStrings, fmt.Sprintf("%v", t))
	}
	
	arguments := make(abi.Arguments, len(typeStrings))
	for i, typeStr := range typeStrings {
		typ, err := abi.NewType(typeStr, "", nil)
		if err != nil {
			return nil, fmt.Errorf("invalid ABI type %s: %w", typeStr, err)
		}
		arguments[i] = abi.Argument{Type: typ}
	}
	
	var values []interface{}
	switch v := input.(type) {
	case []interface{}:
		values = v
	default:
		values = []interface{}{v}
	}
	
	encoded, err := arguments.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("ABI encoding failed: %w", err)
	}
	
	return encoded, nil
}

// encodeHex encodes to hex string
func (dt *DataTransformer) encodeHex(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case []byte:
		return "0x" + hex.EncodeToString(v), nil
	case string:
		return "0x" + hex.EncodeToString([]byte(v)), nil
	case common.Hash:
		return v.Hex(), nil
	case common.Address:
		return v.Hex(), nil
	case *big.Int:
		return fmt.Sprintf("0x%x", v), nil
	default:
		return nil, fmt.Errorf("cannot hex encode %T", v)
	}
}

// encodePacked performs packed encoding (non-standard ABI encoding)
func (dt *DataTransformer) encodePacked(input interface{}, params map[string]interface{}) (interface{}, error) {
	var result []byte
	
	values, ok := input.([]interface{})
	if !ok {
		values = []interface{}{input}
	}
	
	for _, v := range values {
		switch val := v.(type) {
		case common.Address:
			result = append(result, val.Bytes()...)
		case *big.Int:
			result = append(result, common.LeftPadBytes(val.Bytes(), 32)...)
		case string:
			result = append(result, []byte(val)...)
		case []byte:
			result = append(result, val...)
		default:
			return nil, fmt.Errorf("unsupported type for packed encoding: %T", v)
		}
	}
	
	return result, nil
}

// transformToBigInt converts value to big.Int
func (dt *DataTransformer) transformToBigInt(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case *big.Int:
		return v, nil
	case string:
		n := new(big.Int)
		if strings.HasPrefix(v, "0x") {
			n.SetString(v[2:], 16)
		} else {
			n.SetString(v, 10)
		}
		return n, nil
	case float64:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to big.Int", v)
	}
}

// transformToAddress converts value to address
func (dt *DataTransformer) transformToAddress(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case common.Address:
		return v, nil
	case string:
		if !common.IsHexAddress(v) {
			return nil, fmt.Errorf("invalid address: %s", v)
		}
		return common.HexToAddress(v), nil
	case []byte:
		if len(v) != 20 {
			return nil, fmt.Errorf("invalid address length: %d", len(v))
		}
		return common.BytesToAddress(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to address", v)
	}
}

// transformToHex converts value to hex string
func (dt *DataTransformer) transformToHex(input interface{}) (interface{}, error) {
	return dt.encodeHex(input)
}

// transformToString converts value to string
func (dt *DataTransformer) transformToString(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case common.Hash:
		return v.Hex(), nil
	case common.Address:
		return v.Hex(), nil
	case *big.Int:
		return v.String(), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}