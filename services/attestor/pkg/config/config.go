package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	RPC struct {
		URL          string `mapstructure:"url"`
		RegistryURL  string `mapstructure:"registry_url"`
	} `mapstructure:"rpc"`

	Oracle struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"oracle"`

	Registry struct {
		Address string `mapstructure:"address"`
	} `mapstructure:"registry"`

	Attestor struct {
		PrivateKey   string        `mapstructure:"private_key"`
		Symbols      []string      `mapstructure:"symbols"`
		PollingTime  time.Duration `mapstructure:"polling_time"`
		BatchMode    bool          `mapstructure:"batch_mode"`
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
	v.SetDefault("logging.level", "info")
	v.SetDefault("metrics.port", 8080)
	v.SetDefault("api.port", 8081)

	// Bind environment variables for backward compatibility
	v.BindEnv("rpc.url", "RPC_URL")
	v.BindEnv("oracle.address", "ORACLE_ADDRESS")
	v.BindEnv("registry.address", "INTENT_REGISTRY_ADDRESS")
	v.BindEnv("attestor.private_key", "PRIVATE_KEY")
	v.BindEnv("attestor.symbols", "SYMBOLS")
	v.BindEnv("attestor.polling_time", "POLLING_TIME")
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

	return cfg, nil
}

func Get() *Config {
	if cfg == nil {
		panic("config not initialized")
	}
	return cfg
}

