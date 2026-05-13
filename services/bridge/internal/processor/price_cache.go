package processor

import (
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// PriceEntry represents a single price entry with metadata
type PriceEntry struct {
	Symbol     string
	Price      string
	Timestamp  uint64
	IntentHash string

	IntentType string
	Version    string
	Nonce      *big.Int
	Expiry     *big.Int
	Signer     common.Address
	Signature  []byte
	Source     string
	ChainID    *big.Int
}

// PriceCache maintains in-memory prices per symbol
type PriceCache struct {
	mu     sync.RWMutex
	prices map[string]*PriceEntry // symbol -> PriceEntry
}

// NewPriceCache creates a new price cache
func NewPriceCache() *PriceCache {
	return &PriceCache{
		prices: make(map[string]*PriceEntry),
	}
}

// UpdatePrice updates the price for a symbol only if the timestamp is greater than the existing one
func (pc *PriceCache) UpdatePrice(symbol, price, intentHash string, timestamp uint64, nonce, expiry, chainID *big.Int, signer common.Address, signature []byte, source string) {
	pc.UpdatePriceWithMetadata(symbol, price, intentHash, timestamp, "price_update", "1.0", nonce, expiry, chainID, signer, signature, source)
}

// UpdatePriceWithMetadata updates the price with full metadata including intent type and version
func (pc *PriceCache) UpdatePriceWithMetadata(symbol, price, intentHash string, timestamp uint64, intentType, version string, nonce, expiry, chainID *big.Int, signer common.Address, signature []byte, source string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Check if existing entry has a greater or equal timestamp
	if existingEntry, exists := pc.prices[symbol]; exists {
		if existingEntry.Timestamp >= timestamp {
			// Existing price is newer or same age, don't update
			return
		}
	}

	pc.prices[symbol] = &PriceEntry{
		Symbol:     symbol,
		Price:      price,
		Timestamp:  timestamp,
		IntentHash: intentHash,
		IntentType: intentType,
		Version:    version,
		Nonce:      nonce,
		Expiry:     expiry,
		Signer:     signer,
		Signature:  signature,
		Source:     source,
		ChainID:    chainID,
	}
}

// GetPrice retrieves the price entry for a symbol
func (pc *PriceCache) GetPrice(symbol string) (*PriceEntry, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	entry, exists := pc.prices[symbol]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external mutation
	return &PriceEntry{
		Symbol:     entry.Symbol,
		Price:      entry.Price,
		Timestamp:  entry.Timestamp,
		IntentHash: entry.IntentHash,
		IntentType: entry.IntentType,
		Version:    entry.Version,
		Nonce:      entry.Nonce,
		Expiry:     entry.Expiry,
		Signer:     entry.Signer,
		Signature:  entry.Signature,
		Source:     entry.Source,
		ChainID:    entry.ChainID,
	}, true
}

// GetAllPrices returns all prices in the cache
func (pc *PriceCache) GetAllPrices() map[string]*PriceEntry {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	// Return a copy to prevent external mutation
	result := make(map[string]*PriceEntry, len(pc.prices))
	for symbol, entry := range pc.prices {
		result[symbol] = &PriceEntry{
			Symbol:     entry.Symbol,
			Price:      entry.Price,
			Timestamp:  entry.Timestamp,
			IntentHash: entry.IntentHash,
			IntentType: entry.IntentType,
			Version:    entry.Version,
			Nonce:      entry.Nonce,
			Expiry:     entry.Expiry,
			Signer:     entry.Signer,
			Signature:  entry.Signature,
			Source:     entry.Source,
			ChainID:    entry.ChainID,
		}
	}
	return result
}

// GetSymbols returns all symbols in the cache
func (pc *PriceCache) GetSymbols() []string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	symbols := make([]string, 0, len(pc.prices))
	for symbol := range pc.prices {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// RemoveSymbol removes a symbol from the cache
func (pc *PriceCache) RemoveSymbol(symbol string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	delete(pc.prices, symbol)
}

// Clear removes all entries from the cache
func (pc *PriceCache) Clear() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.prices = make(map[string]*PriceEntry)
}

// Size returns the number of symbols in the cache
func (pc *PriceCache) Size() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return len(pc.prices)
}

// GetStalePrices returns symbols with prices older than the given duration
func (pc *PriceCache) GetStalePrices(maxAge time.Duration) []string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	var staleSymbols []string
	cutoffTime := time.Now().Add(-maxAge)

	for symbol, entry := range pc.prices {
		priceTime := time.Unix(int64(entry.Timestamp), 0)
		if priceTime.Before(cutoffTime) {
			staleSymbols = append(staleSymbols, symbol)
		}
	}

	return staleSymbols
}
