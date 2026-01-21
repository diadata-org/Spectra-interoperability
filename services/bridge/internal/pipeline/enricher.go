package pipeline

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// DataEnricher enriches event data with additional information from view calls
type DataEnricher struct {
	client    *ethclient.Client
	eventDefs map[string]*config.EventDefinition
	abiCache  map[string]abi.ABI
}

// NewDataEnricher creates a new data enricher
func NewDataEnricher(client *ethclient.Client, eventDefs map[string]*config.EventDefinition) (*DataEnricher, error) {
	return &DataEnricher{
		client:    client,
		eventDefs: eventDefs,
		abiCache:  make(map[string]abi.ABI),
	}, nil
}

// EnrichEventData enriches event data with view call results
func (de *DataEnricher) EnrichEventData(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	eventDef, exists := de.eventDefs[eventName]
	if !exists {
		return fmt.Errorf("event definition not found: %s", eventName)
	}

	if eventDef.Enrichment == nil {
		return nil
	}

	enrichment := eventDef.Enrichment

	contractAddr := enrichment.Contract
	if contractAddr == "" {
		if addr, ok := extractedData.Event["_contract"].(string); ok {
			contractAddr = addr
		} else {
			return fmt.Errorf("no contract address for enrichment")
		}
	}

	params, err := de.buildParameters(enrichment.Params, extractedData)
	if err != nil {
		return fmt.Errorf("failed to build enrichment parameters: %w", err)
	}

	result, err := de.callViewMethod(ctx, contractAddr, enrichment.Method, enrichment.ABI, params)
	if err != nil {
		return fmt.Errorf("enrichment call failed: %w", err)
	}

	enrichedData := make(map[string]interface{})
	if err := de.processReturnValues(result, enrichment.Returns, enrichedData); err != nil {
		return fmt.Errorf("failed to process return values: %w", err)
	}

	extractedData.Enrichment = enrichedData

	logger.Debugf("Enriched event %s with data: %v", eventName, enrichedData)

	return nil
}

// buildParameters builds parameters for a view call from template strings
func (de *DataEnricher) buildParameters(paramTemplates []string, data *config.ExtractedData) ([]interface{}, error) {
	params := make([]interface{}, len(paramTemplates))

	for i, template := range paramTemplates {
		value, err := de.resolveTemplate(template, data)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %d: %w", i, err)
		}
		params[i] = value
	}

	return params, nil
}

// resolveTemplate resolves a template string like "${event.requestId}" to actual value
func (de *DataEnricher) resolveTemplate(template string, data *config.ExtractedData) (interface{}, error) {
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
		source = data.Enrichment
	case "processed":
		source = data.Processed
	default:
		return nil, fmt.Errorf("unknown template source: %s", parts[0])
	}

	var current interface{} = source
	for i := 1; i < len(parts); i++ {
		switch v := current.(type) {
		case map[string]interface{}:
			var exists bool
			current, exists = v[parts[i]]
			if !exists {
				return nil, fmt.Errorf("field not found: %s", parts[i])
			}
		default:
			return nil, fmt.Errorf("cannot navigate through non-map type at %s", parts[i])
		}
	}

	return current, nil
}

// callViewMethod calls a view method on a contract
func (de *DataEnricher) callViewMethod(ctx context.Context, contractAddr, methodName, methodABI string, params []interface{}) ([]interface{}, error) {
	address := common.HexToAddress(contractAddr)

	contractABI, err := de.getOrParseABI(methodName, methodABI)
	if err != nil {
		return nil, fmt.Errorf("failed to get ABI: %w", err)
	}

	data, err := contractABI.Pack(methodName, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack method call: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &address,
		Data: data,
	}

	result, err := de.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	method, exists := contractABI.Methods[methodName]
	if !exists {
		return nil, fmt.Errorf("method not found in ABI: %s", methodName)
	}

	values, err := method.Outputs.Unpack(result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %w", err)
	}

	return values, nil
}

// getOrParseABI gets ABI from cache or parses it
func (de *DataEnricher) getOrParseABI(methodName, abiStr string) (abi.ABI, error) {
	if cached, exists := de.abiCache[methodName]; exists {
		return cached, nil
	}

	if abiStr == "" {
		return abi.ABI{}, fmt.Errorf("no ABI provided for method %s", methodName)
	}

	contractABI := fmt.Sprintf(`[%s]`, abiStr)
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return abi.ABI{}, fmt.Errorf("failed to parse ABI: %w", err)
	}

	de.abiCache[methodName] = parsed

	return parsed, nil
}

// processReturnValues processes return values according to mapping configuration
func (de *DataEnricher) processReturnValues(values []interface{}, mapping map[string]string, output map[string]interface{}) error {
	if len(mapping) == 0 {
		for i, value := range values {
			output[fmt.Sprintf("return%d", i)] = value
		}
		return nil
	}

	for fieldName, sourcePath := range mapping {
		value, err := de.extractReturnValue(values, sourcePath)
		if err != nil {
			return fmt.Errorf("failed to extract return value for %s: %w", fieldName, err)
		}

		if fieldName == "fullIntent" {
			logger.Debugf("Converting fullIntent from runtime struct (type: %T) to *types.OracleIntent", value)
			convertedValue, err := convertRuntimeStructToOracleIntent(value)
			if err != nil {
				logger.Warnf("Failed to convert fullIntent to typed struct: %v. Storing raw value.", err)
				output[fieldName] = value
			} else {
				logger.Debugf("Successfully converted fullIntent to *types.OracleIntent")
				output[fieldName] = convertedValue
			}
		} else {
			output[fieldName] = value
		}
	}

	return nil
}

// extractReturnValue extracts a value from return values based on path
func (de *DataEnricher) extractReturnValue(values []interface{}, path string) (interface{}, error) {
	if idx, err := de.parseIndex(path); err == nil {
		if idx >= len(values) {
			return nil, fmt.Errorf("return value index out of range: %d", idx)
		}
		return values[idx], nil
	}

	parts := strings.Split(path, ".")
	if len(parts) > 1 {
		return nil, fmt.Errorf("nested return paths not yet implemented: %s", path)
	}

	if path == "tuple" && len(values) == 1 {
		return values[0], nil
	}

	return nil, fmt.Errorf("invalid return path: %s", path)
}

// parseIndex parses an index from strings like "0" or "data[0]"
func (de *DataEnricher) parseIndex(s string) (int, error) {
	var idx int
	if _, err := fmt.Sscanf(s, "%d", &idx); err == nil {
		return idx, nil
	}

	if _, err := fmt.Sscanf(s, "data[%d]", &idx); err == nil {
		return idx, nil
	}

	return 0, fmt.Errorf("not an index: %s", s)
}

// EnrichmentResult represents the result of enrichment
type EnrichmentResult struct {
	Success bool
	Data    map[string]interface{}
	Error   error
}

// BatchEnrich performs enrichment for multiple events in parallel
func (de *DataEnricher) BatchEnrich(ctx context.Context, requests []EnrichmentRequest) []EnrichmentResult {
	results := make([]EnrichmentResult, len(requests))

	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)

	for i, req := range requests {
		i, req := i, req

		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()

			err := de.EnrichEventData(ctx, req.EventName, req.Data)
			if err != nil {
				results[i] = EnrichmentResult{
					Success: false,
					Error:   err,
				}
			} else {
				results[i] = EnrichmentResult{
					Success: true,
					Data:    req.Data.Enrichment,
				}
			}
		}()
	}

	for i := 0; i < maxConcurrent; i++ {
		sem <- struct{}{}
	}

	return results
}

// EnrichmentRequest represents a request to enrich event data
type EnrichmentRequest struct {
	EventName string
	Data      *config.ExtractedData
}

// ConvertTypes converts common types for contract calls
func ConvertTypes(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "0x") {
			n := new(big.Int)
			n.SetString(v[2:], 16)
			return n, nil
		}
		if common.IsHexAddress(v) {
			return common.HexToAddress(v), nil
		}
		return v, nil
	case float64:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case common.Hash:
		return v, nil
	case common.Address:
		return v, nil
	case *big.Int:
		return v, nil
	default:
		return value, nil
	}
}

// convertRuntimeStructToOracleIntent converts go-ethereum's runtime-generated struct to *types.OracleIntent
// The go-ethereum ABI unpacker creates a struct at runtime with the correct fields but unknown type.
// This function uses reflection to copy the fields to our known OracleIntent type.
func convertRuntimeStructToOracleIntent(value interface{}) (*types.OracleIntent, error) {
	// Check if already the right type
	if intent, ok := value.(*types.OracleIntent); ok {
		return intent, nil
	}

	val := reflect.ValueOf(value)
	if !val.IsValid() {
		return nil, fmt.Errorf("invalid value")
	}

	// Handle pointer to struct
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil, fmt.Errorf("nil pointer")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %v", val.Kind())
	}

	intent := &types.OracleIntent{}

	// Helper to get field value by name (case-insensitive)
	getField := func(fieldName string) reflect.Value {
		typ := val.Type()
		for i := 0; i < typ.NumField(); i++ {
			if strings.EqualFold(typ.Field(i).Name, fieldName) {
				return val.Field(i)
			}
		}
		return reflect.Value{}
	}

	// Copy string fields
	if field := getField("IntentType"); field.IsValid() && field.Kind() == reflect.String {
		intent.IntentType = field.String()
	}
	if field := getField("Version"); field.IsValid() && field.Kind() == reflect.String {
		intent.Version = field.String()
	}
	if field := getField("Symbol"); field.IsValid() && field.Kind() == reflect.String {
		intent.Symbol = field.String()
	}
	if field := getField("Source"); field.IsValid() && field.Kind() == reflect.String {
		intent.Source = field.String()
	}

	// Copy *big.Int fields
	if field := getField("ChainId"); field.IsValid() {
		if bi, ok := field.Interface().(*big.Int); ok && bi != nil {
			intent.ChainID = bi
		}
	}
	if field := getField("Nonce"); field.IsValid() {
		if bi, ok := field.Interface().(*big.Int); ok && bi != nil {
			intent.Nonce = bi
		}
	}
	if field := getField("Expiry"); field.IsValid() {
		if bi, ok := field.Interface().(*big.Int); ok && bi != nil {
			intent.Expiry = bi
		}
	}
	if field := getField("Price"); field.IsValid() {
		if bi, ok := field.Interface().(*big.Int); ok && bi != nil {
			intent.Price = bi
		}
	}
	if field := getField("Timestamp"); field.IsValid() {
		if bi, ok := field.Interface().(*big.Int); ok && bi != nil {
			intent.Timestamp = bi
		}
	}

	// Copy signature ([]byte or []uint8)
	if field := getField("Signature"); field.IsValid() {
		if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8 {
			signature := make([]byte, field.Len())
			reflect.Copy(reflect.ValueOf(signature), field)
			intent.Signature = types.HexBytes(signature)
		}
	}

	// Copy signer (common.Address)
	if field := getField("Signer"); field.IsValid() {
		if addr, ok := field.Interface().(common.Address); ok {
			intent.Signer = addr
		}
	}

	return intent, nil
}
