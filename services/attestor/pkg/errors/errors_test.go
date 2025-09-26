package errors

import (
	"errors"
	"testing"
)

func TestOracleError(t *testing.T) {
	tests := []struct {
		name    string
		symbol  string
		reason  string
		wrapped error
		want    string
	}{
		{
			name:    "oracle error without wrapped error",
			symbol:  "BTC/USD",
			reason:  "connection timeout",
			wrapped: nil,
			want:    "oracle error for BTC/USD: connection timeout",
		},
		{
			name:    "oracle error with wrapped error",
			symbol:  "ETH/USD",
			reason:  "fetch failed",
			wrapped: errors.New("network error"),
			want:    "oracle error for ETH/USD: fetch failed: network error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewOracleError(tt.symbol, tt.reason, tt.wrapped)
			if err.Error() != tt.want {
				t.Errorf("OracleError.Error() = %v, want %v", err.Error(), tt.want)
			}
		})
	}
}

func TestRegistryError(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		txHash    string
		wrapped   error
		want      string
	}{
		{
			name:      "registry error with tx hash",
			operation: "publish",
			txHash:    "0x123456",
			wrapped:   errors.New("gas too low"),
			want:      "registry error during publish (tx: 0x123456): gas too low",
		},
		{
			name:      "registry error without tx hash",
			operation: "connect",
			txHash:    "",
			wrapped:   errors.New("connection refused"),
			want:      "registry error during connect: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRegistryError(tt.operation, tt.txHash, tt.wrapped)
			if err.Error() != tt.want {
				t.Errorf("RegistryError.Error() = %v, want %v", err.Error(), tt.want)
			}
		})
	}
}

func TestSignerError(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		details   string
		wrapped   error
		want      string
	}{
		{
			name:      "signer error with wrapped error",
			operation: "sign",
			details:   "invalid private key",
			wrapped:   errors.New("key parse error"),
			want:      "signer error during sign (invalid private key): key parse error",
		},
		{
			name:      "signer error without wrapped error",
			operation: "verify",
			details:   "signature mismatch",
			wrapped:   nil,
			want:      "signer error during verify: signature mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewSignerError(tt.operation, tt.details, tt.wrapped)
			if err.Error() != tt.want {
				t.Errorf("SignerError.Error() = %v, want %v", err.Error(), tt.want)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := NewValidationError("symbol", "BTCUSD", "missing slash separator")
	want := "validation error for symbol (value: BTCUSD): missing slash separator"
	
	if err.Error() != want {
		t.Errorf("ValidationError.Error() = %v, want %v", err.Error(), want)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "oracle connection error",
			err:  ErrOracleConnection,
			want: true,
		},
		{
			name: "registry connection error",
			err:  ErrRegistryConnection,
			want: true,
		},
		{
			name: "transaction failed error",
			err:  ErrTransactionFailed,
			want: true,
		},
		{
			name: "non-retryable error",
			err:  ErrInvalidSymbol,
			want: false,
		},
		{
			name: "wrapped oracle connection error",
			err:  NewOracleError("BTC/USD", "fetch failed", ErrOracleConnection),
			want: true,
		},
		{
			name: "wrapped non-retryable error",
			err:  NewOracleError("BTC/USD", "invalid data", ErrInvalidSymbol),
			want: false,
		},
		{
			name: "wrapped registry connection error",
			err:  NewRegistryError("publish", "", ErrRegistryConnection),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	baseErr := errors.New("base error")
	
	t.Run("oracle error unwrap", func(t *testing.T) {
		err := &OracleError{Symbol: "BTC/USD", Reason: "test", Wrapped: baseErr}
		if !errors.Is(err, baseErr) {
			t.Errorf("Expected error to wrap base error")
		}
	})
	
	t.Run("registry error unwrap", func(t *testing.T) {
		err := &RegistryError{Operation: "test", Wrapped: baseErr}
		if !errors.Is(err, baseErr) {
			t.Errorf("Expected error to wrap base error")
		}
	})
	
	t.Run("signer error unwrap", func(t *testing.T) {
		err := &SignerError{Operation: "test", Wrapped: baseErr}
		if !errors.Is(err, baseErr) {
			t.Errorf("Expected error to wrap base error")
		}
	})
}