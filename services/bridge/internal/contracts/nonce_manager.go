package contracts

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// NonceManager manages nonces for transaction sending with local tracking
type NonceManager struct {
	client        *ethclient.Client
	address       common.Address
	mu            sync.Mutex
	localNonce    uint64           // Our local tracked nonce (next to use)
	initialized   bool             // Whether we've synced with chain
	pendingNonces map[uint64]*NonceInfo // Track nonce usage
	retryCount    map[uint64]int   // Track retry attempts per nonce
	lastSync      time.Time        // Last time we synced with chain
}

// NonceInfo tracks information about a nonce
type NonceInfo struct {
	Allocated time.Time
	Sent      bool
	TxHash    string
}

// NewNonceManager creates a new nonce manager
func NewNonceManager(client *ethclient.Client, address common.Address) *NonceManager {
	return &NonceManager{
		client:        client,
		address:       address,
		pendingNonces: make(map[uint64]*NonceInfo),
		retryCount:    make(map[uint64]int),
		initialized:   false,
	}
}

// GetNextNonce returns the next available nonce with local tracking
func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	// CRITICAL: Fetch chain nonce WITHOUT holding lock for better concurrency
	// This allows multiple threads to make RPC calls in parallel
	pendingNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get pending nonce: %v", err)
		return 0, fmt.Errorf("failed to get pending nonce: %w", err)
	}

	// Now acquire lock for the actual nonce allocation (fast, in-memory operations)
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// If chain is ahead of our tracking, sync up immediately
	if pendingNonce > nm.localNonce {
		logger.Warnf("NonceManager: Chain moved ahead (chain: %d, local: %d), syncing", pendingNonce, nm.localNonce)
		nm.localNonce = pendingNonce

		// Clean up any nonces below chain nonce (they're already confirmed)
		for n := range nm.pendingNonces {
			if n < pendingNonce {
				logger.Debugf("NonceManager: Cleaning up confirmed nonce %d", n)
				delete(nm.pendingNonces, n)
				delete(nm.retryCount, n)
			}
		}
	}

	nm.initialized = true

	// Find the next available nonce
	nonce := nm.localNonce

	// If this nonce is already pending and not sent, reuse it (retry case)
	if info, exists := nm.pendingNonces[nonce]; exists && !info.Sent {
		logger.Infof("NonceManager: Reusing pending nonce %d for retry (not yet sent)", nonce)
		return nonce, nil
	}

	// Otherwise, mark as allocated and increment for next call
	nm.pendingNonces[nonce] = &NonceInfo{
		Allocated: time.Now(),
		Sent:      false,
	}

	nm.localNonce++

	logger.Debugf("NonceManager: Allocated nonce %d for address %s (next: %d, pending: %d)",
		nonce, nm.address.Hex(), nm.localNonce, len(nm.pendingNonces))

	return nonce, nil
}

// syncWithChainLocked syncs local nonce with chain state (must be called with lock held)
func (nm *NonceManager) syncWithChainLocked(ctx context.Context) error {
	// Get confirmed nonce from chain
	confirmedNonce, err := nm.client.NonceAt(ctx, nm.address, nil)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get confirmed nonce for %s: %v", nm.address.Hex(), err)
		return err
	}

	// Get pending nonce from mempool
	pendingNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get pending nonce for %s: %v", nm.address.Hex(), err)
		return err
	}

	logger.Infof("NonceManager: Syncing - confirmed: %d, pending: %d, local: %d",
		confirmedNonce, pendingNonce, nm.localNonce)

	// Clean up confirmed nonces
	for n := range nm.pendingNonces {
		if n < confirmedNonce {
			logger.Debugf("NonceManager: Cleaning up confirmed nonce %d", n)
			delete(nm.pendingNonces, n)
			delete(nm.retryCount, n)
		}
	}

	// Determine which nonce to use
	var targetNonce uint64

	if pendingNonce > confirmedNonce {
		// We have transactions in mempool
		targetNonce = pendingNonce
		logger.Infof("NonceManager: Using pending nonce %d (transactions in mempool)", pendingNonce)
	} else {
		// No pending transactions, use confirmed
		targetNonce = confirmedNonce
		logger.Infof("NonceManager: Using confirmed nonce %d (no pending transactions)", confirmedNonce)
	}

	// If chain is ahead of our local tracking, catch up
	if targetNonce > nm.localNonce {
		logger.Warnf("NonceManager: Chain nonce (%d) ahead of local (%d), catching up",
			targetNonce, nm.localNonce)
		nm.localNonce = targetNonce

		// Clear any stale pending nonces below the chain nonce
		for n := range nm.pendingNonces {
			if n < targetNonce {
				delete(nm.pendingNonces, n)
				delete(nm.retryCount, n)
			}
		}
	} else if targetNonce < nm.localNonce {
		// Chain is behind our local tracking - this is normal (we allocated but haven't sent yet)
		logger.Debugf("NonceManager: Local nonce (%d) ahead of chain (%d) - normal pending state",
			nm.localNonce, targetNonce)
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
		logger.Debugf("NonceManager: Marked nonce %d as sent (tx: %s)", nonce, txHash)
	} else {
		logger.Warnf("NonceManager: Tried to mark unknown nonce %d as sent", nonce)
	}
}

// ConfirmNonce marks a nonce as confirmed (can be cleaned up)
func (nm *NonceManager) ConfirmNonce(nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	logger.Debugf("NonceManager: Confirmed nonce %d", nonce)
	delete(nm.pendingNonces, nonce)
	delete(nm.retryCount, nonce)

	// If all nonces before this one are also confirmed, we can clean them up
	for n := range nm.pendingNonces {
		if n < nonce {
			logger.Debugf("NonceManager: Cleaning up old nonce %d (current confirmed: %d)", n, nonce)
			delete(nm.pendingNonces, n)
			delete(nm.retryCount, n)
		}
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

	logger.Warnf("NonceManager: Resetting for address %s", nm.address.Hex())

	nm.localNonce = 0
	nm.initialized = false
	nm.pendingNonces = make(map[uint64]*NonceInfo)
	nm.retryCount = make(map[uint64]int)
	nm.lastSync = time.Time{} // Force resync on next GetNextNonce
}

// HandleError processes transaction errors and adjusts nonce management
func (nm *NonceManager) HandleError(ctx context.Context, err error, usedNonce uint64) {
	if err == nil {
		return
	}

	errStr := err.Error()

	// Handle different error types
	if strings.Contains(errStr, "nonce too low") {
		logger.Warnf("NonceManager: Nonce too low error for nonce %d - chain is ahead, syncing with chain", usedNonce)

		// Full resync with chain to get current nonce state
		nm.mu.Lock()

		// Sync with chain to get accurate nonce
		chainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := nm.syncWithChainLocked(chainCtx); err != nil {
			logger.Errorf("NonceManager: Failed to sync after nonce too low error: %v", err)
		} else {
			logger.Infof("NonceManager: Resynced after nonce too low, local nonce now: %d", nm.localNonce)
		}

		// Clean up the failed nonce
		delete(nm.pendingNonces, usedNonce)
		delete(nm.retryCount, usedNonce)

		nm.mu.Unlock()

	} else if strings.Contains(errStr, "replacement transaction underpriced") {
		logger.Warnf("NonceManager: Replacement transaction underpriced for nonce %d", usedNonce)
		// This is handled by gas price bump in receiver.go UpdateAuth
		// Just increment retry count
		nm.IncrementRetryCount(usedNonce)

		nm.mu.Lock()
		// Mark as not sent so it can be retried with higher gas
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = false
			info.TxHash = ""
		}
		nm.mu.Unlock()

	} else if strings.Contains(errStr, "already known") {
		logger.Warnf("NonceManager: Transaction already known for nonce %d - already in mempool", usedNonce)
		// Transaction is already in mempool, mark as sent
		nm.mu.Lock()
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = true
			logger.Debugf("NonceManager: Marked nonce %d as sent (already in mempool)", usedNonce)
		}
		nm.mu.Unlock()

	} else if strings.Contains(errStr, "transaction underpriced") {
		logger.Warnf("NonceManager: Transaction underpriced for nonce %d", usedNonce)
		nm.IncrementRetryCount(usedNonce)

		nm.mu.Lock()
		if info, exists := nm.pendingNonces[usedNonce]; exists {
			info.Sent = false
		}
		nm.mu.Unlock()

	} else {
		// Unknown error - log and mark for retry
		logger.Errorf("NonceManager: Unknown error for nonce %d: %v", usedNonce, err)
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
