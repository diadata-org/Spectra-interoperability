package signer

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/interfaces"
)

func TestNewEIP712Signer(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		wantErr    bool
	}{
		{
			name:       "valid private key with 0x prefix",
			privateKey: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantErr:    false,
		},
		{
			name:       "valid private key without 0x prefix",
			privateKey: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantErr:    false,
		},
		{
			name:       "invalid private key",
			privateKey: "invalid",
			wantErr:    true,
		},
		{
			name:       "empty private key",
			privateKey: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := NewEIP712Signer(tt.privateKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEIP712Signer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && signer == nil {
				t.Error("Expected signer to be created")
			}
			if err == nil && signer.GetAddress() == "" {
				t.Error("Expected address to be derived")
			}
		})
	}
}

func TestEIP712Signer_SignIntent(t *testing.T) {
	// Skip this test as it requires config initialization and network access
	t.Skip("Skipping test that requires full config and network access")
	
	// Create a test signer
	signer, err := NewEIP712Signer("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	tests := []struct {
		name    string
		price   *big.Int
		volume  *big.Int
		symbol  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid intent",
			price:   big.NewInt(50000),
			volume:  big.NewInt(1),
			symbol:  "BTC/USD",
			wantErr: false,
		},
		{
			name:    "nil price",
			price:   nil,
			volume:  big.NewInt(1),
			symbol:  "BTC/USD",
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name:    "zero price",
			price:   big.NewInt(0),
			volume:  big.NewInt(1),
			symbol:  "BTC/USD",
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name:    "negative price",
			price:   big.NewInt(-100),
			volume:  big.NewInt(1),
			symbol:  "BTC/USD",
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name:    "nil volume",
			price:   big.NewInt(50000),
			volume:  nil,
			symbol:  "BTC/USD",
			wantErr: true,
			errMsg:  "must be non-negative",
		},
		{
			name:    "negative volume",
			price:   big.NewInt(50000),
			volume:  big.NewInt(-1),
			symbol:  "BTC/USD",
			wantErr: true,
			errMsg:  "must be non-negative",
		},
		{
			name:    "empty symbol",
			price:   big.NewInt(50000),
			volume:  big.NewInt(1),
			symbol:  "",
			wantErr: true,
			errMsg:  "must not be empty",
		},
		{
			name:    "zero volume (valid)",
			price:   big.NewInt(50000),
			volume:  big.NewInt(0),
			symbol:  "BTC/USD",
			wantErr: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := signer.SignIntent(ctx, tt.price, tt.volume, tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Errorf("SignIntent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Expected error to contain '%s', got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestEIP712Signer_SignBatchIntent(t *testing.T) {
	// Skip this test as it requires config initialization and network access
	t.Skip("Skipping test that requires full config and network access")
	
	// Create a test signer
	signer, err := NewEIP712Signer("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	tests := []struct {
		name    string
		values  []interfaces.SymbolData
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid batch",
			values: []interfaces.SymbolData{
				{Symbol: "BTC/USD", Price: big.NewInt(50000), Volume: big.NewInt(1)},
				{Symbol: "ETH/USD", Price: big.NewInt(3000), Volume: big.NewInt(2)},
			},
			wantErr: false,
		},
		{
			name:    "empty values",
			values:  []interfaces.SymbolData{},
			wantErr: true,
			errMsg:  "must not be empty",
		},
		{
			name: "nil price in batch",
			values: []interfaces.SymbolData{
				{Symbol: "BTC/USD", Price: nil, Volume: big.NewInt(1)},
			},
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name: "zero price in batch",
			values: []interfaces.SymbolData{
				{Symbol: "BTC/USD", Price: big.NewInt(0), Volume: big.NewInt(1)},
			},
			wantErr: true,
			errMsg:  "must be positive",
		},
		{
			name: "empty symbol in batch",
			values: []interfaces.SymbolData{
				{Symbol: "", Price: big.NewInt(50000), Volume: big.NewInt(1)},
			},
			wantErr: true,
			errMsg:  "must not be empty",
		},
		{
			name: "mixed valid and invalid",
			values: []interfaces.SymbolData{
				{Symbol: "BTC/USD", Price: big.NewInt(50000), Volume: big.NewInt(1)},
				{Symbol: "ETH/USD", Price: big.NewInt(-100), Volume: big.NewInt(2)},
			},
			wantErr: true,
			errMsg:  "must be positive",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := signer.SignBatchIntent(ctx, tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("SignBatchIntent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Expected error to contain '%s', got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestEIP712Signer_GetAddress(t *testing.T) {
	// Known private key and expected address
	privateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	
	signer, err := NewEIP712Signer(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	address := signer.GetAddress()
	if address == "" {
		t.Error("Expected non-empty address")
	}
	
	// Check that address is valid Ethereum address format
	if !strings.HasPrefix(address, "0x") {
		t.Error("Expected address to start with 0x")
	}
	if len(address) != 42 {
		t.Errorf("Expected address length 42, got %d", len(address))
	}
}

func TestEIP712Signer_VerifySignature(t *testing.T) {
	signer, err := NewEIP712Signer("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	signerAddress := signer.GetAddress()

	tests := []struct {
		name        string
		signedData  map[string]interface{}
		wantValid   bool
		wantErr     bool
	}{
		{
			name: "valid signature from same signer",
			signedData: map[string]interface{}{
				"signature": "0xabcdef",
				"signer":    signerAddress,
			},
			wantValid: true,
			wantErr:   false,
		},
		{
			name: "signature from different signer",
			signedData: map[string]interface{}{
				"signature": "0xabcdef",
				"signer":    "0x0000000000000000000000000000000000000000",
			},
			wantValid: false,
			wantErr:   false,
		},
		{
			name: "missing signature",
			signedData: map[string]interface{}{
				"signer": signerAddress,
			},
			wantValid: false,
			wantErr:   true,
		},
		{
			name: "missing signer",
			signedData: map[string]interface{}{
				"signature": "0xabcdef",
			},
			wantValid: false,
			wantErr:   true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signedIntent, _ := json.Marshal(tt.signedData)
			
			valid, err := signer.VerifySignature(ctx, signedIntent)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifySignature() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && valid != tt.wantValid {
				t.Errorf("VerifySignature() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

func TestEIP712Signer_VerifySignature_InvalidJSON(t *testing.T) {
	signer, err := NewEIP712Signer("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	ctx := context.Background()
	
	// Test with invalid JSON
	_, err = signer.VerifySignature(ctx, []byte("invalid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}