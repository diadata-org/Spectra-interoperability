package router

import (
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
)

// Registry manages all routers in the system
type Registry struct {
	routers map[string]Router
	mu      sync.RWMutex
}

// NewRegistry creates a new router registry
func NewRegistry() *Registry {
	return &Registry{
		routers: make(map[string]Router),
	}
}

// Register adds a router to the registry
func (r *Registry) Register(router Router) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.routers[router.ID()]; exists {
		return fmt.Errorf("router with ID %s already exists", router.ID())
	}

	r.routers[router.ID()] = router
	logger.Infof("Registered router: %s (%s)", router.ID(), router.Name())
	return nil
}

// Unregister removes a router from the registry
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.routers[id]; !exists {
		return fmt.Errorf("router with ID %s not found", id)
	}

	delete(r.routers, id)
	logger.Infof("Unregistered router: %s", id)
	return nil
}

// Get returns a router by ID
func (r *Registry) Get(id string) (Router, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	router, exists := r.routers[id]
	return router, exists
}

// GetAll returns all routers
func (r *Registry) GetAll() []Router {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routers := make([]Router, 0, len(r.routers))
	for _, router := range r.routers {
		routers = append(routers, router)
	}
	return routers
}

// GetActiveRouters returns only enabled routers
func (r *Registry) GetActiveRouters() []Router {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routers := make([]Router, 0, len(r.routers))
	for _, router := range r.routers {
		if router.IsEnabled() {
			routers = append(routers, router)
		}
	}
	return routers
}

// EnableRouter enables a router by ID
func (r *Registry) EnableRouter(id string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	router, exists := r.routers[id]
	if !exists {
		return fmt.Errorf("router with ID %s not found", id)
	}

	router.Enable()
	logger.Infof("Enabled router: %s", id)
	return nil
}

// DisableRouter disables a router by ID
func (r *Registry) DisableRouter(id string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	router, exists := r.routers[id]
	if !exists {
		return fmt.Errorf("router with ID %s not found", id)
	}

	router.Disable()
	logger.Infof("Disabled router: %s", id)
	return nil
}

// GetStats returns statistics for all routers
func (r *Registry) GetStats() []RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make([]RouterStats, 0, len(r.routers))
	for _, router := range r.routers {
		stats = append(stats, router.GetStats())
	}
	return stats
}

// LoadFromConfig loads routers from configuration
func (r *Registry) LoadFromConfig(routerConfigs []config.RouterConfig) error {
	for _, cfg := range routerConfigs {
		router, err := CreateRouterFromConfig(cfg)
		if err != nil {
			logger.Errorf("Failed to create router %s: %v", cfg.ID, err)
			continue
		}

		if err := r.Register(router); err != nil {
			logger.Errorf("Failed to register router %s: %v", cfg.ID, err)
			continue
		}

		if !cfg.Enabled {
			router.Disable()
		}
	}

	logger.Infof("Loaded %d routers from configuration", len(r.routers))
	return nil
}

// CreateRouterFromConfig creates a router instance from configuration
func CreateRouterFromConfig(cfg config.RouterConfig) (Router, error) {
	destinations := make([]Destination, len(cfg.Destinations))
	for i, dest := range cfg.Destinations {
		destinations[i] = Destination{
			ChainID:   dest.ChainID,
			Contracts: []string{dest.Contract},
		}
	}

	symbols := []string{}
	
	switch cfg.Type {
	case "time":
		interval := 5 * time.Minute
		return NewTimeRouter(cfg.ID, cfg.Name, interval, symbols, destinations), nil

	case "deviation":
		threshold := 0.05
		return NewDeviationRouter(cfg.ID, cfg.Name, threshold, symbols, destinations), nil

	case "symbol":
		return NewSymbolRouter(cfg.ID, cfg.Name, symbols, destinations), nil

	case "composite":
		return nil, fmt.Errorf("composite router not yet implemented")

	case "generic":
		// Create GenericRouter and wrap it to implement Router interface
		genericRouter, err := NewGenericRouter(&cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create generic router: %w", err)
		}
		return NewGenericRouterWrapper(genericRouter, destinations), nil

	default:
		return nil, fmt.Errorf("unknown router type: %s", cfg.Type)
	}
}