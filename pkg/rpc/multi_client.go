package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// MultiClient wraps multiple Ethereum clients with automatic failover
type MultiClient struct {
	urls            []string
	clients         []*ethclient.Client
	currentIndex    int
	mu              sync.RWMutex
	lastHealthCheck time.Time
	healthInterval  time.Duration
}

// NewMultiClient creates a new multi-client with failover support
func NewMultiClient(urls []string) (*MultiClient, error) {
	if len(urls) == 0 {
		return nil, errors.New("no RPC URLs provided")
	}

	mc := &MultiClient{
		urls:           urls,
		clients:        make([]*ethclient.Client, len(urls)),
		currentIndex:   0,
		healthInterval: 30 * time.Second,
	}

	// Try to connect to each URL
	var lastErr error
	for i, url := range urls {
		client, err := ethclient.Dial(url)
		if err != nil {
			logger.Warnf("Failed to connect to RPC %s: %v", url, err)
			lastErr = err
			continue
		}
		mc.clients[i] = client

		// Test the connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = client.ChainID(ctx)
		cancel()

		if err != nil {
			logger.Warnf("RPC %s failed health check: %v", url, err)
			client.Close()
			mc.clients[i] = nil
			lastErr = err
		} else {
			logger.Infof("Successfully connected to RPC: %s", url)
			if mc.currentIndex == -1 {
				mc.currentIndex = i
			}
		}
	}

	// Find first working client
	mc.currentIndex = -1
	for i, client := range mc.clients {
		if client != nil {
			mc.currentIndex = i
			break
		}
	}

	if mc.currentIndex == -1 {
		return nil, fmt.Errorf("failed to connect to any RPC URL: %v", lastErr)
	}

	// Start health check routine
	go mc.healthCheckLoop()

	return mc, nil
}

// getCurrentClient returns the current active client
func (mc *MultiClient) getCurrentClient() (*ethclient.Client, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.currentIndex >= 0 && mc.currentIndex < len(mc.clients) && mc.clients[mc.currentIndex] != nil {
		return mc.clients[mc.currentIndex], nil
	}

	return nil, errors.New("no active RPC client available")
}

// ActiveURL returns the currently selected RPC endpoint.
func (mc *MultiClient) ActiveURL() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.currentIndex >= 0 && mc.currentIndex < len(mc.urls) {
		return mc.urls[mc.currentIndex]
	}

	return ""
}

// failover switches to the next available RPC
func (mc *MultiClient) failover() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	originalIndex := mc.currentIndex
	logger.Warnf("RPC failover triggered from %s", mc.urls[originalIndex])

	// Try next RPCs in order
	for i := 0; i < len(mc.clients); i++ {
		nextIndex := (originalIndex + i + 1) % len(mc.clients)

		// Skip if client is nil
		if mc.clients[nextIndex] == nil {
			// Try to reconnect
			client, err := ethclient.Dial(mc.urls[nextIndex])
			if err != nil {
				continue
			}

			// Test connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = client.ChainID(ctx)
			cancel()

			if err != nil {
				client.Close()
				continue
			}

			mc.clients[nextIndex] = client
		}

		// Test if client is working
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := mc.clients[nextIndex].BlockNumber(ctx)
		cancel()

		if err == nil {
			mc.currentIndex = nextIndex
			logger.Infof("Failed over to RPC: %s", mc.urls[nextIndex])
			return nil
		}
	}

	return fmt.Errorf("all RPC endpoints are unavailable")
}

// healthCheckLoop periodically checks RPC health
func (mc *MultiClient) healthCheckLoop() {
	ticker := time.NewTicker(mc.healthInterval)
	defer ticker.Stop()

	for range ticker.C {
		mc.performHealthCheck()
	}
}

// performHealthCheck checks all RPC endpoints
func (mc *MultiClient) performHealthCheck() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for i, url := range mc.urls {
		if mc.clients[i] == nil {
			// Try to reconnect
			client, err := ethclient.Dial(url)
			if err != nil {
				continue
			}
			mc.clients[i] = client
			logger.Infof("Reconnected to RPC: %s", url)
		}

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := mc.clients[i].BlockNumber(ctx)
		cancel()

		if err != nil {
			logger.Debugf("RPC %s health check failed: %v", url, err)
			if mc.clients[i] != nil {
				mc.clients[i].Close()
				mc.clients[i] = nil
			}
		}
	}

	mc.lastHealthCheck = time.Now()
}

// withRetry executes a function with automatic failover on error
func (mc *MultiClient) withRetry(fn func(*ethclient.Client) error) error {
	maxRetries := len(mc.clients)
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		client, err := mc.getCurrentClient()
		if err != nil {
			if err := mc.failover(); err != nil {
				return err
			}
			continue
		}

		err = fn(client)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is network related
		if isNetworkError(err) {
			logger.Warnf("Network error on RPC %s: %v", mc.urls[mc.currentIndex], err)
			if err := mc.failover(); err != nil {
				return fmt.Errorf("failover failed: %v, original error: %v", err, lastErr)
			}
			continue
		}

		// Non-network error, don't retry
		return err
	}

	return fmt.Errorf("all retries exhausted: %v", lastErr)
}

// isNetworkError checks if an error is network related
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	networkErrors := []string{
		"connection refused",
		"no such host",
		"timeout",
		"EOF",
		"broken pipe",
		"reset by peer",
		"i/o timeout",
		"TLS handshake timeout",
		"dial tcp",
		"read tcp",
		"write tcp",
	}

	for _, netErr := range networkErrors {
		if contains(errStr, netErr) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				len(s) > len(substr) && findSubstring(s[1:len(s)-1], substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Close closes all clients
func (mc *MultiClient) Close() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for i, client := range mc.clients {
		if client != nil {
			client.Close()
			mc.clients[i] = nil
		}
	}
}

// GetClient returns the underlying ethclient for compatibility
func (mc *MultiClient) GetClient() (*ethclient.Client, error) {
	return mc.getCurrentClient()
}

// Implement ethclient.Client interface methods with automatic failover

func (mc *MultiClient) ChainID(ctx context.Context) (*big.Int, error) {
	var result *big.Int
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.ChainID(ctx)
		return err
	})
	return result, err
}

func (mc *MultiClient) BlockNumber(ctx context.Context) (uint64, error) {
	var result uint64
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.BlockNumber(ctx)
		return err
	})
	return result, err
}

func (mc *MultiClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	var result *types.Block
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.BlockByNumber(ctx, number)
		return err
	})
	return result, err
}

func (mc *MultiClient) TransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	var tx *types.Transaction
	var isPending bool
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		tx, isPending, err = client.TransactionByHash(ctx, hash)
		return err
	})
	return tx, isPending, err
}

func (mc *MultiClient) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	var result *types.Receipt
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.TransactionReceipt(ctx, txHash)
		return err
	})
	return result, err
}

func (mc *MultiClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	var result *big.Int
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.BalanceAt(ctx, account, blockNumber)
		return err
	})
	return result, err
}

func (mc *MultiClient) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	var result uint64
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.NonceAt(ctx, account, blockNumber)
		return err
	})
	return result, err
}

func (mc *MultiClient) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	var result uint64
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.PendingNonceAt(ctx, account)
		return err
	})
	return result, err
}

func (mc *MultiClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	var result *big.Int
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.SuggestGasPrice(ctx)
		return err
	})
	return result, err
}

func (mc *MultiClient) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	var result uint64
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.EstimateGas(ctx, msg)
		return err
	})
	return result, err
}

func (mc *MultiClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return mc.withRetry(func(client *ethclient.Client) error {
		return client.SendTransaction(ctx, tx)
	})
}

func (mc *MultiClient) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	var result []byte
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.CallContract(ctx, msg, blockNumber)
		return err
	})
	return result, err
}

func (mc *MultiClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	var result []types.Log
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.FilterLogs(ctx, q)
		return err
	})
	return result, err
}

func (mc *MultiClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	client, err := mc.getCurrentClient()
	if err != nil {
		return nil, err
	}
	// Note: Subscriptions don't support automatic failover
	return client.SubscribeFilterLogs(ctx, q, ch)
}

func (mc *MultiClient) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	var result []byte
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.PendingCodeAt(ctx, account)
		return err
	})
	return result, err
}

func (mc *MultiClient) PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	var result []byte
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.PendingCallContract(ctx, msg)
		return err
	})
	return result, err
}

func (mc *MultiClient) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	var result []byte
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.CodeAt(ctx, account, blockNumber)
		return err
	})
	return result, err
}

func (mc *MultiClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	var result *types.Header
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.HeaderByNumber(ctx, number)
		return err
	})
	return result, err
}

func (mc *MultiClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	var result *big.Int
	err := mc.withRetry(func(client *ethclient.Client) error {
		var err error
		result, err = client.SuggestGasTipCap(ctx)
		return err
	})
	return result, err
}

func (mc *MultiClient) Client() *rpc.Client {
	// Return the underlying RPC client of the current active client
	client, err := mc.getCurrentClient()
	if err != nil {
		return nil
	}
	return client.Client()
}

// GetCurrentRPCURL returns the currently active RPC URL
func (mc *MultiClient) GetCurrentRPCURL() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.currentIndex >= 0 && mc.currentIndex < len(mc.urls) {
		return mc.urls[mc.currentIndex]
	}
	return ""
}

// GetHealthStatus returns the health status of all RPCs
func (mc *MultiClient) GetHealthStatus() map[string]bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	status := make(map[string]bool)
	for i, url := range mc.urls {
		status[url] = mc.clients[i] != nil
	}
	return status
}
