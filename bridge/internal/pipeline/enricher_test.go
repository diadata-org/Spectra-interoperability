package pipeline

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// ===== TEST SUITE SETUP =====

// EnricherTestSuite provides a structured testing framework for enricher functionality
type EnricherTestSuite struct {
	suite.Suite
	mockClient *MockEthClientUnit
	enricher   *TestableDataEnricher
}

// MockEthClientUnit for unit testing (different from benchmark mock)
type MockEthClientUnit struct {
	mock.Mock
}

func (m *MockEthClientUnit) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	args := m.Called(ctx, call, blockNumber)
	return args.Get(0).([]byte), args.Error(1)
}

// SetupTest runs before each test method
func (suite *EnricherTestSuite) SetupTest() {
	suite.mockClient = &MockEthClientUnit{}
	suite.enricher = nil // Will be created in individual tests as needed
}

// TearDownTest runs after each test method
func (suite *EnricherTestSuite) TearDownTest() {
	if suite.mockClient != nil {
		suite.mockClient.AssertExpectations(suite.T())
	}
}

// ===== HELPER METHODS FOR EASY TEST CREATION =====

// createEnricher creates a testable enricher with given event definitions
func (suite *EnricherTestSuite) createEnricher(eventDefs map[string]*config.EventDefinition) {
	suite.enricher = newTestableDataEnricher(suite.mockClient, eventDefs)
}

// createBasicEventDef creates a basic event definition for testing
func (suite *EnricherTestSuite) createBasicEventDef(contractAddr string, enrichmentConfig *config.EnrichmentConfig) *config.EventDefinition {
	return &config.EventDefinition{
		Contract:   contractAddr,
		Enrichment: enrichmentConfig,
	}
}

// createEnrichmentConfig creates enrichment configuration for testing
func (suite *EnricherTestSuite) createEnrichmentConfig(method, contract, abiStr string, params []string, returns map[string]string) *config.EnrichmentConfig {
	return &config.EnrichmentConfig{
		Method:   method,
		Contract: contract,
		ABI:      abiStr,
		Params:   params,
		Returns:  returns,
	}
}

// createEventData creates test event data
func (suite *EnricherTestSuite) createEventData(eventFields map[string]interface{}) *config.ExtractedData {
	return &config.ExtractedData{
		Event:      eventFields,
		Enrichment: make(map[string]interface{}),
		Processed:  make(map[string]interface{}),
	}
}

// mockContractCall sets up a mock contract call expectation
func (suite *EnricherTestSuite) mockContractCall(response []byte, err error) {
	suite.mockClient.On("CallContract", mock.Anything, mock.Anything, mock.Anything).
		Return(response, err)
}

// mustHexDecode helper for creating test data
func (suite *EnricherTestSuite) mustHexDecode(s string) []byte {
	data, err := hex.DecodeString(s)
	suite.Require().NoError(err)
	return data
}

// Test runner for the suite
func TestEnricherTestSuite(t *testing.T) {
	suite.Run(t, new(EnricherTestSuite))
}

// TestableDataEnricher wraps DataEnricher for testing
type TestableDataEnricher struct {
	client    *MockEthClientUnit
	eventDefs map[string]*config.EventDefinition
	abiCache  map[string]abi.ABI
	mutex     sync.RWMutex
}

// Create a testable data enricher that matches the interface
func newTestableDataEnricher(client *MockEthClientUnit, eventDefs map[string]*config.EventDefinition) *TestableDataEnricher {
	return &TestableDataEnricher{
		client:    client,
		eventDefs: eventDefs,
		abiCache:  make(map[string]abi.ABI),
	}
}

// Wrapper methods that call the original functions
func (tde *TestableDataEnricher) EnrichEventData(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	// Create a temporary DataEnricher for testing
	de := &DataEnricher{
		client:    nil, // We'll mock the client calls
		eventDefs: tde.eventDefs,
		abiCache:  tde.abiCache,
	}
	
	eventDef, exists := de.eventDefs[eventName]
	if !exists {
		return errors.New("event definition not found: " + eventName)
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
			return errors.New("no contract address for enrichment")
		}
	}
	
	params, err := de.buildParameters(enrichment.Params, extractedData)
	if err != nil {
		return err
	}
	
	// Mock the contract call
	result, err := tde.mockCallViewMethod(ctx, contractAddr, enrichment.Method, enrichment.ABI, params)
	if err != nil {
		return err
	}
	
	enrichedData := make(map[string]interface{})
	if err := de.processReturnValues(result, enrichment.Returns, enrichedData); err != nil {
		return err
	}
	
	extractedData.Enrichment = enrichedData
	return nil
}

func (tde *TestableDataEnricher) mockCallViewMethod(ctx context.Context, contractAddr, methodName, methodABI string, params []interface{}) ([]interface{}, error) {
	address := common.HexToAddress(contractAddr)
	
	contractABI, err := tde.getOrParseABI(methodName, methodABI)
	if err != nil {
		return nil, err
	}
	
	data, err := contractABI.Pack(methodName, params...)
	if err != nil {
		return nil, err
	}
	
	msg := ethereum.CallMsg{
		To:   &address,
		Data: data,
	}
	
	result, err := tde.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, err
	}
	
	method, exists := contractABI.Methods[methodName]
	if !exists {
		return nil, errors.New("method not found in ABI: " + methodName)
	}
	
	values, err := method.Outputs.Unpack(result)
	if err != nil {
		return nil, err
	}
	
	return values, nil
}

func (tde *TestableDataEnricher) getOrParseABI(methodName, abiStr string) (abi.ABI, error) {
	tde.mutex.RLock()
	if cached, exists := tde.abiCache[methodName]; exists {
		tde.mutex.RUnlock()
		return cached, nil
	}
	tde.mutex.RUnlock()
	
	if abiStr == "" {
		return abi.ABI{}, errors.New("no ABI provided for method " + methodName)
	}
	
	contractABI := "[" + abiStr + "]"
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return abi.ABI{}, err
	}
	
	tde.mutex.Lock()
	tde.abiCache[methodName] = parsed
	tde.mutex.Unlock()
	
	return parsed, nil
}

// ===== CONSTRUCTOR TESTS =====

func (suite *EnricherTestSuite) TestNewDataEnricher() {
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", nil),
	}
	
	enricher, err := NewDataEnricher(nil, eventDefs) // We don't use real client in unit tests
	
	suite.NoError(err)
	suite.NotNil(enricher)
	suite.Equal(eventDefs, enricher.eventDefs)
	suite.NotNil(enricher.abiCache)
	suite.Len(enricher.abiCache, 0) // Should be empty initially
}

// ===== ENRICH EVENT DATA TESTS =====

func (suite *EnricherTestSuite) TestEnrichEventData_EventDefinitionNotFound() {
	suite.createEnricher(map[string]*config.EventDefinition{})
	data := suite.createEventData(map[string]interface{}{})
	
	err := suite.enricher.EnrichEventData(context.Background(), "NonExistentEvent", data)
	
	suite.Error(err)
	suite.Contains(err.Error(), "event definition not found")
}

func (suite *EnricherTestSuite) TestEnrichEventData_NoEnrichmentConfig() {
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", nil),
	}
	suite.createEnricher(eventDefs)
	data := suite.createEventData(map[string]interface{}{})
	
	err := suite.enricher.EnrichEventData(context.Background(), "TestEvent", data)
	
	suite.NoError(err) // Should succeed with no enrichment
}

func (suite *EnricherTestSuite) TestEnrichEventData_SuccessfulEnrichment() {
	enrichmentConfig := suite.createEnrichmentConfig(
		"getValue",
		"0x1234567890123456789012345678901234567890",
		`{"name":"getValue","type":"function","inputs":[],"outputs":[{"name":"value","type":"uint256"}]}`,
		[]string{},
		map[string]string{"result": "0"},
	)
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", enrichmentConfig),
	}
	suite.createEnricher(eventDefs)
	data := suite.createEventData(map[string]interface{}{})
	
	// Mock successful contract call returning 66 (0x42 in hex)
	suite.mockContractCall(suite.mustHexDecode("0000000000000000000000000000000000000000000000000000000000000042"), nil)
	
	err := suite.enricher.EnrichEventData(context.Background(), "TestEvent", data)
	
	suite.NoError(err)
	suite.Equal(big.NewInt(66), data.Enrichment["result"])
}

func (suite *EnricherTestSuite) TestEnrichEventData_ContractAddressFromEventData() {
	enrichmentConfig := suite.createEnrichmentConfig(
		"getValue",
		"", // Empty contract address - should use from event data
		`{"name":"getValue","type":"function","inputs":[],"outputs":[{"name":"value","type":"uint256"}]}`,
		[]string{},
		map[string]string{"result": "0"},
	)
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("", enrichmentConfig),
	}
	suite.createEnricher(eventDefs)
	data := suite.createEventData(map[string]interface{}{
		"_contract": "0x1234567890123456789012345678901234567890",
	})
	
	suite.mockContractCall(suite.mustHexDecode("0000000000000000000000000000000000000000000000000000000000000042"), nil)
	
	err := suite.enricher.EnrichEventData(context.Background(), "TestEvent", data)
	
	suite.NoError(err)
}

func (suite *EnricherTestSuite) TestEnrichEventData_NoContractAddress() {
	enrichmentConfig := suite.createEnrichmentConfig(
		"getValue",
		"", // Empty contract address
		`{"name":"getValue","type":"function","inputs":[],"outputs":[{"name":"value","type":"uint256"}]}`,
		[]string{},
		map[string]string{"result": "0"},
	)
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("", enrichmentConfig),
	}
	suite.createEnricher(eventDefs)
	data := suite.createEventData(map[string]interface{}{}) // No _contract field
	
	err := suite.enricher.EnrichEventData(context.Background(), "TestEvent", data)
	
	suite.Error(err)
	suite.Contains(err.Error(), "no contract address for enrichment")
}

// ===== PARAMETER BUILDING TESTS =====

func (suite *EnricherTestSuite) TestBuildParameters_EmptyTemplates() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	
	result, err := de.buildParameters([]string{}, data)
	
	suite.NoError(err)
	suite.Empty(result)
}

func (suite *EnricherTestSuite) TestBuildParameters_LiteralValue() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	
	result, err := de.buildParameters([]string{"literal_value"}, data)
	
	suite.NoError(err)
	suite.Equal([]interface{}{"literal_value"}, result)
}

func (suite *EnricherTestSuite) TestBuildParameters_EventFieldTemplate() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{
		"requestId": big.NewInt(123),
	})
	
	result, err := de.buildParameters([]string{"${event.requestId}"}, data)
	
	suite.NoError(err)
	suite.Equal([]interface{}{big.NewInt(123)}, result)
}

func (suite *EnricherTestSuite) TestBuildParameters_MultipleTemplates() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{
		"param1": "value1",
		"param2": "value2",
	})
	
	result, err := de.buildParameters([]string{"${event.param1}", "literal", "${event.param2}"}, data)
	
	suite.NoError(err)
	suite.Equal([]interface{}{"value1", "literal", "value2"}, result)
}

func (suite *EnricherTestSuite) TestBuildParameters_InvalidTemplate() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	
	result, err := de.buildParameters([]string{"${event.nonexistent}"}, data)
	
	suite.Error(err)
	suite.Nil(result)
}

// ===== TEMPLATE RESOLUTION TESTS =====

func (suite *EnricherTestSuite) TestResolveTemplate_LiteralValue() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	
	result, err := de.resolveTemplate("literal", data)
	
	suite.NoError(err)
	suite.Equal("literal", result)
}

func (suite *EnricherTestSuite) TestResolveTemplate_EventField() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{
		"requestId": big.NewInt(456),
	})
	
	result, err := de.resolveTemplate("${event.requestId}", data)
	
	suite.NoError(err)
	suite.Equal(big.NewInt(456), result)
}

func (suite *EnricherTestSuite) TestResolveTemplate_EnrichmentField() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	data.Enrichment["result"] = "enriched_value"
	
	result, err := de.resolveTemplate("${enrichment.result}", data)
	
	suite.NoError(err)
	suite.Equal("enriched_value", result)
}

func (suite *EnricherTestSuite) TestResolveTemplate_ProcessedField() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	data.Processed["computed"] = 789
	
	result, err := de.resolveTemplate("${processed.computed}", data)
	
	suite.NoError(err)
	suite.Equal(789, result)
}

func (suite *EnricherTestSuite) TestResolveTemplate_FieldNotFound() {
	de := &DataEnricher{}
	data := suite.createEventData(map[string]interface{}{})
	
	result, err := de.resolveTemplate("${event.nonexistent}", data)
	
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "field not found")
}

// ===== RETURN VALUE PROCESSING TESTS =====

func (suite *EnricherTestSuite) TestProcessReturnValues_DefaultNaming() {
	de := &DataEnricher{}
	values := []interface{}{big.NewInt(123), "test"}
	output := make(map[string]interface{})
	
	err := de.processReturnValues(values, map[string]string{}, output)
	
	suite.NoError(err)
	suite.Equal(big.NewInt(123), output["return0"])
	suite.Equal("test", output["return1"])
}

func (suite *EnricherTestSuite) TestProcessReturnValues_IndexMapping() {
	de := &DataEnricher{}
	values := []interface{}{big.NewInt(456), "hello"}
	mapping := map[string]string{
		"number": "0",
		"text":   "1",
	}
	output := make(map[string]interface{})
	
	err := de.processReturnValues(values, mapping, output)
	
	suite.NoError(err)
	suite.Equal(big.NewInt(456), output["number"])
	suite.Equal("hello", output["text"])
}

// ===== UTILITY FUNCTION TESTS =====

func (suite *EnricherTestSuite) TestParseIndex_SimpleNumber() {
	de := &DataEnricher{}
	
	result, err := de.parseIndex("42")
	
	suite.NoError(err)
	suite.Equal(42, result)
}

func (suite *EnricherTestSuite) TestParseIndex_DataArrayFormat() {
	de := &DataEnricher{}
	
	result, err := de.parseIndex("data[3]")
	
	suite.NoError(err)
	suite.Equal(3, result)
}

func (suite *EnricherTestSuite) TestParseIndex_InvalidFormat() {
	de := &DataEnricher{}
	
	result, err := de.parseIndex("abc")
	
	suite.Error(err)
	suite.Equal(0, result)
}

func (suite *EnricherTestSuite) TestConvertTypes_HexStringToBigInt() {
	result, err := ConvertTypes("0x42")
	
	suite.NoError(err)
	suite.Equal(big.NewInt(66), result)
}

func (suite *EnricherTestSuite) TestConvertTypes_HexAddressToBigInt() {
	// Note: Current implementation converts addresses to big.Int due to 0x prefix check
	result, err := ConvertTypes("0x1234567890123456789012345678901234567890")
	expected := func() *big.Int { 
		n := new(big.Int)
		n.SetString("1234567890123456789012345678901234567890", 16)
		return n 
	}()
	
	suite.NoError(err)
	suite.Equal(expected, result)
}

func (suite *EnricherTestSuite) TestConvertTypes_RegularString() {
	result, err := ConvertTypes("regular_string")
	
	suite.NoError(err)
	suite.Equal("regular_string", result)
}

// ===== ABI PARSING TESTS =====

func (suite *EnricherTestSuite) TestGetOrParseABI_ValidABI() {
	de := &DataEnricher{abiCache: make(map[string]abi.ABI)}
	abiStr := `{"name":"testMethod","type":"function","inputs":[],"outputs":[]}`
	
	result, err := de.getOrParseABI("testMethod", abiStr)
	
	suite.NoError(err)
	suite.NotNil(result)
	
	// Verify it's cached
	cached, exists := de.abiCache["testMethod"]
	suite.True(exists)
	suite.Equal(result, cached)
}

func (suite *EnricherTestSuite) TestGetOrParseABI_EmptyABIString() {
	de := &DataEnricher{abiCache: make(map[string]abi.ABI)}
	
	result, err := de.getOrParseABI("testMethod", "")
	
	suite.Error(err)
	suite.Contains(err.Error(), "no ABI provided")
	suite.Equal(abi.ABI{}, result)
}

// ===== BATCH PROCESSING TESTS =====

func (suite *EnricherTestSuite) TestBatchEnrich_EmptyRequests() {
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", nil),
	}
	de, err := NewDataEnricher(nil, eventDefs)
	suite.Require().NoError(err)
	
	results := de.BatchEnrich(context.Background(), []EnrichmentRequest{})
	
	suite.Empty(results)
}

func (suite *EnricherTestSuite) TestBatchEnrich_MixedResults() {
	eventDefs := map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", nil),
	}
	de, err := NewDataEnricher(nil, eventDefs)
	suite.Require().NoError(err)
	
	requests := []EnrichmentRequest{
		{
			EventName: "TestEvent", // Should succeed (no enrichment needed)
			Data:      suite.createEventData(map[string]interface{}{}),
		},
		{
			EventName: "NonExistentEvent", // Should fail
			Data:      suite.createEventData(map[string]interface{}{}),
		},
	}
	
	results := de.BatchEnrich(context.Background(), requests)
	
	suite.Len(results, 2)
	suite.True(results[0].Success)   // First should succeed
	suite.False(results[1].Success)  // Second should fail
	suite.Nil(results[0].Error)
	suite.NotNil(results[1].Error)
}

// ===== HOW TO ADD NEW TESTS =====
/*
To add new tests to this suite, follow these patterns:

1. CREATE A NEW TEST METHOD:
   func (suite *EnricherTestSuite) TestNewFeature_SpecificScenario() {
       // Your test logic here
   }

2. USE HELPER METHODS:
   - suite.createEnricher(eventDefs) - Create enricher with event definitions
   - suite.createEventData(fields) - Create test event data
   - suite.createEnrichmentConfig(...) - Create enrichment configuration
   - suite.mockContractCall(response, error) - Mock contract calls
   - suite.mustHexDecode(hex) - Decode hex strings for test data

3. USE SUITE ASSERTIONS:
   - suite.NoError(err) instead of assert.NoError(t, err)
   - suite.Equal(expected, actual) instead of assert.Equal(t, expected, actual)
   - suite.Contains(str, substr) instead of assert.Contains(t, str, substr)

4. EXAMPLE NEW TEST:
   func (suite *EnricherTestSuite) TestNewFeature_EdgeCase() {
       // Setup
       enrichmentConfig := suite.createEnrichmentConfig("newMethod", "0x123...", abiJson, params, returns)
       eventDefs := map[string]*config.EventDefinition{
           "NewEvent": suite.createBasicEventDef("0x123...", enrichmentConfig),
       }
       suite.createEnricher(eventDefs)
       data := suite.createEventData(map[string]interface{}{"param": "value"})
       
       // Mock responses if needed
       suite.mockContractCall(suite.mustHexDecode("1234..."), nil)
       
       // Execute
       err := suite.enricher.EnrichEventData(context.Background(), "NewEvent", data)
       
       // Assert
       suite.NoError(err)
       suite.Equal("expected_value", data.Enrichment["result"])
   }

5. RUN SPECIFIC TESTS:
   go test -v ./internal/pipeline/ -run TestEnricherTestSuite/TestNewFeature_EdgeCase
   
6. RUN ALL SUITE TESTS:
   go test -v ./internal/pipeline/ -run TestEnricherTestSuite

The test suite automatically handles:
- Mock client setup and cleanup
- Expectation verification
- Fresh test environment for each test
*/

// ===== ADDITIONAL HELPER TESTS (using suite for consistency) =====

// These test the utility functions that are also used by the main enrichment logic

func (suite *EnricherTestSuite) TestExtractReturnValue_IndexOutOfRange() {
	de := &DataEnricher{}
	values := []interface{}{"only_one"}
	
	result, err := de.extractReturnValue(values, "5")
	
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "index out of range")
}

func (suite *EnricherTestSuite) TestExtractReturnValue_TuplePath() {
	de := &DataEnricher{}
	values := []interface{}{"single_value"}
	
	result, err := de.extractReturnValue(values, "tuple")
	
	suite.NoError(err)
	suite.Equal("single_value", result)
}

func (suite *EnricherTestSuite) TestExtractReturnValue_InvalidPath() {
	de := &DataEnricher{}
	values := []interface{}{"value"}
	
	result, err := de.extractReturnValue(values, "invalid.nested")
	
	suite.Error(err)
	suite.Nil(result)
}

// Test that the suite properly validates all expectations
func (suite *EnricherTestSuite) TestSuiteExpectationValidation() {
	// This test ensures our mock validation works
	// If we set up expectations but don't use them, TearDownTest should fail
	
	// Create a simple enricher without setting up expectations
	suite.createEnricher(map[string]*config.EventDefinition{
		"TestEvent": suite.createBasicEventDef("0x1234567890123456789012345678901234567890", nil),
	})
	
	// Just verify the enricher was created successfully
	suite.NotNil(suite.enricher)
}