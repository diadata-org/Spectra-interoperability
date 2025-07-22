package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
)

// MigrationReport tracks what was migrated
type MigrationReport struct {
	LegacyContracts int      `json:"legacy_contracts"`
	RoutersCreated  int      `json:"routers_created"`
	Warnings        []string `json:"warnings"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: migrate-config <config.json>")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	outputFile := inputFile + ".migrated.json"

	// Read config
	data, err := ioutil.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	// Perform migration
	report := migrateConfig(&cfg)

	// Write migrated config
	output, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling config: %v\n", err)
		os.Exit(1)
	}

	if err := ioutil.WriteFile(outputFile, output, 0644); err != nil {
		fmt.Printf("Error writing output: %v\n", err)
		os.Exit(1)
	}

	// Print report
	fmt.Println("\nMigration Report:")
	fmt.Printf("- Legacy contracts found: %d\n", report.LegacyContracts)
	fmt.Printf("- Routers created: %d\n", report.RoutersCreated)
	if len(report.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, warning := range report.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	fmt.Printf("\nMigrated config written to: %s\n", outputFile)
	fmt.Println("\nNOTE: The bridge will automatically create legacy routers for backward compatibility.")
	fmt.Println("You can optionally convert these to explicit routers for better clarity.")
}

func migrateConfig(cfg *config.Config) *MigrationReport {
	report := &MigrationReport{}

	// Check each destination for legacy routing configuration
	for _, dest := range cfg.Destinations {
		for i := range dest.Contracts {
			contract := &dest.Contracts[i]
			
			// Check if contract has legacy routing configuration
			if contract.MinUpdateInterval > 0 || contract.MaxPriceDeviation > 0 || len(contract.SupportedSymbols) > 0 {
				report.LegacyContracts++
				
				// Create explicit router (optional - the bridge will do this automatically)
				if contract.MinUpdateInterval > 0 && contract.MaxPriceDeviation > 0 {
					// Both time and deviation - suggest composite router
					report.Warnings = append(report.Warnings, 
						fmt.Sprintf("Contract %s has both time and deviation criteria. Consider creating a composite router.", contract.Name))
				}
				
				// Clear legacy fields (optional - they can remain for backward compatibility)
				// contract.MinUpdateInterval = 0
				// contract.MaxPriceDeviation = 0
				// contract.SupportedSymbols = nil
			}
		}
	}

	// Add example routers if none exist
	if len(cfg.Routers) == 0 && report.LegacyContracts > 0 {
		report.Warnings = append(report.Warnings, 
			"No explicit routers defined. Consider adding routers for better control and clarity.")
		
		// Add commented example
		cfg.Routers = []config.RouterConfig{
			{
				ID:      "example-time-router",
				Name:    "Example time-based router (uncomment and modify)",
				Type:    "time",
				Enabled: false,
				Filter: config.RouterFilter{
					Symbols: []string{"ETH/USD", "BTC/USD"},
				},
				Config: map[string]interface{}{
					"interval": "5m",
				},
				Destinations: []config.RouterDestination{
					{
						ChainID:   11155420,
						Contracts: []string{"0xYOUR_CONTRACT_ADDRESS"},
					},
				},
			},
		}
	}

	return report
}