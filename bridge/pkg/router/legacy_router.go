package router

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// LegacyRouter implements the old per-contract routing logic as a router
// This allows existing configurations to work with the new router system
type LegacyRouter struct {
	*BaseRouter
	contractConfig    *config.ContractConfig
	destinationConfig *config.DestinationConfig
	lastUpdate        map[string]time.Time
	lastPrices        map[string]*big.Int
	mu                sync.RWMutex
}

// NewLegacyRouter creates a router from legacy contract configuration
func NewLegacyRouter(contract *config.ContractConfig, destination *config.DestinationConfig) *LegacyRouter {
	id := fmt.Sprintf("legacy-%s-%d", contract.Address, destination.ChainID)
	name := fmt.Sprintf("Legacy %s on %s", contract.Name, destination.Name)
	
	destinations := []Destination{
		{
			ChainID:   destination.ChainID,
			Contracts: []string{contract.Address},
		},
	}

	return &LegacyRouter{
		BaseRouter:        NewBaseRouter(id, name, destinations),
		contractConfig:    contract,
		destinationConfig: destination,
		lastUpdate:        make(map[string]time.Time),
		lastPrices:        make(map[string]*big.Int),
	}
}

// ShouldRoute implements the legacy routing logic
func (r *LegacyRouter) ShouldRoute(intent *types.OracleIntent) (bool, string) {
	r.IncrementChecked()

	if !r.enabled {
		return false, "router disabled"
	}

	// Check if contract is enabled
	if !r.contractConfig.Enabled {
		return false, "contract disabled"
	}

	// Check if symbol is supported
	symbolSupported := false
	for _, symbol := range r.contractConfig.SupportedSymbols {
		if symbol == intent.Symbol {
			symbolSupported = true
			break
		}
	}
	if !symbolSupported {
		return false, fmt.Sprintf("symbol %s not supported by contract", intent.Symbol)
	}

	r.mu.RLock()
	lastUpdateTime, hasLastUpdate := r.lastUpdate[intent.Symbol]
	lastPrice, hasLastPrice := r.lastPrices[intent.Symbol]
	r.mu.RUnlock()

	// Check minimum update interval
	if r.contractConfig.MinUpdateInterval > 0 && hasLastUpdate {
		elapsed := time.Since(lastUpdateTime)
		if elapsed < r.contractConfig.MinUpdateInterval.Duration() {
			remaining := r.contractConfig.MinUpdateInterval.Duration() - elapsed
			return false, fmt.Sprintf("min interval not met: %s remaining", remaining.Round(time.Second))
		}
	}

	// Check price deviation
	if r.contractConfig.MaxPriceDeviation > 0 && hasLastPrice {
		deviation := calculatePriceDeviation(lastPrice, intent.Price)
		if deviation < r.contractConfig.MaxPriceDeviation {
			return false, fmt.Sprintf("price deviation %.4f%% below threshold %.4f%%", 
				deviation*100, r.contractConfig.MaxPriceDeviation*100)
		}
	}

	// If we have neither time nor price criteria, always route
	if r.contractConfig.MinUpdateInterval == 0 && r.contractConfig.MaxPriceDeviation == 0 {
		return true, "no routing criteria configured"
	}

	return true, "legacy criteria met"
}

// OnRouted updates the internal state
func (r *LegacyRouter) OnRouted(intent *types.OracleIntent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastUpdate[intent.Symbol] = time.Now()
	r.lastPrices[intent.Symbol] = new(big.Int).Set(intent.Price)
	r.IncrementRouted()
	r.stats.LastRouted = time.Now().Unix()
}

// GetContractConfig returns the underlying contract configuration
func (r *LegacyRouter) GetContractConfig() *config.ContractConfig {
	return r.contractConfig
}

// CreateLegacyRoutersFromConfig creates legacy routers from destination configurations
func CreateLegacyRoutersFromConfig(destinations []*config.DestinationConfig) []Router {
	var routers []Router

	for _, dest := range destinations {
		if !dest.Enabled {
			continue
		}

		for i := range dest.Contracts {
			contract := &dest.Contracts[i]
			if !contract.Enabled {
				continue
			}

			// Only create legacy router if there are routing criteria
			if contract.MinUpdateInterval > 0 || contract.MaxPriceDeviation > 0 || len(contract.SupportedSymbols) > 0 {
				router := NewLegacyRouter(contract, dest)
				routers = append(routers, router)
			}
		}
	}

	return routers
}