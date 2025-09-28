package contracts

import (
	"context"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// NonceManager manages nonces for transaction sending
type NonceManager struct {
	client      *ethclient.Client
	address     common.Address
	mu          sync.Mutex
	currentNonce uint64
	pendingNonces map[uint64]bool
	retryCount   map[uint64]int  // Track retry attempts per nonce
}

// NewNonceManager creates a new nonce manager
func NewNonceManager(client *ethclient.Client, address common.Address) *NonceManager {
	return &NonceManager{
		client:        client,
		address:       address,
		pendingNonces: make(map[uint64]bool),
		retryCount:    make(map[uint64]int),
	}
}

// GetNextNonce returns the next available nonce
func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// ALWAYS get the confirmed nonce from chain - ignore any local state
	// This ensures we're always in sync with the blockchain
	confirmedNonce, err := nm.client.NonceAt(ctx, nm.address, nil)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get confirmed nonce for %s: %v", nm.address.Hex(), err)
		return 0, err
	}

	// Also check pending to see if we have stuck transactions
	pendingNonce, err := nm.client.PendingNonceAt(ctx, nm.address)
	if err != nil {
		logger.Errorf("NonceManager: Failed to get pending nonce for %s: %v", nm.address.Hex(), err)
		return 0, err
	}

	logger.Infof("NonceManager: Chain state - confirmed: %d, pending: %d", confirmedNonce, pendingNonce)

	// Prefer pending nonce when it's ahead of confirmed nonce
	var nonce uint64
	if pendingNonce > confirmedNonce {
		// Use pending nonce to continue from where we left off
		nonce = pendingNonce
		logger.Infof("NonceManager: Using pending nonce %d (ahead of confirmed %d)", pendingNonce, confirmedNonce)
		// Reset retry count since we're advancing
		nm.retryCount[nonce] = 0
	} else {
		// Use confirmed nonce when pending is not ahead
		nonce = confirmedNonce
		nm.retryCount[nonce] = 0
		logger.Infof("NonceManager: Using confirmed nonce %d (pending: %d)", confirmedNonce, pendingNonce)
	}

	// Clear any old retry counts
	for n := range nm.retryCount {
		if n < confirmedNonce {
			delete(nm.retryCount, n)
		}
	}

	logger.Infof("NonceManager: Returning nonce %d for address %s", nonce, nm.address.Hex())

	return nonce, nil
}

// ReturnNonce returns a nonce that failed to send
func (nm *NonceManager) ReturnNonce(nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	delete(nm.pendingNonces, nonce)
	
	// If this was the current nonce, decrement
	if nonce+1 == nm.currentNonce && len(nm.pendingNonces) == 0 {
		nm.currentNonce = nonce
	}
	
	logger.Debugf("NonceManager: Returned nonce %d for address %s", nonce, nm.address.Hex())
}

// ConfirmNonce marks a nonce as confirmed
func (nm *NonceManager) ConfirmNonce(nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	delete(nm.pendingNonces, nonce)
	delete(nm.retryCount, nonce)
	logger.Debugf("NonceManager: Confirmed nonce %d for address %s", nonce, nm.address.Hex())
}

// GetRetryCount returns the retry count for a nonce
func (nm *NonceManager) GetRetryCount(nonce uint64) int {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.retryCount[nonce]
}

// Reset resets the nonce manager
func (nm *NonceManager) Reset() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	nm.currentNonce = 0
	nm.pendingNonces = make(map[uint64]bool)
	nm.retryCount = make(map[uint64]int)
	logger.Infof("NonceManager: Reset for address %s", nm.address.Hex())
}

// HandleError processes transaction errors and adjusts nonce management
func (nm *NonceManager) HandleError(ctx context.Context, err error, usedNonce uint64) {
	if err == nil {
		return
	}
	
	errStr := err.Error()
	
	// Handle different error types
	if strings.Contains(errStr, "nonce too low") {
		logger.Warnf("NonceManager: Nonce too low error for nonce %d, resetting", usedNonce)
		nm.Reset()
	} else if strings.Contains(errStr, "replacement transaction underpriced") {
		logger.Warnf("NonceManager: Replacement transaction underpriced for nonce %d, likely stuck transactions", usedNonce)
		// Too many stuck transactions, reset to get fresh nonce from chain
		nm.Reset()
	} else if strings.Contains(errStr, "already known") {
		logger.Warnf("NonceManager: Transaction already known for nonce %d", usedNonce)
		// The nonce is already in use
		nm.mu.Lock()
		delete(nm.pendingNonces, usedNonce)
		nm.mu.Unlock()
	}
}