package main

import (
	"fmt"
	"os"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}

	configFile := os.Args[1]
	
	fmt.Printf("Validating configuration file: %s\n", configFile)
	
	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Printf("❌ Configuration validation failed: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Configuration is valid!\n")
	fmt.Printf("\nSource chain: %s (Chain ID: %d)\n", cfg.Source.Name, cfg.Source.ChainID)
	fmt.Printf("RPC URL: %s\n", cfg.Source.RPCURL)
	fmt.Printf("Start block: %d\n", cfg.Source.StartBlock)
	
	// Print registry address if available
	if registryContract, ok := cfg.Source.Contracts["registry"]; ok {
		if addr, ok := registryContract["address"].(string); ok {
			fmt.Printf("Registry address: %s\n", addr)
		}
	}
	
	fmt.Printf("\nEvent filters:\n")
	fmt.Printf("  - Symbols: %v\n", cfg.Source.EventFilters.Symbols)
	fmt.Printf("  - Max age: %d seconds\n", cfg.Source.EventFilters.MaxAge)
	
	fmt.Printf("\nEnabled destinations: %d\n", len(cfg.GetEnabledDestinations()))
	
	for _, dest := range cfg.GetEnabledDestinations() {
		fmt.Printf("\n  - %s (Chain ID: %d)\n", dest.Name, dest.ChainID)
		fmt.Printf("    RPC URL: %s\n", dest.RPCURL)
		fmt.Printf("    Contracts: %d\n", len(dest.Contracts))
		
		for _, contract := range dest.Contracts {
			if !contract.Enabled {
				continue
			}
			fmt.Printf("      - %s (%s)\n", contract.Name, contract.Type)
			fmt.Printf("        Address: %s\n", contract.Address)
			fmt.Printf("        Supported symbols: %v\n", contract.SupportedSymbols)
			fmt.Printf("        Update interval: %s\n", contract.MinUpdateInterval)
			fmt.Printf("        Gas limit: %d\n", contract.GasLimit)
		}
	}
	
	fmt.Printf("\nWorker pool settings:\n")
	fmt.Printf("  - Max workers: %d\n", cfg.WorkerPool.MaxWorkers)
	fmt.Printf("  - Task queue size: %d\n", cfg.WorkerPool.TaskQueueSize)
	fmt.Printf("  - Task timeout: %s\n", cfg.WorkerPool.TaskTimeout)
	
	fmt.Printf("\nBlock scanner settings:\n")
	fmt.Printf("  - Enabled: %v\n", cfg.BlockScanner.Enabled)
	fmt.Printf("  - Scan interval: %s\n", cfg.BlockScanner.ScanInterval)
	fmt.Printf("  - Block range: %d\n", cfg.BlockScanner.BlockRange)
	
	fmt.Printf("\nEvent processor settings:\n")
	fmt.Printf("  - Batch size: %d\n", cfg.EventProcessor.BatchSize)
	fmt.Printf("  - Validation timeout: %s\n", cfg.EventProcessor.ValidationTimeout)
	fmt.Printf("  - Dedup cache size: %d\n", cfg.EventProcessor.DedupCacheSize)
	
	if cfg.API.Enabled {
		fmt.Printf("\nAPI server:\n")
		fmt.Printf("  - Listen address: %s\n", cfg.API.ListenAddr)
		fmt.Printf("  - CORS enabled: %v\n", cfg.API.EnableCORS)
	}
	
	if cfg.Metrics.Enabled {
		fmt.Printf("\nMetrics:\n")
		fmt.Printf("  - Namespace: %s\n", cfg.Metrics.Namespace)
	}
	
	fmt.Printf("\nDry run mode: %v\n", cfg.DryRun)
}