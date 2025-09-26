package utils

import (
	"os"
)

// GetEnv gets an environment variable or returns a default value
func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// CreateEnvTemplate creates a .env file template
func CreateEnvTemplate() error {
	envContent := `# Oracle Attestor Configuration
RPC_URL=https://testnet-rpc.diadata.org
ORACLE_ADDRESS=0x0087342f5f4c7AB23a37c045c3EF710749527c88
PRIVATE_KEY=
SYMBOLS=BTC/USD,ETH/USD
POLLING_TIME=0.3
INTENT_TYPE=OracleUpdate
INTENT_VERSION=1.0

# L2 Chain Configuration
L2_RPC_URL=https://testnet-rpc.diadata.org
INTENT_REGISTRY_ADDRESS=
`
	return os.WriteFile(".env.example", []byte(envContent), 0644)
}
