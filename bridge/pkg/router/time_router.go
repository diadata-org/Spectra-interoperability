package router

import (
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// TimeRouter routes intents based on time intervals
type TimeRouter struct {
	*BaseRouter
	interval   time.Duration
	symbols    map[string]bool
	lastUpdate map[string]time.Time
	mu         sync.RWMutex
}

// NewTimeRouter creates a new time-based router
func NewTimeRouter(id, name string, interval time.Duration, symbols []string, destinations []Destination) *TimeRouter {
	symbolMap := make(map[string]bool)
	for _, symbol := range symbols {
		symbolMap[symbol] = true
	}

	return &TimeRouter{
		BaseRouter: NewBaseRouter(id, name, destinations),
		interval:   interval,
		symbols:    symbolMap,
		lastUpdate: make(map[string]time.Time),
	}
}

// ShouldRoute checks if the intent should be routed based on time interval
func (r *TimeRouter) ShouldRoute(intent *types.OracleIntent) (bool, string) {
	r.IncrementChecked()

	if !r.enabled {
		return false, "router disabled"
	}

	// Check symbol filter
	if len(r.symbols) > 0 && !r.symbols[intent.Symbol] {
		return false, fmt.Sprintf("symbol %s not in filter", intent.Symbol)
	}

	r.mu.RLock()
	lastUpdate, exists := r.lastUpdate[intent.Symbol]
	r.mu.RUnlock()

	if !exists {
		return true, "first update for symbol"
	}

	elapsed := time.Since(lastUpdate)
	if elapsed >= r.interval {
		return true, fmt.Sprintf("interval elapsed: %s >= %s", elapsed.Round(time.Second), r.interval)
	}

	remaining := r.interval - elapsed
	return false, fmt.Sprintf("too soon: %s remaining", remaining.Round(time.Second))
}

// OnRouted updates the last update time for the symbol
func (r *TimeRouter) OnRouted(intent *types.OracleIntent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastUpdate[intent.Symbol] = time.Now()
	r.IncrementRouted()
	r.stats.LastRouted = time.Now().Unix()
}

// GetInterval returns the configured interval
func (r *TimeRouter) GetInterval() time.Duration {
	return r.interval
}

// GetSymbols returns the list of filtered symbols
func (r *TimeRouter) GetSymbols() []string {
	symbols := make([]string, 0, len(r.symbols))
	for symbol := range r.symbols {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetLastUpdate returns the last update time for a symbol
func (r *TimeRouter) GetLastUpdate(symbol string) (time.Time, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, exists := r.lastUpdate[symbol]
	return t, exists
}