package config

import "time"

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
