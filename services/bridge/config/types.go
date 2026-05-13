package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Config represents the complete bridge configuration
type Config struct {
	Database         DatabaseConfig               `json:"database"`
	Source           SourceConfig                 `json:"source"`
	EventDefinitions map[string]*EventDefinition  `json:"event_definitions"`
	Destinations     map[int64]*DestinationConfig `json:"destinations"`
	Routers          []LegacyRouterConfig         `json:"routers"`
	PrivateKey       string                       `json:"private_key"` // Default private key (deprecated - use per-router keys)
	EventMonitor     EventMonitorConfig           `json:"event_monitor"`
	BlockScanner     BlockScannerConfig           `json:"block_scanner"`
	EventProcessor   EventProcessorConfig         `json:"event_processor"`
	WorkerPool       WorkerPoolConfig             `json:"worker_pool"`
	HealthCheck      HealthCheckConfig            `json:"health_check"`
	Recovery         RecoveryConfig               `json:"recovery"`
	API              APIConfig                    `json:"api"`
	Metrics          MetricsConfig                `json:"metrics"`
	DryRun           bool                         `json:"dry_run"`
	CronService      CronServiceConfig            `json:"cron_service"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Driver string `yaml:"driver" json:"driver"`
	DSN    string `yaml:"dsn" json:"dsn"`
	DSNEnv string `yaml:"dsn_env,omitempty" json:"dsn_env,omitempty"`
}

// SourceConfig represents source chain configuration
type SourceConfig struct {
	ChainID    int64    `yaml:"chain_id" json:"chain_id"`
	Name       string   `yaml:"name" json:"name"`
	RPCURLs    []string `yaml:"rpc_urls" json:"rpc_urls"` // Multiple RPC URLs for failover
	WsURL      string   `yaml:"ws_url" json:"ws_url"`     // WebSocket URL for event monitoring
	StartBlock uint64   `yaml:"start_block" json:"start_block"`
}

// DestinationConfig represents destination chain configuration
type DestinationConfig struct {
	ChainID   int64            `json:"chain_id"`
	Name      string           `json:"name"`
	RPCURLs   []string         `json:"rpc_urls"` // Multiple RPC URLs for failover
	Enabled   bool             `json:"enabled"`
	Contracts []ContractConfig `json:"contracts"`
}

// MethodConfig represents a contract method configuration
type MethodConfig struct {
	MethodName    string            `json:"method_name"`
	FieldsMapping map[string]string `json:"fields_mapping"`
	GasLimit      uint64            `json:"gas_limit"`
}

// EventMonitorConfig represents event monitor configuration
type EventMonitorConfig struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	ReconnectInterval    Duration `yaml:"reconnectinterval" json:"reconnect_interval"`
	MaxReconnectAttempts int      `yaml:"maxreconnectattempts" json:"max_reconnect_attempts"`
}

// BlockScannerConfig represents block scanner configuration
type BlockScannerConfig struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	ScanInterval         Duration `yaml:"scaninterval" json:"scan_interval"`
	BlockRange           uint64   `yaml:"blockrange" json:"block_range"`
	MaxBlockGap          uint64   `yaml:"maxblockgap" json:"max_block_gap"`
	BackwardSync         bool     `yaml:"backwardsync" json:"backward_sync"` // Enable backward sync for faster gap recovery
	HeadTrackerInterval  Duration `yaml:"headtrackerinterval,omitempty" json:"headTrackerInterval,omitempty"`
	GapDetectionInterval Duration `yaml:"gapdetectioninterval,omitempty" json:"gapDetectionInterval,omitempty"`
}

// EventProcessorConfig represents event processor configuration
type EventProcessorConfig struct {
	BatchSize           int      `yaml:"batchsize" json:"batch_size"`
	ValidationTimeout   Duration `yaml:"validationtimeout" json:"validation_timeout"`
	DedupCacheSize      int      `yaml:"dedupcachesize" json:"dedup_cache_size"`
	DedupCacheTTL       Duration `yaml:"dedupcachettl" json:"dedup_cache_ttl"`
	EnableParallelMode  bool     `yaml:"enableparallelmode" json:"enable_parallel_mode"`
	ParallelWorkerCount int      `yaml:"parallelworkercount" json:"parallel_worker_count"`
	ParallelQueueSize   int      `yaml:"parallelqueuesize" json:"parallel_queue_size"`
	ParallelTimeout     Duration `yaml:"paralleltimeout" json:"parallel_timeout"`
}

// WorkerPoolConfig represents worker pool configuration
type WorkerPoolConfig struct {
	MaxWorkers    int      `yaml:"maxworkers" json:"max_workers"`
	TaskQueueSize int      `yaml:"taskqueuesize" json:"task_queue_size"`
	TaskTimeout   Duration `yaml:"tasktimeout" json:"task_timeout"`
	RetryDelay    Duration `yaml:"retrydelay" json:"retry_delay"`
	MaxRetries    int      `yaml:"maxretries" json:"max_retries"`
}

// HealthCheckConfig represents health check configuration
type HealthCheckConfig struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	CheckInterval    Duration `yaml:"checkinterval" json:"check_interval"`
	Timeout          Duration `yaml:"timeout" json:"timeout"`
	MaxProcessingLag Duration `yaml:"maxprocessinglag" json:"max_processing_lag"`
	MaxQueueSize     int      `yaml:"maxqueuesize" json:"max_queue_size"`
}

// RecoveryConfig represents recovery configuration
type RecoveryConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	MinFailures     int      `yaml:"minfailures" json:"min_failures"`
	MaxAttempts     int      `yaml:"maxattempts" json:"max_attempts"`
	RetryInterval   Duration `yaml:"retryinterval" json:"retry_interval"`
	RecoveryTimeout Duration `yaml:"recoverytimeout" json:"recovery_timeout"`
}

// APIConfig represents API server configuration
type APIConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	ListenAddr string `yaml:"listenaddr" json:"listen_addr"`
	EnableCORS bool   `yaml:"enablecors" json:"enable_cors"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Namespace string `yaml:"namespace" json:"namespace"`
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

// UnmarshalYAML implements yaml.Unmarshaler
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var v interface{}
	if err := unmarshal(&v); err != nil {
		return err
	}
	switch value := v.(type) {
	case int:
		// Treat integers as nanoseconds (raw time.Duration value)
		*d = Duration(time.Duration(value))
		return nil
	case float64:
		// Treat floats as nanoseconds (raw time.Duration value)
		*d = Duration(time.Duration(value))
		return nil
	case string:
		// Parse string durations like "30s", "10m", etc.
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

// MarshalYAML implements yaml.Marshaler
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// CronServiceConfig represents cron-based update service configuration
type CronServiceConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	Schedule       string   `yaml:"schedule" json:"schedule"` // Default cron expression, used if router doesn't have time_threshold
	PriceDeviation float64  `yaml:"price_deviation" json:"price_deviation"` // Default minimum price deviation to trigger update (as percentage)
}
