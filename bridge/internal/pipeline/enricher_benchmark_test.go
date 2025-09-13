package pipeline

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/mock"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// ContractCaller interface for mocking
type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// MockEthClient for benchmarking
type MockEthClient struct {
	mock.Mock
	CallLatencyMS int // Configurable network latency simulation
}

func (m *MockEthClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	// Simulate network latency
	if m.CallLatencyMS > 0 {
		time.Sleep(time.Duration(m.CallLatencyMS) * time.Millisecond)
	}
	
	args := m.Called(ctx, call, blockNumber)
	return args.Get(0).([]byte), args.Error(1)
}

// setupMockIntentEnrichment sets up mock for IntentRegistered enrichment
func setupMockIntentEnrichment(client *MockEthClient, latencyMS int) {
	client.CallLatencyMS = latencyMS
	
	// Mock getOracleIntent response
	// This simulates the registry contract call that takes ~300ms in real world
	intentData := encodeMockOracleIntent()
	
	client.On("CallContract", mock.Anything, mock.MatchedBy(func(call ethereum.CallMsg) bool {
		// Match getOracleIntent call by checking if call data starts with method signature
		return len(call.Data) >= 4 && call.To != nil
	}), mock.Anything).Return(intentData, nil)
}

// setupMockRandomnessEnrichment sets up mock for IntArraySet enrichment  
func setupMockRandomnessEnrichment(client *MockEthClient, latencyMS int) {
	client.CallLatencyMS = latencyMS
	
	// Mock getIntArray response
	// This simulates the randomness contract call for getting random integers
	randomData := encodeMockRandomArray()
	
	client.On("CallContract", mock.Anything, mock.MatchedBy(func(call ethereum.CallMsg) bool {
		// Match getIntArray call
		return len(call.Data) >= 4 && call.To != nil
	}), mock.Anything).Return(randomData, nil)
}

// Helper functions for encoding mock data

// encodeMockOracleIntent encodes an oracle intent for mock response
func encodeMockOracleIntent() []byte {
	// Create a proper ABI-encoded response for getOracleIntent
	// This returns a tuple with the oracle intent structure
	abiDef := `[{"name":"getOracleIntent","type":"function","inputs":[{"name":"intentHash","type":"bytes32"}],"outputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}]`
	
	parsedABI, err := abi.JSON(strings.NewReader(abiDef))
	if err != nil {
		// Fallback to simple encoding
		return []byte("mock_encoded_oracle_intent_data")
	}
	
	// Create mock intent data
	intentData := struct {
		IntentType string
		Version    string
		ChainId    *big.Int
		Nonce      *big.Int
		Expiry     *big.Int
		Symbol     string
		Price      *big.Int
		Timestamp  *big.Int
		Source     string
		Signature  []byte
		Signer     common.Address
	}{
		IntentType: "OracleUpdate",
		Version:    "1.0",
		ChainId:    big.NewInt(100640),
		Nonce:      big.NewInt(1757675030778102549),
		Expiry:     big.NewInt(1757678630),
		Symbol:     "BTC/USD",
		Price:      big.NewInt(11495342260533),
		Timestamp:  big.NewInt(1757675030),
		Source:     "DIA Oracle",
		Signature:  []byte("mock_signature_data"),
		Signer:     common.HexToAddress("0x0Fa4D71382178ecB0DBA9961cB31153819043DfE"),
	}
	
	encoded, err := parsedABI.Methods["getOracleIntent"].Outputs.Pack(intentData)
	if err != nil {
		// Fallback to simple encoding
		return []byte("mock_encoded_oracle_intent_data")
	}
	
	return encoded
}

// encodeMockRandomArray encodes random array data for mock response
// TestDataEnricher is a testable version of DataEnricher
type TestDataEnricher struct {
	client    ContractCaller
	eventDefs map[string]*config.EventDefinition
	abiCache  map[string]abi.ABI
	mutex     sync.RWMutex
}

// newTestDataEnricher creates a DataEnricher for testing with a mock client
func newTestDataEnricher(client ContractCaller, eventDefs map[string]*config.EventDefinition) *TestDataEnricher {
	return &TestDataEnricher{
		client:    client,
		eventDefs: eventDefs,
		abiCache:  make(map[string]abi.ABI),
	}
}

// EnrichEventData enriches event data with view call results (test version)
func (de *TestDataEnricher) EnrichEventData(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
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
	
	return nil
}

// Helper methods for TestDataEnricher (copied from original enricher)
func (de *TestDataEnricher) buildParameters(paramTemplates []string, data *config.ExtractedData) ([]interface{}, error) {
	params := make([]interface{}, len(paramTemplates))
	
	for i, template := range paramTemplates {
		value, err := de.resolveTemplate(template, data)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %d: %w", i, err)
		}
		
		// Convert types for contract calls (especially hex strings to proper types)
		convertedValue, err := de.convertTypes(value)
		if err != nil {
			return nil, fmt.Errorf("failed to convert parameter %d: %w", i, err)
		}
		
		params[i] = convertedValue
	}
	
	return params, nil
}

// convertTypes converts common types for contract calls (adapted from enricher.go)
func (de *TestDataEnricher) convertTypes(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "0x") && len(v) == 66 {
			// This looks like a bytes32 hash
			return common.HexToHash(v), nil
		}
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

func (de *TestDataEnricher) resolveTemplate(template string, data *config.ExtractedData) (interface{}, error) {
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

func (de *TestDataEnricher) callViewMethod(ctx context.Context, contractAddr, methodName, methodABI string, params []interface{}) ([]interface{}, error) {
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

func (de *TestDataEnricher) getOrParseABI(methodName, abiStr string) (abi.ABI, error) {
	// Try to read from cache first with read lock
	de.mutex.RLock()
	if cached, exists := de.abiCache[methodName]; exists {
		de.mutex.RUnlock()
		return cached, nil
	}
	de.mutex.RUnlock()
	
	if abiStr == "" {
		return abi.ABI{}, fmt.Errorf("no ABI provided for method %s", methodName)
	}
	
	contractABI := fmt.Sprintf(`[%s]`, abiStr)
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return abi.ABI{}, fmt.Errorf("failed to parse ABI: %w", err)
	}
	
	// Write to cache with write lock
	de.mutex.Lock()
	de.abiCache[methodName] = parsed
	de.mutex.Unlock()
	
	return parsed, nil
}

func (de *TestDataEnricher) processReturnValues(values []interface{}, mapping map[string]string, output map[string]interface{}) error {
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
		output[fieldName] = value
	}
	
	return nil
}

func (de *TestDataEnricher) extractReturnValue(values []interface{}, path string) (interface{}, error) {
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

func (de *TestDataEnricher) parseIndex(s string) (int, error) {
	var idx int
	if _, err := fmt.Sscanf(s, "%d", &idx); err == nil {
		return idx, nil
	}
	
	if _, err := fmt.Sscanf(s, "data[%d]", &idx); err == nil {
		return idx, nil
	}
	
	return 0, fmt.Errorf("not an index: %s", s)
}

func encodeMockRandomArray() []byte {
	// Create a proper ABI-encoded response for getIntArray
	abiDef := `[{"name":"getIntArray","type":"function","inputs":[{"name":"requestId_","type":"uint256"}],"outputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"},{"name":"round","type":"int64"},{"name":"seed","type":"string"},{"name":"signature","type":"string"}]}]`
	
	parsedABI, err := abi.JSON(strings.NewReader(abiDef))
	if err != nil {
		// Fallback to simple hex-encoded data
		mockData, _ := hex.DecodeString("00000000000000000000000000000000000000000000000000000000000001ce0000000000000000000000000000000000000000000000000000000000000a0000000000000000000000000000000000000000000000000000000000066b9e00000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000140000000000000000000000000000000000000000000000000000000000000000500000000000000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000fffffb80000000000000000000000000000000000000000000000000000000000000030900000000000000000000000000000000000000000000000000000000000000007b00000000000000000000000000000000000000000000000000000000fffffe380000000000000000000000000000000000000000000000000000000000000012random_seed_string00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001672616e646f6d5f7369676e61747572655f737472696e6700000000000000000")
		return mockData
	}
	
	// Create mock random data
	randomInts := []*big.Int{
		big.NewInt(999),
		big.NewInt(-888),
		big.NewInt(777),
		big.NewInt(123),
		big.NewInt(-456),
	}
	
	encoded, err := parsedABI.Methods["getIntArray"].Outputs.Pack(
		big.NewInt(462),        // requestId
		randomInts,             // randomInts
		int64(421614),          // round
		"random_seed_string",   // seed
		"random_signature_string", // signature
	)
	if err != nil {
		// Fallback to simple hex-encoded data
		mockData, _ := hex.DecodeString("00000000000000000000000000000000000000000000000000000000000001ce0000000000000000000000000000000000000000000000000000000000000a0000000000000000000000000000000000000000000000000000000000066b9e00000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000140000000000000000000000000000000000000000000000000000000000000000500000000000000000000000000000000000000000000000000000000000003e700000000000000000000000000000000000000000000000000000000fffffb80000000000000000000000000000000000000000000000000000000000000030900000000000000000000000000000000000000000000000000000000000000007b00000000000000000000000000000000000000000000000000000000fffffe380000000000000000000000000000000000000000000000000000000000000012random_seed_string00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001672616e646f6d5f7369676e61747572655f737472696e6700000000000000000")
		return mockData
	}
	
	return encoded
}

// createIntentRegisteredEventData creates test data for IntentRegistered event
func createIntentRegisteredEventData() *config.ExtractedData {
	return &config.ExtractedData{
		Event: map[string]interface{}{
			"_blockNumber": uint64(26507256),
			"_contract":    "0x84cabdE3B8f739fa265f1A2076370e2E0E8944E2",
			"_logIndex":    0,
			"_txHash":      "0xac1a0d0e1d3ecd67d973722f743a6e0e86c7b456fd73c412c598ea4c2f69cab0",
			"intentHash":   "0xcbd949d6bb1335bdefb178711b7549137acf841002c9320e695a15d637001660",
			"price":        "0x00000000000000000000000000000000000000000000000000000a7477cac135",
			"signer":       "0x0Fa4D71382178ecB0DBA9961cB31153819043DfE",
			"symbol":       "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489",
			"timestamp":    big.NewInt(1757675030),
		},
		Enrichment: make(map[string]interface{}),
		Processed:  make(map[string]interface{}),
	}
}

// createIntArraySetEventData creates test data for IntArraySet event
func createIntArraySetEventData() *config.ExtractedData {
	return &config.ExtractedData{
		Event: map[string]interface{}{
			"_blockNumber": uint64(26507300),
			"_contract":    "0x736A07F7dBa949FC459fFfc1D0c8e63362E71503",
			"_logIndex":    0,
			"_txHash":      "0x42ca8207a2b7fe7dcc9487128c55eb31ea88e184d884684ed8e31a0fb63845cb",
			"requestId":    big.NewInt(462),
			"round":        int64(421614),
			"seed":         "random_seed_value",
			"signature":    "signature_value",
		},
		Enrichment: make(map[string]interface{}),
		Processed:  make(map[string]interface{}),
	}
}

// createIntentRegisteredEventDef creates event definition for IntentRegistered
func createIntentRegisteredEventDef() *config.EventDefinition {
	return &config.EventDefinition{
		Contract: "0x84cabdE3B8f739fa265f1A2076370e2E0E8944E2",
		Enrichment: &config.EnrichmentConfig{
			Method:   "getOracleIntent",
			Contract: "0x84cabdE3B8f739fa265f1A2076370e2E0E8944E2",
			ABI:      `{"name":"getOracleIntent","type":"function","inputs":[{"name":"intentHash","type":"bytes32"}],"outputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}`,
			Params: []string{
				"${event.intentHash}",
			},
			Returns: map[string]string{
				"fullIntent": "0",
			},
		},
	}
}

// createIntArraySetEventDef creates event definition for IntArraySet
func createIntArraySetEventDef() *config.EventDefinition {
	return &config.EventDefinition{
		Contract: "0x736A07F7dBa949FC459fFfc1D0c8e63362E71503",
		Enrichment: &config.EnrichmentConfig{
			Method:   "getIntArray",
			Contract: "0x736A07F7dBa949FC459fFfc1D0c8e63362E71503",
			ABI:      `{"name":"getIntArray","type":"function","inputs":[{"name":"requestId_","type":"uint256"}],"outputs":[{"name":"requestId","type":"uint256"},{"name":"randomInts","type":"int256[]"},{"name":"round","type":"int64"},{"name":"seed","type":"string"},{"name":"signature","type":"string"}]}`,
			Params: []string{
				"${event.requestId}",
			},
			Returns: map[string]string{
				"randomInts":    "1",
				"round":         "2", 
				"fullSeed":      "3",
				"fullSignature": "4",
			},
		},
	}
}

// Benchmark Tests for IntentRegistered Event Enrichment

func BenchmarkIntentRegistered_Enrichment_FastNetwork(b *testing.B) {
	benchmarkIntentEnrichment(b, 50, "Fast network (50ms latency)")
}

func BenchmarkIntentRegistered_Enrichment_MediumNetwork(b *testing.B) {
	benchmarkIntentEnrichment(b, 150, "Medium network (150ms latency)")
}

func BenchmarkIntentRegistered_Enrichment_SlowNetwork(b *testing.B) {
	benchmarkIntentEnrichment(b, 300, "Slow network (300ms latency)")
}

func BenchmarkIntentRegistered_Enrichment_VerySlowNetwork(b *testing.B) {
	benchmarkIntentEnrichment(b, 500, "Very slow network (500ms latency)")
}

// benchmarkIntentEnrichment runs the benchmark for IntentRegistered enrichment
func benchmarkIntentEnrichment(b *testing.B, latencyMS int, description string) {
	// Setup
	mockClient := &MockEthClient{}
	setupMockIntentEnrichment(mockClient, latencyMS)
	
	eventDefs := map[string]*config.EventDefinition{
		"IntentRegistered": createIntentRegisteredEventDef(),
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.ResetTimer()
	b.Run(description, func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create fresh event data for each iteration
			data := createIntentRegisteredEventData()
			
			err := enricher.EnrichEventData(ctx, "IntentRegistered", data)
			if err != nil {
				b.Fatalf("Enrichment failed: %v", err)
			}
		}
	})
	
	// Report metrics
	b.ReportMetric(float64(latencyMS), "network_latency_ms")
}

// Benchmark Tests for IntArraySet Event Enrichment

func BenchmarkIntArraySet_Enrichment_FastNetwork(b *testing.B) {
	benchmarkRandomnessEnrichment(b, 50, "Fast network (50ms latency)")
}

func BenchmarkIntArraySet_Enrichment_MediumNetwork(b *testing.B) {
	benchmarkRandomnessEnrichment(b, 150, "Medium network (150ms latency)")
}

func BenchmarkIntArraySet_Enrichment_SlowNetwork(b *testing.B) {
	benchmarkRandomnessEnrichment(b, 300, "Slow network (300ms latency)")
}

func BenchmarkIntArraySet_Enrichment_VerySlowNetwork(b *testing.B) {
	benchmarkRandomnessEnrichment(b, 500, "Very slow network (500ms latency)")
}

// benchmarkRandomnessEnrichment runs the benchmark for IntArraySet enrichment
func benchmarkRandomnessEnrichment(b *testing.B, latencyMS int, description string) {
	// Setup
	mockClient := &MockEthClient{}
	setupMockRandomnessEnrichment(mockClient, latencyMS)
	
	eventDefs := map[string]*config.EventDefinition{
		"IntArraySet": createIntArraySetEventDef(),
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.ResetTimer()
	b.Run(description, func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create fresh event data for each iteration
			data := createIntArraySetEventData()
			
			err := enricher.EnrichEventData(ctx, "IntArraySet", data)
			if err != nil {
				b.Fatalf("Enrichment failed: %v", err)
			}
		}
	})
	
	// Report metrics
	b.ReportMetric(float64(latencyMS), "network_latency_ms")
}

// Comparative Benchmarks

func BenchmarkEnrichment_Comparison(b *testing.B) {
	latencies := []int{50, 150, 300, 500}
	eventTypes := []string{"IntentRegistered", "IntArraySet"}
	
	for _, eventType := range eventTypes {
		for _, latency := range latencies {
			testName := fmt.Sprintf("%s_Latency_%dms", eventType, latency)
			
			b.Run(testName, func(b *testing.B) {
				switch eventType {
				case "IntentRegistered":
					benchmarkIntentEnrichment(b, latency, testName)
				case "IntArraySet":
					benchmarkRandomnessEnrichment(b, latency, testName)
				}
			})
		}
	}
}

// Concurrent Enrichment Benchmarks

func BenchmarkIntentRegistered_Concurrent_10Workers(b *testing.B) {
	benchmarkConcurrentEnrichment(b, "IntentRegistered", 10, 200)
}

func BenchmarkIntentRegistered_Concurrent_50Workers(b *testing.B) {
	benchmarkConcurrentEnrichment(b, "IntentRegistered", 50, 200)
}

func BenchmarkIntArraySet_Concurrent_10Workers(b *testing.B) {
	benchmarkConcurrentEnrichment(b, "IntArraySet", 10, 200)
}

func BenchmarkIntArraySet_Concurrent_50Workers(b *testing.B) {
	benchmarkConcurrentEnrichment(b, "IntArraySet", 50, 200)
}

// benchmarkConcurrentEnrichment tests enrichment under concurrent load
func benchmarkConcurrentEnrichment(b *testing.B, eventType string, workers int, latencyMS int) {
	// Setup
	mockClient := &MockEthClient{}
	var eventDefs map[string]*config.EventDefinition
	
	switch eventType {
	case "IntentRegistered":
		setupMockIntentEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntentRegistered": createIntentRegisteredEventDef(),
		}
	case "IntArraySet":
		setupMockRandomnessEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntArraySet": createIntArraySetEventDef(),
		}
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.SetParallelism(workers)
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var data *config.ExtractedData
			switch eventType {
			case "IntentRegistered":
				data = createIntentRegisteredEventData()
			case "IntArraySet":
				data = createIntArraySetEventData()
			}
			
			err := enricher.EnrichEventData(ctx, eventType, data)
			if err != nil {
				b.Fatalf("Enrichment failed: %v", err)
			}
		}
	})
	
	// Report metrics
	b.ReportMetric(float64(workers), "concurrent_workers")
	b.ReportMetric(float64(latencyMS), "network_latency_ms")
}

// Memory Usage Benchmarks

func BenchmarkEnrichment_MemoryUsage_IntentRegistered(b *testing.B) {
	benchmarkMemoryUsage(b, "IntentRegistered", 100)
}

func BenchmarkEnrichment_MemoryUsage_IntArraySet(b *testing.B) {
	benchmarkMemoryUsage(b, "IntArraySet", 100)
}

// benchmarkMemoryUsage measures memory allocation during enrichment
func benchmarkMemoryUsage(b *testing.B, eventType string, latencyMS int) {
	// Setup
	mockClient := &MockEthClient{}
	var eventDefs map[string]*config.EventDefinition
	
	switch eventType {
	case "IntentRegistered":
		setupMockIntentEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntentRegistered": createIntentRegisteredEventDef(),
		}
	case "IntArraySet":
		setupMockRandomnessEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntArraySet": createIntArraySetEventDef(),
		}
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		var data *config.ExtractedData
		switch eventType {
		case "IntentRegistered":
			data = createIntentRegisteredEventData()
		case "IntArraySet":
			data = createIntArraySetEventData()
		}
		
		err := enricher.EnrichEventData(ctx, eventType, data)
		if err != nil {
			b.Fatalf("Enrichment failed: %v", err)
		}
	}
}

// Stress Test Benchmarks

func BenchmarkEnrichment_StressTest_1000_IntentRegistered(b *testing.B) {
	stressTestEnrichment(b, "IntentRegistered", 1000, 100)
}

func BenchmarkEnrichment_StressTest_1000_IntArraySet(b *testing.B) {
	stressTestEnrichment(b, "IntArraySet", 1000, 100)
}

// stressTestEnrichment runs high-volume enrichment tests
func stressTestEnrichment(b *testing.B, eventType string, iterations int, latencyMS int) {
	// Setup
	mockClient := &MockEthClient{}
	var eventDefs map[string]*config.EventDefinition
	
	switch eventType {
	case "IntentRegistered":
		setupMockIntentEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntentRegistered": createIntentRegisteredEventDef(),
		}
	case "IntArraySet":
		setupMockRandomnessEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntArraySet": createIntArraySetEventDef(),
		}
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		start := time.Now()
		
		// Process batch of events
		for j := 0; j < iterations; j++ {
			var data *config.ExtractedData
			switch eventType {
			case "IntentRegistered":
				data = createIntentRegisteredEventData()
			case "IntArraySet":
				data = createIntArraySetEventData()
			}
			
			err := enricher.EnrichEventData(ctx, eventType, data)
			if err != nil {
				b.Fatalf("Enrichment failed: %v", err)
			}
		}
		
		duration := time.Since(start)
		b.ReportMetric(float64(iterations), "events_processed")
		b.ReportMetric(duration.Seconds(), "batch_duration_seconds")
		b.ReportMetric(float64(iterations)/duration.Seconds(), "events_per_second")
	}
}

// Performance Analysis Functions

func BenchmarkEnrichment_FullAnalysis(b *testing.B) {
	// This benchmark provides a comprehensive performance analysis
	eventTypes := []string{"IntentRegistered", "IntArraySet"}
	scenarios := []struct {
		name      string
		latency   int
		workers   int
		batchSize int
	}{
		{"Optimal_Conditions", 50, 1, 1},
		{"Production_Load", 200, 10, 10},
		{"High_Latency", 500, 5, 5},
		{"Burst_Load", 100, 50, 100},
	}
	
	for _, eventType := range eventTypes {
		for _, scenario := range scenarios {
			testName := fmt.Sprintf("%s_%s", eventType, scenario.name)
			
			b.Run(testName, func(b *testing.B) {
				performAnalysisBenchmark(b, eventType, scenario.latency, scenario.workers, scenario.batchSize)
			})
		}
	}
}

func performAnalysisBenchmark(b *testing.B, eventType string, latencyMS, workers, batchSize int) {
	// Setup
	mockClient := &MockEthClient{}
	var eventDefs map[string]*config.EventDefinition
	
	switch eventType {
	case "IntentRegistered":
		setupMockIntentEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntentRegistered": createIntentRegisteredEventDef(),
		}
	case "IntArraySet":
		setupMockRandomnessEnrichment(mockClient, latencyMS)
		eventDefs = map[string]*config.EventDefinition{
			"IntArraySet": createIntArraySetEventDef(),
		}
	}
	
	enricher := newTestDataEnricher(mockClient, eventDefs)
	
	ctx := context.Background()
	
	b.SetParallelism(workers)
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			start := time.Now()
			
			// Process batch
			for i := 0; i < batchSize; i++ {
				var data *config.ExtractedData
				switch eventType {
				case "IntentRegistered":
					data = createIntentRegisteredEventData()
				case "IntArraySet":
					data = createIntArraySetEventData()
				}
				
				err := enricher.EnrichEventData(ctx, eventType, data)
				if err != nil {
					b.Fatalf("Enrichment failed: %v", err)
				}
			}
			
			duration := time.Since(start)
			b.ReportMetric(duration.Seconds()/float64(batchSize), "avg_enrichment_time_seconds")
		}
	})
	
	// Report configuration metrics
	b.ReportMetric(float64(latencyMS), "network_latency_ms")
	b.ReportMetric(float64(workers), "concurrent_workers")
	b.ReportMetric(float64(batchSize), "batch_size")
}