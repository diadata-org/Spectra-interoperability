package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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
		if c.PrivateKey == "" {
			return fmt.Errorf("private_key is required")
		}
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