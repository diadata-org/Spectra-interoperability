package signer

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// EIP712Signer implements the IntentSigner interface using EIP-712
type EIP712Signer struct {
	privateKey    *ecdsa.PrivateKey
	address       common.Address
	privateKeyHex string
}

// NewEIP712Signer creates a new EIP-712 signer
func NewEIP712Signer(privateKeyHex string) (*EIP712Signer, error) {
	// Remove 0x prefix if present
	cleanKey := strings.TrimPrefix(privateKeyHex, "0x")

	// Parse the private key
	privateKey, err := crypto.HexToECDSA(cleanKey)
	if err != nil {
		return nil, errors.NewSignerError("parse private key", "", err)
	}

	// Derive the address
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	return &EIP712Signer{
		privateKey:    privateKey,
		address:       address,
		privateKeyHex: privateKeyHex,
	}, nil
}

// SignIntent creates an EIP-712 signed intent for a single value
func (s *EIP712Signer) SignIntent(ctx context.Context, price, volume *big.Int, symbol string) ([]byte, error) {
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

	// Use the intent package to create the signed intent
	signedIntentJSON, err := intent.AttestValue(ctx, s.privateKeyHex, s.address.Hex(), price, volume, symbol)
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
			Symbol: v.Symbol,
			Price:  v.Price,
			Volume: v.Volume,
		})
	}

	// Use the intent package to create the batch signed intent
	batchIntentJSON, err := intent.AttestMultipleValues(ctx, s.privateKeyHex, s.address.Hex(), symbolData)
	if err != nil {
		return nil, errors.NewSignerError("sign batch intent", fmt.Sprintf("%d values", len(values)), err)
	}

	return []byte(batchIntentJSON), nil
}
