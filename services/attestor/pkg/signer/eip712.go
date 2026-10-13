package signer

import (
	"context"
	"fmt"
	"math/big"

	multirpc "github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/keysigner"
)

// EIP712Signer implements the IntentSigner interface using EIP-712
type EIP712Signer struct {
	keySigner     keysigner.KeySigner
	signingClient *multirpc.MultiClient
}

// NewEIP712Signer creates an EIP-712 signer backed by the given key signer.
func NewEIP712Signer(ks keysigner.KeySigner, rpcURLs []string) (*EIP712Signer, error) {
	if ks == nil {
		return nil, errors.NewSignerError("create signer", "", fmt.Errorf("key signer is required"))
	}

	signingClient, err := multirpc.NewMultiClient(rpcURLs)
	if err != nil {
		return nil, errors.NewSignerError("create signing client", "", err)
	}

	return &EIP712Signer{
		keySigner:     ks,
		signingClient: signingClient,
	}, nil
}

// SignIntent creates an EIP-712 signed intent for a single value
func (s *EIP712Signer) SignIntent(ctx context.Context, price, volume, timestamp *big.Int, symbol string) ([]byte, error) {
	// Validate inputs
	if price == nil || price.Sign() <= 0 {
		return nil, errors.NewValidationError("price", price, "must be positive")
	}
	if volume == nil || volume.Sign() < 0 {
		return nil, errors.NewValidationError("volume", volume, "must be non-negative")
	}
	if symbol == "" {
		return nil, errors.NewValidationError("symbol", symbol, "must not be empty")
	}

	signedIntentJSON, err := intent.AttestValue(ctx, s.signingClient, s.keySigner, price, volume, symbol, timestamp)
	if err != nil {
		return nil, errors.NewSignerError("sign intent", symbol, err)
	}

	return []byte(signedIntentJSON), nil
}

// SignBatchIntent creates an EIP-712 signed intent for multiple values
func (s *EIP712Signer) SignBatchIntent(ctx context.Context, values []interfaces.SymbolData) ([]byte, error) {
	// Validate inputs
	if len(values) == 0 {
		return nil, errors.NewValidationError("values", values, "must not be empty")
	}

	// Convert to intent package format
	symbolData := make([]intent.SymbolData, 0, len(values))
	for i, v := range values {
		if v.Price == nil || v.Price.Sign() <= 0 {
			return nil, errors.NewValidationError(
				fmt.Sprintf("values[%d].price", i),
				v.Price,
				"must be positive",
			)
		}
		if v.Volume == nil || v.Volume.Sign() < 0 {
			return nil, errors.NewValidationError(
				fmt.Sprintf("values[%d].volume", i),
				v.Volume,
				"must be non-negative",
			)
		}
		if v.Symbol == "" {
			return nil, errors.NewValidationError(
				fmt.Sprintf("values[%d].symbol", i),
				v.Symbol,
				"must not be empty",
			)
		}

		symbolData = append(symbolData, intent.SymbolData{
			Symbol:    v.Symbol,
			Price:     v.Price,
			Volume:    v.Volume,
			Timestamp: v.Timestamp,
		})
	}

	batchIntentJSON, err := intent.AttestMultipleValues(ctx, s.signingClient, s.keySigner, symbolData)
	if err != nil {
		return nil, errors.NewSignerError("sign batch intent", fmt.Sprintf("%d values", len(values)), err)
	}

	return []byte(batchIntentJSON), nil
}

// Close
func (s *EIP712Signer) Close() {
	if s.signingClient != nil {
		s.signingClient.Close()
	}
}
