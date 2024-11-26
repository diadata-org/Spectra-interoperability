package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Configuration struct {
	PrivateKey           string
	OracleTriggerAddress string
	RPCURL               string
	DestinationChains    []string
	SupportedAssets      []string
	DeviationPermille    int64
}

func LoadConfiguration() (*Configuration, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf("Error loading .env file: %v", err)
	}

	privateKey := getEnv("PRIVATE_KEY", "")
	if privateKey == "" {
		return nil, fmt.Errorf("PRIVATE_KEY environment variable not set")
	}

	deviationPermille, err := strconv.ParseInt(getEnv("DEVIATION_PERMILLE", "50"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DEVIATION_PERMILLE: %w", err)
	}

	return &Configuration{
		PrivateKey:           privateKey,
		OracleTriggerAddress: getEnv("ORACLE_TRIGGER_ADDRESS", "0x252Cd6aEe2E776f6B80d92DB360e8D9716eA25Bc"),
		RPCURL:               getEnv("DIA_RPC", "https://rpc-static-violet-vicuna-qhcog2uell.t.conduit.xyz"),
		DestinationChains:    strings.Split(getEnv("DESTINATION_CHAINS", "43113"), ","),
		SupportedAssets:      strings.Split(getEnv("SUPPORTED_ASSETS", "BTC/USD,ETH/USD"), ","),
		DeviationPermille:    deviationPermille,
	}, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
