package router

import (
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// SymbolRouter routes intents based on symbol filtering only
// This is the simplest router - it forwards all intents for specified symbols
type SymbolRouter struct {
	*BaseRouter
	symbols map[string]bool
}

// NewSymbolRouter creates a new symbol-based router
func NewSymbolRouter(id, name string, symbols []string, destinations []Destination) *SymbolRouter {
	symbolMap := make(map[string]bool)
	for _, symbol := range symbols {
		symbolMap[symbol] = true
	}

	return &SymbolRouter{
		BaseRouter: NewBaseRouter(id, name, destinations),
		symbols:    symbolMap,
	}
}

// ShouldRoute checks if the intent should be routed based on symbol
func (r *SymbolRouter) ShouldRoute(intent *types.OracleIntent) (bool, string) {
	r.IncrementChecked()

	if !r.enabled {
		return false, "router disabled"
	}

	// If no symbols specified, route everything
	if len(r.symbols) == 0 {
		return true, "no symbol filter configured"
	}

	// Check if symbol is in the filter
	if r.symbols[intent.Symbol] {
		return true, fmt.Sprintf("symbol %s is in filter", intent.Symbol)
	}

	return false, fmt.Sprintf("symbol %s not in filter", intent.Symbol)
}

// OnRouted is called after routing
func (r *SymbolRouter) OnRouted(intent *types.OracleIntent) {
	r.IncrementRouted()
	r.stats.LastRouted = intent.Timestamp.Int64()
}

// GetSymbols returns the list of filtered symbols
func (r *SymbolRouter) GetSymbols() []string {
	symbols := make([]string, 0, len(r.symbols))
	for symbol := range r.symbols {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// AddSymbol adds a symbol to the filter
func (r *SymbolRouter) AddSymbol(symbol string) {
	r.symbols[symbol] = true
}

// RemoveSymbol removes a symbol from the filter
func (r *SymbolRouter) RemoveSymbol(symbol string) {
	delete(r.symbols, symbol)
}

// HasSymbol checks if a symbol is in the filter
func (r *SymbolRouter) HasSymbol(symbol string) bool {
	return r.symbols[symbol]
}