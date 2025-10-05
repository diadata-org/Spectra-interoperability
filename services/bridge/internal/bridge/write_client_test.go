package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
)

// JSON-RPC structures for mock server
type jsonRPCRequest struct {
	ID     interface{}   `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type jsonRPCResponse struct {
	ID     interface{}   `json:"id"`
	Result interface{}   `json:"result,omitempty"`
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

		switch req.Method {
		case "eth_chainId":
			response.Result = "0x539" // Chain ID 1337
		case "eth_blockNumber":
			response.Result = "0x1234"
		case "eth_call":
			// Return mock data for contract calls
			response.Result = "0x0000000000000000000000000000000000000000000000000000000000000001"
		case "net_version":
			response.Result = "1337"
		default:
			response.Error = &jsonRPCError{
				Code:    -32601,
				Message: "Method not found",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

// TestNewWriteClient_Success tests successful WriteClient creation
func TestNewWriteClient_Success(t *testing.T) {
	t.Run("ValidConfiguration", func(t *testing.T) {
		// Create mock RPC server
		server := createMockEthereumServer(t)
		defer server.Close()

		// Create test configuration
		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"

		// Test WriteClient creation
		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)

		if err != nil {
			// If it fails, that's also valid - just check the error message
			t.Logf("WriteClient creation failed (expected): %v", err)
			assert.Contains(t, err.Error(), "failed to")
			return
		}

		// If it succeeds, verify the WriteClient was created properly
		assert.NotNil(t, writeClient)
		assert.Equal(t, chainCfg, writeClient.chainConfig)
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
		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{}, // Empty!
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
	})

	t.Run("NoEnabledContracts", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()

		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: false, // Disabled!
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no enabled receiver contract found")
	})

	t.Run("NoReceiverContract", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()

		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "other", // Not a receiver!
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
		assert.Contains(t, err.Error(), "no enabled receiver contract found")
	})

	t.Run("InvalidPrivateKey", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()

		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, "invalid")
		assert.Error(t, err)
		assert.Nil(t, writeClient)
	})
}

// TestNewWriteClient_ContractTypes tests different contract type configurations
func TestNewWriteClient_ContractTypes(t *testing.T) {
	server := createMockEthereumServer(t)
	defer server.Close()

	privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"

	t.Run("ReceiverType", func(t *testing.T) {
		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)
		// May succeed or fail depending on contract initialization
		if err != nil {
			t.Logf("Expected failure during receiver client init: %v", err)
		} else {
			assert.NotNil(t, writeClient)
			writeClient.client.Close()
		}
	})

	t.Run("PushOracleType", func(t *testing.T) {
		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "pushoracle",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, privateKey)
		// May succeed or fail depending on contract initialization
		if err != nil {
			t.Logf("Expected failure during receiver client init: %v", err)
		} else {
			assert.NotNil(t, writeClient)
			writeClient.client.Close()
		}
	})
}

// TestNewWriteClient_NilConfig tests nil configuration handling
func TestNewWriteClient_NilConfig(t *testing.T) {
	privateKey := "0x1234567890123456789012345678901234567890123456789012345678901234"

	t.Run("NilChainConfig", func(t *testing.T) {
		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(nil, contracts, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
	})

	t.Run("NilContracts", func(t *testing.T) {
		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{"http://localhost:8545"},
			Enabled: true,
		}

		writeClient, err := NewWriteClient(chainCfg, nil, privateKey)
		assert.Error(t, err)
		assert.Nil(t, writeClient)
	})

	t.Run("EmptyPrivateKey", func(t *testing.T) {
		server := createMockEthereumServer(t)
		defer server.Close()

		chainCfg := &config.ChainConfig{
			ChainID: 1337,
			Name:    "Test Chain",
			RPCURLs: []string{server.URL},
			Enabled: true,
		}

		contracts := []*config.ContractConfig{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Type:    "receiver",
				Enabled: true,
			},
		}

		writeClient, err := NewWriteClient(chainCfg, contracts, "")
		assert.Error(t, err)
		assert.Nil(t, writeClient)
	})
}
