package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/logger"
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

// parseAssetGuardianFromJSON parses per-asset guardian config from viper
// Expected JSON format (set via ATTESTOR_GUARDIAN_ASSETS_CONFIG env var):
//
//	{
//	  "BTC/USD": {
//	    "max_deviation_bips": 100,
//	    "max_timestamp_age": 1800,
//	    "min_guardian_matches": 1
//	  },
//	  "DEX:BTC/USD": {
//	    "max_deviation_bips": 150,
//	    "min_guardian_matches": 2
//	  }
//	}
func parseAssetGuardianFromJSON(v *viper.Viper) (map[string]GuardianParams, error) {
	// Get the JSON string from viper (bound to ATTESTOR_GUARDIAN_ASSETS_CONFIG)
	jsonConfig := v.GetString("attestor.guardian.assets_config")

	if jsonConfig == "" {
		return nil, nil
	}

	jsonConfig = strings.TrimSpace(jsonConfig)
	if jsonConfig == "" {
		return nil, nil
	}

	var rawConfig map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonConfig), &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse ATTESTOR_GUARDIAN_ASSETS_CONFIG JSON: %w\n", err)
	}

	if len(rawConfig) == 0 {
		return nil, nil
	}

	assetConfig := make(map[string]GuardianParams)

	// Parse each asset's configuration
	for symbol, rawParams := range rawConfig {
		// Validate symbol is not empty
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			return nil, fmt.Errorf("invalid ATTESTOR_GUARDIAN_ASSETS_CONFIG: empty symbol key found")
		}

		var params GuardianParams
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return nil, fmt.Errorf("failed to parse guardian parameters for symbol %q: %w\n"+
				"Please ensure the parameters are valid. Expected format:\n"+
				`{"max_deviation_bips": int, "max_timestamp_age": int, "min_guardian_matches": int}`, symbol, err)
		}

		assetConfig[symbol] = params
	}

	return assetConfig, nil
}

type Config struct {
	RPC struct {
		URL          string   `mapstructure:"url"`
		URLs         []string `mapstructure:"urls"`
		RegistryURL  string   `mapstructure:"registry_url"`
		RegistryURLs []string `mapstructure:"registry_urls"`
	} `mapstructure:"rpc"`

	Oracle struct {
		Address    string           `mapstructure:"address"`
		ClientType OracleClientType `mapstructure:"client_type"`
	} `mapstructure:"oracle"`

	Registry struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"registry"`

	Attestor struct {
		PrivateKey          string         `mapstructure:"private_key"`
		Symbols             []string       `mapstructure:"symbols"`
		PollingTime         time.Duration  `mapstructure:"polling_time"`
		BatchMode           bool           `mapstructure:"batch_mode"`
		Mode                AttestorMode   `mapstructure:"mode"`
		ReplicaBackupDelay  int            `mapstructure:"replica_backup_delay"`
		IntentType          string         `mapstructure:"intent_type"`
		IntentVersion       string         `mapstructure:"intent_version"`
		DeviationTrigger    bool           `mapstructure:"deviation_trigger"`
		DeviationThreshold  int            `mapstructure:"deviation_threshold"`
		ForceUpdateInterval time.Duration  `mapstructure:"force_update_interval"`
		UseOracleTimestamp  bool           `mapstructure:"use_oracle_timestamp"`
		Guardian            GuardianConfig `mapstructure:"guardian"`
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
	MaxDeviationBips   int `mapstructure:"max_deviation_bips" json:"max_deviation_bips"`
	MaxTimestampAge    int `mapstructure:"max_timestamp_age" json:"max_timestamp_age"`
	MinGuardianMatches int `mapstructure:"min_guardian_matches" json:"min_guardian_matches"`
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
	v.BindEnv("attestor.deviation_trigger", "ATTESTOR_ATTESTOR_DEVIATION_TRIGGER")
	v.BindEnv("attestor.deviation_threshold", "ATTESTOR_ATTESTOR_DEVIATION_THRESHOLD")
	v.BindEnv("attestor.force_update_interval", "ATTESTOR_ATTESTOR_FORCE_UPDATE_INTERVAL")
	v.BindEnv("attestor.use_oracle_timestamp", "ATTESTOR_ATTESTOR_USE_ORACLE_TIMESTAMP")
	v.BindEnv("oracle.client_type", "ATTESTOR_ORACLE_CLIENT_TYPE")
	v.BindEnv("attestor.guardian.default.max_deviation_bips", "ATTESTOR_GUARDIAN_MAX_DEVIATION_BIPS")
	v.BindEnv("attestor.guardian.default.max_timestamp_age", "ATTESTOR_GUARDIAN_MAX_TIMESTAMP_AGE")
	v.BindEnv("attestor.guardian.default.min_guardian_matches", "ATTESTOR_GUARDIAN_MIN_GUARDIAN_MATCHES")
	v.BindEnv("attestor.guardian.assets_config", "ATTESTOR_GUARDIAN_ASSETS_CONFIG")

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
	v.SetDefault("attestor.deviation_trigger", false)
	v.SetDefault("attestor.deviation_threshold", 50)
	v.SetDefault("attestor.force_update_interval", "0s")
	v.SetDefault("attestor.use_oracle_timestamp", false)
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

	assetGuardianFromJSON, err := parseAssetGuardianFromJSON(v)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ATTESTOR_GUARDIAN_ASSETS_CONFIG: %w", err)
	}

	if len(assetGuardianFromJSON) > 0 {
		if cfg.Attestor.Guardian.Symbols == nil {
			cfg.Attestor.Guardian.Symbols = make(map[string]GuardianParams)
		}

		logger.Info("Loading per-asset guardian configuration from ATTESTOR_GUARDIAN_ASSETS_CONFIG")

		for symbol, params := range assetGuardianFromJSON {
			cfg.Attestor.Guardian.Symbols[symbol] = params

			logger.WithFields(map[string]interface{}{
				"symbol":               symbol,
				"max_deviation_bips":   params.MaxDeviationBips,
				"max_timestamp_age":    params.MaxTimestampAge,
				"min_guardian_matches": params.MinGuardianMatches,
			}).Info("Loaded per-asset guardian configuration")
		}
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

	if cfg.Attestor.DeviationThreshold < 0 || cfg.Attestor.DeviationThreshold > 10000 {
		return fmt.Errorf("invalid deviation_threshold: %d (must be between 0 and 10000)", cfg.Attestor.DeviationThreshold)
	}

	if cfg.Attestor.ForceUpdateInterval < 0 {
		return fmt.Errorf("invalid force_update_interval: %v (must be zero or positive)", cfg.Attestor.ForceUpdateInterval)
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
