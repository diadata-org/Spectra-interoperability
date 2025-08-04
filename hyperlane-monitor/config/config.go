package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Database           DatabaseConfig               `json:"database" mapstructure:"database"`
	ChainConfigs       map[string]ChainConfig       `json:"chain_configs" mapstructure:"chain_configs"`
	MonitoringPairs    []MonitoringPairConfig       `json:"monitoring_pairs" mapstructure:"monitoring_pairs"`
	MonitoringProfiles map[string]MonitoringProfile `json:"monitoring_profiles" mapstructure:"monitoring_profiles"`
	BridgeAPI          BridgeAPIConfig              `json:"bridge_api" mapstructure:"bridge_api"`
	Metrics            MetricsConfig                `json:"metrics" mapstructure:"metrics"`
	MetricsPort        int                          `json:"metrics_port" mapstructure:"metrics_port"`
}

type DatabaseConfig struct {
	Driver string `json:"driver" mapstructure:"driver"`
	DSN    string `json:"dsn" mapstructure:"dsn"`
}

type ChainConfig struct {
	Name                string   `json:"name" mapstructure:"name"`
	RPCURLs             []string `json:"rpc_urls" mapstructure:"rpc_urls"`
	ScanInterval        string   `json:"scan_interval,omitempty" mapstructure:"scan_interval"`
	HealthCheckInterval string   `json:"health_check_interval,omitempty" mapstructure:"health_check_interval"`
}

type MonitoringPairConfig struct {
	Source      SourceConfig      `json:"source" mapstructure:"source"`
	Destination DestinationConfig `json:"destination" mapstructure:"destination"`
}

type SourceConfig struct {
	ChainID        int    `json:"chain_id" mapstructure:"chain_id"`
	OracleTrigger  string `json:"oracle_trigger" mapstructure:"oracle_trigger"`
	OracleRegistry string `json:"oracle_registry" mapstructure:"oracle_registry"`
	StartBlock     uint64 `json:"start_block,omitempty" mapstructure:"start_block"`
}

type DestinationConfig struct {
	ChainID   int              `json:"chain_id" mapstructure:"chain_id"`
	Receivers []ReceiverConfig `json:"receivers" mapstructure:"receivers"`
}

type ReceiverConfig struct {
	Address    string           `json:"address" mapstructure:"address"`
	Name       string           `json:"name" mapstructure:"name"`
	Monitoring MonitoringConfig `json:"monitoring" mapstructure:"monitoring"`
}

type MonitoringConfig struct {
	Enabled          bool   `json:"enabled" mapstructure:"enabled"`
	Profile          string `json:"profile,omitempty" mapstructure:"profile"`
	CheckInterval    string `json:"check_interval,omitempty" mapstructure:"check_interval"`
	InitialWait      string `json:"initial_wait,omitempty" mapstructure:"initial_wait"`
	MaxDeliveryWait  string `json:"max_delivery_wait,omitempty" mapstructure:"max_delivery_wait"`
	MaxCheckAttempts int    `json:"max_check_attempts,omitempty" mapstructure:"max_check_attempts"`
	AlertOnFailure   bool   `json:"alert_on_failure,omitempty" mapstructure:"alert_on_failure"`
	AlertWebhook     string `json:"alert_webhook,omitempty" mapstructure:"alert_webhook"`
	Reason           string `json:"reason,omitempty" mapstructure:"reason"`
}

type MonitoringProfile struct {
	CheckInterval      string `json:"check_interval"`
	InitialWait        string `json:"initial_wait"`
	MaxDeliveryWait    string `json:"max_delivery_wait"`
	MaxCheckAttempts   int    `json:"max_check_attempts"`
	ConcurrentChecks   int    `json:"concurrent_checks"`
	Priority           string `json:"priority"`
	ExponentialBackoff bool   `json:"exponential_backoff"`
}

// BridgeAPIConfig holds Bridge service API settings
type BridgeAPIConfig struct {
	BaseURL       string `json:"base_url" mapstructure:"base_url"`
	GRPCAddress   string `json:"grpc_address" mapstructure:"grpc_address"`
	UseGRPC       bool   `json:"use_grpc" mapstructure:"use_grpc"`
	Timeout       string `json:"timeout" mapstructure:"timeout"`
	RetryAttempts int    `json:"retry_attempts" mapstructure:"retry_attempts"`
	RetryDelay    string `json:"retry_delay" mapstructure:"retry_delay"`
}

// MetricsConfig holds metrics server settings
type MetricsConfig struct {
	Enabled bool   `json:"enabled"`
	Port    string `json:"port"`
}

// LoadConfig loads configuration from file
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("json")

	// Set defaults
	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.port", "9091")
	viper.SetDefault("bridge_api.timeout", "30s")
	viper.SetDefault("bridge_api.retry_attempts", 3)
	viper.SetDefault("bridge_api.retry_delay", "5s")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config from %s: %w", configPath, err)
	}

	var config Config
	// Fix for viper not properly unmarshaling nested structs
	// Manually get the monitoring_pairs
	if err := viper.UnmarshalKey("database", &config.Database); err != nil {
		return nil, fmt.Errorf("failed to unmarshal database config: %w", err)
	}
	if err := viper.UnmarshalKey("chain_configs", &config.ChainConfigs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chain configs: %w", err)
	}
	if err := viper.UnmarshalKey("monitoring_pairs", &config.MonitoringPairs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal monitoring pairs: %w", err)
	}
	if err := viper.UnmarshalKey("monitoring_profiles", &config.MonitoringProfiles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal monitoring profiles: %w", err)
	}
	if err := viper.UnmarshalKey("bridge_api", &config.BridgeAPI); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bridge api config: %w", err)
	}

	// Debug: print loaded bridge API config
	fmt.Printf("Loaded Bridge API config: BaseURL=%s, Timeout=%s, RetryAttempts=%d\n",
		config.BridgeAPI.BaseURL, config.BridgeAPI.Timeout, config.BridgeAPI.RetryAttempts)
	if err := viper.UnmarshalKey("metrics", &config.Metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics config: %w", err)
	}
	config.MetricsPort = viper.GetInt("metrics_port")

	// Override database configuration from environment if set
	if postgresHost := os.Getenv("POSTGRES_HOST"); postgresHost != "" {
		postgresUser := os.Getenv("POSTGRES_USER")
		if postgresUser == "" {
			postgresUser = "postgres"
		}
		postgresPassword := os.Getenv("POSTGRES_PASSWORD")
		postgresDB := os.Getenv("POSTGRES_DB")
		if postgresDB == "" {
			postgresDB = "hyperlane_monitor"
		}
		postgresPort := os.Getenv("POSTGRES_PORT")
		if postgresPort == "" {
			postgresPort = "5432"
		}

		// For cloud databases, we need to use sslmode=require
		sslMode := "disable"
		if strings.Contains(postgresHost, "supabase.co") || strings.Contains(postgresHost, "amazonaws.com") || strings.Contains(postgresHost, "rlwy.net") {
			sslMode = "require"
		}

		// Add connection parameters to help with cloud databases
		config.Database.DSN = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&connect_timeout=30",
			postgresUser, postgresPassword, postgresHost, postgresPort, postgresDB, sslMode)
	}

	// Override Bridge API base URL from environment if set
	if bridgeAPIURL := os.Getenv("BRIDGE_API_URL"); bridgeAPIURL != "" {
		config.BridgeAPI.BaseURL = bridgeAPIURL
	}

	// Debug: print loaded pairs
	fmt.Printf("Loaded %d monitoring pairs\n", len(config.MonitoringPairs))

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Database.DSN == "" {
		return fmt.Errorf("database DSN is required")
	}

	if len(c.MonitoringPairs) == 0 {
		return fmt.Errorf("at least one monitoring pair is required")
	}

	for i, pair := range c.MonitoringPairs {
		if pair.Source.ChainID == 0 {
			return fmt.Errorf("source chain ID is required for pair %d", i)
		}
		if pair.Source.OracleTrigger == "" {
			return fmt.Errorf("oracle trigger address is required for pair %d", i)
		}
		if pair.Destination.ChainID == 0 {
			return fmt.Errorf("destination chain ID is required for pair %d", i)
		}
		if len(pair.Destination.Receivers) == 0 {
			return fmt.Errorf("at least one receiver is required for pair %d", i)
		}
	}

	return nil
}

// GetDuration parses a duration string with default fallback
func GetDuration(value, defaultValue string) time.Duration {
	if value == "" {
		value = defaultValue
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		defaultDuration, _ := time.ParseDuration(defaultValue)
		return defaultDuration
	}
	return duration
}

// GetPairID generates a unique ID for a monitoring pair
func GetPairID(sourceChainID, destChainID int, oracleTrigger string) string {
	// Include oracle trigger address to support multiple triggers per chain pair
	return fmt.Sprintf("%d_%d_%s", sourceChainID, destChainID, oracleTrigger)
}

// GetChainConfig retrieves a chain configuration by ID
func (c *Config) GetChainConfig(chainID int) (*ChainConfig, bool) {
	chainIDStr := fmt.Sprintf("%d", chainID)
	config, exists := c.ChainConfigs[chainIDStr]
	return &config, exists
}
