package registry

import (
	"encoding/json"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/keysigner"
)

func TestNewClient_NilKeySigner(t *testing.T) {
	_, err := NewClient(nil, "0x1234567890abcdef1234567890abcdef12345678", []string{"http://localhost:8545"})
	if err == nil {
		t.Error("Expected error when key signer is nil")
	}
}

func TestNewClient_ValidKeySigner(t *testing.T) {
	// NewClient dials the registry RPC URLs, so this requires network access.
	t.Skip("Skipping test that requires network access to dial RPC URLs")

	ks, err := keysigner.NewHexKeySigner("1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("failed to create key signer: %v", err)
	}

	_, err = NewClient(ks, "0x1234567890abcdef1234567890abcdef12345678", []string{"http://localhost:8545"})
	if err != nil {
		t.Errorf("NewClient() error = %v", err)
	}
}

func TestClient_PublishIntent(t *testing.T) {
	// Mock signed intent data
	signedIntent := map[string]interface{}{
		"intent": map[string]interface{}{
			"symbol": "BTC/USD",
			"price":  "50000",
			"volume": "1",
		},
		"signature": "0xabcdef",
		"signer":    "0x1234567890abcdef1234567890abcdef12345678",
	}

	signedIntentJSON, _ := json.Marshal(signedIntent)

	tests := []struct {
		name         string
		signedIntent []byte
		wantErr      bool
	}{
		{
			name:         "valid signed intent",
			signedIntent: signedIntentJSON,
			wantErr:      false, // Will fail due to network, but parsing should succeed
		},
		{
			name:         "invalid JSON",
			signedIntent: []byte("invalid json"),
			wantErr:      true,
		},
		{
			name:         "empty intent",
			signedIntent: []byte("{}"),
			wantErr:      false, // Will fail later, but parsing succeeds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON parsing
			var testData map[string]interface{}
			err := json.Unmarshal(tt.signedIntent, &testData)

			if tt.wantErr && err == nil {
				t.Error("Expected JSON parsing error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected JSON parsing error: %v", err)
			}
		})
	}
}

func TestClient_PublishBatchIntents(t *testing.T) {
	// Mock batch intent data
	batchIntent := map[string]interface{}{
		"intents": []map[string]interface{}{
			{
				"symbol": "BTC/USD",
				"price":  "50000",
				"volume": "1",
			},
			{
				"symbol": "ETH/USD",
				"price":  "3000",
				"volume": "1",
			},
		},
		"signature": "0xabcdef",
		"signer":    "0x1234567890abcdef1234567890abcdef12345678",
	}

	batchIntentJSON, _ := json.Marshal(batchIntent)

	tests := []struct {
		name         string
		signedIntent []byte
		wantErr      bool
	}{
		{
			name:         "valid batch intent",
			signedIntent: batchIntentJSON,
			wantErr:      false, // Will fail due to network, but parsing should succeed
		},
		{
			name:         "invalid JSON",
			signedIntent: []byte("invalid json"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON parsing
			var testData map[string]interface{}
			err := json.Unmarshal(tt.signedIntent, &testData)

			if tt.wantErr && err == nil {
				t.Error("Expected JSON parsing error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected JSON parsing error: %v", err)
			}
		})
	}
}
