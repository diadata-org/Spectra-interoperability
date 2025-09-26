package bridge

import (
	"context"
	"fmt"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/scanner"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/rpc"
)

// CreateBlockScanner creates the enhanced block scanner
func CreateBlockScanner(
	cfg *config.Config,
	client rpc.EthClient,
	db *database.DB,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (BlockScanner, error) {
	// Always use enhanced scanner for all scenarios
	if !cfg.BlockScanner.Enabled {
		return nil, fmt.Errorf("block scanner is disabled")
	}

	// Create the database adapter
	dbAdapter := scanner.NewDatabaseAdapter(db)

	// Get the underlying ethclient for scanner
	ethClient, err := client.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	// Create enhanced scanner
	enhancedScanner, err := scanner.NewEnhancedBlockScanner(
		&cfg.BlockScanner,
		&cfg.Source,
		cfg.EventDefinitions,
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
