package contracts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/ethereum/go-ethereum/common"
)

// staleNonceThreshold defines how long to keep pending nonces before eviction
const staleNonceThreshold = 10 * time.Minute

// maxRetryCount defines maximum retry attempts for a nonce before forcing eviction
const maxRetryCount = 5

// NonceManager manages nonces for transaction sending with local tracking
type NonceManager struct {
	client        rpc.EthClient
	address       common.Address
	chainID       int64 // Chain ID for logging
	mu            sync.Mutex
	localNonce    uint64                // Our local tracked nonce (next to use)
	initialized   bool                  // Whether we've synced with chain
	pendingNonces map[uint64]*NonceInfo // Track nonce usage
	retryCount    map[uint64]int        // Track retry attempts per nonce
	lastSync      time.Time             // Last time we synced with chain
	metrics       *metrics.Collector    // Optional metrics collector for tracking pending nonces
	maxSafeGap    uint64                // Maximum safe gap before emergency reset
}

// getRPCURL returns the RPC URL if the client is a MultiClient, otherwise returns "unknown"
func (nm *NonceManager) getRPCURL() string {
	// Use type assertion to check if client is a MultiClient
	if mc, ok := nm.client.(interface{ GetCurrentRPCURL() string }); ok {
		return mc.GetCurrentRPCURL()
	}
	return "unknown"
}

// NonceInfo tracks information about a nonce
type NonceInfo struct {
	Allocated time.Time
	Sent      bool
	TxHash    string
}

// NewNonceManager creates a new nonce manager
func NewNonceManager(client rpc.EthClient, address common.Address, chainID int64, maxSafeGap uint64) *NonceManager {
	nm := &NonceManager{
		client:        client,
		address:       address,
		chainID:       chainID,
		pendingNonces: make(map[uint64]*NonceInfo),
		retryCount:    make(map[uint64]int),
		initialized:   false,
		metrics:       metrics.NewCollector(), // Use singleton metrics collector
		maxSafeGap:    maxSafeGap,
	}
	return nm
}

// updatePendingNonceMetrics updates the pending nonce count metric
func (nm *NonceManager) updatePendingNonceMetrics() {
	if nm.metrics != nil {
		nm.metrics.SetPendingNonceCount(nm.address.Hex(), nm.chainID, len(nm.pendingNonces))
	}
}

// cleanupAndSyncLocked deletes confirmed nonces and catches up localNonce if behind
func (nm *NonceManager) cleanupAndSyncLocked(chainNonce uint64) {
	updated := false
	for n := range nm.pendingNonces {
		if n < chainNonce {
			logger.Debugf("NonceManager: Cleaning up confirmed nonce %d, wallet=%s, chain=%d, rpc=%s", n, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			delete(nm.pendingNonces, n)
			delete(nm.retryCount, n)
			updated = true
		}
	}
	if chainNonce > nm.localNonce {
		nm.localNonce = chainNonce
	}
	if updated {
		nm.updatePendingNonceMetrics()
	}
}

// evictStalePendingLocked removes pending nonces older than staleNonceThreshold
// evictStalePendingLocked removes pending nonces older than staleNonceThreshold
func (nm *NonceManager) evictStalePendingLocked(now time.Time) {
	var minEvicted uint64 = 0
	hasEvicted := false

	// First pass: identify if any are stale and find the minimum nonce
	for n, info := range nm.pendingNonces {
		if now.Sub(info.Allocated) > staleNonceThreshold {
			logger.Warnf("NonceManager: Evicting stale pending nonce %d (allocated at %v), wallet=%s, chain=%d, rpc=%s", n, info.Allocated, nm.address.Hex(), nm.chainID, nm.getRPCURL())

			if !hasEvicted || n < minEvicted {
				minEvicted = n
				hasEvicted = true
			}
		}
	}

	// If we evicted anything, we must reset the nonce stream from that point
	// to avoid gaps. We remove ALL pending nonces >= minEvicted.
	if hasEvicted {
		logger.Warnf("NonceManager: Stale nonces detected (min: %d). Resetting local nonce to %d and clearing all subsequent pending nonces to prevent gaps.", minEvicted, minEvicted)

		// Reset local nonce to the first evicted one
		// This forces us to re-use this nonce for the next transaction
		nm.localNonce = minEvicted

		// Clear all pending nonces from this point onwards
		// This ensures we don't have "future" nonces waiting while the gap is unfilled
		for n := range nm.pendingNonces {
			if n >= minEvicted {
				delete(nm.pendingNonces, n)
				delete(nm.retryCount, n)
			}
		}

		nm.updatePendingNonceMetrics()
	}
}

// GetNextNonce returns the next available nonce with local tracking
func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Get confirmed nonce
	chainNonce, err := nm.client.NonceAt(ctx, nm.address, nil)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get confirmed nonce, wallet=%s, chain=%d, rpc=%s, error=%v", nm.address.Hex(), nm.chainID, nm.getRPCURL(), err)
		return 0, fmt.Errorf("failed to get confirmed nonce: %w", err)
	}
	if !nm.initialized {
		logger.Infof("NonceManager: Initializing with confirmed nonce %d for wallet=%s, chain=%d, rpc=%s", chainNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	} else if chainNonce > nm.localNonce {
		logger.Infof("NonceManager: Confirmed nonce ahead (confirmed: %d, local: %d) - transactions were mined, syncing, wallet=%s, chain=%d, rpc=%s", chainNonce, nm.localNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	}

	// Sync local state with chain
	nm.cleanupAndSyncLocked(chainNonce)
	// Evict stale pending nonces
	nm.evictStalePendingLocked(time.Now())

	// Handle gap when localNonce is ahead of chainNonce
	if nm.localNonce > chainNonce {
		gap := nm.localNonce - chainNonce
		if gap > nm.maxSafeGap {
			logger.Errorf("NonceManager: ERROR - Local nonce (%d) is %d ahead of chain (%d), wallet=%s, chain=%d, rpc=%s", nm.localNonce, gap, chainNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			logger.Errorf("NonceManager: Gap of %d exceeds max safe gap of %d, wallet=%s, chain=%d, rpc=%s", gap, nm.maxSafeGap, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			logger.Errorf("NonceManager: This means transactions are NOT being broadcast to network! wallet=%s, chain=%d, rpc=%s", nm.address.Hex(), nm.chainID, nm.getRPCURL())
			logger.Errorf("NonceManager: Forcing nonce reset to prevent infinite gap growth, wallet=%s, chain=%d, rpc=%s", nm.address.Hex(), nm.chainID, nm.getRPCURL())

			nm.localNonce = chainNonce
			nm.pendingNonces = make(map[uint64]*NonceInfo)
			nm.retryCount = make(map[uint64]int)
			nm.updatePendingNonceMetrics()

			logger.Warnf("NonceManager: Emergency reset complete. Local nonce: %d, wallet=%s, chain=%d, rpc=%s", nm.localNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			return 0, fmt.Errorf("nonce gap exceeded %d (had %d pending), forced reset to chain nonce %d - check transaction broadcast", nm.maxSafeGap, gap, chainNonce)
		}
		if gap > 50 && gap%10 == 0 {
			logger.Warnf("NonceManager: Local nonce %d ahead of chain %d by %d (pending transactions), wallet=%s, chain=%d, rpc=%s", nm.localNonce, chainNonce, gap, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		}
	}

	nm.initialized = true
	nm.lastSync = time.Now()

	// Allocate or reuse nonce
	nonce := nm.localNonce
	if info, exists := nm.pendingNonces[nonce]; exists && !info.Sent {
		logger.Infof("NonceManager: Reusing pending nonce %d for retry (not yet sent), wallet=%s, chain=%d, rpc=%s", nonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		return nonce, nil
	}
	nm.pendingNonces[nonce] = &NonceInfo{Allocated: time.Now(), Sent: false}
	nm.localNonce++
	nm.updatePendingNonceMetrics()
	logger.Debugf("NonceManager: Allocated nonce %d for wallet=%s (next: %d, pending: %d), chain=%d, rpc=%s", nonce, nm.address.Hex(), nm.localNonce, len(nm.pendingNonces), nm.chainID, nm.getRPCURL())
	return nonce, nil
}

// syncWithChainLocked syncs local nonce with chain state (must be called with lock held)
func (nm *NonceManager) syncWithChainLocked(ctx context.Context) error {
	// Get confirmed nonce from chain
	confirmedNonce, err := nm.client.NonceAt(ctx, nm.address, nil)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get confirmed nonce, wallet=%s, chain=%d, rpc=%s, error=%v", nm.address.Hex(), nm.chainID, nm.getRPCURL(), err)
		return err
	}
	logger.Infof("NonceManager: Syncing - confirmed: %d, local: %d, wallet=%s, chain=%d, rpc=%s", confirmedNonce, nm.localNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())

	// Sync local state with chain
	nm.cleanupAndSyncLocked(confirmedNonce)

	if confirmedNonce < nm.localNonce {
		logger.Debugf("NonceManager: Local nonce (%d) ahead of confirmed (%d) - normal pending state, wallet=%s, chain=%d, rpc=%s", nm.localNonce, confirmedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	}

	nm.initialized = true
	nm.lastSync = time.Now()
	return nil
}

// MarkSent marks a nonce as sent (transaction submitted to mempool)
func (nm *NonceManager) MarkSent(nonce uint64, txHash string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if info, exists := nm.pendingNonces[nonce]; exists {
		info.Sent = true
		info.TxHash = txHash
		logger.Debugf("NonceManager: Marked nonce %d as sent (tx: %s), wallet=%s, chain=%d, rpc=%s", nonce, txHash, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	} else {
		logger.Warnf("NonceManager: Tried to mark unknown nonce %d as sent, wallet=%s, chain=%d, rpc=%s", nonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	}
}

// ConfirmNonce marks a nonce as confirmed (can be cleaned up)
func (nm *NonceManager) ConfirmNonce(nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	logger.Debugf("NonceManager: Confirmed nonce %d, wallet=%s, chain=%d, rpc=%s", nonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
	delete(nm.pendingNonces, nonce)
	delete(nm.retryCount, nonce)

	// If all nonces before this one are also confirmed, we can clean them up
	updated := false
	for n := range nm.pendingNonces {
		if n < nonce {
			logger.Debugf("NonceManager: Cleaning up old nonce %d (current confirmed: %d), wallet=%s, chain=%d, rpc=%s", n, nonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			delete(nm.pendingNonces, n)
			delete(nm.retryCount, n)
			updated = true
		}
	}
	if updated {
		nm.updatePendingNonceMetrics()
	} else {
		nm.updatePendingNonceMetrics() // Always update after deletion
	}
}

// GetRetryCount returns the retry count for a nonce
func (nm *NonceManager) GetRetryCount(nonce uint64) int {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.retryCount[nonce]
}

// IncrementRetryCount increments the retry count for a nonce
func (nm *NonceManager) IncrementRetryCount(nonce uint64) int {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.retryCount[nonce]++
	return nm.retryCount[nonce]
}

// Reset resets the nonce manager and forces resync with chain
func (nm *NonceManager) Reset() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	logger.Warnf("NonceManager: Resetting for wallet=%s, chain=%d, rpc=%s", nm.address.Hex(), nm.chainID, nm.getRPCURL())

	nm.localNonce = 0
	nm.initialized = false
	nm.pendingNonces = make(map[uint64]*NonceInfo)
	nm.retryCount = make(map[uint64]int)
	nm.lastSync = time.Time{} // Force resync on next GetNextNonce
	nm.updatePendingNonceMetrics()
}

// classifyTxError categorizes transaction errors into known types
func classifyTxError(err error) (tooLow bool, replacementUnderpriced bool, txUnderpriced bool, alreadyKnown bool) {
	if err == nil {
		return false, false, false, false
	}
	msg := strings.ToLower(err.Error())
	tooLow = strings.Contains(msg, "nonce too low")
	replacementUnderpriced = strings.Contains(msg, "replacement transaction underpriced")
	txUnderpriced = strings.Contains(msg, "transaction underpriced")
	alreadyKnown = strings.Contains(msg, "already known")
	return
}

// HandleError processes transaction errors and adjusts nonce management
func (nm *NonceManager) HandleError(ctx context.Context, err error, usedNonce uint64) {
	if err == nil {
		return
	}
	tooLow, replUnderpriced, txUnderpriced, alreadyKnown := classifyTxError(err)

	switch {
	case tooLow:
		logger.Warnf("NonceManager: Nonce too low error for nonce %d - chain is ahead, syncing with chain, wallet=%s, chain=%d, rpc=%s", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		nm.mu.Lock()
		chainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if syncErr := nm.syncWithChainLocked(chainCtx); syncErr != nil {
			logger.Errorf("NonceManager: Failed to sync after nonce too low error, wallet=%s, chain=%d, rpc=%s, error=%v", nm.address.Hex(), nm.chainID, nm.getRPCURL(), syncErr)
		} else {
			logger.Infof("NonceManager: Resynced after nonce too low, local nonce now: %d, wallet=%s, chain=%d, rpc=%s", nm.localNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		}
		delete(nm.pendingNonces, usedNonce)
		delete(nm.retryCount, usedNonce)
		nm.updatePendingNonceMetrics()
		nm.mu.Unlock()

	case replUnderpriced || txUnderpriced:
		if replUnderpriced {
			logger.Warnf("NonceManager: Replacement transaction underpriced for nonce %d, wallet=%s, chain=%d, rpc=%s", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		} else {
			logger.Warnf("NonceManager: Transaction underpriced for nonce %d, wallet=%s, chain=%d, rpc=%s", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		}
		count := nm.IncrementRetryCount(usedNonce)
		if count > maxRetryCount {
			logger.Errorf("NonceManager: Nonce %d retry count %d exceeds max %d, evicting pending nonce, wallet=%s, chain=%d, rpc=%s", usedNonce, count, maxRetryCount, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			nm.mu.Lock()
			delete(nm.pendingNonces, usedNonce)
			delete(nm.retryCount, usedNonce)
			nm.updatePendingNonceMetrics()
			nm.mu.Unlock()
			return
		}
		nm.mu.Lock()
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = false
			if replUnderpriced {
				info.TxHash = ""
			}
		}
		nm.mu.Unlock()

	case alreadyKnown:
		logger.Warnf("NonceManager: Transaction already known for nonce %d - already in mempool, wallet=%s, chain=%d, rpc=%s", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		nm.mu.Lock()
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = true
			logger.Debugf("NonceManager: Marked nonce %d as sent (already in mempool), wallet=%s, chain=%d, rpc=%s", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL())
		}
		nm.mu.Unlock()

	default:
		logger.Errorf("NonceManager: Unknown error for nonce %d, wallet=%s, chain=%d, rpc=%s, error=%v", usedNonce, nm.address.Hex(), nm.chainID, nm.getRPCURL(), err)
		count := nm.IncrementRetryCount(usedNonce)
		if count > maxRetryCount {
			logger.Errorf("NonceManager: Nonce %d retry count %d exceeds max %d, evicting pending nonce, wallet=%s, chain=%d, rpc=%s", usedNonce, count, maxRetryCount, nm.address.Hex(), nm.chainID, nm.getRPCURL())
			nm.mu.Lock()
			delete(nm.pendingNonces, usedNonce)
			delete(nm.retryCount, usedNonce)
			nm.updatePendingNonceMetrics()
			nm.mu.Unlock()
			return
		}
		nm.mu.Lock()
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = false
			info.TxHash = ""
		}
		nm.mu.Unlock()
	}
}

// GetPendingNonces returns the count of pending nonces
func (nm *NonceManager) GetPendingNonces() int {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return len(nm.pendingNonces)
}

// ForceSyncWithChain forces immediate sync with chain (useful for debugging)
func (nm *NonceManager) ForceSyncWithChain(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.syncWithChainLocked(ctx)
}
