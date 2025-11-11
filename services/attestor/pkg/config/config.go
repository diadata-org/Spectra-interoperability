package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

func parseCSV(input string) []string {
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

type Config struct {
	RPC struct {
		URL          string   `mapstructure:"url"`
		URLs         []string `mapstructure:"urls"`
		RegistryURL  string   `mapstructure:"registry_url"`
		RegistryURLs []string `mapstructure:"registry_urls"`
	} `mapstructure:"rpc"`

	Oracle struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"oracle"`

	Registry struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"registry"`

	Attestor struct {
		PrivateKey         string         `mapstructure:"private_key"`
		Symbols            []string       `mapstructure:"symbols"`
		PollingTime        time.Duration  `mapstructure:"polling_time"`
		BatchMode          bool           `mapstructure:"batch_mode"`
		Mode               AttestorMode   `mapstructure:"mode"`
		ReplicaBackupDelay int            `mapstructure:"replica_backup_delay"`
		IntentType         string         `mapstructure:"intent_type"`
		IntentVersion      string         `mapstructure:"intent_version"`
		Guardian           GuardianConfig `mapstructure:"guardian"`
	} `mapstructure:"attestor"`

	Logging struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"logging"`

	Metrics struct {
		Port int `mapstructure:"port"`
	} `mapstructure:"metrics"`

	API struct {
		Port int `mapstructure:"port"`
	} `mapstructure:"api"`
}

type GuardianConfig struct {
	Default GuardianParams            `mapstructure:"default"`
	Symbols map[string]GuardianParams `mapstructure:"symbols"`
}

type GuardianParams struct {
	MaxDeviationBips   int `mapstructure:"max_deviation_bips"`
	MaxTimestampAge    int `mapstructure:"max_timestamp_age"`
	MinGuardianMatches int `mapstructure:"min_guardian_matches"`
}

// GetParamsForSymbol returns guardian parameters for a specific symbol.
func (gc *GuardianConfig) GetParamsForSymbol(symbol string) GuardianParams {
	if params, ok := gc.Symbols[symbol]; ok {
		return params
	}
	return gc.Default
}

var cfg *Config

func Init(configPath string) (*Config, error) {
	v := viper.New()

	// Set config name and path
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/attestor/")
	}

	// Set environment variable support
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetEnvPrefix("ATTESTOR")

	// Set defaults
	v.SetDefault("rpc.url", "https://testnet-rpc.diadata.org")
	v.SetDefault("rpc.registry_url", "https://testnet-rpc.diadata.org")
	v.SetDefault("oracle.address", "0x0087342f5f4c7AB23a37c045c3EF710749527c88")
	v.SetDefault("attestor.symbols", []string{"BTC/USD", "ETH/USD"})
	v.SetDefault("attestor.polling_time", "300ms")
	v.SetDefault("attestor.batch_mode", true)
	v.SetDefault("attestor.mode", "prime")
	v.SetDefault("attestor.replica_backup_delay", 300)
	v.SetDefault("attestor.intent_type", "OracleUpdate")
	v.SetDefault("attestor.intent_version", "1.0")
	v.SetDefault("attestor.guardian.default.max_deviation_bips", 500)
	v.SetDefault("attestor.guardian.default.max_timestamp_age", 3600)
	v.SetDefault("attestor.guardian.default.min_guardian_matches", 1)
	v.SetDefault("logging.level", "info")
	v.SetDefault("metrics.port", 8080)
	v.SetDefault("api.port", 8081)

	// Bind environment variables for backward compatibility
	v.BindEnv("rpc.url", "RPC_URL")
	v.BindEnv("rpc.urls", "RPC_URLS")
	v.BindEnv("rpc.registry_urls", "RPC_REGISTRY_URLS")
	v.BindEnv("oracle.address", "ORACLE_ADDRESS")
	v.BindEnv("registry.address", "INTENT_REGISTRY_ADDRESS")
	v.BindEnv("attestor.private_key", "PRIVATE_KEY")
	v.BindEnv("attestor.symbols", "SYMBOLS")
	v.BindEnv("attestor.polling_time", "POLLING_TIME")
	v.BindEnv("attestor.mode", "MODE")
	v.BindEnv("attestor.replica_backup_delay", "REPLICA_BACKUP_DELAY")
	v.BindEnv("attestor.intent_type", "INTENT_TYPE")
	v.BindEnv("attestor.intent_version", "INTENT_VERSION")
	v.BindEnv("logging.level", "LOG_LEVEL")
	v.BindEnv("metrics.port", "METRICS_PORT")

	// Also bind with L2_ prefix for backward compatibility
	v.BindEnv("rpc.registry_url", "L2_RPC_URL")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; use defaults and environment
	}

	// Parse config
	cfg = &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unable to decode config: %w", err)
	}

	if !cfg.Attestor.Mode.IsValid() {
		return nil, fmt.Errorf("invalid attestor mode: %s (must be 'prime' or 'replica')", cfg.Attestor.Mode)
	}

	// Handle SYMBOLS environment variable for backward compatibility
	if symbolsEnv := v.GetString("SYMBOLS"); symbolsEnv != "" {
		symbols := strings.Split(symbolsEnv, ",")
		for i := range symbols {
			symbols[i] = strings.TrimSpace(symbols[i])
		}
		cfg.Attestor.Symbols = symbols
	}

	// Handle POLLING_TIME for backward compatibility (convert seconds to duration)
	if pollingEnv := v.GetString("POLLING_TIME"); pollingEnv != "" {
		duration, err := time.ParseDuration(pollingEnv + "s")
		if err == nil {
			cfg.Attestor.PollingTime = duration
		}
	}

	// Normalize RPC URLs configuration
	if urlsEnv := v.GetString("RPC_URLS"); urlsEnv != "" {
		cfg.RPC.URLs = parseCSV(urlsEnv)
	}
	if len(cfg.RPC.URLs) == 0 {
		if cfg.RPC.URL != "" {
			cfg.RPC.URLs = []string{strings.TrimSpace(cfg.RPC.URL)}
		}
	}
	if len(cfg.RPC.URLs) == 0 {
		cfg.RPC.URLs = []string{"https://testnet-rpc.diadata.org"}
	}

	if registryURLsEnv := v.GetString("RPC_REGISTRY_URLS"); registryURLsEnv != "" {
		cfg.RPC.RegistryURLs = parseCSV(registryURLsEnv)
	}
	if len(cfg.RPC.RegistryURLs) == 0 {
		if cfg.RPC.RegistryURL != "" {
			cfg.RPC.RegistryURLs = []string{strings.TrimSpace(cfg.RPC.RegistryURL)}
		}
	}
	if len(cfg.RPC.RegistryURLs) == 0 && len(cfg.RPC.URLs) > 0 {
		cfg.RPC.RegistryURLs = append([]string(nil), cfg.RPC.URLs...)
	}
	if len(cfg.RPC.RegistryURLs) == 0 {
		cfg.RPC.RegistryURLs = []string{"https://testnet-rpc.diadata.org"}
	}

	return cfg, nil
}

func Get() *Config {
	if cfg == nil {
		panic("config not initialized")
	}
	return cfg
}

// GetSafe returns the config safely with error handling
func GetSafe() (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config not initialized")
	}
	return cfg, nil
}
