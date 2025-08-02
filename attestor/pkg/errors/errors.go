package errors

import (
	"errors"
	"fmt"
)

var (
	// ErrOracleConnection indicates a connection error to the oracle
	ErrOracleConnection = errors.New("oracle connection failed")
	
	// ErrOracleValueNotFound indicates the requested value was not found
	ErrOracleValueNotFound = errors.New("oracle value not found")
	
	// ErrOracleStaleData indicates the oracle data is too old
	ErrOracleStaleData = errors.New("oracle data is stale")
	
	// ErrInvalidSymbol indicates an invalid symbol format
	ErrInvalidSymbol = errors.New("invalid symbol format")
	
	// ErrSigningFailed indicates a failure in signing operation
	ErrSigningFailed = errors.New("signing operation failed")
	
	// ErrInvalidSignature indicates an invalid signature
	ErrInvalidSignature = errors.New("invalid signature")
	
	// ErrRegistryConnection indicates a connection error to the registry
	ErrRegistryConnection = errors.New("registry connection failed")
	
	// ErrPublishFailed indicates a failure to publish intent
	ErrPublishFailed = errors.New("failed to publish intent")
	
	// ErrInvalidConfiguration indicates invalid configuration
	ErrInvalidConfiguration = errors.New("invalid configuration")
	
	// ErrInsufficientBalance indicates insufficient balance for transaction
	ErrInsufficientBalance = errors.New("insufficient balance")
	
	// ErrTransactionFailed indicates a transaction failure
	ErrTransactionFailed = errors.New("transaction failed")
)

// OracleError represents an oracle-specific error
type OracleError struct {
	Symbol  string
	Reason  string
	Wrapped error
}

func (e *OracleError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("oracle error for %s: %s: %v", e.Symbol, e.Reason, e.Wrapped)
	}
	return fmt.Sprintf("oracle error for %s: %s", e.Symbol, e.Reason)
}

func (e *OracleError) Unwrap() error {
	return e.Wrapped
}

// RegistryError represents a registry-specific error
type RegistryError struct {
	Operation string
	TxHash    string
	Wrapped   error
}

func (e *RegistryError) Error() string {
	if e.TxHash != "" {
		return fmt.Sprintf("registry error during %s (tx: %s): %v", e.Operation, e.TxHash, e.Wrapped)
	}
	return fmt.Sprintf("registry error during %s: %v", e.Operation, e.Wrapped)
}

func (e *RegistryError) Unwrap() error {
	return e.Wrapped
}

// SignerError represents a signer-specific error
type SignerError struct {
	Operation string
	Details   string
	Wrapped   error
}

func (e *SignerError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("signer error during %s (%s): %v", e.Operation, e.Details, e.Wrapped)
	}
	return fmt.Sprintf("signer error during %s: %s", e.Operation, e.Details)
}

func (e *SignerError) Unwrap() error {
	return e.Wrapped
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s (value: %v): %s", e.Field, e.Value, e.Message)
}

// Helper functions

// NewOracleError creates a new oracle error
func NewOracleError(symbol, reason string, wrapped error) error {
	return &OracleError{
		Symbol:  symbol,
		Reason:  reason,
		Wrapped: wrapped,
	}
}

// NewRegistryError creates a new registry error
func NewRegistryError(operation, txHash string, wrapped error) error {
	return &RegistryError{
		Operation: operation,
		TxHash:    txHash,
		Wrapped:   wrapped,
	}
}

// NewSignerError creates a new signer error
func NewSignerError(operation, details string, wrapped error) error {
	return &SignerError{
		Operation: operation,
		Details:   details,
		Wrapped:   wrapped,
	}
}

// NewValidationError creates a new validation error
func NewValidationError(field string, value interface{}, message string) error {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// IsRetryable determines if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	
	// Check for specific retryable errors
	if errors.Is(err, ErrOracleConnection) ||
		errors.Is(err, ErrRegistryConnection) ||
		errors.Is(err, ErrTransactionFailed) {
		return true
	}
	
	// Check for wrapped errors
	var oracleErr *OracleError
	if errors.As(err, &oracleErr) {
		return errors.Is(oracleErr.Wrapped, ErrOracleConnection)
	}
	
	var registryErr *RegistryError
	if errors.As(err, &registryErr) {
		return errors.Is(registryErr.Wrapped, ErrRegistryConnection)
	}
	
	return false
}