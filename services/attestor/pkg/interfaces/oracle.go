package interfaces

import (
	"context"
	"math/big"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
)

// OracleReader defines the interface for reading oracle values
type OracleReader interface {
	// GetValue retrieves the current value and timestamp for a symbol
	GetGuardedValue(ctx context.Context, symbol string, params config.GuardianParams) (*big.Int, *big.Int, error)
}

// OracleValue represents a single oracle value with metadata
type OracleValue struct {
	Symbol    string
	Price     *big.Int
	Timestamp *big.Int
	Volume    *big.Int
}
