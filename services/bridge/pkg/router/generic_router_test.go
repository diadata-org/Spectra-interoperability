package router

import (
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/stretchr/testify/assert"
)

func TestGenericRouter_ShouldRoute(t *testing.T) {
	// Setup config similar to user's report
	cfg := &config.RouterConfig{
		ID:      "test_router",
		Enabled: true,
		Triggers: config.RouterTriggers{
			Events: []string{"IntentRegistered"},
			Conditions: []config.TriggerCondition{
				{
					Field:    "${enrichment.fullIntent.Symbol}",
					Operator: "eq",
					Value:    "ETH/USD",
				},
			},
		},
	}

	router, err := NewGenericRouter(cfg)
	assert.NoError(t, err)

	// Case 1: Matching event (ETH/USD) -> Should pass
	dataMatch := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": map[string]interface{}{
				"Symbol": "ETH/USD",
			},
		},
	}
	shouldRoute, reason := router.ShouldRoute("IntentRegistered", dataMatch)
	assert.True(t, shouldRoute, "Should route matching event")
	assert.Equal(t, "all conditions met", reason)

	// Case 2: Non-matching event (BTC/USD) -> Should fail
	dataMismatch := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": map[string]interface{}{
				"Symbol": "BTC/USD",
			},
		},
	}
	shouldRoute, reason = router.ShouldRoute("IntentRegistered", dataMismatch)
	assert.False(t, shouldRoute, "Should NOT route non-matching event")
	assert.Contains(t, reason, "condition failed")

	// Case 3: Missing symbol -> Should fail
	dataMissing := &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": map[string]interface{}{
				// No Symbol
			},
		},
	}
	shouldRoute, reason = router.ShouldRoute("IntentRegistered", dataMissing)
	assert.False(t, shouldRoute, "Should NOT route event with missing symbol")
}

func TestGetSymbolsFromConfig(t *testing.T) {
	tests := []struct {
		name            string
		routerConfig    *config.RouterConfig
		expectedSymbols []string
	}{
		{
			name: "operator 'in' with multiple symbols",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{"ETH/USD", "BTC/USD", "LINK/USD"},
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD", "BTC/USD", "LINK/USD"},
		},
		{
			name: "operator 'eq' with single symbol",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "eq",
							Value:    "ETH/USD",
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD"},
		},
		{
			name: "operator '==' with single symbol",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "==",
							Value:    "BTC/USD",
						},
					},
				},
			},
			expectedSymbols: []string{"BTC/USD"},
		},
		{
			name: "multiple conditions with different operators",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{"ETH/USD", "BTC/USD"},
						},
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "eq",
							Value:    "LINK/USD",
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD", "BTC/USD", "LINK/USD"},
		},
		{
			name: "non-symbol field condition - should be ignored",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${event.price}",
							Operator: "gt",
							Value:    1000,
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "operator 'ne' - should be ignored",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "ne",
							Value:    "ETH/USD",
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "operator '!=' - should be ignored",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "!=",
							Value:    "ETH/USD",
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "operator 'gt' - should be ignored",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "gt",
							Value:    "ETH/USD",
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "operator 'contains' - should be ignored",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "contains",
							Value:    "ETH",
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "empty value array for 'in' operator",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{},
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "empty string value for 'eq' operator",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "eq",
							Value:    "",
						},
					},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name:            "nil router config",
			routerConfig:    nil,
			expectedSymbols: []string{},
		},
		{
			name: "no conditions",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events:     []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{},
				},
			},
			expectedSymbols: []string{},
		},
		{
			name: "mixed symbol and non-symbol conditions",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{"ETH/USD", "BTC/USD"},
						},
						{
							Field:    "${event.price}",
							Operator: "gt",
							Value:    1000,
						},
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "eq",
							Value:    "LINK/USD",
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD", "BTC/USD", "LINK/USD"},
		},
		{
			name: "case sensitivity - Symbol vs symbol",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{"ETH/USD"},
						},
						{
							Field:    "${enrichment.fullIntent.symbol}",
							Operator: "eq",
							Value:    "BTC/USD",
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD", "BTC/USD"},
		},
		{
			name: "non-string values in 'in' array - should skip",
			routerConfig: &config.RouterConfig{
				ID:      "test_router",
				Enabled: true,
				Triggers: config.RouterTriggers{
					Events: []string{"IntentRegistered"},
					Conditions: []config.TriggerCondition{
						{
							Field:    "${enrichment.fullIntent.Symbol}",
							Operator: "in",
							Value:    []interface{}{"ETH/USD", 123, "BTC/USD", nil, ""},
						},
					},
				},
			},
			expectedSymbols: []string{"ETH/USD", "BTC/USD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbols := GetSymbolsFromConfig(tt.routerConfig)
			assert.ElementsMatch(t, tt.expectedSymbols, symbols, "Symbols should match expected")
		})
	}
}
