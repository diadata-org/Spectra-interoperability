package signer

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/intent"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
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

// GetAddress returns the signer's address
func (s *EIP712Signer) GetAddress() string {
	return s.address.Hex()
}

// VerifySignature verifies an intent signature
func (s *EIP712Signer) VerifySignature(ctx context.Context, signedIntent []byte) (bool, error) {
	var intentPayload types.SignedIntent
	if err := json.Unmarshal(signedIntent, &intentPayload); err != nil {
		return false, errors.NewSignerError("parse signed intent", "", err)
	}

	if intentPayload.Signature == "" {
		return false, errors.NewSignerError("verify", "missing signature", nil)
	}
	if intentPayload.Signer == "" {
		return false, errors.NewSignerError("verify", "missing signer", nil)
	}

	if !strings.EqualFold(intentPayload.Signer, s.address.Hex()) {
		return false, nil
	}

	chainID := intentPayload.Intent.ChainId
	if chainID == nil || chainID.Sign() <= 0 {
		return false, errors.NewSignerError("verify", "invalid chainId", nil)
	}
	if intentPayload.Intent.Nonce == nil {
		return false, errors.NewSignerError("verify", "missing nonce", nil)
	}
	if intentPayload.Intent.Expiry == nil {
		return false, errors.NewSignerError("verify", "missing expiry", nil)
	}
	if intentPayload.Intent.Price == nil {
		return false, errors.NewSignerError("verify", "missing price", nil)
	}
	if intentPayload.Intent.Timestamp == nil {
		return false, errors.NewSignerError("verify", "missing timestamp", nil)
	}

	cfg := config.Get()
	contractAddr := cfg.Registry.Address
	if contractAddr == "" {
		contractAddr = "0x0000000000000000000000000000000000000000"
	}

	signatureBytes, err := hexutil.Decode(intentPayload.Signature)
	if err != nil {
		return false, errors.NewSignerError("verify", "decode signature", err)
	}
	if len(signatureBytes) != crypto.SignatureLength {
		return false, errors.NewSignerError("verify", "invalid signature length", nil)
	}

	// Normalize recovery ID to 0/1 as required by go-ethereum crypto primitives
	if signatureBytes[crypto.RecoveryIDOffset] == 27 || signatureBytes[crypto.RecoveryIDOffset] == 28 {
		signatureBytes[crypto.RecoveryIDOffset] -= 27
	}
	if signatureBytes[crypto.RecoveryIDOffset] != 0 && signatureBytes[crypto.RecoveryIDOffset] != 1 {
		return false, errors.NewSignerError("verify", "unsupported recovery id", nil)
	}

	chainIDCopy := gethmath.HexOrDecimal256(*chainID)
	domain := apitypes.TypedDataDomain{
		Name:              "DIA Oracle Intent",
		Version:           "1",
		ChainId:           &chainIDCopy,
		VerifyingContract: contractAddr,
		Salt:              "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
				{Name: "salt", Type: "bytes32"},
			},
			"OracleIntent": []apitypes.Type{
				{Name: "intentType", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "expiry", Type: "uint256"},
				{Name: "symbol", Type: "string"},
				{Name: "price", Type: "uint256"},
				{Name: "timestamp", Type: "uint256"},
				{Name: "source", Type: "string"},
			},
		},
		PrimaryType: "OracleIntent",
		Domain:      domain,
		Message: map[string]interface{}{
			"intentType": intentPayload.Intent.IntentType,
			"version":    intentPayload.Intent.Version,
			"chainId":    intentPayload.Intent.ChainId,
			"nonce":      intentPayload.Intent.Nonce,
			"expiry":     intentPayload.Intent.Expiry,
			"symbol":     intentPayload.Intent.Symbol,
			"price":      intentPayload.Intent.Price,
			"timestamp":  intentPayload.Intent.Timestamp,
			"source":     intentPayload.Intent.Source,
		},
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return false, errors.NewSignerError("verify", "hash domain", err)
	}
	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return false, errors.NewSignerError("verify", "hash typed data", err)
	}
	dataToSign := append([]byte{0x19, 0x01}, domainSeparator[:]...)
	dataToSign = append(dataToSign, typedDataHash[:]...)
	hash := crypto.Keccak256Hash(dataToSign)

	pubKey, err := crypto.SigToPub(hash.Bytes(), signatureBytes)
	if err != nil {
		return false, errors.NewSignerError("verify", "recover signer", err)
	}
	recovered := crypto.PubkeyToAddress(*pubKey)
	if !strings.EqualFold(recovered.Hex(), intentPayload.Signer) {
		return false, nil
	}

	return true, nil
}
