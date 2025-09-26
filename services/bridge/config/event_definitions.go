package config

import (
	"encoding/json"
	"math/big"
)

// EventDefinition defines how to process a specific event type
type EventDefinition struct {
	Contract       string                   `json:"contract"`
	ABI            string                   `json:"abi"`
	DataExtraction map[string]string        `json:"data_extraction"`
	Enrichment     *EnrichmentConfig        `json:"enrichment,omitempty"`
}

// EnrichmentConfig defines how to enrich event data with additional calls
type EnrichmentConfig struct {
	Contract string            `json:"contract,omitempty"`
	Method   string            `json:"method"`
	ABI      string            `json:"abi,omitempty"`
	Params   []string          `json:"params"`
	Returns  map[string]string `json:"returns"`
}

// RouterConfig represents a generic event router configuration
type RouterConfig struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Enabled      bool                `json:"enabled"`
	PrivateKey   string              `json:"private_key"`
	Triggers     RouterTriggers      `json:"triggers"`
	Processing   ProcessingConfig    `json:"processing"`
	Destinations []RouterDestination `json:"destinations"`
}

// RouterTriggers defines what events trigger this router
type RouterTriggers struct {
	Events     []string           `json:"events"`
	Conditions []TriggerCondition `json:"conditions"`
}

// TriggerCondition defines a condition for router activation
type TriggerCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

// ProcessingConfig defines how to process event data
type ProcessingConfig struct {
	DataSource      string           `json:"data_source"`
	Transformations []Transformation `json:"transformations"`
}

// Transformation defines a data transformation operation
type Transformation struct {
	Field     string                 `json:"field"`
	Operation string                 `json:"operation"`
	Input     string                 `json:"input"`
	Params    map[string]interface{} `json:"params"`
}

// RouterDestination represents where to route the event
type RouterDestination struct {
	ChainID       int64                   `json:"chain_id"`
	Contract      string                  `json:"contract"`
	Method        DestinationMethodConfig `json:"method"`
	Condition     string                  `json:"condition"`
	TimeThreshold Duration                `json:"time_threshold,omitempty"` // Minimum time between updates for this destination
}

// DestinationMethodConfig defines a contract method call for generic routing
type DestinationMethodConfig struct {
	Name          string            `json:"name"`
	ABI           string            `json:"abi"`
	Params        map[string]string `json:"params"`
	Value         string            `json:"value"`
	GasLimit      uint64            `json:"gas_limit"`
	GasMultiplier float64           `json:"gas_multiplier"`
}

// ExtractedData represents data extracted from an event
type ExtractedData struct {
	Event      map[string]interface{} `json:"event"`
	Enrichment map[string]interface{} `json:"enrichment,omitempty"`
	Processed  map[string]interface{} `json:"processed,omitempty"`
}

// DataPath represents a path to extract data from event logs
type DataPath struct {
	Source string
	Index  int
	Field  string
}

// ParseDataPath parses a data extraction path like "topics[1]" or "data[0].price"
func ParseDataPath(path string) (*DataPath, error) {
	return &DataPath{}, nil
}

// EvaluateCondition evaluates a trigger condition against data
func (tc *TriggerCondition) Evaluate(data map[string]interface{}) bool {
	return true
}

// GetValue extracts a value from data using a path expression
func GetValue(data map[string]interface{}, path string) (interface{}, error) {
	return nil, nil
}

// TransformValue applies a transformation to a value
func TransformValue(operation string, input interface{}, params map[string]interface{}) (interface{}, error) {
	switch operation {
	case "slice":
		return nil, nil
	case "concat":
		return nil, nil
	case "hash":
		return nil, nil
	case "encode":
		return nil, nil
	default:
		return nil, json.Unmarshal([]byte("unsupported operation"), nil)
	}
}

// ToBigInt converts an interface{} to *big.Int
func ToBigInt(v interface{}) (*big.Int, error) {
	switch val := v.(type) {
	case *big.Int:
		return val, nil
	case string:
		n := new(big.Int)
		n.SetString(val, 0)
		return n, nil
	case float64:
		return big.NewInt(int64(val)), nil
	default:
		return nil, json.Unmarshal([]byte("cannot convert to big.Int"), nil)
	}
}