package bridge

import (
	"context"
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/scanner"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// CreateBlockScanner creates the enhanced block scanner
func CreateBlockScanner(
	cfgService *config.ConfigService,
	client rpc.EthClient,
	db *database.DB,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (BlockScanner, error) {
	// Always use enhanced scanner for all scenarios
	infrastructure := cfgService.GetInfrastructure()
	if !infrastructure.BlockScanner.Enabled {
		return nil, fmt.Errorf("block scanner is disabled")
	}

	// Create the database adapter
	dbAdapter := scanner.NewDatabaseAdapter(db)

	// Get the underlying ethclient for scanner
	ethClient, err := client.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	// Collect all event definitions
	eventDefinitions := make(map[string]*config.EventDefinition)
	for name, eventDef := range cfgService.GetEventDefinitions() {
		eventDefinitions[name] = eventDef
	}

	// Create enhanced scanner
	enhancedScanner, err := scanner.NewEnhancedBlockScanner(
		&infrastructure.BlockScanner,
		&infrastructure.Source,
		eventDefinitions,
		ethClient,
		dbAdapter,
		eventChan,
		errorChan,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create enhanced block scanner: %w", err)
	}
	return &enhancedScannerAdapter{enhancedScanner}, nil
}

// BlockScanner interface for enhanced scanner
type BlockScanner interface {
	Start(ctx context.Context) error
	Stop() error
	GetStats() *bridgeTypes.ScannerStats
}

// Adapter for enhanced scanner
type enhancedScannerAdapter struct {
	*scanner.EnhancedBlockScanner
}
