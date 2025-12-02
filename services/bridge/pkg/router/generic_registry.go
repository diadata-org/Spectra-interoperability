package router

import (
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
)

// GenericRegistry manages generic routers
type GenericRegistry struct {
	mu             sync.RWMutex
	routers        map[string]*GenericRouter
	eventToRouters map[string][]*GenericRouter
}

// NewGenericRegistry creates a new router registry
func NewGenericRegistry() *GenericRegistry {
	return &GenericRegistry{
		routers:        make(map[string]*GenericRouter),
		eventToRouters: make(map[string][]*GenericRouter),
	}
}

// LoadRouters loads routers from configuration
func (gr *GenericRegistry) LoadRouters(routerConfigs []config.RouterConfig) error {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	gr.routers = make(map[string]*GenericRouter)
	gr.eventToRouters = make(map[string][]*GenericRouter)

	for _, cfg := range routerConfigs {
		routerCfg := cfg

		router, err := NewGenericRouter(&routerCfg)
		if err != nil {
			logger.Errorf("Failed to create router %s: %v", cfg.ID, err)
			continue
		}

		gr.routers[router.ID()] = router
		for _, eventName := range router.config.Triggers.Events {
			gr.eventToRouters[eventName] = append(gr.eventToRouters[eventName], router)
		}

		logger.Infof("Loaded router: %s (type: %s, enabled: %v)",
			router.ID(), router.Type(), router.IsEnabled())
	}

	logger.Infof("Loaded %d routers and built event index", len(gr.routers))
	return nil
}

// GetRouter returns a router by ID
func (gr *GenericRegistry) GetRouter(id string) (*GenericRouter, bool) {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	router, exists := gr.routers[id]
	return router, exists
}

// GetActiveRouters returns all enabled routers
func (gr *GenericRegistry) GetActiveRouters() []*GenericRouter {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	var active []*GenericRouter
	for _, router := range gr.routers {
		if router.IsEnabled() {
			active = append(active, router)
		}
	}

	return active
}

// GetRouterByID returns a router by its ID
func (gr *GenericRegistry) GetRouterByID(routerID string) GenericRouterInterface {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	for _, router := range gr.routers {
		if router.ID() == routerID {
			return router
		}
	}

	return nil
}

// GetRoutersForEvent returns routers that handle a specific event
func (gr *GenericRegistry) GetRoutersForEvent(eventName string) []*GenericRouter {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	routers, exists := gr.eventToRouters[eventName]
	if !exists {
		return nil
	}

	// Filter out disabled routers
	activeRouters := make([]*GenericRouter, 0, len(routers))
	for _, router := range routers {
		if router.IsEnabled() {
			activeRouters = append(activeRouters, router)
		}
	}

	return activeRouters
}

// RouteEvent routes an event through all applicable routers
func (gr *GenericRegistry) RouteEvent(eventName string, data *config.ExtractedData) []RoutingResult {
	start := time.Now()
	routers := gr.GetRoutersForEvent(eventName)
	var wg sync.WaitGroup

	// var results []RoutingResult
	results := make([]RoutingResult, len(routers))

	if len(routers) == 0 {
		return []RoutingResult{}
	}
	for i, router := range routers {
		wg.Add(1)

		go func(idx int, r *GenericRouter) {
			defer wg.Done()
			shouldRoute, reason := r.ShouldRoute(eventName, data)

			result := RoutingResult{
				RouterID: r.ID(),
				Routed:   shouldRoute,
				Reason:   reason,
			}

			if shouldRoute {
				result.Destinations = r.GetDestinations(data)
			}
			results[idx] = result

		}(i, router)
	}

	wg.Wait()

	elapsed := time.Since(start)
	logger.Infof("RouteEvent for %s with %d routers took %s", eventName, len(routers), elapsed)

	return results
}

// RoutingResult represents the result of routing an event
type RoutingResult struct {
	RouterID     string
	Routed       bool
	Reason       string
	Destinations []config.RouterDestination
}

// GetAllStats returns statistics for all routers
func (gr *GenericRegistry) GetAllStats() map[string]GenericRouterStats {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	stats := make(map[string]GenericRouterStats)
	for id, router := range gr.routers {
		stats[id] = router.GetStats()
	}

	return stats
}

// EnableRouter enables a router by ID
func (gr *GenericRegistry) EnableRouter(id string) error {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	router, exists := gr.routers[id]
	if !exists {
		return fmt.Errorf("router not found: %s", id)
	}

	router.config.Enabled = true
	logger.Infof("Enabled router: %s", id)
	return nil
}

// DisableRouter disables a router by ID
func (gr *GenericRegistry) DisableRouter(id string) error {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	router, exists := gr.routers[id]
	if !exists {
		return fmt.Errorf("router not found: %s", id)
	}

	router.config.Enabled = false
	logger.Infof("Disabled router: %s", id)
	return nil
}

// ReloadRouter reloads a specific router configuration
func (gr *GenericRegistry) ReloadRouter(cfg *config.RouterConfig) error {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	if old, exists := gr.routers[cfg.ID]; exists {
		logger.Infof("Replacing router: %s", cfg.ID)
		delete(gr.routers, old.ID())
	}

	router, err := NewGenericRouter(cfg)
	if err != nil {
		return fmt.Errorf("failed to create router %s: %w", cfg.ID, err)
	}

	gr.routers[router.ID()] = router

	// Rebuild the index
	gr.eventToRouters = make(map[string][]*GenericRouter)
	for _, r := range gr.routers {
		for _, eventName := range r.config.Triggers.Events {
			gr.eventToRouters[eventName] = append(gr.eventToRouters[eventName], r)
		}
	}

	logger.Infof("Reloaded router: %s and rebuilt event index", cfg.ID)
	return nil
}

// Count returns the number of routers
func (gr *GenericRegistry) Count() int {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return len(gr.routers)
}

// CountActive returns the number of active routers
func (gr *GenericRegistry) CountActive() int {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	count := 0
	for _, router := range gr.routers {
		if router.IsEnabled() {
			count++
		}
	}
	return count
}
