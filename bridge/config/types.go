package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Config represents the complete bridge configuration
type Config struct {
	Database        DatabaseConfig                `json:"database"`
	Source          SourceConfig                  `json:"source"`
	EventDefinitions map[string]*EventDefinition  `json:"event_definitions"`
	Destinations    map[int64]*DestinationConfig  `json:"destinations"`
	Routers         []RouterConfig                `json:"routers"`
	PrivateKey      string                        `json:"private_key"` // Default private key (deprecated - use per-router keys)
	EventMonitor    EventMonitorConfig            `json:"event_monitor"`
	BlockScanner    BlockScannerConfig            `json:"block_scanner"`
	EventProcessor  EventProcessorConfig          `json:"event_processor"`
	WorkerPool      WorkerPoolConfig              `json:"worker_pool"`
	HealthCheck     HealthCheckConfig             `json:"health_check"`
	Recovery        RecoveryConfig                `json:"recovery"`
	API             APIConfig                     `json:"api"`
	Metrics         MetricsConfig                 `json:"metrics"`
	DryRun          bool                          `json:"dry_run"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

// SourceConfig represents source chain configuration
type SourceConfig struct {
	ChainID      int64                          `json:"chain_id"`
	Name         string                         `json:"name"`
	RPCURLs      []string                       `json:"rpc_urls"`      // Multiple RPC URLs for failover
	WsURL        string                         `json:"ws_url"`        // WebSocket URL for event monitoring
	StartBlock   uint64                         `json:"start_block"`
}


// DestinationConfig represents destination chain configuration
type DestinationConfig struct {
	ChainID   int64             `json:"chain_id"`
	Name      string            `json:"name"`
	RPCURLs   []string          `json:"rpc_urls"`   // Multiple RPC URLs for failover
	Enabled   bool              `json:"enabled"`
	Contracts []ContractConfig  `json:"contracts"`
}

// ContractConfig represents a contract configuration
type ContractConfig struct {
	Name               string                       `json:"name"`
	Address            string                       `json:"address"`
	Type               string                       `json:"type"`
	Enabled            bool                         `json:"enabled"`
	GasLimit           uint64                       `json:"gas_limit"`
	GasMultiplier      float64                      `json:"gas_multiplier"`
	MaxGasPrice        string                       `json:"max_gas_price"`
	ABI                string                       `json:"abi"`
	Methods            map[string]MethodConfig      `json:"methods"`
}

// MethodConfig represents a contract method configuration
type MethodConfig struct {
	MethodName    string            `json:"method_name"`
	FieldsMapping map[string]string `json:"fields_mapping"`
	GasLimit      uint64            `json:"gas_limit"`
}

// EventMonitorConfig represents event monitor configuration
type EventMonitorConfig struct {
	Enabled              bool          `json:"enabled"`
	ReconnectInterval    Duration `json:"reconnect_interval"`
	MaxReconnectAttempts int           `json:"max_reconnect_attempts"`
}

// BlockScannerConfig represents block scanner configuration
type BlockScannerConfig struct {
	Enabled        bool          `json:"enabled"`
	ScanInterval   Duration      `json:"scan_interval"`
	BlockRange     uint64        `json:"block_range"`
	MaxBlockGap    uint64        `json:"max_block_gap"`
	BackwardSync   bool          `json:"backward_sync"`   // Enable backward sync for faster gap recovery
}

// EventProcessorConfig represents event processor configuration
type EventProcessorConfig struct {
	BatchSize         int           `json:"batch_size"`
	ValidationTimeout Duration `json:"validation_timeout"`
	DedupCacheSize    int           `json:"dedup_cache_size"`
	DedupCacheTTL     Duration `json:"dedup_cache_ttl"`
}

// WorkerPoolConfig represents worker pool configuration
type WorkerPoolConfig struct {
	MaxWorkers     int           `json:"max_workers"`
	TaskQueueSize  int           `json:"task_queue_size"`
	TaskTimeout    Duration `json:"task_timeout"`
	RetryDelay     Duration `json:"retry_delay"`
	MaxRetries     int           `json:"max_retries"`
}

// HealthCheckConfig represents health check configuration
type HealthCheckConfig struct {
	Enabled          bool          `json:"enabled"`
	CheckInterval    Duration `json:"check_interval"`
	Timeout          Duration `json:"timeout"`
	MaxProcessingLag Duration `json:"max_processing_lag"`
	MaxQueueSize     int           `json:"max_queue_size"`
}

// RecoveryConfig represents recovery configuration
type RecoveryConfig struct {
	Enabled         bool          `json:"enabled"`
	MinFailures     int           `json:"min_failures"`
	MaxAttempts     int           `json:"max_attempts"`
	RetryInterval   Duration `json:"retry_interval"`
	RecoveryTimeout Duration `json:"recovery_timeout"`
}

// APIConfig represents API server configuration
type APIConfig struct {
	Enabled     bool   `json:"enabled"`
	ListenAddr  string `json:"listen_addr"`
	EnableCORS  bool   `json:"enable_cors"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Namespace string `json:"namespace"`
}

// NOTE: RouterConfig, RouterFilter, and RouterDestination are now defined in event_definitions.go
// as part of the generic event handling system

// Duration wrapper for JSON marshaling
type Duration time.Duration

// Duration returns the time.Duration value
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// MarshalJSON implements json.Marshaler
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON implements json.Unmarshaler
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value) * time.Second)
		return nil
	case string:
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(dur)
		return nil
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
}