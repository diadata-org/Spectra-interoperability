package router

import (
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// Router defines the interface for all routing implementations
type Router interface {
	// ID returns the unique identifier for this router
	ID() string

	// Name returns a human-readable name for this router
	Name() string

	// IsEnabled returns whether this router is currently active
	IsEnabled() bool

	// Enable activates this router
	Enable()

	// Disable deactivates this router
	Disable()

	// ShouldRoute determines if an intent should be routed
	// Returns (shouldRoute, reason)
	ShouldRoute(intent *types.OracleIntent) (bool, string)

	// GetDestinations returns the list of destinations for this router
	GetDestinations() []Destination

	// OnRouted is called after an intent has been successfully routed
	// This allows routers to update their internal state
	OnRouted(intent *types.OracleIntent)

	// GetStats returns routing statistics
	GetStats() RouterStats
}

// Destination represents a target chain and contracts
type Destination struct {
	ChainID   int64    `json:"chain_id"`
	Contracts []string `json:"contracts"`
}

// RouterStats contains statistics about router performance
type RouterStats struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	IntentsChecked uint64 `json:"intents_checked"`
	IntentsRouted  uint64 `json:"intents_routed"`
	LastRouted     int64  `json:"last_routed"` // Unix timestamp
}

// BaseRouter provides common functionality for all routers
type BaseRouter struct {
	id           string
	name         string
	enabled      bool
	destinations []Destination
	stats        RouterStats
}

// NewBaseRouter creates a new base router
func NewBaseRouter(id, name string, destinations []Destination) *BaseRouter {
	return &BaseRouter{
		id:           id,
		name:         name,
		enabled:      true,
		destinations: destinations,
		stats: RouterStats{
			ID:      id,
			Name:    name,
			Enabled: true,
		},
	}
}

// ID returns the router's unique identifier
func (r *BaseRouter) ID() string {
	return r.id
}

// Name returns the router's human-readable name
func (r *BaseRouter) Name() string {
	return r.name
}

// IsEnabled returns whether the router is active
func (r *BaseRouter) IsEnabled() bool {
	return r.enabled
}

// Enable activates the router
func (r *BaseRouter) Enable() {
	r.enabled = true
	r.stats.Enabled = true
}

// Disable deactivates the router
func (r *BaseRouter) Disable() {
	r.enabled = false
	r.stats.Enabled = false
}

// GetDestinations returns the router's destinations
func (r *BaseRouter) GetDestinations() []Destination {
	return r.destinations
}

// GetStats returns router statistics
func (r *BaseRouter) GetStats() RouterStats {
	return r.stats
}

// IncrementChecked increments the checked counter
func (r *BaseRouter) IncrementChecked() {
	r.stats.IntentsChecked++
}

// IncrementRouted increments the routed counter
func (r *BaseRouter) IncrementRouted() {
	r.stats.IntentsRouted++
}