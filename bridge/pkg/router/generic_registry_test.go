
package router

import (
	"testing"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/stretchr/testify/assert"
)

func TestNewGenericRegistry(t *testing.T) {
	registry := NewGenericRegistry()
	assert.NotNil(t, registry)
	assert.Empty(t, registry.routers)
}

func TestLoadRouters(t *testing.T) {
	configs := []config.RouterConfig{
		{ID: "router-1", Enabled: true, Triggers: config.RouterTriggers{Events: []string{"event-A"}}},
		{ID: "router-2", Enabled: false, Triggers: config.RouterTriggers{Events: []string{"event-B"}}},
		{ID: "router-3", PrivateKey: "invalid-key"}, // Invalid config
	}

	registry := NewGenericRegistry()
	err := registry.LoadRouters(configs)
	assert.NoError(t, err) // LoadRouters logs errors but doesn't return one

	assert.Equal(t, 2, registry.Count()) // router-3 fails to load
	
	r1, ok := registry.GetRouter("router-1")
	assert.True(t, ok)
	assert.Equal(t, "router-1", r1.ID())

	_, ok = registry.GetRouter("router-3")
	assert.False(t, ok)
}

func TestGetActiveRouters(t *testing.T) {
	configs := []config.RouterConfig{
		{ID: "router-1", Enabled: true},
		{ID: "router-2", Enabled: false},
		{ID: "router-3", Enabled: true},
	}
	registry := NewGenericRegistry()
	registry.LoadRouters(configs)

	activeRouters := registry.GetActiveRouters()
	assert.Len(t, activeRouters, 2)
	assert.ElementsMatch(t, []string{"router-1", "router-3"}, []string{activeRouters[0].ID(), activeRouters[1].ID()})
}

func TestGetRoutersForEvent(t *testing.T) {
	configs := []config.RouterConfig{
		{ID: "router-1", Enabled: true, Triggers: config.RouterTriggers{Events: []string{"event-A", "event-C"}}},
		{ID: "router-2", Enabled: true, Triggers: config.RouterTriggers{Events: []string{"event-B"}}},
		{ID: "router-3", Enabled: false, Triggers: config.RouterTriggers{Events: []string{"event-A"}}}, // Disabled
	}
	registry := NewGenericRegistry()
	registry.LoadRouters(configs)

	routers := registry.GetRoutersForEvent("event-A")
	assert.Len(t, routers, 1)
	assert.Equal(t, "router-1", routers[0].ID())
}

func TestRouteEvent(t *testing.T) {
	configs := []config.RouterConfig{
		{
			ID:      "router-A",
			Enabled: true,
			Triggers: config.RouterTriggers{
				Events:     []string{"PriceUpdate"},
				Conditions: []config.TriggerCondition{{Field: "${event.symbol}", Operator: "eq", Value: "ETH"}},
			},
			Destinations: []config.RouterDestination{{ChainID: 1}},
		},
		{
			ID:      "router-B",
			Enabled: true,
			Triggers: config.RouterTriggers{
				Events:     []string{"PriceUpdate"},
				Conditions: []config.TriggerCondition{{Field: "${event.symbol}", Operator: "eq", Value: "BTC"}},
			},
		},
		{
			ID:      "router-C", // Does not trigger on PriceUpdate
			Enabled: true,
			Triggers: config.RouterTriggers{Events: []string{"OtherEvent"}},
		},
	}
	registry := NewGenericRegistry()
	registry.LoadRouters(configs)

	data := &config.ExtractedData{Event: map[string]interface{}{"symbol": "ETH"}}

	results := registry.RouteEvent("PriceUpdate", data)

	assert.Len(t, results, 2) // router-A and router-B trigger on the event

	var routedResult, filteredResult RoutingResult
	for _, r := range results {
		if r.RouterID == "router-A" {
			routedResult = r
		} else if r.RouterID == "router-B" {
			filteredResult = r
		}
	}

	assert.True(t, routedResult.Routed)
	assert.Len(t, routedResult.Destinations, 1)
	assert.Equal(t, int64(1), routedResult.Destinations[0].ChainID)

	assert.False(t, filteredResult.Routed)
	assert.Empty(t, filteredResult.Destinations)
}

func TestEnableDisableRouter(t *testing.T) {
	configs := []config.RouterConfig{{ID: "router-1", Enabled: false}}
	registry := NewGenericRegistry()
	registry.LoadRouters(configs)

	assert.Equal(t, 0, registry.CountActive())

	err := registry.EnableRouter("router-1")
	assert.NoError(t, err)
	assert.Equal(t, 1, registry.CountActive())

	err = registry.DisableRouter("router-1")
	assert.NoError(t, err)
	assert.Equal(t, 0, registry.CountActive())

	err = registry.EnableRouter("non-existent")
	assert.Error(t, err)
}
