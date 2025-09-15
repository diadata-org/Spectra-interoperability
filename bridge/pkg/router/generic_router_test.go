package router

import (
	"testing"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
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