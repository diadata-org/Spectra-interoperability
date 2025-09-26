package interfaces

import (
	"context"
	"math/big"
)

// IntentSigner defines the interface for signing intents
type IntentSigner interface {
	// SignIntent creates an EIP-712 signed intent for a single value
	SignIntent(ctx context.Context, price, volume *big.Int, symbol string) ([]byte, error)
	
	// SignBatchIntent creates an EIP-712 signed intent for multiple values
	SignBatchIntent(ctx context.Context, values []SymbolData) ([]byte, error)
	
	// GetAddress returns the signer's address
	GetAddress() string
	
	// VerifySignature verifies an intent signature
	VerifySignature(ctx context.Context, signedIntent []byte) (bool, error)
}

// SymbolData represents data for a single symbol in a batch
type SymbolData struct {
	Symbol string
	Price  *big.Int
	Volume *big.Int
}