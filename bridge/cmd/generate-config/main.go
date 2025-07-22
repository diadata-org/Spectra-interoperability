package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

func main() {
	// Create a sample configuration
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			DSN:    "bridge.db",
		},
		Source: config.SourceConfig{
			Name:       "DIA Testnet",
			ChainID:    100640,
			RPCURL:     "https://rpc-dia-lasernet-dipfsyyx2w.t.conduit.xyz",
			WsURL:      "wss://rpc-dia-lasernet-dipfsyyx2w.t.conduit.xyz",
			StartBlock: 15000000,
			Contracts: map[string]map[string]interface{}{
				"registry": {
					"address": "0xd2313dcabB0E9447d800546b953E05dD47EB2eB9",
					"abi":     "OracleIntentRegistry.json",
				},
			},
			EventFilters: config.EventFilters{
				Symbols:  []string{"ETH/USD", "BTC/USD", "USDC/USD"},
				Signers:  []string{},
				MinPrice: "0",
				MaxPrice: "1000000000000000000000", // 1M USD
				MaxAge:   3600,                      // 1 hour
			},
		},
		Destinations: []*config.DestinationConfig{
			{
				Name:    "OP Sepolia",
				ChainID: 11155420,
				RPCURL:  "https://sepolia.optimism.io",
				Enabled: true,
				Contracts: []config.ContractConfig{
					{
						Name:              "PushOracleReceiver",
						Address:           "0xF359f17Fc18f7d7c3Ed6b2FAAdbE66ec0c7894de",
						Type:              "receiver",
						Enabled:           true,
						SupportedSymbols:  []string{"ETH/USD", "BTC/USD", "USDC/USD"},
						Priority:          1,
						MinUpdateInterval: 30 * time.Second,
						MaxPriceDeviation: 0.05, // 5%
						GasLimit:          500000,
						GasMultiplier:     1.1,
						MaxGasPrice:       "50000000000", // 50 gwei
						ABI:               "PushOracleReceiver.json",
						Methods: map[string]config.MethodConfig{
							"updatePrice": {
								MethodName: "handleIntentUpdate",
								FieldsMapping: map[string]string{
									"symbol":    "symbol",
									"price":     "price",
									"timestamp": "timestamp",
								},
								GasLimit: 300000,
							},
						},
					},
				},
			},
		},
		PrivateKey: "YOUR_PRIVATE_KEY_HERE",
		EventMonitor: config.EventMonitorConfig{
			Enabled:              true,
			ReconnectInterval:    10 * time.Second,
			MaxReconnectAttempts: 5,
		},
		BlockScanner: config.BlockScannerConfig{
			Enabled:      true,
			ScanInterval: 10 * time.Second,
			BlockRange:   100,
			MaxBlockGap:  1000,
		},
		EventProcessor: config.EventProcessorConfig{
			BatchSize:         100,
			ValidationTimeout: 5 * time.Second,
			DedupCacheSize:    10000,
			DedupCacheTTL:     1 * time.Hour,
		},
		WorkerPool: config.WorkerPoolConfig{
			MaxWorkers:    5,
			TaskQueueSize: 1000,
			TaskTimeout:   2 * time.Minute,
			RetryDelay:    5 * time.Second,
			MaxRetries:    3,
		},
		HealthCheck: config.HealthCheckConfig{
			Enabled:          true,
			CheckInterval:    1 * time.Minute,
			Timeout:          10 * time.Second,
			MaxProcessingLag: 5 * time.Minute,
			MaxQueueSize:     500,
		},
		Recovery: config.RecoveryConfig{
			Enabled:         true,
			MinFailures:     3,
			MaxAttempts:     10,
			RetryInterval:   30 * time.Second,
			RecoveryTimeout: 5 * time.Minute,
		},
		API: config.APIConfig{
			Enabled:    true,
			ListenAddr: ":8080",
			EnableCORS: true,
		},
		Metrics: config.MetricsConfig{
			Enabled:   true,
			Namespace: "bridge",
		},
		DryRun: false,
	}

	// Marshal to JSON with pretty formatting
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling config: %v\n", err)
		os.Exit(1)
	}

	// Write to file or stdout
	if len(os.Args) > 1 {
		filename := os.Args[1]
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			fmt.Printf("Error writing config file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Configuration written to %s\n", filename)
	} else {
		fmt.Println(string(data))
	}
}