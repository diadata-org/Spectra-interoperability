package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Config represents the complete bridge configuration
type Config struct {
	Database        DatabaseConfig       `json:"database"`
	Source          SourceConfig         `json:"source"`
	Destinations    []*DestinationConfig `json:"destinations"`
	Routers         []RouterConfig       `json:"routers"`
	PrivateKey      string              `json:"private_key"`
	EventMonitor    EventMonitorConfig   `json:"event_monitor"`
	BlockScanner    BlockScannerConfig   `json:"block_scanner"`
	EventProcessor  EventProcessorConfig `json:"event_processor"`
	WorkerPool      WorkerPoolConfig     `json:"worker_pool"`
	HealthCheck     HealthCheckConfig    `json:"health_check"`
	Recovery        RecoveryConfig       `json:"recovery"`
	API             APIConfig            `json:"api"`
	Metrics         MetricsConfig        `json:"metrics"`
	DryRun          bool                `json:"dry_run"`
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
	WsURL        string                         `json:"ws_url"`
	StartBlock   uint64                         `json:"start_block"`
	Contracts    map[string]map[string]interface{} `json:"contracts"`
	EventFilters EventFilters                   `json:"event_filters"`
}

// EventFilters represents event filtering configuration
type EventFilters struct {
	Symbols   []string `json:"symbols"`
	Signers   []string `json:"signers"`
	MinPrice  string   `json:"min_price"`
	MaxPrice  string   `json:"max_price"`
	MaxAge    int      `json:"max_age"`
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
	SupportedSymbols   []string                     `json:"supported_symbols"`
	Priority           int                          `json:"priority"`
	MinUpdateInterval  Duration                     `json:"min_update_interval"`
	MaxPriceDeviation  float64                      `json:"max_price_deviation"`
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

// RouterConfig represents router configuration
type RouterConfig struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Enabled      bool                   `json:"enabled"`
	Filter       RouterFilter           `json:"filter"`
	Config       map[string]interface{} `json:"config"`
	Destinations []RouterDestination    `json:"destinations"`
}

// RouterFilter represents router filtering options
type RouterFilter struct {
	Symbols []string `json:"symbols"`
	Signers []string `json:"signers"`
}

// RouterDestination represents a router destination
type RouterDestination struct {
	ChainID   int64    `json:"chain_id"`
	Contracts []string `json:"contracts"`
}

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