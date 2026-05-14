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
