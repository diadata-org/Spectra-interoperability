package router

import (
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/stretchr/testify/assert"
)

func TestNewGenericRouter(t *testing.T) {
	cfg := &config.RouterConfig{ID: "test-router"}
	router, err := NewGenericRouter(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, router)
	assert.Equal(t, "test-router", router.ID())
	assert.NotNil(t, router.triggerEvents)
}

func TestShouldRoute_Disabled(t *testing.T) {
	cfg := &config.RouterConfig{ID: "test-router", Enabled: false}
	router, _ := NewGenericRouter(cfg)
	should, reason := router.ShouldRoute("any-event", nil)
	assert.False(t, should)
	assert.Equal(t, "router disabled", reason)
}

func TestShouldRoute_EventNotinTrigger(t *testing.T) {
	cfg := &config.RouterConfig{
		ID:      "test-router",
		Enabled: true,
		Triggers: config.RouterTriggers{
			Events: []string{"event-A"},
		},
	}
	router, _ := NewGenericRouter(cfg)
	should, reason := router.ShouldRoute("event-B", nil)
	assert.False(t, should)
	assert.Equal(t, "event event-B not in trigger list", reason)
}

func TestShouldRoute_ConditionsMet(t *testing.T) {
	cfg := &config.RouterConfig{
		ID:      "test-router",
		Enabled: true,
		Triggers: config.RouterTriggers{
			Events: []string{"event-A"},
			Conditions: []config.TriggerCondition{
				{Field: "${event.symbol}", Operator: "eq", Value: "ETH"},
			},
		},
	}
	data := &config.ExtractedData{
		Event: map[string]interface{}{"symbol": "ETH"},
	}
	router, _ := NewGenericRouter(cfg)
	should, reason := router.ShouldRoute("event-A", data)
	assert.True(t, should)
	assert.Equal(t, "all conditions met", reason)
}

func TestShouldRoute_ConditionFails(t *testing.T) {
	cfg := &config.RouterConfig{
		ID:      "test-router",
		Enabled: true,
		Triggers: config.RouterTriggers{
			Events: []string{"event-A"},
			Conditions: []config.TriggerCondition{
				{Field: "${event.symbol}", Operator: "eq", Value: "BTC"},
			},
		},
	}
	data := &config.ExtractedData{
		Event: map[string]interface{}{"symbol": "ETH"},
	}
	router, _ := NewGenericRouter(cfg)
	should, reason := router.ShouldRoute("event-A", data)
	assert.False(t, should)
	assert.Equal(t, "condition failed: ${event.symbol} eq BTC", reason)
}

func TestEvaluateCondition_Operators(t *testing.T) {
	cfg := &config.RouterConfig{}
	router, _ := NewGenericRouter(cfg)
	data := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol":     "ETH",
			"price":      3000,
			"price_str":  "3000",
			"source":     "exchange-A",
			"tags":       []interface{}{"tag1", "tag2"},
		},
	}

	testCases := []struct {
		name      string
		condition config.TriggerCondition
		expected  bool
	}{
		// Standard comparisons
		{"eq_true", config.TriggerCondition{Field: "${event.symbol}", Operator: "eq", Value: "ETH"}, true},
		{"eq_false", config.TriggerCondition{Field: "${event.symbol}", Operator: "eq", Value: "BTC"}, false},
		{"ne_true", config.TriggerCondition{Field: "${event.symbol}", Operator: "ne", Value: "BTC"}, true},
		{"ne_false", config.TriggerCondition{Field: "${event.symbol}", Operator: "ne", Value: "ETH"}, false},
		{"gt_true", config.TriggerCondition{Field: "${event.price}", Operator: "gt", Value: 2000}, true},
		{"gt_false", config.TriggerCondition{Field: "${event.price}", Operator: "gt", Value: 4000}, false},
		{"lt_true", config.TriggerCondition{Field: "${event.price}", Operator: "lt", Value: 4000}, true},
		{"lt_false", config.TriggerCondition{Field: "${event.price}", Operator: "lt", Value: 2000}, false},
		{"gte_true", config.TriggerCondition{Field: "${event.price}", Operator: "gte", Value: 3000}, true},
		{"gte_false", config.TriggerCondition{Field: "${event.price}", Operator: "gte", Value: 4000}, false},
		{"lte_true", config.TriggerCondition{Field: "${event.price}", Operator: "lte", Value: 3000}, true},
		{"lte_false", config.TriggerCondition{Field: "${event.price}", Operator: "lte", Value: 2000}, false},
		{"contains_true", config.TriggerCondition{Field: "${event.source}", Operator: "contains", Value: "exchange"}, true},
		{"contains_false", config.TriggerCondition{Field: "${event.source}", Operator: "contains", Value: "oracle"}, false},
		{"in_true", config.TriggerCondition{Field: "${event.symbol}", Operator: "in", Value: []interface{}{"ETH", "BTC"}}, true},
		{"in_false", config.TriggerCondition{Field: "${event.symbol}", Operator: "in", Value: []interface{}{"BTC", "XRP"}}, false},

		// Type-aware numeric comparisons
		{"eq_num_str_true", config.TriggerCondition{Field: "${event.price}", Operator: "eq", Value: "3000"}, true},
		{"eq_num_str_false", config.TriggerCondition{Field: "${event.price}", Operator: "eq", Value: "3001"}, false},
		{"gt_num_str_true", config.TriggerCondition{Field: "${event.price}", Operator: "gt", Value: "2000"}, true},
		{"lt_num_str_true", config.TriggerCondition{Field: "${event.price}", Operator: "lt", Value: "4000"}, true},
		{"eq_str_num_true", config.TriggerCondition{Field: "${event.price_str}", Operator: "eq", Value: 3000}, true},
		{"in_num_str", config.TriggerCondition{Field: "${event.price}", Operator: "in", Value: []interface{}{"1000", 3000, "5000"}}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := router.evaluateCondition(tc.condition, data)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetFieldValue(t *testing.T) {
	router := &GenericRouter{}
	data := &config.ExtractedData{
		Event:      map[string]interface{}{"symbol": "ETH"},
		Enrichment: map[string]interface{}{"usd_price": 3000},
		Processed:  map[string]interface{}{"deep": map[string]interface{}{"value": "foo"}},
	}

	val, err := router.getFieldValue("${event.symbol}", data)
	assert.NoError(t, err)
	assert.Equal(t, "ETH", val)

	val, err = router.getFieldValue("${enrichment.usd_price}", data)
	assert.NoError(t, err)
	assert.Equal(t, 3000, val)

	val, err = router.getFieldValue("${processed.deep.value}", data)
	assert.NoError(t, err)
	assert.Equal(t, "foo", val)

	_, err = router.getFieldValue("${event.nonexistent}", data)
	assert.Error(t, err)

	_, err = router.getFieldValue("event.symbol", data)
	assert.Error(t, err)
}

func TestGetDestinations(t *testing.T) {
	cfg := &config.RouterConfig{
		Destinations: []config.RouterDestination{
			{ChainID: 1, Condition: ""},
			{ChainID: 2, Condition: "true"},
			{ChainID: 3, Condition: "false"},
		},
	}
	router, _ := NewGenericRouter(cfg)
	dests := router.GetDestinations(nil)
	assert.Len(t, dests, 2)
	assert.Equal(t, int64(1), dests[0].ChainID)
	assert.Equal(t, int64(2), dests[1].ChainID)
}

func TestGetSymbolFromData_NilHandling(t *testing.T) {
	gr := &GenericRouter{}

	tests := []struct {
		name     string
		data     *config.ExtractedData
		expected string
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: "unknown",
		},
		{
			name: "nil event and enrichment",
			data: &config.ExtractedData{
				Event:      nil,
				Enrichment: nil,
			},
			expected: "unknown",
		},
		{
			name: "empty event and enrichment maps",
			data: &config.ExtractedData{
				Event:      make(map[string]interface{}),
				Enrichment: make(map[string]interface{}),
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gr.GetSymbolFromData(tt.data)
			if result != tt.expected {
				t.Errorf("getSymbolFromData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetSymbolFromData_SymbolExtraction(t *testing.T) {
	gr := &GenericRouter{}

	tests := []struct {
		name     string
		data     *config.ExtractedData
		expected string
	}{
		{
			name: "symbol from enrichment direct",
			data: &config.ExtractedData{
				Event: map[string]interface{}{},
				Enrichment: map[string]interface{}{
					"symbol": "ETH/USD",
				},
			},
			expected: "ETH/USD",
		},
		{
			name: "symbol from fullIntent map",
			data: &config.ExtractedData{
				Event: map[string]interface{}{},
				Enrichment: map[string]interface{}{
					"fullIntent": map[string]interface{}{
						"symbol": "SOL/USD",
					},
				},
			},
			expected: "SOL/USD",
		},
		{
			name: "empty symbol string fallback",
			data: &config.ExtractedData{
				Event: map[string]interface{}{
					"symbol": "",
				},
				Enrichment: map[string]interface{}{
					"symbol": "AVAX/USD",
				},
			},
			expected: "AVAX/USD",
		},
		{
			name: "no enrichment fallback",
			data: &config.ExtractedData{
				Event: map[string]interface{}{
					"symbol": "BTC/USD",
				},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gr.GetSymbolFromData(tt.data)
			if result != tt.expected {
				t.Errorf("getSymbolFromData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetSymbolFromData_StructuredType(t *testing.T) {
	gr := &GenericRouter{}

	type MockOracleIntent struct {
		Symbol    string
		Price     uint64
		Timestamp uint64
	}

	tests := []struct {
		name     string
		data     *config.ExtractedData
		expected string
	}{
		{
			name: "symbol from structured type pointer",
			data: &config.ExtractedData{
				Event: map[string]interface{}{},
				Enrichment: map[string]interface{}{
					"fullIntent": &MockOracleIntent{
						Symbol:    "DOT/USD",
						Price:     12345,
						Timestamp: 1234567890,
					},
				},
			},
			expected: "DOT/USD",
		},
		{
			name: "symbol from structured type value",
			data: &config.ExtractedData{
				Event: map[string]interface{}{},
				Enrichment: map[string]interface{}{
					"fullIntent": MockOracleIntent{
						Symbol:    "LINK/USD",
						Price:     12345,
						Timestamp: 1234567890,
					},
				},
			},
			expected: "LINK/USD",
		},
		{
			name: "invalid structured type without Symbol field",
			data: &config.ExtractedData{
				Event: map[string]interface{}{},
				Enrichment: map[string]interface{}{
					"fullIntent": struct {
						Price     uint64
						Timestamp uint64
					}{
						Price:     12345,
						Timestamp: 1234567890,
					},
				},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gr.GetSymbolFromData(tt.data)
			if result != tt.expected {
				t.Errorf("getSymbolFromData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckTimeThreshold(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	dest := config.RouterDestination{
		ChainID:  421614,
		Contract: "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
	}

	data := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489",
		},
		Enrichment: map[string]interface{}{
			"symbol": "BTC/USD",
		},
	}

	tests := []struct {
		name           string
		timeThreshold  string
		setupTime      *time.Time
		expected       bool
		description    string
	}{
		{
			name:          "first time no threshold",
			timeThreshold: "0s",
			setupTime:     nil,
			expected:      true,
			description:   "first time should always return true",
		},
		{
			name:          "first time with threshold",
			timeThreshold: "2m",
			setupTime:     nil,
			expected:      true,
			description:   "first time should always return true even with threshold",
		},
		{
			name:          "threshold not met",
			timeThreshold: "2m",
			setupTime:     timePtr(time.Now().Add(-1 * time.Minute)),
			expected:      false,
			description:   "should return false when threshold not met",
		},
		{
			name:          "threshold exactly met",
			timeThreshold: "2m",
			setupTime:     timePtr(time.Now().Add(-2 * time.Minute)),
			expected:      true,
			description:   "should return true when threshold exactly met",
		},
		{
			name:          "threshold exceeded",
			timeThreshold: "2m",
			setupTime:     timePtr(time.Now().Add(-3 * time.Minute)),
			expected:      true,
			description:   "should return true when threshold exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dur, _ := time.ParseDuration(tt.timeThreshold)
			dest.TimeThreshold = config.Duration(dur)

			gr.destinationTimes = make(map[string]time.Time)
			if tt.setupTime != nil {
				destKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD"
				gr.destinationTimes[destKey] = *tt.setupTime
			}

			result := gr.checkTimeThreshold(dest, data)
			if result != tt.expected {
				t.Errorf("checkTimeThreshold() = %v, want %v - %s", result, tt.expected, tt.description)
			}
		})
	}
}

func TestUpdateDestinationTime(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	dest := config.RouterDestination{
		ChainID:  421614,
		Contract: "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
	}

	symbol := "BTC/USD"
	beforeUpdate := time.Now()

	gr.UpdateDestinationTime(dest, symbol)

	destKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD"
	updatedTime, exists := gr.destinationTimes[destKey]

	if !exists {
		t.Fatal("UpdateDestinationTime() did not create entry in destinationTimes map")
	}

	if updatedTime.Before(beforeUpdate) {
		t.Errorf("UpdateDestinationTime() set time %v before call time %v", updatedTime, beforeUpdate)
	}

	if time.Since(updatedTime) > time.Second {
		t.Errorf("UpdateDestinationTime() set time %v too far in past", updatedTime)
	}
}

func TestMultipleSymbolTimeTracking(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	dest := config.RouterDestination{
		ChainID:  421614,
		Contract: "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
	}
	dur, _ := time.ParseDuration("2m")
	dest.TimeThreshold = config.Duration(dur)

	btcData := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489",
		},
		Enrichment: map[string]interface{}{
			"symbol": "BTC/USD",
		},
	}

	ethData := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0x0b43555ace6b39aae1b894097d0a9fc17f504c62fea598fa206cc6f5088e6e45",
		},
		Enrichment: map[string]interface{}{
			"symbol": "ETH/USD",
		},
	}

	// First BTC update should pass
	if !gr.checkTimeThreshold(dest, btcData) {
		t.Error("First BTC update should pass time threshold")
	}
	gr.UpdateDestinationTime(dest, "BTC/USD")

	// First ETH update should pass (different symbol)
	if !gr.checkTimeThreshold(dest, ethData) {
		t.Error("First ETH update should pass time threshold")
	}
	gr.UpdateDestinationTime(dest, "ETH/USD")

	// Immediate second BTC update should fail
	if gr.checkTimeThreshold(dest, btcData) {
		t.Error("Second immediate BTC update should fail time threshold")
	}

	// Immediate second ETH update should fail
	if gr.checkTimeThreshold(dest, ethData) {
		t.Error("Second immediate ETH update should fail time threshold")
	}

	// Simulate time passage for BTC only
	btcKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD"
	gr.destinationTimes[btcKey] = time.Now().Add(-3 * time.Minute)

	// BTC should now pass
	if !gr.checkTimeThreshold(dest, btcData) {
		t.Error("BTC update after time threshold should pass")
	}

	// ETH should still fail
	if gr.checkTimeThreshold(dest, ethData) {
		t.Error("ETH update should still fail time threshold")
	}
}

func TestGetSymbolFromData_RealEnrichmentFormat(t *testing.T) {
	gr := &GenericRouter{}

	// Simulate the actual enrichment data format from logs
	type RealOracleIntent struct {
		IntentType string
		Version    string
		ChainId    uint64
		Nonce      uint64
		Expiry     uint64
		Symbol     string
		Price      uint64
		Timestamp  uint64
		Source     string
		Signature  []byte
		Signer     string
	}

	tests := []struct {
		name     string
		data     *config.ExtractedData
		expected string
	}{
		{
			name: "real enrichment format with BTC/USD",
			data: &config.ExtractedData{
				Event: map[string]interface{}{
					"symbol": "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489", // hex encoded
				},
				Enrichment: map[string]interface{}{
					"fullIntent": RealOracleIntent{
						IntentType: "OracleUpdate",
						Version:    "1.0",
						ChainId:    100640,
						Symbol:     "BTC/USD",
						Price:      11249198000000,
						Timestamp:  1758771691,
						Source:     "DIA Oracle",
					},
				},
			},
			expected: "BTC/USD",
		},
		{
			name: "real enrichment format with ETH/USD",
			data: &config.ExtractedData{
				Event: map[string]interface{}{
					"symbol": "0x0b43555ace6b39aae1b894097d0a9fc17f504c62fea598fa206cc6f5088e6e45", // hex encoded
				},
				Enrichment: map[string]interface{}{
					"fullIntent": RealOracleIntent{
						IntentType: "OracleUpdate",
						Version:    "1.0",
						ChainId:    100640,
						Symbol:     "ETH/USD",
						Price:      405549620091,
						Timestamp:  1758771693,
						Source:     "DIA Oracle",
					},
				},
			},
			expected: "ETH/USD",
		},
		{
			name: "enrichment not populated yet",
			data: &config.ExtractedData{
				Event: map[string]interface{}{
					"symbol": "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489",
				},
				Enrichment: map[string]interface{}{},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gr.GetSymbolFromData(tt.data)
			if result != tt.expected {
				t.Errorf("getSymbolFromData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilterDestinationsByTimeThreshold(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	type TestOracleIntent struct {
		Symbol string
		Price  uint64
	}

	// Create test destinations
	destinations := []config.RouterDestination{
		{
			ChainID:       421614,
			Contract:      "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
			TimeThreshold: config.Duration(2 * time.Minute), // 2 minute threshold
		},
		{
			ChainID:       1,
			Contract:      "0x1234567890123456789012345678901234567890",
			TimeThreshold: config.Duration(0), // No threshold
		},
		{
			ChainID:       42161,
			Contract:      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			TimeThreshold: config.Duration(5 * time.Minute), // 5 minute threshold
		},
	}

	data := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": TestOracleIntent{
				Symbol: "BTC/USD",
				Price:  112492,
			},
		},
	}

	tests := []struct {
		name              string
		setupTimes        map[string]time.Time
		expectedDestCount int
		description       string
	}{
		{
			name:              "first time - all destinations returned",
			setupTimes:        map[string]time.Time{},
			expectedDestCount: 3,
			description:       "all destinations should be returned on first routing",
		},
		{
			name: "threshold not met for some destinations",
			setupTimes: map[string]time.Time{
				"421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD": time.Now().Add(-1 * time.Minute), // 1 min ago, 2min threshold
				"42161-0xabcdefabcdefabcdefabcdefabcdefabcdefabcd-BTC/USD":   time.Now().Add(-3 * time.Minute), // 3 min ago, 5min threshold
			},
			expectedDestCount: 1, // Only the one with no threshold
			description:       "destinations with unmet thresholds should be filtered",
		},
		{
			name: "all thresholds met",
			setupTimes: map[string]time.Time{
				"421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD": time.Now().Add(-3 * time.Minute), // 3 min ago, 2min threshold
				"42161-0xabcdefabcdefabcdefabcdefabcdefabcdefabcd-BTC/USD":   time.Now().Add(-6 * time.Minute), // 6 min ago, 5min threshold
			},
			expectedDestCount: 3,
			description:       "all destinations should be returned when thresholds are met",
		},
		{
			name: "mixed threshold states",
			setupTimes: map[string]time.Time{
				"421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD": time.Now().Add(-3 * time.Minute), // 3 min ago, 2min threshold - PASS
				"42161-0xabcdefabcdefabcdefabcdefabcdefabcdefabcd-BTC/USD":   time.Now().Add(-2 * time.Minute), // 2 min ago, 5min threshold - FAIL
			},
			expectedDestCount: 2, // First destination + no-threshold destination
			description:       "mixed states should return correct subset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset and setup destination times
			gr.destinationTimes = make(map[string]time.Time)
			for key, timeValue := range tt.setupTimes {
				gr.destinationTimes[key] = timeValue
			}

			// Filter destinations
			filtered := gr.FilterDestinationsByTimeThreshold(destinations, data)

			if len(filtered) != tt.expectedDestCount {
				t.Errorf("FilterDestinationsByTimeThreshold() returned %d destinations, want %d - %s",
					len(filtered), tt.expectedDestCount, tt.description)
			}

			// Verify no-threshold destination is always included
			hasNoThreshold := false
			for _, dest := range filtered {
				if dest.TimeThreshold.Duration() == 0 {
					hasNoThreshold = true
					break
				}
			}
			if tt.expectedDestCount > 0 && !hasNoThreshold && tt.name != "threshold not met for some destinations" {
				t.Error("No-threshold destination should always be included when destinations are expected")
			}
		})
	}
}

func TestFilterDestinationsByTimeThreshold_DifferentSymbols(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	type TestOracleIntent struct {
		Symbol string
		Price  uint64
	}

	dest := config.RouterDestination{
		ChainID:       421614,
		Contract:      "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
		TimeThreshold: config.Duration(2 * time.Minute),
	}

	// Setup: BTC/USD was updated 1 minute ago (should be blocked)
	btcKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD"
	gr.destinationTimes[btcKey] = time.Now().Add(-1 * time.Minute)

	// Test BTC/USD - should be filtered out
	btcData := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": TestOracleIntent{
				Symbol: "BTC/USD",
				Price:  112492,
			},
		},
	}

	btcFiltered := gr.FilterDestinationsByTimeThreshold([]config.RouterDestination{dest}, btcData)
	if len(btcFiltered) != 0 {
		t.Error("BTC/USD destination should be filtered due to time threshold")
	}

	// Test ETH/USD - should pass (different symbol, no previous update)
	ethData := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": TestOracleIntent{
				Symbol: "ETH/USD",
				Price:  40555,
			},
		},
	}

	ethFiltered := gr.FilterDestinationsByTimeThreshold([]config.RouterDestination{dest}, ethData)
	if len(ethFiltered) != 1 {
		t.Error("ETH/USD destination should pass (different symbol)")
	}
}

func TestFilterDestinationsByTimeThreshold_NilData(t *testing.T) {
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	dest := config.RouterDestination{
		ChainID:       421614,
		Contract:      "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
		TimeThreshold: config.Duration(2 * time.Minute),
	}

	// Test with nil data - should pass (fallback to "unknown" symbol)
	filtered := gr.FilterDestinationsByTimeThreshold([]config.RouterDestination{dest}, nil)
	if len(filtered) != 1 {
		t.Error("FilterDestinationsByTimeThreshold with nil data should pass (first time for 'unknown' symbol)")
	}

	// Update "unknown" symbol time to recent past (within threshold)
	unknownKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-unknown"
	gr.destinationTimes[unknownKey] = time.Now().Add(-30 * time.Second) // 30 seconds ago, within 2min threshold

	// Second call with nil data should be blocked
	filtered = gr.FilterDestinationsByTimeThreshold([]config.RouterDestination{dest}, nil)
	if len(filtered) != 0 {
		t.Error("Second FilterDestinationsByTimeThreshold with nil data should be blocked")
	}
}

func TestRealWorldTimeThresholdScenario(t *testing.T) {
	// Simulate the exact issue from the transactions
	gr := &GenericRouter{
		destinationTimes: make(map[string]time.Time),
	}

	dest := config.RouterDestination{
		ChainID:  421614,
		Contract: "0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd",
	}
	dur, _ := time.ParseDuration("2m")
	dest.TimeThreshold = config.Duration(dur)

	type RealOracleIntent struct {
		Symbol string
		Price  uint64
	}

	// First ETH/USD event - should pass (first time)
	ethData1 := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0x0b43555ace6b39aae1b894097d0a9fc17f504c62fea598fa206cc6f5088e6e45",
		},
		Enrichment: map[string]interface{}{
			"fullIntent": RealOracleIntent{
				Symbol: "ETH/USD",
				Price:  404459266622,
			},
		},
	}

	if !gr.checkTimeThreshold(dest, ethData1) {
		t.Error("First ETH/USD event should pass time threshold")
	}
	gr.UpdateDestinationTime(dest, "ETH/USD")

	// Second ETH/USD event 5 seconds later - should be blocked
	ethData2 := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0x0b43555ace6b39aae1b894097d0a9fc17f504c62fea598fa206cc6f5088e6e45",
		},
		Enrichment: map[string]interface{}{
			"fullIntent": RealOracleIntent{
				Symbol: "ETH/USD",
				Price:  404584776014,
			},
		},
	}

	if gr.checkTimeThreshold(dest, ethData2) {
		t.Error("Second ETH/USD event (5 seconds later) should be blocked by time threshold")
	}

	// BTC/USD event should pass (different symbol)
	btcData := &config.ExtractedData{
		Event: map[string]interface{}{
			"symbol": "0xee62665949c883f9e0f6f002eac32e00bd59dfe6c34e92a91c37d6a8322d6489",
		},
		Enrichment: map[string]interface{}{
			"fullIntent": RealOracleIntent{
				Symbol: "BTC/USD",
				Price:  11249249402641,
			},
		},
	}

	if !gr.checkTimeThreshold(dest, btcData) {
		t.Error("BTC/USD event should pass time threshold (different symbol from ETH/USD)")
	}

	// Verify separate tracking
	ethKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-ETH/USD"
	btcKey := "421614-0x5e66Aba065Dc38e64D7a9D55c3F0c2CbDab2E2fd-BTC/USD"

	if _, exists := gr.destinationTimes[ethKey]; !exists {
		t.Error("ETH/USD destination time should be tracked")
	}

	// BTC hasn't been updated yet
	if _, exists := gr.destinationTimes[btcKey]; exists {
		t.Error("BTC/USD destination time should not be tracked until after UpdateDestinationTime")
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
