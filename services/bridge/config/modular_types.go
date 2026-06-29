package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

func init() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

type ModularConfig struct {
	Infrastructure *InfrastructureConfig       `yaml:"infrastructure,omitempty" json:"infrastructure,omitempty"`
	Chains         map[string]*ChainConfig     `yaml:"chains,omitempty" json:"chains,omitempty"`
	Contracts      map[string]*ContractConfig  `yaml:"contracts,omitempty" json:"contracts,omitempty"`
	Events         map[string]*EventDefinition `yaml:"event_definitions,omitempty" json:"event_definitions,omitempty"`
	Routers        map[string]*RouterConfig    `yaml:"routers,omitempty" json:"routers,omitempty"`
}

type InfrastructureConfig struct {
	Database       DatabaseConfig       `yaml:"database" json:"database"`
	Source         SourceConfig         `yaml:"source" json:"source"`
	PrivateKey     string               `yaml:"private_key,omitempty" json:"private_key,omitempty"`
	PrivateKeyEnv  string               `yaml:"private_key_env,omitempty" json:"private_key_env,omitempty"`
	EventMonitor   EventMonitorConfig   `yaml:"event_monitor" json:"event_monitor"`
	BlockScanner   BlockScannerConfig   `yaml:"block_scanner" json:"block_scanner"`
	EventProcessor EventProcessorConfig `yaml:"event_processor" json:"event_processor"`
	WorkerPool     WorkerPoolConfig     `yaml:"worker_pool" json:"worker_pool"`
	HealthCheck    HealthCheckConfig    `yaml:"health_check" json:"health_check"`
	Recovery       RecoveryConfig       `yaml:"recovery" json:"recovery"`
	API            APIConfig            `yaml:"api" json:"api"`
	Metrics        MetricsConfig        `yaml:"metrics" json:"metrics"`
	Replica        *ReplicaConfig       `yaml:"replica,omitempty" json:"replica,omitempty"`
	DryRun         bool                 `yaml:"dry_run,omitempty" json:"dry_run,omitempty"`
}

type ChainConfig struct {
	ChainID         int64    `yaml:"chain_id" json:"chain_id"`
	Name            string   `yaml:"name" json:"name"`
	RPCURLs         []string `yaml:"rpc_urls" json:"rpc_urls"`
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	DefaultGasLimit uint64   `yaml:"default_gas_limit,omitempty" json:"default_gas_limit,omitempty"`
	GasMultiplier   float64  `yaml:"gas_multiplier,omitempty" json:"gas_multiplier,omitempty"`
	MaxGasPrice     string   `yaml:"max_gas_price,omitempty" json:"max_gas_price,omitempty"`
	// Kind discriminates the chain backend. Empty string or "evm" selects the
	// default EVM path. "arch" selects the Arch Network path.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

type ContractConfig struct {
	// Core identification
	Name    string `yaml:"name,omitempty" json:"name,omitempty"` // For legacy compatibility
	ChainID int64  `yaml:"chain_id" json:"chain_id"`             // For modular system
	Address string `yaml:"address" json:"address"`
	Type    string `yaml:"type" json:"type"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	ABI     string `yaml:"abi" json:"abi"`

	// Gas configuration
	GasLimit      uint64  `yaml:"gas_limit,omitempty" json:"gas_limit,omitempty"`
	GasMultiplier float64 `yaml:"gas_multiplier,omitempty" json:"gas_multiplier,omitempty"`
	MaxGasPrice   string  `yaml:"max_gas_price,omitempty" json:"max_gas_price,omitempty"`

	// Method configuration
	Methods map[string]MethodConfig `yaml:"methods,omitempty" json:"methods,omitempty"`

	// FeeHookProgramID is the hex-encoded 32-byte program ID of the fee-hook
	// program on Arch Network. Required when the chain Kind is "arch".
	FeeHookProgramID string `yaml:"fee_hook_program_id,omitempty" json:"fee_hook_program_id,omitempty"`
}

type RouterConfig struct {
	ID            string              `yaml:"id" json:"id"`
	Name          string              `yaml:"name" json:"name"`
	Type          string              `yaml:"type" json:"type"`
	Enabled       bool                `yaml:"enabled" json:"enabled"`
	PrivateKey    string              `yaml:"private_key,omitempty" json:"private_key,omitempty"`
	PrivateKeyEnv string              `yaml:"private_key_env,omitempty" json:"private_key_env,omitempty"`
	Triggers      RouterTriggers      `yaml:"triggers" json:"triggers"`
	Processing    ProcessingConfig    `yaml:"processing" json:"processing"`
	Destinations  []RouterDestination `yaml:"destinations" json:"destinations"`
}

type RouterDestination struct {
	// Legacy fields (direct contract reference)
	ChainID  int64  `yaml:"chain_id,omitempty" json:"chain_id,omitempty"`
	Contract string `yaml:"contract,omitempty" json:"contract,omitempty"`

	// Modular fields (contract reference by name)
	ContractRef string `yaml:"contract_ref,omitempty" json:"contract_ref,omitempty"`

	// Common fields
	Method         DestinationMethodConfig `yaml:"method" json:"method"`
	Condition      string                  `yaml:"condition,omitempty" json:"condition,omitempty"`
	TimeThreshold  Duration                `yaml:"time_threshold,omitempty" json:"time_threshold,omitempty"`
	PriceDeviation string                  `yaml:"price_deviation,omitempty" json:"price_deviation,omitempty"` // e.g., "0.5%" or "1.0%"

	// Gas configuration (from modular version)
	GasLimit      uint64  `yaml:"gas_limit,omitempty" json:"gas_limit,omitempty"`
	GasMultiplier float64 `yaml:"gas_multiplier,omitempty" json:"gas_multiplier,omitempty"`
	MaxGasPrice   string  `yaml:"max_gas_price,omitempty" json:"max_gas_price,omitempty"`
}

type ConfigurationFiles struct {
	Infrastructure string   `yaml:"infrastructure"`
	Chains         string   `yaml:"chains"`
	Contracts      string   `yaml:"contracts"`
	Events         string   `yaml:"events"`
	Routers        []string `yaml:"routers"`
}

func DefaultConfigurationFiles() ConfigurationFiles {
	return ConfigurationFiles{
		Infrastructure: "infrastructure.yaml",
		Chains:         "chains.yaml",
		Contracts:      "contracts.yaml",
		Events:         "events.yaml",
		Routers:        []string{"routers/*.yaml"},
	}
}

func (mc *ModularConfig) Validate() error {
	if mc.Infrastructure == nil {
		return fmt.Errorf("infrastructure configuration is required")
	}

	if err := mc.validateInfrastructure(); err != nil {
		return fmt.Errorf("infrastructure validation failed: %w", err)
	}

	if len(mc.Chains) == 0 {
		return fmt.Errorf("at least one chain configuration is required")
	}

	for chainID, chain := range mc.Chains {
		if err := mc.validateChain(chainID, chain); err != nil {
			return fmt.Errorf("chain %s validation failed: %w", chainID, err)
		}
	}

	for contractName, contract := range mc.Contracts {
		if err := mc.validateContract(contractName, contract); err != nil {
			return fmt.Errorf("contract %s validation failed: %w", contractName, err)
		}
	}

	for routerID, router := range mc.Routers {
		if err := mc.validateRouter(routerID, router); err != nil {
			return fmt.Errorf("router %s validation failed: %w", routerID, err)
		}
	}

	return nil
}

func (mc *ModularConfig) validateInfrastructure() error {
	infra := mc.Infrastructure

	if infra.Source.ChainID == 0 {
		return fmt.Errorf("source chain_id is required")
	}
	if len(infra.Source.RPCURLs) == 0 {
		return fmt.Errorf("source rpc_urls is required")
	}

	if infra.EventMonitor.ReconnectInterval == 0 {
		infra.EventMonitor.ReconnectInterval = Duration(5 * time.Second)
	}
	if infra.BlockScanner.ScanInterval == 0 {
		infra.BlockScanner.ScanInterval = Duration(60 * time.Second)
	}
	if infra.WorkerPool.MaxWorkers == 0 {
		infra.WorkerPool.MaxWorkers = 10
	}

	return nil
}

func (mc *ModularConfig) validateChain(chainID string, chain *ChainConfig) error {
	if chain.ChainID == 0 {
		return fmt.Errorf("chain_id is required")
	}
	if len(chain.RPCURLs) == 0 {
		return fmt.Errorf("rpc_urls is required")
	}
	if chain.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func (mc *ModularConfig) validateContract(contractName string, contract *ContractConfig) error {
	if contract.ChainID == 0 {
		return fmt.Errorf("chain_id is required")
	}
	if contract.Address == "" {
		return fmt.Errorf("address is required")
	}
	if contract.ABI == "" {
		return fmt.Errorf("abi is required")
	}

	chainExists := false
	for _, chain := range mc.Chains {
		if chain.ChainID == contract.ChainID {
			chainExists = true
			break
		}
	}
	if !chainExists {
		return fmt.Errorf("chain_id %d not found in chains configuration", contract.ChainID)
	}

	return nil
}

func (mc *ModularConfig) validateRouter(routerID string, router *RouterConfig) error {
	if router.ID == "" {
		router.ID = routerID
	}
	if router.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(router.Destinations) == 0 {
		return fmt.Errorf("at least one destination is required")
	}

	for i, dest := range router.Destinations {
		// Support both modular (contract_ref) and legacy (chain_id + contract) approaches
		if dest.ContractRef == "" && (dest.ChainID == 0 || dest.Contract == "") {
			return fmt.Errorf("destination[%d] must specify either contract_ref or both chain_id and contract", i)
		}

		// If using contract_ref, validate it exists
		if dest.ContractRef != "" {
			if _, exists := mc.Contracts[dest.ContractRef]; !exists {
				return fmt.Errorf("destination[%d] contract_ref %s not found in contracts configuration", i, dest.ContractRef)
			}
		}
	}

	return nil
}

// ConfigService provides convenient access to modular configuration with reference resolution
type ConfigService struct {
	config *ModularConfig
}

// NewConfigService creates a new configuration service
func NewConfigService(config *ModularConfig) *ConfigService {
	return &ConfigService{
		config: config,
	}
}

// GetContractConfig returns contract configuration by name
func (cs *ConfigService) GetContractConfig(name string) *ContractConfig {
	return cs.config.Contracts[name]
}

// GetInfrastructure returns infrastructure configuration
func (cs *ConfigService) GetInfrastructure() *InfrastructureConfig {
	return cs.config.Infrastructure
}

// GetEventDefinitions returns all event definitions
func (cs *ConfigService) GetEventDefinitions() map[string]*EventDefinition {
	return cs.config.Events
}

// GetEnabledChains returns only enabled chain configurations
func (cs *ConfigService) GetEnabledChains() []*ChainConfig {
	var enabled []*ChainConfig
	for _, chain := range cs.config.Chains {
		if chain.Enabled {
			enabled = append(enabled, chain)
		}
	}
	return enabled
}

// GetContractsForChain returns all enabled contracts for a specific chain
func (cs *ConfigService) GetContractsForChain(chainID int64) []*ContractConfig {
	var contracts []*ContractConfig
	for _, contract := range cs.config.Contracts {
		if contract.ChainID == chainID && contract.Enabled {
			contracts = append(contracts, contract)
		}
	}
	return contracts
}

// GetEnabledRouters returns only enabled router configurations
func (cs *ConfigService) GetEnabledRouters() []*RouterConfig {
	var enabled []*RouterConfig
	for _, router := range cs.config.Routers {
		if router.Enabled {
			enabled = append(enabled, router)
		}
	}
	return enabled
}

// ReplicaConfig configures replica monitoring and failover
type ReplicaConfig struct {
	Enabled              bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	TimeThresholdOffset  Duration `yaml:"time_threshold_offset,omitempty" json:"time_threshold_offset,omitempty"`
	PriceDeviationOffset string   `yaml:"price_deviation_offset,omitempty" json:"price_deviation_offset,omitempty"`
	CheckInterval        Duration `yaml:"check_interval,omitempty" json:"check_interval,omitempty"`
}

func (rc *ReplicaConfig) ApplyEnvOverrides() {
	if viper.IsSet("replica_enabled") {
		envEnabled := strings.TrimSpace(viper.GetString("replica_enabled"))
		if envEnabled == "" {
			return
		}
		oldValue := rc.Enabled
		rc.Enabled = strings.ToLower(envEnabled) == "true" || envEnabled == "1"
		if oldValue != rc.Enabled {
			fmt.Printf("Replica enabled overridden from environment variable REPLICA_ENABLED=%q (was: %v, now: %v)\n", envEnabled, oldValue, rc.Enabled)
		}
	}
}
