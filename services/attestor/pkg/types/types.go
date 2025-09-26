package types

import (
	"math/big"
)

// OracleIntent represents a cross-chain oracle intent structure
type OracleIntent struct {
	// Metadata
	IntentType string   `json:"intentType"` // "OracleUpdate"
	Version    string   `json:"version"`    // "1.0"
	ChainId    *big.Int `json:"chainId"`    // Chain ID where the intent originates
	Nonce      *big.Int `json:"nonce"`      // Unique identifier for this intent
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

