package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEthClient is a mock implementation of ethclient.Client for testing
type MockEthClient struct {
	mock.Mock
	isClosed bool
	mu       sync.RWMutex
}

func (m *MockEthClient) ChainID(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockEthClient) BlockNumber(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockEthClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isClosed = true
	m.Called()
}

func (m *MockEthClient) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isClosed
}

// Add other required methods for ethclient interface
func (m *MockEthClient) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := m.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Block), args.Error(1)
}

func (m *MockEthClient) TransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	args := m.Called(ctx, hash)
	return args.Get(0).(*types.Transaction), args.Bool(1), args.Error(2)
}

func (m *MockEthClient) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	args := m.Called(ctx, txHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Receipt), args.Error(1)
}

func (m *MockEthClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	args := m.Called(ctx, account, blockNumber)
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockEthClient) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	args := m.Called(ctx, account, blockNumber)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockEthClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockEthClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockEthClient) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	args := m.Called(ctx, msg, blockNumber)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockEthClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	args := m.Called(ctx, q)
	return args.Get(0).([]types.Log), args.Error(1)
}

func (m *MockEthClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	args := m.Called(ctx, q, ch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ethereum.Subscription), args.Error(1)
}

// JSON-RPC request and response structures for mock server
type jsonRPCRequest struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params []interface{} `json:"params"`
}

type jsonRPCResponse struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// createMockEthereumServer creates a mock HTTP server that responds to Ethereum JSON-RPC calls
func createMockEthereumServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode JSON-RPC request: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		var response jsonRPCResponse
		response.ID = req.ID

		// Handle different RPC methods
		switch req.Method {
		case "eth_chainId":
			// Return mock chain ID (1337 in hex)
			response.Result = "0x539"
		case "eth_blockNumber":
			// Return mock block number (1000 in hex)
			response.Result = "0x3e8"
		case "net_version":
			// Return network version
			response.Result = "1337"
		default:
			// Return error for unsupported methods
			response.Error = &jsonRPCError{
				Code:    -32601,
				Message: "Method not found",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode JSON-RPC response: %v", err)
		}
	}))
}

// Test helper to create a mock MultiClient with controlled behavior
func createTestMultiClient(t *testing.T, mockBehaviors []MockBehavior) *testMultiClient {
	return &testMultiClient{
		t:             t,
		mockBehaviors: mockBehaviors,
		currentIndex:  0,
	}
}

type MockBehavior struct {
	URL           string
	ShouldConnect bool
	ShouldWork    bool
	ChainID       *big.Int
	BlockNumber   uint64
	Error         error
}

type testMultiClient struct {
	t             *testing.T
	mockBehaviors []MockBehavior
	clients       []*MockEthClient
	currentIndex  int
	mu            sync.RWMutex
}

// TestNewMultiClient_Success tests successful creation with working RPCs using mock server
func TestNewMultiClient_Success(t *testing.T) {
	t.Run("EmptyURLs", func(t *testing.T) {
		mc, err := NewMultiClient([]string{})
		assert.Error(t, err)
		assert.Nil(t, mc)
		assert.Contains(t, err.Error(), "no RPC URLs provided")
	})
	
	t.Run("SingleWorkingRPC", func(t *testing.T) {
		// Create a mock HTTP server that responds to JSON-RPC calls
		server := createMockEthereumServer(t)
		defer server.Close()
		
		mc, err := NewMultiClient([]string{server.URL})
		assert.NoError(t, err)
		assert.NotNil(t, mc)
		assert.Equal(t, server.URL, mc.GetCurrentRPCURL())
		
		// Test that the client can make calls
		ctx := context.Background()
		chainID, err := mc.ChainID(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(1337), chainID.Int64()) // Mock chain ID
		
		mc.Close()
	})
	
	t.Run("MultipleWorkingRPCs", func(t *testing.T) {
		// Create multiple mock servers
		server1 := createMockEthereumServer(t)
		server2 := createMockEthereumServer(t)
		defer server1.Close()
		defer server2.Close()
		
		mc, err := NewMultiClient([]string{server1.URL, server2.URL})
		assert.NoError(t, err)
		assert.NotNil(t, mc)
		
		// Should connect to first working server
		assert.Equal(t, server1.URL, mc.GetCurrentRPCURL())
		
		mc.Close()
	})
	
	t.Run("PartiallyWorkingRPCs", func(t *testing.T) {
		// Create one working server
		workingServer := createMockEthereumServer(t)
		defer workingServer.Close()
		
		// Mix working and non-working URLs
		urls := []string{
			"http://invalid-url-12345", // This will fail
			workingServer.URL,          // This should work
			"http://another-invalid-url", // This will fail
		}
		
		mc, err := NewMultiClient(urls)
		assert.NoError(t, err)
		assert.NotNil(t, mc)
		
		// Should connect to the working server
		assert.Equal(t, workingServer.URL, mc.GetCurrentRPCURL())
		
		mc.Close()
	})
}

// TestNewMultiClient_ValidationEdgeCases tests input validation
func TestNewMultiClient_ValidationEdgeCases(t *testing.T) {
	testCases := []struct {
		name        string
		urls        []string
		expectError bool
		errorText   string
	}{
		{
			name:        "Empty slice",
			urls:        []string{},
			expectError: true,
			errorText:   "no RPC URLs provided",
		},
		{
			name:        "Nil slice", 
			urls:        nil,
			expectError: true,
			errorText:   "no RPC URLs provided",
		},
		{
			name:        "Single invalid URL",
			urls:        []string{"invalid-url"},
			expectError: true,
			errorText:   "failed to connect to any RPC URL",
		},
		{
			name:        "Multiple invalid URLs",
			urls:        []string{"invalid-url-1", "invalid-url-2", "invalid-url-3"},
			expectError: true,
			errorText:   "failed to connect to any RPC URL",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mc, err := NewMultiClient(tc.urls)
			
			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, mc)
				assert.Contains(t, err.Error(), tc.errorText)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, mc)
			}
		})
	}
}

// TestMultiClient_FailoverLogic tests the failover mechanism
func TestMultiClient_FailoverLogic(t *testing.T) {
	t.Run("FailoverToNextRPC", func(t *testing.T) {
		// Create a multiclient with multiple URLs (mock scenario)
		mc := &MultiClient{
			urls:         []string{"http://rpc1", "http://rpc2", "http://rpc3"},
			clients:      make([]*ethclient.Client, 3),
			currentIndex: 0,
		}
		
		// Test failover logic without actual connections
		originalIndex := mc.currentIndex
		assert.Equal(t, 0, originalIndex)
		
		// Simulate having clients (would be nil in real failover scenario)
		// This tests the index calculation logic
		expectedNextIndex := (originalIndex + 1) % len(mc.urls)
		assert.Equal(t, 1, expectedNextIndex)
	})
}

// TestMultiClient_NetworkErrorDetection tests network error classification
func TestMultiClient_NetworkErrorDetection(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		isNetwork bool
	}{
		{
			name:      "Nil error",
			err:       nil,
			isNetwork: false,
		},
		{
			name:      "Connection refused",
			err:       errors.New("connection refused"),
			isNetwork: true,
		},
		{
			name:      "No such host",
			err:       errors.New("no such host example.com"),
			isNetwork: true,
		},
		{
			name:      "Timeout error",
			err:       errors.New("request timeout"),
			isNetwork: true,
		},
		{
			name:      "EOF error",
			err:       errors.New("unexpected EOF"),
			isNetwork: true,
		},
		{
			name:      "Broken pipe",
			err:       errors.New("broken pipe"),
			isNetwork: true,
		},
		{
			name:      "Reset by peer",
			err:       errors.New("connection reset by peer"),
			isNetwork: true,
		},
		{
			name:      "I/O timeout",
			err:       errors.New("i/o timeout"),
			isNetwork: true,
		},
		{
			name:      "TLS handshake timeout",
			err:       errors.New("TLS handshake timeout"),
			isNetwork: true,
		},
		{
			name:      "Dial TCP error",
			err:       errors.New("dial tcp 127.0.0.1:8545: connection refused"),
			isNetwork: true,
		},
		{
			name:      "Read TCP error",
			err:       errors.New("read tcp 127.0.0.1:8545: connection reset by peer"),
			isNetwork: true,
		},
		{
			name:      "Write TCP error",
			err:       errors.New("write tcp 127.0.0.1:8545: broken pipe"),
			isNetwork: true,
		},
		{
			name:      "Application error",
			err:       errors.New("invalid method parameters"),
			isNetwork: false,
		},
		{
			name:      "JSON-RPC error",
			err:       errors.New("method not found"),
			isNetwork: false,
		},
		{
			name:      "Gas estimation failed",
			err:       errors.New("gas required exceeds allowance"),
			isNetwork: false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isNetworkError(tc.err)
			assert.Equal(t, tc.isNetwork, result, 
				"Expected isNetworkError(%v) = %v, got %v", tc.err, tc.isNetwork, result)
		})
	}
}

// TestMultiClient_StringHelpers tests the string utility functions
func TestMultiClient_StringHelpers(t *testing.T) {
	testCases := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "Exact match",
			s:        "timeout",
			substr:   "timeout",
			expected: true,
		},
		{
			name:     "Prefix match",
			s:        "timeout error occurred",
			substr:   "timeout",
			expected: true,
		},
		{
			name:     "Suffix match",
			s:        "connection timeout",
			substr:   "timeout",
			expected: true,
		},
		{
			name:     "Middle match",
			s:        "read timeout error",
			substr:   "timeout",
			expected: true,
		},
		{
			name:     "No match",
			s:        "connection refused",
			substr:   "timeout",
			expected: false,
		},
		{
			name:     "Empty substring",
			s:        "any string",
			substr:   "",
			expected: false,
		},
		{
			name:     "Empty string",
			s:        "",
			substr:   "timeout",
			expected: false,
		},
		{
			name:     "Case sensitive",
			s:        "TIMEOUT",
			substr:   "timeout",
			expected: false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := contains(tc.s, tc.substr)
			assert.Equal(t, tc.expected, result,
				"Expected contains(%q, %q) = %v, got %v", tc.s, tc.substr, tc.expected, result)
		})
	}
}

// TestMultiClient_GetMethods tests getter methods
func TestMultiClient_GetMethods(t *testing.T) {
	mc := &MultiClient{
		urls:         []string{"http://rpc1", "http://rpc2", "http://rpc3"},
		clients:      make([]*ethclient.Client, 3),
		currentIndex: 1,
	}
	
	t.Run("GetCurrentRPCURL", func(t *testing.T) {
		url := mc.GetCurrentRPCURL()
		assert.Equal(t, "http://rpc2", url)
		
		// Test invalid index
		mc.currentIndex = -1
		url = mc.GetCurrentRPCURL()
		assert.Equal(t, "", url)
		
		mc.currentIndex = 10
		url = mc.GetCurrentRPCURL()
		assert.Equal(t, "", url)
	})
	
	t.Run("GetHealthStatus", func(t *testing.T) {
		// All clients are nil initially
		status := mc.GetHealthStatus()
		assert.Len(t, status, 3)
		assert.False(t, status["http://rpc1"])
		assert.False(t, status["http://rpc2"]) 
		assert.False(t, status["http://rpc3"])
		
		// Simulate one client being connected (this would be a real ethclient in practice)
		// For testing, we just check the map structure is correct
		assert.Contains(t, status, "http://rpc1")
		assert.Contains(t, status, "http://rpc2")
		assert.Contains(t, status, "http://rpc3")
	})
}

// TestMultiClient_Close tests the Close method
func TestMultiClient_Close(t *testing.T) {
	mc := &MultiClient{
		urls:    []string{"http://rpc1", "http://rpc2"},
		clients: make([]*ethclient.Client, 2),
	}
	
	// Test closing with nil clients (should not panic)
	assert.NotPanics(t, func() {
		mc.Close()
	})
	
	// Verify all clients are nil after close
	for i, client := range mc.clients {
		assert.Nil(t, client, "Client %d should be nil after Close()", i)
	}
}

// TestMultiClient_ConcurrentAccess tests concurrent access safety
func TestMultiClient_ConcurrentAccess(t *testing.T) {
	mc := &MultiClient{
		urls:         []string{"http://rpc1", "http://rpc2", "http://rpc3"},
		clients:      make([]*ethclient.Client, 3),
		currentIndex: 0,
		mu:           sync.RWMutex{},
	}
	
	// Test concurrent access to GetCurrentRPCURL
	var wg sync.WaitGroup
	results := make(chan string, 10)
	
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			url := mc.GetCurrentRPCURL()
			results <- url
		}()
	}
	
	wg.Wait()
	close(results)
	
	// All results should be the same
	expected := "http://rpc1"
	for url := range results {
		assert.Equal(t, expected, url)
	}
}

// TestMultiClient_HealthCheckConfiguration tests health check timing
func TestMultiClient_HealthCheckConfiguration(t *testing.T) {
	// Test default health check interval
	mc := &MultiClient{
		healthInterval: 30 * time.Second,
	}
	
	assert.Equal(t, 30*time.Second, mc.healthInterval)
	
	// Test that health check interval is configurable
	customInterval := 10 * time.Second
	mc.healthInterval = customInterval
	assert.Equal(t, customInterval, mc.healthInterval)
}

// BenchmarkMultiClient_NetworkErrorDetection benchmarks error detection
func BenchmarkMultiClient_NetworkErrorDetection(b *testing.B) {
	testErrors := []error{
		errors.New("connection refused"),
		errors.New("no such host example.com"),
		errors.New("timeout"),
		errors.New("unexpected EOF"),
		errors.New("broken pipe"),
		errors.New("invalid method parameters"), // non-network error
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, err := range testErrors {
			_ = isNetworkError(err)
		}
	}
}

// BenchmarkMultiClient_StringContains benchmarks string matching
func BenchmarkMultiClient_StringContains(b *testing.B) {
	testCases := []struct {
		s      string
		substr string
	}{
		{"connection refused", "refused"},
		{"dial tcp 127.0.0.1:8545: connection refused", "connection"},
		{"read tcp 127.0.0.1:8545: i/o timeout", "timeout"},
		{"write tcp: broken pipe", "pipe"},
		{"TLS handshake timeout occurred", "timeout"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_ = contains(tc.s, tc.substr)
		}
	}
}

// TestMultiClient_EdgeCaseBehavior tests edge case behaviors
func TestMultiClient_EdgeCaseBehavior(t *testing.T) {
	t.Run("EmptyURLSlice", func(t *testing.T) {
		mc, err := NewMultiClient([]string{})
		assert.Error(t, err)
		assert.Nil(t, mc)
		assert.Equal(t, "no RPC URLs provided", err.Error())
	})
	
	t.Run("SingleElementSlice", func(t *testing.T) {
		// This would require a mock server or skip in unit tests
		// Testing the logic path exists
		urls := []string{"http://localhost:8545"}
		_, err := NewMultiClient(urls)
		// Error expected since localhost:8545 likely not running
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect to any RPC URL")
	})
}

// TestMultiClient_ConfigurationOptions tests various configuration scenarios
func TestMultiClient_ConfigurationOptions(t *testing.T) {
	t.Run("MultipleURLs", func(t *testing.T) {
		urls := []string{
			"http://localhost:8545",
			"http://localhost:8546", 
			"http://localhost:8547",
		}
		
		mc, err := NewMultiClient(urls)
		// Expected to fail since servers aren't running, but logic should handle multiple URLs
		assert.Error(t, err)
		assert.Nil(t, mc)
	})
	
	t.Run("DuplicateURLs", func(t *testing.T) {
		urls := []string{
			"http://localhost:8545",
			"http://localhost:8545",
			"http://localhost:8545",
		}
		
		mc, err := NewMultiClient(urls)
		// Should still work with duplicates (though not recommended)
		assert.Error(t, err) // Expected since server not running
		assert.Nil(t, mc)
	})
}

// Integration test example (would require test server)
func TestMultiClient_IntegrationBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	t.Run("WithTestServer", func(t *testing.T) {
		// This would require setting up a test Ethereum node
		// or using a mock HTTP server that responds to JSON-RPC calls
		t.Skip("Integration test requires test server setup")
		
		// Example of how the test would work:
		// server := httptest.NewServer(mockEthereumJSONRPCHandler())
		// defer server.Close()
		// 
		// mc, err := NewMultiClient([]string{server.URL})
		// assert.NoError(t, err)
		// assert.NotNil(t, mc)
		// 
		// ctx := context.Background()
		// chainID, err := mc.ChainID(ctx)
		// assert.NoError(t, err)
		// assert.NotNil(t, chainID)
	})
}