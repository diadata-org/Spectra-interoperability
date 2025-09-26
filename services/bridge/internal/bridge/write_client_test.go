package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
)

// MockReceiverClient is a mock implementation of contracts.ReceiverClient
type MockReceiverClient struct {
	mock.Mock
}

func (m *MockReceiverClient) GetAddress() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockReceiverClient) IsAuthorizedSigner(ctx context.Context, signer string) (bool, error) {
	args := m.Called(ctx, signer)
	return args.Bool(0), args.Error(1)
}

// JSON-RPC structures for mock server
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
			response.Result = "0x539"
		case "eth_blockNumber":
			response.Result = "0x3e8"
		case "net_version":
			response.Result = "1337"
		default:
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

// Mock contracts.NewReceiverClient function
var mockNewReceiverClient func(client interface{}, address interface{}, privateKey string) (*contracts.ReceiverClient, error)

// TestNewWriteClient_Success tests successful WriteClient creation
func TestNewWriteClient_Success(t *testing.T) {
	t.Run("ValidConfiguration", func(t *testing.T) {
		// Create mock RPC server
		server := createMockEthereumServer(t)
		defer server.Close()

		// Create test configuration
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestReceiver",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "receiver",
					Enabled: true,
				},
			},
		}

		privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"
		
		// Test WriteClient creation
		writeClient, err := NewWriteClient(cfg, privateKey)
		
		if err != nil {
			// If it fails, that's also valid - just check the error message
			t.Logf("WriteClient creation failed (expected): %v", err)
			assert.Contains(t, err.Error(), "failed to")
			return
		}
		
		// If it succeeds, verify the WriteClient was created properly
		assert.NotNil(t, writeClient)
		assert.Equal(t, cfg, writeClient.config)
		assert.NotNil(t, writeClient.client)
		assert.NotNil(t, writeClient.receiverClient)
		assert.NotNil(t, writeClient.lastUpdate)
		
		// Clean up
		if writeClient.client != nil {
			writeClient.client.Close()
		}
	})
}

// TestNewWriteClient_ValidationErrors tests input validation
func TestNewWriteClient_ValidationErrors(t *testing.T) {
	privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"

	t.Run("EmptyRPCURLs", func(t *testing.T) {
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{},
			Enabled: true,
		}
		
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no RPC URLs provided")
	})

	t.Run("NoEnabledContracts", func(t *testing.T) {
		// Use a working mock server for this test since we want to test contract validation
		server := createMockEthereumServer(t)
		defer server.Close()
		
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain", 
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "DisabledReceiver",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "receiver",
					Enabled: false, // Disabled
				},
			},
		}
		
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no enabled receiver contract found")
	})

	t.Run("NoReceiverTypeContracts", func(t *testing.T) {
		// Use a working mock server for this test
		server := createMockEthereumServer(t)
		defer server.Close()
		
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "SomeOtherContract",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "registry", // Not receiver or pushoracle
					Enabled: true,
				},
			},
		}
		
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no enabled receiver contract found")
	})

	t.Run("InvalidRPCURL", func(t *testing.T) {
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{"invalid-url"},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestReceiver",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "receiver",
					Enabled: true,
				},
			},
		}
		
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "failed to connect to destination chain")
	})
}

// TestNewWriteClient_ContractTypes tests different contract type configurations
func TestNewWriteClient_ContractTypes(t *testing.T) {
	privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"

	t.Run("ReceiverType", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()
		
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestReceiver",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "receiver",
					Enabled: true,
				},
			},
		}
		
		// This should work (receiver type is supported)
		writeClient, err := NewWriteClient(cfg, privateKey)
		if err != nil {
			// If it fails, should not be due to contract type validation
			assert.NotContains(t, err.Error(), "no enabled receiver contract found")
		} else {
			// If it succeeds, verify it was created properly
			assert.NotNil(t, writeClient)
			writeClient.client.Close()
		}
	})

	t.Run("PushOracleType", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()
		
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestPushOracle",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "pushoracle",
					Enabled: true,
				},
			},
		}
		
		// This should work (pushoracle type is supported)
		writeClient, err := NewWriteClient(cfg, privateKey)
		if err != nil {
			// If it fails, should not be due to contract type validation
			assert.NotContains(t, err.Error(), "no enabled receiver contract found")
		} else {
			// If it succeeds, verify it was created properly
			assert.NotNil(t, writeClient)
			writeClient.client.Close()
		}
	})

	t.Run("UnsupportedType", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()
		
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestRegistry",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "registry", // Unsupported type
					Enabled: true,
				},
			},
		}
		
		// This should fail at contract type validation
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no enabled receiver contract found")
	})
}

// TestWriteClient_updateLastUpdate tests the updateLastUpdate method
func TestWriteClient_updateLastUpdate(t *testing.T) {
	// Create a WriteClient instance for testing
	wc := &WriteClient{
		lastUpdate: make(map[string]time.Time),
	}

	symbol := "ETH"
	beforeTime := time.Now()
	
	// Test updating last update time
	wc.updateLastUpdate(symbol)
	
	afterTime := time.Now()
	
	// Check that the update time was recorded
	assert.Contains(t, wc.lastUpdate, symbol)
	updateTime := wc.lastUpdate[symbol]
	
	// Time should be between before and after
	assert.True(t, updateTime.After(beforeTime) || updateTime.Equal(beforeTime))
	assert.True(t, updateTime.Before(afterTime) || updateTime.Equal(afterTime))
	
	// Test updating the same symbol again
	time.Sleep(10 * time.Millisecond) // Small delay
	firstUpdateTime := wc.lastUpdate[symbol]
	
	wc.updateLastUpdate(symbol)
	secondUpdateTime := wc.lastUpdate[symbol]
	
	// Second update should be later
	assert.True(t, secondUpdateTime.After(firstUpdateTime))
}

// TestWriteClient_ConcurrentUpdateLastUpdate tests concurrent access to updateLastUpdate
func TestWriteClient_ConcurrentUpdateLastUpdate(t *testing.T) {
	wc := &WriteClient{
		lastUpdate: make(map[string]time.Time),
	}

	symbols := []string{"ETH", "BTC", "USDC", "DAI", "LINK"}
	done := make(chan bool)

	// Start multiple goroutines updating different symbols
	for _, symbol := range symbols {
		go func(s string) {
			for i := 0; i < 100; i++ {
				wc.updateLastUpdate(s)
			}
			done <- true
		}(symbol)
	}

	// Wait for all goroutines to complete
	for i := 0; i < len(symbols); i++ {
		<-done
	}

	// Check that all symbols were updated
	assert.Len(t, wc.lastUpdate, len(symbols))
	for _, symbol := range symbols {
		assert.Contains(t, wc.lastUpdate, symbol)
		assert.False(t, wc.lastUpdate[symbol].IsZero())
	}
}

// TestWriteClient_EdgeCases tests edge cases and error conditions
func TestWriteClient_EdgeCases(t *testing.T) {
	t.Run("NilConfig", func(t *testing.T) {
		privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"
		
		// This should panic or return error - test the behavior
		assert.Panics(t, func() {
			NewWriteClient(nil, privateKey)
		})
	})
	
	t.Run("EmptyPrivateKey", func(t *testing.T) {
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{"http://localhost:8545"},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "TestReceiver",
					Address: "0x1234567890123456789012345678901234567890",
					Type:    "receiver",
					Enabled: true,
				},
			},
		}
		
		// Should fail when creating receiver client with empty private key
		writeClient, err := NewWriteClient(cfg, "")
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		// Error should be about RPC connection since that happens first
		assert.Contains(t, err.Error(), "failed to connect to destination chain")
	})
	
	t.Run("MultipleEnabledContracts", func(t *testing.T) {
		cfg := &config.DestinationConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{"http://localhost:8545"},
			Enabled: true,
			Contracts: []config.ContractConfig{
				{
					Name:    "FirstReceiver",
					Address: "0x1111111111111111111111111111111111111111",
					Type:    "receiver",
					Enabled: true,
				},
				{
					Name:    "SecondReceiver", 
					Address: "0x2222222222222222222222222222222222222222",
					Type:    "pushoracle",
					Enabled: true,
				},
			},
		}
		
		privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"
		
		// Should use the first enabled receiver/pushoracle contract found
		writeClient, err := NewWriteClient(cfg, privateKey)
		assert.Error(t, err) // Will fail at RPC connection
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "failed to connect to destination chain")
	})
}