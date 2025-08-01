package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

// Load loads configuration from a JSON file
func Load(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Override private key from environment if set
	if envPrivateKey := os.Getenv("BRIDGE_PRIVATE_KEY"); envPrivateKey != "" {
		config.PrivateKey = envPrivateKey
	}

	// Override database configuration from environment if set
	if postgresHost := os.Getenv("POSTGRES_HOST"); postgresHost != "" {
		postgresUser := os.Getenv("POSTGRES_USER")
		if postgresUser == "" {
			postgresUser = "postgres"
		}
		postgresPassword := os.Getenv("POSTGRES_PASSWORD")
		postgresDB := os.Getenv("POSTGRES_DB")
		if postgresDB == "" {
			postgresDB = "postgres"
		}
		postgresPort := os.Getenv("POSTGRES_PORT")
		if postgresPort == "" {
			postgresPort = "5432"
		}

		// For Supabase and cloud databases, we need to use sslmode=require
		sslMode := "disable"
		if strings.Contains(postgresHost, "supabase.co") || strings.Contains(postgresHost, "amazonaws.com") {
			sslMode = "require"
		}

		// Add connection parameters to help with cloud databases
		config.Database.DSN = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&connect_timeout=30",
			postgresUser, postgresPassword, postgresHost, postgresPort, postgresDB, sslMode)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate source configuration
	if c.Source.ChainID == 0 {
		return fmt.Errorf("source chain_id is required")
	}
	if len(c.Source.RPCURLs) == 0 {
		return fmt.Errorf("source rpc_urls is required")
	}

	// Validate destination configurations
	if len(c.Destinations) == 0 {
		return fmt.Errorf("at least one destination is required")
	}

	for i, dest := range c.Destinations {
		if dest.ChainID == 0 {
			return fmt.Errorf("destination[%d] chain_id is required", i)
		}
		if len(dest.RPCURLs) == 0 {
			return fmt.Errorf("destination[%d] rpc_urls is required", i)
		}
	}

	// Validate private key
	if c.PrivateKey == "" {
		return fmt.Errorf("private_key is required (set in config or BRIDGE_PRIVATE_KEY env var)")
	}

	// Ensure private key has 0x prefix
	if !strings.HasPrefix(c.PrivateKey, "0x") {
		c.PrivateKey = "0x" + c.PrivateKey
	}

	// Set default values
	if c.EventMonitor.ReconnectInterval == 0 {
		c.EventMonitor.ReconnectInterval = Duration(5 * time.Second)
	}
	if c.BlockScanner.ScanInterval == 0 {
		c.BlockScanner.ScanInterval = Duration(60 * time.Second)
	}
	if c.WorkerPool.MaxWorkers == 0 {
		c.WorkerPool.MaxWorkers = 10
	}

	return nil
}

// GetDestinationByChainID returns the destination configuration for a given chain ID
func (c *Config) GetDestinationByChainID(chainID int64) *DestinationConfig {
	for i := range c.Destinations {
		if c.Destinations[i].ChainID == chainID {
			return c.Destinations[i]
		}
	}
	return nil
}

// GetEnabledDestinations returns only enabled destination configurations
func (c *Config) GetEnabledDestinations() []*DestinationConfig {
	var enabled []*DestinationConfig
	for i := range c.Destinations {
		if c.Destinations[i].Enabled {
			enabled = append(enabled, c.Destinations[i])
		}
	}
	return enabled
}
