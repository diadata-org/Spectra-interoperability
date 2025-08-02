package interfaces

import (
	"context"
	"math/big"
)

// OracleReader defines the interface for reading oracle values
type OracleReader interface {
	// GetValue retrieves the current value and timestamp for a symbol
	GetValue(ctx context.Context, symbol string) (*big.Int, *big.Int, error)
	
	// GetMultipleValues retrieves values for multiple symbols in one call
	GetMultipleValues(ctx context.Context, symbols []string) (map[string]OracleValue, error)
}

// OracleValue represents a single oracle value with metadata
type OracleValue struct {
	Symbol    string
	Price     *big.Int
	Timestamp *big.Int
	Volume    *big.Int
}