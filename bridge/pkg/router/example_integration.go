package router

import (
	"log"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)


// ExampleProgrammaticRouters shows how to create routers programmatically
func ExampleProgrammaticRouters() {
	// Create registry
	registry := NewRegistry()

	// Create a time-based router for ETH/USD
	ethRouter := NewTimeRouter(
		"eth-5min",
		"ETH/USD 5-minute updates",
		5*time.Minute,
		[]string{"ETH/USD"},
		[]Destination{
			{
				ChainID:   11155420,
				Contracts: []string{"0xf359f17fc18f7d7c3ed6b2faadbe66ec0c7894de"},
			},
		},
	)
	registry.Register(ethRouter)

	// Create a deviation-based router for BTC/USD
	btcRouter := NewDeviationRouter(
		"btc-deviation",
		"BTC/USD 0.5% deviation updates",
		0.005, // 0.5%
		[]string{"BTC/USD"},
		[]Destination{
			{
				ChainID:   11155420,
				Contracts: []string{"0xf359f17fc18f7d7c3ed6b2faadbe66ec0c7894de"},
			},
		},
	)
	registry.Register(btcRouter)

	// Create a symbol filter router for all stablecoins
	stableRouter := NewSymbolRouter(
		"stablecoins",
		"All stablecoin updates",
		[]string{"USDT/USD", "USDC/USD", "DAI/USD", "BUSD/USD"},
		[]Destination{
			{
				ChainID:   1, // Ethereum mainnet
				Contracts: []string{"0x1234567890123456789012345678901234567890"},
			},
			{
				ChainID:   137, // Polygon
				Contracts: []string{"0x0987654321098765432109876543210987654321"},
			},
		},
	)
	registry.Register(stableRouter)

	// Use the registry with EventProcessor
	log.Printf("Registered %d routers", len(registry.GetAll()))
}

// ExampleRouterUsage shows how routers make routing decisions
func ExampleRouterUsage() {
	// Create a time-based router
	router := NewTimeRouter(
		"example",
		"Example Router",
		1*time.Minute,
		[]string{"ETH/USD"},
		[]Destination{{ChainID: 1, Contracts: []string{"0xAAA"}}},
	)

	// Create a test intent
	intent := &types.OracleIntent{
		Symbol:    "ETH/USD",
		Price:     nil, // Would be actual price
		Timestamp: nil, // Would be actual timestamp
	}

	// First check - should route (no previous update)
	shouldRoute, reason := router.ShouldRoute(intent)
	log.Printf("Should route: %v, Reason: %s", shouldRoute, reason)

	if shouldRoute {
		// Simulate routing
		router.OnRouted(intent)
	}

	// Second check immediately after - should not route
	shouldRoute, reason = router.ShouldRoute(intent)
	log.Printf("Should route: %v, Reason: %s", shouldRoute, reason)

	// Check after interval - should route
	time.Sleep(1 * time.Minute)
	shouldRoute, reason = router.ShouldRoute(intent)
	log.Printf("Should route: %v, Reason: %s", shouldRoute, reason)
}

// ExampleMonitoring shows how to monitor router statistics
func ExampleMonitoring(registry *Registry) {
	// Get all router stats
	stats := registry.GetStats()
	for _, stat := range stats {
		log.Printf("Router %s: Checked=%d, Routed=%d, LastRouted=%s",
			stat.ID,
			stat.IntentsChecked,
			stat.IntentsRouted,
			time.Unix(stat.LastRouted, 0).Format(time.RFC3339),
		)
	}

	// Enable/disable routers dynamically
	if err := registry.DisableRouter("eth-5min"); err != nil {
		log.Printf("Failed to disable router: %v", err)
	}

	// Get active routers only
	activeRouters := registry.GetActiveRouters()
	log.Printf("Active routers: %d", len(activeRouters))
}