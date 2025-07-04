package utils

import (
	"log"
	"os"
)

var DebugMode bool

// DebugLog logs a message if debug mode is enabled
func DebugLog(format string, v ...interface{}) {
	if DebugMode {
		log.Printf("DEBUG: "+format, v...)
	}
}

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
SIGNED_ORACLE_ADDRESS=
PRIVATE_KEY=
SYMBOL=BTC/USD
POLLING_TIME=60
DEBUG=false

# L2 Chain Configuration for Cross-Chain Intent System
L2_RPC_URL=https://testnet-rpc.diadata.org
L2_INTENT_CONTRACT=0x30c0A25a54e156487f8FF2F5836c5150A2828632

# OracleIntentConsumer Configuration
CONSUMER_ADDRESS=
`
	return os.WriteFile(".env.example", []byte(envContent), 0644)
}
