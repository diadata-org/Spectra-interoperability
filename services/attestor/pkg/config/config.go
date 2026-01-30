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
		Address    string          `mapstructure:"address"`
		ClientType OracleClientType `mapstructure:"client_type"`
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

	// Explicitly bind nested attestor.* config keys to ATTESTOR_ATTESTOR_* env vars.
	//
	// WHY THIS IS NEEDED:
	// Without explicit binding, Viper's AutomaticEnv() CAN read the env var via GetString(),
	// but Unmarshal() won't populate nested struct fields. This is a known Viper limitation.
	//
	// ENV VAR NAMING:
	// Config key "attestor.private_key" with prefix "ATTESTOR" and replacer "." -> "_"
	// becomes: ATTESTOR_ATTESTOR_PRIVATE_KEY
	// (prefix + key_with_underscores_uppercased)
	v.BindEnv("attestor.private_key", "ATTESTOR_ATTESTOR_PRIVATE_KEY")
	v.BindEnv("attestor.symbols", "ATTESTOR_ATTESTOR_SYMBOLS")
	v.BindEnv("attestor.polling_time", "ATTESTOR_ATTESTOR_POLLING_TIME")
	v.BindEnv("attestor.batch_mode", "ATTESTOR_ATTESTOR_BATCH_MODE")
	v.BindEnv("attestor.mode", "ATTESTOR_ATTESTOR_MODE")
	v.BindEnv("attestor.replica_backup_delay", "ATTESTOR_ATTESTOR_REPLICA_BACKUP_DELAY")
	v.BindEnv("attestor.intent_type", "ATTESTOR_ATTESTOR_INTENT_TYPE")
	v.BindEnv("attestor.intent_version", "ATTESTOR_ATTESTOR_INTENT_VERSION")
	v.BindEnv("oracle.client_type", "ATTESTOR_ORACLE_CLIENT_TYPE")
	v.BindEnv("attestor.guardian.default.max_deviation_bips", "ATTESTOR_GUARDIAN_MAX_DEVIATION_BIPS")
	v.BindEnv("attestor.guardian.default.max_timestamp_age", "ATTESTOR_GUARDIAN_MAX_TIMESTAMP_AGE")
	v.BindEnv("attestor.guardian.default.min_guardian_matches", "ATTESTOR_GUARDIAN_MIN_GUARDIAN_MATCHES")

	// Set defaults
	v.SetDefault("rpc.url", "https://testnet-rpc.diadata.org")
	v.SetDefault("rpc.registry_url", "https://testnet-rpc.diadata.org")
	v.SetDefault("oracle.address", "0x0087342f5f4c7AB23a37c045c3EF710749527c88")
	v.SetDefault("oracle.client_type", "guarded")
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

	// Parse and validate oracle client type
	if cfg.Oracle.ClientType == "" {
		cfg.Oracle.ClientType = OracleClientTypeGuarded // Default to guarded
	} else {
		clientType, err := ParseOracleClientType(string(cfg.Oracle.ClientType))
		if err != nil {
			return nil, fmt.Errorf("invalid oracle client type: %w", err)
		}
		cfg.Oracle.ClientType = clientType
	}

	// Normalize RPC URLs configuration: convert single URL to array if needed
	if len(cfg.RPC.URLs) == 0 {
		if cfg.RPC.URL != "" {
			cfg.RPC.URLs = []string{strings.TrimSpace(cfg.RPC.URL)}
		}
	}
	if len(cfg.RPC.URLs) == 0 {
		cfg.RPC.URLs = []string{"https://testnet-rpc.diadata.org"}
	}

	// Normalize registry RPC URLs configuration: convert single URL to array if needed
	if len(cfg.RPC.RegistryURLs) == 0 {
		if cfg.RPC.RegistryURL != "" {
			cfg.RPC.RegistryURLs = []string{strings.TrimSpace(cfg.RPC.RegistryURL)}
		}
	}
	// Fallback to RPC.URLs if registry URLs not specified
	if len(cfg.RPC.RegistryURLs) == 0 && len(cfg.RPC.URLs) > 0 {
		cfg.RPC.RegistryURLs = append([]string(nil), cfg.RPC.URLs...)
	}
	if len(cfg.RPC.RegistryURLs) == 0 {
		cfg.RPC.RegistryURLs = []string{"https://testnet-rpc.diadata.org"}
	}

	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// validateConfig validates the configuration after loading
func validateConfig(cfg *Config) error {
	if cfg.Attestor.PrivateKey == "" {
		return fmt.Errorf("private key not configured")
	}

	if cfg.Oracle.Address == "" {
		return fmt.Errorf("oracle address not configured")
	}

	if !cfg.Oracle.ClientType.IsValid() {
		return fmt.Errorf("invalid oracle client type: %s (must be 'guarded' or 'dia_v2')", cfg.Oracle.ClientType)
	}

	if cfg.Registry.Address == "" {
		return fmt.Errorf("registry address not configured")
	}

	if len(cfg.Attestor.Symbols) == 0 {
		return fmt.Errorf("no symbols configured")
	}

	if cfg.Attestor.PollingTime <= 0 {
		return fmt.Errorf("invalid polling time: %v", cfg.Attestor.PollingTime)
	}

	// Validate guardian params
	if err := validateGuardianConfig(&cfg.Attestor.Guardian); err != nil {
		return fmt.Errorf("guardian configuration invalid: %w", err)
	}

	return nil
}

// validateGuardianConfig validates guardian configuration
func validateGuardianConfig(gc *GuardianConfig) error {
	// Validate default params
	if err := validateGuardianParams(gc.Default); err != nil {
		return fmt.Errorf("default guardian params: %w", err)
	}

	// Validate per-symbol params
	for symbol, params := range gc.Symbols {
		if err := validateGuardianParams(params); err != nil {
			return fmt.Errorf("guardian params for symbol %s: %w", symbol, err)
		}
	}

	return nil
}

// validateGuardianParams validates individual guardian parameters
func validateGuardianParams(params GuardianParams) error {
	if params.MaxDeviationBips < 0 || params.MaxDeviationBips > 10000 {
		return fmt.Errorf("max_deviation_bips must be between 0 and 10000, got %d", params.MaxDeviationBips)
	}

	if params.MaxTimestampAge <= 0 {
		return fmt.Errorf("max_timestamp_age must be positive, got %d", params.MaxTimestampAge)
	}

	if params.MinGuardianMatches < 0 {
		return fmt.Errorf("min_guardian_matches must be positive, got %d", params.MinGuardianMatches)
	}

	return nil
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
