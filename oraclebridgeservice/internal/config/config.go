package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type ReceiverTarget struct {
	ChainID uint32
	Address string
}

type Configuration struct {
	PrivateKey           string
	OracleTriggerAddress string
	RPCURL               string
	SupportedAssets      []string
	DeviationPermille    int64
	IntentType           string
	MetadataAddress      string
	Receivers            []ReceiverTarget
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

	receivers, err := parseReceivers(getEnv("RECEIVER_ADDRESS", ""))
	if err != nil {
		return nil, err
	}

	return &Configuration{
		PrivateKey:           privateKey,
		OracleTriggerAddress: getEnv("ORACLE_TRIGGER_ADDRESS", "0x252Cd6aEe2E776f6B80d92DB360e8D9716eA25Bc"),
		RPCURL:               getEnv("DIA_RPC", "https://rpc-static-violet-vicuna-qhcog2uell.t.conduit.xyz"),
		SupportedAssets:      strings.Split(getEnv("SUPPORTED_ASSETS", "BTC/USD,ETH/USD"), ","),
		DeviationPermille:    deviationPermille,
		IntentType:           getEnv("INTENT_TYPE", "OracleUpdate"),
		MetadataAddress:      getEnv("METADATA_ADDRESS", "0x0087342f5f4c7AB23a37c045c3EF710749527c88"),
		Receivers:            receivers,
	}, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func parseReceivers(raw string) ([]ReceiverTarget, error) {
	if raw == "" {
		return nil, fmt.Errorf("RECEIVER_ADDRESS environment variable must be set")
	}

	parts := strings.Split(raw, ",")
	receivers := make([]ReceiverTarget, 0, len(parts))

	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}

		segments := strings.SplitN(token, "-", 2)
		if len(segments) != 2 {
			return nil, fmt.Errorf("invalid receiver entry %q, expected format <chainId>-<address>", token)
		}

		chainIDStr := strings.TrimSpace(segments[0])
		addr := strings.TrimSpace(segments[1])

		if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
			return nil, fmt.Errorf("invalid receiver address %q", addr)
		}

		chainIDUint, err := strconv.ParseUint(chainIDStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid chain id %q: %w", chainIDStr, err)
		}

		receivers = append(receivers, ReceiverTarget{
			ChainID: uint32(chainIDUint),
			Address: addr,
		})
	}

	if len(receivers) == 0 {
		return nil, fmt.Errorf("no valid receiver entries found in RECEIVER_ADDRESS")
	}

	return receivers, nil
}
