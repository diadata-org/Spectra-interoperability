package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModularLoader struct {
	baseDir string
}

func NewModularLoader(baseDir string) *ModularLoader {
	return &ModularLoader{
		baseDir: baseDir,
	}
}

func (ml *ModularLoader) LoadModular(files ConfigurationFiles) (*ModularConfig, error) {
	config := &ModularConfig{
		Chains:    make(map[string]*ChainConfig),
		Contracts: make(map[string]*ContractConfig),
		Events:    make(map[string]*EventDefinition),
		Routers:   make(map[string]*RouterConfig),
	}

	if files.Infrastructure != "" {
		if err := ml.loadInfrastructure(files.Infrastructure, config); err != nil {
			return nil, fmt.Errorf("failed to load infrastructure config: %w", err)
		}
	}

	if files.Chains != "" {
		if err := ml.loadChains(files.Chains, config); err != nil {
			return nil, fmt.Errorf("failed to load chains config: %w", err)
		}
	}

	if files.Contracts != "" {
		if err := ml.loadContracts(files.Contracts, config); err != nil {
			return nil, fmt.Errorf("failed to load contracts config: %w", err)
		}
	}

	if files.Events != "" {
		if err := ml.loadEvents(files.Events, config); err != nil {
			return nil, fmt.Errorf("failed to load events config: %w", err)
		}
	}

	if err := ml.loadRouters(files.Routers, config); err != nil {
		return nil, fmt.Errorf("failed to load router configs: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

func (ml *ModularLoader) LoadFromDirectory() (*ModularConfig, error) {
	files := DefaultConfigurationFiles()

	absBaseDir, err := filepath.Abs(ml.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for base directory: %w", err)
	}

	files.Infrastructure = filepath.Join(absBaseDir, files.Infrastructure)
	files.Chains = filepath.Join(absBaseDir, files.Chains)
	files.Contracts = filepath.Join(absBaseDir, files.Contracts)
	files.Events = filepath.Join(absBaseDir, files.Events)

	routerPattern := filepath.Join(absBaseDir, "routers", "*.yaml")
	routerFiles, err := filepath.Glob(routerPattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find router files: %w", err)
	}
	files.Routers = routerFiles

	return ml.LoadModular(files)
}

func (ml *ModularLoader) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(ml.baseDir, path)
}

func (ml *ModularLoader) loadInfrastructure(path string, config *ModularConfig) error {
	data, err := ml.readFile(path)
	if err != nil {
		return err
	}

	var infraConfig InfrastructureConfig
	if err := ml.unmarshal(data, &infraConfig); err != nil {
		return fmt.Errorf("failed to unmarshal infrastructure config: %w", err)
	}

	// Resolve environment variables
	if err := ml.resolveInfrastructureEnvVars(&infraConfig); err != nil {
		return fmt.Errorf("failed to resolve environment variables: %w", err)
	}

	config.Infrastructure = &infraConfig
	return nil
}

func (ml *ModularLoader) loadChains(path string, config *ModularConfig) error {
	data, err := ml.readFile(path)
	if err != nil {
		return err
	}

	var chainsData struct {
		Chains map[string]*ChainConfig `yaml:"chains" json:"chains"`
	}
	if err := ml.unmarshal(data, &chainsData); err != nil {
		return fmt.Errorf("failed to unmarshal chains config: %w", err)
	}

	for chainID, chain := range chainsData.Chains {
		// Resolve environment variables in RPC URLs
		ml.resolveStringArrayEnvVars(&chain.RPCURLs)
		config.Chains[chainID] = chain
	}
	return nil
}

func (ml *ModularLoader) loadContracts(path string, config *ModularConfig) error {
	data, err := ml.readFile(path)
	if err != nil {
		return err
	}

	var contractsData struct {
		Contracts map[string]*ContractConfig `yaml:"contracts" json:"contracts"`
	}
	if err := ml.unmarshal(data, &contractsData); err != nil {
		return fmt.Errorf("failed to unmarshal contracts config: %w", err)
	}

	for contractName, contract := range contractsData.Contracts {
		config.Contracts[contractName] = contract
	}
	return nil
}

func (ml *ModularLoader) loadEvents(path string, config *ModularConfig) error {
	data, err := ml.readFile(path)
	if err != nil {
		return err
	}

	var eventsData struct {
		Events map[string]*EventDefinition `yaml:"event_definitions" json:"event_definitions"`
	}
	if err := ml.unmarshal(data, &eventsData); err != nil {
		return fmt.Errorf("failed to unmarshal events config: %w", err)
	}

	for eventName, event := range eventsData.Events {
		config.Events[eventName] = event
	}
	return nil
}

func (ml *ModularLoader) loadRouters(routerPaths []string, config *ModularConfig) error {
	for _, routerPath := range routerPaths {
		if strings.Contains(routerPath, "*") {
			matches, err := filepath.Glob(routerPath)
			if err != nil {
				return fmt.Errorf("failed to expand router path pattern %s: %w", routerPath, err)
			}
			for _, match := range matches {
				if err := ml.loadSingleRouter(match, config); err != nil {
					return err
				}
			}
		} else {
			if err := ml.loadSingleRouter(routerPath, config); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ml *ModularLoader) loadSingleRouter(path string, config *ModularConfig) error {
	data, err := ml.readFile(path)
	if err != nil {
		return err
	}

	var routerData struct {
		Router *RouterConfig `yaml:"router" json:"router"`
	}
	if err := ml.unmarshal(data, &routerData); err != nil {
		return fmt.Errorf("failed to unmarshal router config from %s: %w", path, err)
	}

	if routerData.Router == nil {
		return fmt.Errorf("no router configuration found in %s", path)
	}

	if routerData.Router.ID == "" {
		routerData.Router.ID = ml.getRouterIDFromPath(path)
	}

	// Resolve environment variables for router private key
	ml.resolveStringEnvVar(&routerData.Router.PrivateKey, routerData.Router.PrivateKeyEnv)

	config.Routers[routerData.Router.ID] = routerData.Router
	return nil
}

func (ml *ModularLoader) getRouterIDFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func (ml *ModularLoader) readFile(path string) ([]byte, error) {
	var finalPath string
	if filepath.IsAbs(path) {
		finalPath = path
	} else {
		finalPath = ml.resolvePath(path)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configuration file not found: %s", finalPath)
		}
		return nil, fmt.Errorf("failed to read file %s: %w", finalPath, err)
	}
	return data, nil
}

func (ml *ModularLoader) unmarshal(data []byte, v interface{}) error {
	if err := yaml.Unmarshal(data, v); err != nil {
		if jsonErr := json.Unmarshal(data, v); jsonErr != nil {
			return fmt.Errorf("failed to unmarshal as YAML: %w", err)
		}
	}
	return nil
}

// resolveInfrastructureEnvVars resolves environment variables in infrastructure config
func (ml *ModularLoader) resolveInfrastructureEnvVars(infra *InfrastructureConfig) error {
	// Resolve private key from env var
	ml.resolveStringEnvVar(&infra.PrivateKey, infra.PrivateKeyEnv)

	// Resolve database DSN from env var
	ml.resolveStringEnvVar(&infra.Database.DSN, infra.Database.DSNEnv)

	// Resolve source chain RPC URLs
	ml.resolveStringArrayEnvVars(&infra.Source.RPCURLs)

	return nil
}

// resolveStringEnvVar resolves a single string field from environment variable
// If envVarName is set, it reads from os.Getenv and populates the target field
func (ml *ModularLoader) resolveStringEnvVar(target *string, envVarName string) {
	if envVarName != "" {
		if envValue := os.Getenv(envVarName); envValue != "" {
			*target = envValue
		}
	}
}

// resolveStringArrayEnvVars resolves environment variables in a string array
// Elements prefixed with "env:" are replaced with the value from environment variable
func (ml *ModularLoader) resolveStringArrayEnvVars(arr *[]string) {
	if arr == nil {
		return
	}

	for i, val := range *arr {
		if strings.HasPrefix(val, "env:") {
			envVarName := strings.TrimPrefix(val, "env:")
			if envValue := os.Getenv(envVarName); envValue != "" {
				(*arr)[i] = envValue
			}
		}
	}
}

// LoadConfig loads modular configuration from a path (file or directory)
func LoadConfig(configPath string) (*ModularConfig, error) {
	if configPath == "" {
		configPath = "."
	}

	// Check if path exists
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("config path not found: %s", configPath)
	}

	loader := NewModularLoader(configPath)

	if info.IsDir() {
		// Directory - load from default file structure
		return loader.LoadFromDirectory()
	}

	// Single file - must be a complete modular config YAML
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ModularConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}
