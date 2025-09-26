package pipeline

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// DataExtractor extracts data from event logs based on configuration
type DataExtractor struct {
	eventDefs map[string]*config.EventDefinition
	abiCache  map[string]abi.Event
}

// NewDataExtractor creates a new data extractor
func NewDataExtractor(eventDefs map[string]*config.EventDefinition) (*DataExtractor, error) {
	extractor := &DataExtractor{
		eventDefs: eventDefs,
		abiCache:  make(map[string]abi.Event),
	}
	
	for eventName, def := range eventDefs {
		event, err := parseEventABI(def.ABI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ABI for event %s: %w", eventName, err)
		}
		extractor.abiCache[eventName] = event
	}
	
	return extractor, nil
}

// ExtractEventData extracts data from a raw log based on event definition
func (de *DataExtractor) ExtractEventData(eventName string, log types.Log) (*config.ExtractedData, error) {
	eventDef, exists := de.eventDefs[eventName]
	if !exists {
		return nil, fmt.Errorf("event definition not found: %s", eventName)
	}
	
	eventABI, exists := de.abiCache[eventName]
	if !exists {
		return nil, fmt.Errorf("event ABI not found in cache: %s", eventName)
	}
	
	indexedData := make(map[string]interface{})
	if err := de.extractIndexedData(&eventABI, log.Topics, indexedData); err != nil {
		return nil, fmt.Errorf("failed to extract indexed data: %w", err)
	}
	
	nonIndexedData := make(map[string]interface{})
	if len(log.Data) > 0 {
		if err := de.extractNonIndexedData(&eventABI, log.Data, nonIndexedData); err != nil {
			return nil, fmt.Errorf("failed to extract non-indexed data: %w", err)
		}
	}
	
	allData := make(map[string]interface{})
	for k, v := range indexedData {
		allData[k] = v
	}
	for k, v := range nonIndexedData {
		allData[k] = v
	}
	
	eventData := make(map[string]interface{})
	for fieldName, extractPath := range eventDef.DataExtraction {
		value, err := de.extractValue(allData, log, extractPath)
		if err != nil {
			return nil, fmt.Errorf("failed to extract field %s: %w", fieldName, err)
		}
		eventData[fieldName] = value
	}
	
	eventData["_contract"] = log.Address.Hex()
	eventData["_blockNumber"] = log.BlockNumber
	eventData["_txHash"] = log.TxHash.Hex()
	eventData["_logIndex"] = log.Index
	
	return &config.ExtractedData{
		Event: eventData,
	}, nil
}

// extractIndexedData extracts indexed parameters from event topics
func (de *DataExtractor) extractIndexedData(event *abi.Event, topics []common.Hash, output map[string]interface{}) error {
	if len(topics) == 0 {
		return fmt.Errorf("no topics in log")
	}
	
	topicIndex := 1
	
	for _, input := range event.Inputs {
		if !input.Indexed {
			continue
		}
		
		if topicIndex >= len(topics) {
			return fmt.Errorf("not enough topics for indexed parameter %s", input.Name)
		}
		
		value, err := de.decodeIndexedValue(&input, topics[topicIndex])
		if err != nil {
			return fmt.Errorf("failed to decode indexed parameter %s: %w", input.Name, err)
		}
		
		output[input.Name] = value
		topicIndex++
	}
	
	return nil
}

// extractNonIndexedData extracts non-indexed parameters from event data
func (de *DataExtractor) extractNonIndexedData(event *abi.Event, data []byte, output map[string]interface{}) error {
	var nonIndexedArgs abi.Arguments
	for _, input := range event.Inputs {
		if !input.Indexed {
			nonIndexedArgs = append(nonIndexedArgs, input)
		}
	}
	
	if len(nonIndexedArgs) == 0 {
		return nil
	}
	
	values, err := nonIndexedArgs.Unpack(data)
	if err != nil {
		return fmt.Errorf("failed to unpack data: %w", err)
	}
	
	for i, arg := range nonIndexedArgs {
		if i < len(values) {
			output[arg.Name] = values[i]
		}
	}
	
	return nil
}

// decodeIndexedValue decodes an indexed parameter value
func (de *DataExtractor) decodeIndexedValue(arg *abi.Argument, topic common.Hash) (interface{}, error) {
	switch arg.Type.T {
	case abi.StringTy, abi.BytesTy, abi.SliceTy, abi.ArrayTy:
		return topic.Hex(), nil
	case abi.AddressTy:
		return common.HexToAddress(topic.Hex()), nil
	case abi.IntTy, abi.UintTy:
		return new(big.Int).SetBytes(topic[:]), nil
	case abi.BoolTy:
		return topic[31] != 0, nil
	case abi.FixedBytesTy:
		return topic[:arg.Type.Size], nil
	default:
		return topic, nil
	}
}

// extractValue extracts a value using a path expression
func (de *DataExtractor) extractValue(data map[string]interface{}, log types.Log, path string) (interface{}, error) {
	if strings.HasPrefix(path, "topics[") {
		return de.extractTopicValue(log.Topics, path)
	}
	
	if strings.HasPrefix(path, "data[") {
		return de.extractDataValue(data, path)
	}
	
	if value, exists := data[path]; exists {
		return value, nil
	}
	
	return nil, fmt.Errorf("path not found: %s", path)
}

// extractTopicValue extracts a value from topics array
func (de *DataExtractor) extractTopicValue(topics []common.Hash, path string) (interface{}, error) {
	re := regexp.MustCompile(`topics\[(\d+)\]`)
	matches := re.FindStringSubmatch(path)
	if len(matches) != 2 {
		return nil, fmt.Errorf("invalid topic path: %s", path)
	}
	
	index, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, fmt.Errorf("invalid topic index: %s", matches[1])
	}
	
	if index >= len(topics) {
		return nil, fmt.Errorf("topic index out of range: %d", index)
	}
	
	return topics[index], nil
}

// extractDataValue extracts a value from data map
func (de *DataExtractor) extractDataValue(data map[string]interface{}, path string) (interface{}, error) {
	if strings.Contains(path, "[") {
		re := regexp.MustCompile(`data\[(\d+)\]`)
		matches := re.FindStringSubmatch(path)
		if len(matches) != 2 {
			return nil, fmt.Errorf("invalid data path: %s", path)
		}
		
		return nil, fmt.Errorf("array access not yet implemented: %s", path)
	}
	
	parts := strings.Split(path, ".")
	if len(parts) > 1 && parts[0] == "data" {
		fieldName := strings.Join(parts[1:], ".")
		if value, exists := data[fieldName]; exists {
			return value, nil
		}
	}
	
	return nil, fmt.Errorf("data path not found: %s", path)
}

// parseEventABI parses an event ABI string
func parseEventABI(abiStr string) (abi.Event, error) {
	contractABI := fmt.Sprintf(`[%s]`, abiStr)
	
	parsedABI, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return abi.Event{}, fmt.Errorf("failed to parse ABI: %w", err)
	}
	
	for _, event := range parsedABI.Events {
		return event, nil
	}
	
	return abi.Event{}, fmt.Errorf("no event found in ABI")
}

// MatchEventDefinition matches a log to an event definition by signature
func (de *DataExtractor) MatchEventDefinition(log types.Log) (string, *config.EventDefinition, error) {
	if len(log.Topics) == 0 {
		return "", nil, fmt.Errorf("log has no topics")
	}
	
	eventSig := log.Topics[0]
	
	for eventName, def := range de.eventDefs {
		if !strings.EqualFold(def.Contract, log.Address.Hex()) {
			continue
		}
		
		event, exists := de.abiCache[eventName]
		if !exists {
			continue
		}
		
		if event.ID == eventSig {
			return eventName, def, nil
		}
	}
	
	return "", nil, fmt.Errorf("no matching event definition for signature %s from contract %s", 
		eventSig.Hex(), log.Address.Hex())
}

// ConvertToEventData converts extracted data to bridge event data type
func (de *DataExtractor) ConvertToEventData(eventName string, extracted *config.ExtractedData, log types.Log) *bridgeTypes.EventData {
	eventData := &bridgeTypes.EventData{
		EventName:       eventName,
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Data:            extracted.Event,
		Raw:             log,
	}
	
	if intentHash, ok := extracted.Event["intentHash"].(common.Hash); ok {
		eventData.IntentHash = [32]byte(intentHash)
	}
	
	if symbol, ok := extracted.Event["symbol"].(string); ok {
		eventData.Symbol = symbol
	}
	
	if price, ok := extracted.Event["price"].(*big.Int); ok {
		eventData.Price = price
	}
	
	if timestamp, ok := extracted.Event["timestamp"].(*big.Int); ok {
		eventData.Timestamp = timestamp
	}
	
	if signer, ok := extracted.Event["signer"].(common.Address); ok {
		eventData.Signer = signer
	}
	
	return eventData
}

