package types

import (
	"context"
	"encoding/json"
	"math/big"
)

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// OracleIntent represents a cross-chain oracle intent structure
type OracleIntent struct {
	// Metadata
	IntentType string   `json:"intentType"` // "OracleUpdate"
	Version    string   `json:"version"`    // "1.0"
	ChainId    int64    `json:"chainId"`    // Chain ID where the intent originates
	Nonce      uint64   `json:"nonce"`      // Unique identifier for this intent
	Expiry     *big.Int `json:"expiry"`     // When this intent expires (unix timestamp)

	// Oracle data
	Symbol    string   `json:"symbol"`
	Price     *big.Int `json:"price"`
	Timestamp *big.Int `json:"timestamp"`
	Source    string   `json:"source"` // Source of the oracle data
}

// SignedIntent represents a signed intent that can be used across chains
type SignedIntent struct {
	Intent    OracleIntent `json:"intent"`
	Signature string       `json:"signature"`
	Signer    string       `json:"signer"`
}

// RPCClient interface for mocking ethclient.Client().CallContext
type RPCClient interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}
