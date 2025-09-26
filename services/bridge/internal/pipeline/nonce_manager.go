package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// NonceManager manages nonce allocation for addresses across different chains
type NonceManager struct {
	clients    map[int64]*ethclient.Client
	nonces     map[string]*NonceTracker // key: chainID-address
	mutex      sync.RWMutex
}

// NonceTracker tracks nonce state for a specific address on a chain
type NonceTracker struct {
	currentNonce uint64
	lastSync     time.Time
	mutex        sync.Mutex
}

// NewNonceManager creates a new nonce manager
func NewNonceManager(clients map[int64]*ethclient.Client) *NonceManager {
	return &NonceManager{
		clients: clients,
		nonces:  make(map[string]*NonceTracker),
	}
}

// GetNextNonce returns the next available nonce for an address on a chain
func (nm *NonceManager) GetNextNonce(ctx context.Context, chainID int64, address common.Address) (uint64, error) {
	key := nm.getKey(chainID, address)
	
	nm.mutex.Lock()
	tracker, exists := nm.nonces[key]
	if !exists {
		tracker = &NonceTracker{
			lastSync: time.Time{}, // Force initial sync
		}
		nm.nonces[key] = tracker
	}
	nm.mutex.Unlock()

	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()

	// Sync with network periodically or if never synced
	if time.Since(tracker.lastSync) > 30*time.Second {
		if err := nm.syncNonce(ctx, chainID, address, tracker); err != nil {
			logger.Warnf("Failed to sync nonce for %s on chain %d: %v", address.Hex(), chainID, err)
			// Continue with cached nonce on error, but try to increment it
		}
	}

	nonce := tracker.currentNonce
	tracker.currentNonce++
	
	logger.Debugf("Allocated nonce %d for address %s on chain %d", nonce, address.Hex(), chainID)
	return nonce, nil
}

// syncNonce synchronizes the nonce with the network
func (nm *NonceManager) syncNonce(ctx context.Context, chainID int64, address common.Address, tracker *NonceTracker) error {
	client, exists := nm.clients[chainID]
	if !exists {
		return fmt.Errorf("no client for chain %d", chainID)
	}

	networkNonce, err := client.PendingNonceAt(ctx, address)
	if err != nil {
		return err
	}

	// Use the higher of network nonce or current tracked nonce
	if networkNonce > tracker.currentNonce {
		logger.Debugf("Syncing nonce for %s on chain %d: local=%d network=%d", 
			address.Hex(), chainID, tracker.currentNonce, networkNonce)
		tracker.currentNonce = networkNonce
	}
	
	tracker.lastSync = time.Now()
	return nil
}

// ResetNonce forces a nonce reset for an address (useful after failed transactions)
func (nm *NonceManager) ResetNonce(ctx context.Context, chainID int64, address common.Address) error {
	key := nm.getKey(chainID, address)
	
	nm.mutex.Lock()
	tracker, exists := nm.nonces[key]
	if !exists {
		tracker = &NonceTracker{}
		nm.nonces[key] = tracker
	}
	nm.mutex.Unlock()

	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()

	return nm.syncNonce(ctx, chainID, address, tracker)
}

// getKey generates a unique key for chainID and address combination
func (nm *NonceManager) getKey(chainID int64, address common.Address) string {
	return address.Hex() + "-" + fmt.Sprintf("%d", chainID)
}