package contracts

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// NonceManager manages nonces for transaction sending
type NonceManager struct {
	client      *ethclient.Client
	address     common.Address
	mu          sync.Mutex
	currentNonce uint64
	pendingNonces map[uint64]bool
}

// NewNonceManager creates a new nonce manager
func NewNonceManager(client *ethclient.Client, address common.Address) *NonceManager {
	return &NonceManager{
		client:        client,
		address:       address,
		pendingNonces: make(map[uint64]bool),
	}
}

// GetNextNonce returns the next available nonce
func (nm *NonceManager) GetNextNonce(ctx context.Context) (uint64, error) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Get the latest nonce from chain if we don't have one
	if nm.currentNonce == 0 {
		nonce, err := nm.client.PendingNonceAt(ctx, nm.address)
		if err != nil {
			return 0, err
		}
		nm.currentNonce = nonce
	}

	// Find next available nonce
	nonce := nm.currentNonce
	for nm.pendingNonces[nonce] {
		nonce++
	}

	// Mark as pending
	nm.pendingNonces[nonce] = true
	
	// Update current nonce if needed
	if nonce >= nm.currentNonce {
		nm.currentNonce = nonce + 1
	}

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
}

// ConfirmNonce marks a nonce as confirmed
func (nm *NonceManager) ConfirmNonce(nonce uint64) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	delete(nm.pendingNonces, nonce)
}

// Reset resets the nonce manager
func (nm *NonceManager) Reset() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	nm.currentNonce = 0
	nm.pendingNonces = make(map[uint64]bool)
}