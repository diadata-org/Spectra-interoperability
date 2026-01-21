package bridge

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// TestExtractIdentifier tests the extractIdentifier helper function
func TestExtractIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		request    *bridgetypes.UpdateRequest
		expectedID string
	}{
		{
			name: "intent request",
			request: &bridgetypes.UpdateRequest{
				Intent: &bridgetypes.OracleIntent{
					Symbol: "BTC/USD",
				},
			},
			expectedID: "BTC/USD",
		},
		{
			name: "event request",
			request: &bridgetypes.UpdateRequest{
				Event: &bridgetypes.EventData{
					EventName: "TestEvent",
					RequestId: big.NewInt(123),
				},
			},
			expectedID: "TestEvent(requestId:123)",
		},
		{
			name:       "unknown request",
			request:    &bridgetypes.UpdateRequest{},
			expectedID: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identifier := extractIdentifier(tt.request)
			assert.Equal(t, tt.expectedID, identifier)
		})
	}
}

// TestExtractSymbol tests the extractSymbol helper function
func TestExtractSymbol(t *testing.T) {
	tests := []struct {
		name           string
		request        *bridgetypes.UpdateRequest
		expectedSymbol string
	}{
		{
			name: "symbol from intent",
			request: &bridgetypes.UpdateRequest{
				Intent: &bridgetypes.OracleIntent{
					Symbol: "ETH/USD",
				},
			},
			expectedSymbol: "ETH/USD",
		},
		{
			name:           "unknown when no data",
			request:        &bridgetypes.UpdateRequest{},
			expectedSymbol: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol := extractSymbol(tt.request, nil)
			assert.Equal(t, tt.expectedSymbol, symbol)
		})
	}
}

// TestGetGasLimit tests the getGasLimit helper function
func TestGetGasLimit(t *testing.T) {
	tests := []struct {
		name         string
		methodConfig *config.DestinationMethodConfig
		expectedGas  uint64
	}{
		{
			name: "custom gas limit",
			methodConfig: &config.DestinationMethodConfig{
				GasLimit: 500000,
			},
			expectedGas: 500000,
		},
		{
			name: "default gas limit when zero",
			methodConfig: &config.DestinationMethodConfig{
				GasLimit: 0,
			},
			expectedGas: DefaultGasLimit,
		},
		{
			name:         "default gas limit when not set",
			methodConfig: &config.DestinationMethodConfig{},
			expectedGas:  DefaultGasLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gasLimit := getGasLimit(tt.methodConfig)
			assert.Equal(t, tt.expectedGas, gasLimit)
		})
	}
}

// TestIsExpired tests the isExpired helper function
func TestIsExpired(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name     string
		intent   *bridgetypes.OracleIntent
		expected bool
	}{
		{
			name: "valid future expiry",
			intent: &bridgetypes.OracleIntent{
				Symbol: "ETH/USD",
				Expiry: big.NewInt(now + 3600),
			},
			expected: false,
		},
		{
			name: "expired intent",
			intent: &bridgetypes.OracleIntent{
				Symbol: "ETH/USD",
				Expiry: big.NewInt(now - 3600),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExpired(tt.intent)
			assert.Equal(t, tt.expected, result)
		})
	}
}
