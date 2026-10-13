package config

import (
	"fmt"
	"time"
)

type AttestorMode string

const (
	ModePrime   AttestorMode = "prime"
	ModeReplica AttestorMode = "replica"
)

// String returns the string representation of the mode
func (m AttestorMode) String() string {
	return string(m)
}

// IsValid checks if the mode is valid
func (m AttestorMode) IsValid() bool {
	return m == ModePrime || m == ModeReplica
}

// ParseAttestorMode parses a string into AttestorMode
func ParseAttestorMode(s string) (AttestorMode, error) {
	mode := AttestorMode(s)
	if !mode.IsValid() {
		return "", fmt.Errorf("invalid attestor mode: %s (must be 'prime' or 'replica')", s)
	}
	return mode, nil
}

type OracleClientType string

const (
	OracleClientTypeGuarded OracleClientType = "guarded"
	OracleClientTypeDIAV2   OracleClientType = "dia_v2"
)

// String returns the string representation of the oracle client type
func (t OracleClientType) String() string {
	return string(t)
}

// IsValid checks if the oracle client type is valid
func (t OracleClientType) IsValid() bool {
	return t == OracleClientTypeGuarded || t == OracleClientTypeDIAV2
}

// ParseOracleClientType parses a string into OracleClientType
func ParseOracleClientType(s string) (OracleClientType, error) {
	clientType := OracleClientType(s)
	if !clientType.IsValid() {
		return "", fmt.Errorf("invalid oracle client type: %s (must be 'guarded' or 'dia_v2')", s)
	}
	return clientType, nil
}

// SigningBackend selects where the attestor's ECDSA signing key material lives.
type SigningBackend string

const (
	SigningBackendPrivateKey SigningBackend = "privatekey"
	SigningBackendPKCS11     SigningBackend = "pkcs11"
)

// String returns the string representation of the signing backend.
func (b SigningBackend) String() string {
	return string(b)
}

// IsValid checks if the signing backend is valid.
func (b SigningBackend) IsValid() bool {
	return b == SigningBackendPrivateKey || b == SigningBackendPKCS11
}

// ParseSigningBackend parses a string into SigningBackend.
func ParseSigningBackend(s string) (SigningBackend, error) {
	backend := SigningBackend(s)
	if !backend.IsValid() {
		return "", fmt.Errorf("invalid signing backend: %s (must be 'privatekey' or 'pkcs11')", s)
	}
	return backend, nil
}

// PKCS11Config holds PKCS#11 (e.g. SoftHSM) signing backend configuration.
// The PIN is deliberately not a field here: it is read exclusively from the
// ATTESTOR_ATTESTOR_PKCS11_PIN environment variable at the point of use, so
// it never round-trips through YAML/mapstructure or gets logged as part of
// a config dump.
type PKCS11Config struct {
	ModulePath string `mapstructure:"module_path"`
	TokenLabel string `mapstructure:"token_label"`
	SlotNumber *int   `mapstructure:"slot_number"`
	KeyLabel   string `mapstructure:"key_label"`
	// PublicKeyLabel is the label of the public key object, if it differs
	// from KeyLabel. Some tokens (e.g. AWS CloudHSM) require separate
	// private/public key labels; others (e.g. SoftHSM) use one shared
	// label for both. Defaults to KeyLabel when empty.
	PublicKeyLabel string `mapstructure:"public_key_label"`
	KeyID          string `mapstructure:"key_id"`
	MaxSessions    int    `mapstructure:"max_sessions"`
}

// AttestorConfig holds attestor-specific configuration
type AttestorConfig struct {
	PrivateKey          string        `mapstructure:"private_key"`
	Symbols             []string      `mapstructure:"symbols"`
	PollingTime         time.Duration `mapstructure:"polling_time"`
	BatchMode           bool          `mapstructure:"batch_mode"`
	IntentType          string        `mapstructure:"intent_type"`
	IntentVersion       string        `mapstructure:"intent_version"`
	DeviationTrigger    bool          `mapstructure:"deviation_trigger"`
	DeviationThreshold  int           `mapstructure:"deviation_threshold"`
	ForceUpdateInterval time.Duration `mapstructure:"force_update_interval"`
}

// OracleConfig holds oracle configuration
type OracleConfig struct {
	Address    string           `mapstructure:"address"`
	ClientType OracleClientType `mapstructure:"client_type"`
}

// RegistryConfig holds registry configuration
type RegistryConfig struct {
	Address string `mapstructure:"address"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Port int `mapstructure:"port"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

// APIConfig holds API server configuration
type APIConfig struct {
	Port int `mapstructure:"port"`
}
