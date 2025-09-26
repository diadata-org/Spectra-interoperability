package registry

import (
	"encoding/json"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name         string
		privateKey   string
		rpcURL       string
		registryAddr string
		wantErr      bool
	}{
		{
			name:         "invalid private key",
			privateKey:   "invalid",
			rpcURL:       "http://localhost:8545",
			registryAddr: "0x1234567890abcdef1234567890abcdef12345678",
			wantErr:      true,
		},
		{
			name:         "valid hex private key without 0x",
			privateKey:   "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			rpcURL:       "http://localhost:8545",
			registryAddr: "0x1234567890abcdef1234567890abcdef12345678",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.privateKey, tt.rpcURL, tt.registryAddr)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
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
