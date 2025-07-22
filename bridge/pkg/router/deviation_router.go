package router

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// DeviationRouter routes intents based on price deviation thresholds
type DeviationRouter struct {
	*BaseRouter
	threshold  float64 // Percentage as decimal (0.01 = 1%)
	symbols    map[string]bool
	lastPrices map[string]*big.Int
	mu         sync.RWMutex
}

// NewDeviationRouter creates a new deviation-based router
func NewDeviationRouter(id, name string, threshold float64, symbols []string, destinations []Destination) *DeviationRouter {
	symbolMap := make(map[string]bool)
	for _, symbol := range symbols {
		symbolMap[symbol] = true
	}

	return &DeviationRouter{
		BaseRouter: NewBaseRouter(id, name, destinations),
		threshold:  threshold,
		symbols:    symbolMap,
		lastPrices: make(map[string]*big.Int),
	}
}

// ShouldRoute checks if the intent should be routed based on price deviation
func (r *DeviationRouter) ShouldRoute(intent *types.OracleIntent) (bool, string) {
	r.IncrementChecked()

	if !r.enabled {
		return false, "router disabled"
	}

	// Check symbol filter
	if len(r.symbols) > 0 && !r.symbols[intent.Symbol] {
		return false, fmt.Sprintf("symbol %s not in filter", intent.Symbol)
	}

	r.mu.RLock()
	lastPrice, exists := r.lastPrices[intent.Symbol]
	r.mu.RUnlock()

	if !exists {
		return true, "first price for symbol"
	}

	// Calculate price deviation
	deviation := calculatePriceDeviation(lastPrice, intent.Price)
	
	if deviation >= r.threshold {
		return true, fmt.Sprintf("price deviation %.4f%% exceeds threshold %.4f%%", 
			deviation*100, r.threshold*100)
	}

	return false, fmt.Sprintf("price deviation %.4f%% below threshold %.4f%%", 
		deviation*100, r.threshold*100)
}

// OnRouted updates the last price for the symbol
func (r *DeviationRouter) OnRouted(intent *types.OracleIntent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clone the big.Int to avoid reference issues
	r.lastPrices[intent.Symbol] = new(big.Int).Set(intent.Price)
	r.IncrementRouted()
	r.stats.LastRouted = intent.Timestamp.Int64()
}

// GetThreshold returns the configured deviation threshold
func (r *DeviationRouter) GetThreshold() float64 {
	return r.threshold
}

// GetSymbols returns the list of filtered symbols
func (r *DeviationRouter) GetSymbols() []string {
	symbols := make([]string, 0, len(r.symbols))
	for symbol := range r.symbols {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetLastPrice returns the last price for a symbol
func (r *DeviationRouter) GetLastPrice(symbol string) (*big.Int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	price, exists := r.lastPrices[symbol]
	if exists {
		// Return a copy to avoid external modifications
		return new(big.Int).Set(price), true
	}
	return nil, false
}

// calculatePriceDeviation calculates the percentage deviation between two prices
func calculatePriceDeviation(oldPrice, newPrice *big.Int) float64 {
	if oldPrice.Sign() == 0 {
		return 1.0 // 100% deviation if old price is 0
	}

	// Calculate absolute difference
	diff := new(big.Int).Sub(newPrice, oldPrice)
	if diff.Sign() < 0 {
		diff.Neg(diff)
	}

	// Convert to float for percentage calculation
	// Note: This may lose precision for very large numbers
	diffFloat := new(big.Float).SetInt(diff)
	oldPriceFloat := new(big.Float).SetInt(oldPrice)
	
	// Calculate deviation as a decimal (0.01 = 1%)
	deviation := new(big.Float).Quo(diffFloat, oldPriceFloat)
	deviationFloat, _ := deviation.Float64()
	
	return deviationFloat
}