package processor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// GasEstimationServiceImpl implements gas estimation for multiple destinations
type GasEstimationServiceImpl struct {
	destClients    map[int64]*ethclient.Client
	gasCache       *GasEstimateCache
	defaultGasLimits map[string]uint64 // method -> default gas limit
	gasMultipliers   map[int64]float64 // chainID -> gas multiplier
	mutex            sync.RWMutex
	stats            *GasEstimationStats
}

// GasEstimateCache caches gas estimates to avoid repeated calls
type GasEstimateCache struct {
	estimates map[string]*CachedGasEstimate
	mutex     sync.RWMutex
	ttl       time.Duration
}

// CachedGasEstimate represents a cached gas estimate
type CachedGasEstimate struct {
	GasLimit   uint64
	Timestamp  time.Time
	ChainID    int64
	MethodName string
}

// GasEstimationStats tracks gas estimation statistics
type GasEstimationStats struct {
	TotalEstimations     uint64
	SuccessfulEstimations uint64
	CacheHits            uint64
	CacheMisses          uint64
	AverageEstimationTime float64 // milliseconds
	EstimationTimeouts   uint64
}

// NewGasEstimationService creates a new gas estimation service
func NewGasEstimationService(destClients map[int64]*ethclient.Client) *GasEstimationServiceImpl {
	service := &GasEstimationServiceImpl{
		destClients:      destClients,
		gasCache:         NewGasEstimateCache(5 * time.Minute), // 5 minute cache
		defaultGasLimits: make(map[string]uint64),
		gasMultipliers:   make(map[int64]float64),
		stats:            &GasEstimationStats{},
	}
	
	// Initialize default gas limits for common methods
	service.initializeDefaults()
	
	return service
}

// NewGasEstimateCache creates a new gas estimate cache
func NewGasEstimateCache(ttl time.Duration) *GasEstimateCache {
	cache := &GasEstimateCache{
		estimates: make(map[string]*CachedGasEstimate),
		ttl:       ttl,
	}
	
	// Start cleanup goroutine
	go cache.cleanupExpired()
	
	return cache
}

// initializeDefaults sets up default gas limits for common operations
func (ges *GasEstimationServiceImpl) initializeDefaults() {
	// Common method gas limits (increased for oracle operations based on real usage)
	ges.defaultGasLimits["fulfillRandomInt"] = 200000
	ges.defaultGasLimits["handleIntentUpdate"] = 400000  // Increased from 200k due to out-of-gas failures
	ges.defaultGasLimits["updateOracle"] = 150000
	ges.defaultGasLimits["submitAttestation"] = 180000
	
	// Chain-specific gas multipliers
	ges.gasMultipliers[1] = 1.1     // Ethereum mainnet (higher fees, lower buffer)
	ges.gasMultipliers[421614] = 1.2 // Arbitrum Sepolia (lower fees, higher buffer)
	ges.gasMultipliers[11155111] = 1.2 // Sepolia testnet
}

// EstimateGasForDestinations estimates gas for multiple destinations in parallel
func (ges *GasEstimationServiceImpl) EstimateGasForDestinations(
	ctx context.Context,
	event *types.EventData,
	destinations []config.RouterDestination,
) (map[string]uint64, map[string]error) {
	
	estimates := make(map[string]uint64)
	errors := make(map[string]error)
	
	if len(destinations) == 0 {
		return estimates, errors
	}
	
	// Process destinations in parallel
	var wg sync.WaitGroup
	var mutex sync.Mutex
	
	for _, dest := range destinations {
		wg.Add(1)
		go func(destination config.RouterDestination) {
			defer wg.Done()
			
			destKey := fmt.Sprintf("%d-%s-%s", destination.ChainID, destination.Contract, destination.Method.Name)
			startTime := time.Now()
			
			estimate, err := ges.estimateGasForDestination(ctx, event, destination)
			
			estimationTime := time.Since(startTime)
			ges.updateEstimationStats(estimationTime, err == nil)
			
			mutex.Lock()
			if err != nil {
				errors[destKey] = err
				// Use default gas limit as fallback
				if defaultGas, exists := ges.defaultGasLimits[destination.Method.Name]; exists {
					estimates[destKey] = defaultGas
					logger.Warnf("Gas estimation failed for %s, using default %d: %v", destKey, defaultGas, err)
				}
			} else {
				estimates[destKey] = estimate
				logger.Debugf("Gas estimated for %s: %d (took %v)", destKey, estimate, estimationTime)
			}
			mutex.Unlock()
		}(dest)
	}
	
	// Wait for all estimations to complete
	wg.Wait()
	
	return estimates, errors
}

// estimateGasForDestination estimates gas for a single destination
func (ges *GasEstimationServiceImpl) estimateGasForDestination(
	ctx context.Context,
	event *types.EventData,
	destination config.RouterDestination,
) (uint64, error) {
	
	// Check cache first
	cacheKey := ges.buildCacheKey(destination, event.EventName)
	if cached := ges.gasCache.Get(cacheKey); cached != nil {
		ges.stats.CacheHits++
		return cached.GasLimit, nil
	}
	ges.stats.CacheMisses++
	
	// Check if we have a configured gas limit (non-zero means use static)
	if destination.Method.GasLimit > 0 {
		return uint64(destination.Method.GasLimit), nil
	}
	
	// Get client for destination chain
	client, exists := ges.destClients[destination.ChainID]
	if !exists {
		return 0, fmt.Errorf("no client for chain %d", destination.ChainID)
	}
	
	// Build transaction data for gas estimation
	callData, err := ges.buildTransactionData(event, destination)
	if err != nil {
		return 0, fmt.Errorf("failed to build transaction data: %w", err)
	}
	
	// Create call message for gas estimation
	msg := ethereum.CallMsg{
		To:   &common.Address{}, // Will be set from destination.Contract
		Data: callData,
		// Note: From address and value would be set based on the actual transaction
	}
	
	// Parse contract address
	contractAddr := common.HexToAddress(destination.Contract)
	msg.To = &contractAddr
	
	// Estimate gas with timeout
	estimateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	gasLimit, err := client.EstimateGas(estimateCtx, msg)
	if err != nil {
		return 0, fmt.Errorf("gas estimation failed: %w", err)
	}
	
	// Apply chain-specific multiplier for safety buffer
	multiplier := ges.gasMultipliers[destination.ChainID]
	if multiplier == 0 {
		multiplier = 1.2 // Default 20% buffer
	}
	
	finalGasLimit := uint64(float64(gasLimit) * multiplier)
	
	// Cache the result
	cached := &CachedGasEstimate{
		GasLimit:   finalGasLimit,
		Timestamp:  time.Now(),
		ChainID:    destination.ChainID,
		MethodName: destination.Method.Name,
	}
	ges.gasCache.Set(cacheKey, cached)
	
	return finalGasLimit, nil
}

// buildTransactionData builds transaction data for gas estimation
func (ges *GasEstimationServiceImpl) buildTransactionData(event *types.EventData, destination config.RouterDestination) ([]byte, error) {
	// This is a simplified version - in reality, this would need to:
	// 1. Load the contract ABI for the method
	// 2. Encode the method call with appropriate parameters
	// 3. Handle different event types (IntentRegistered, IntArraySet, etc.)
	
	// For now, return a placeholder that represents typical transaction data
	// In production, this would integrate with the actual contract binding generation
	
	methodName := destination.Method.Name
	switch methodName {
	case "fulfillRandomInt":
		// Example: fulfillRandomInt(uint256 requestId, int256[] randomInts)
		// Return mock encoded data for gas estimation
		return []byte{0x12, 0x34, 0x56, 0x78}, nil // Placeholder
	case "handleIntentUpdate":
		// Example: handleIntentUpdate(bytes32 intentHash, OracleIntent intent)
		return []byte{0x87, 0x65, 0x43, 0x21}, nil // Placeholder
	default:
		// Generic method call data
		return []byte{0xaa, 0xbb, 0xcc, 0xdd}, nil // Placeholder
	}
}

// buildCacheKey builds a cache key for gas estimates
func (ges *GasEstimationServiceImpl) buildCacheKey(destination config.RouterDestination, eventName string) string {
	return fmt.Sprintf("%d-%s-%s-%s", 
		destination.ChainID, 
		destination.Contract, 
		destination.Method.Name, 
		eventName,
	)
}

// updateEstimationStats updates gas estimation statistics
func (ges *GasEstimationServiceImpl) updateEstimationStats(duration time.Duration, success bool) {
	ges.mutex.Lock()
	defer ges.mutex.Unlock()
	
	ges.stats.TotalEstimations++
	if success {
		ges.stats.SuccessfulEstimations++
	}
	
	// Update average estimation time
	estimationTimeMs := float64(duration) / 1e6
	ges.stats.AverageEstimationTime = updateRollingAverage(
		ges.stats.AverageEstimationTime,
		estimationTimeMs,
		ges.stats.TotalEstimations,
	)
}

// GetStats returns current gas estimation statistics
func (ges *GasEstimationServiceImpl) GetStats() *GasEstimationStats {
	ges.mutex.RLock()
	defer ges.mutex.RUnlock()
	
	statsCopy := *ges.stats
	return &statsCopy
}

// Cache methods

// Get retrieves a cached gas estimate
func (cache *GasEstimateCache) Get(key string) *CachedGasEstimate {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	
	estimate, exists := cache.estimates[key]
	if !exists {
		return nil
	}
	
	// Check if expired
	if time.Since(estimate.Timestamp) > cache.ttl {
		return nil
	}
	
	return estimate
}

// Set stores a gas estimate in the cache
func (cache *GasEstimateCache) Set(key string, estimate *CachedGasEstimate) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	
	cache.estimates[key] = estimate
}

// cleanupExpired removes expired entries from the cache
func (cache *GasEstimateCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		cache.mutex.Lock()
		now := time.Now()
		for key, estimate := range cache.estimates {
			if now.Sub(estimate.Timestamp) > cache.ttl {
				delete(cache.estimates, key)
			}
		}
		cache.mutex.Unlock()
	}
}