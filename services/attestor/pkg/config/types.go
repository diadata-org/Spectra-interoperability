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

// AttestorConfig holds attestor-specific configuration
type AttestorConfig struct {
	PrivateKey    string        `mapstructure:"private_key"`
	Symbols       []string      `mapstructure:"symbols"`
	PollingTime   time.Duration `mapstructure:"polling_time"`
	BatchMode     bool          `mapstructure:"batch_mode"`
	IntentType    string        `mapstructure:"intent_type"`
	IntentVersion string        `mapstructure:"intent_version"`
}

// OracleConfig holds oracle configuration
type OracleConfig struct {
	Address string `mapstructure:"address"`
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
