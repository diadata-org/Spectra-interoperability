package bridge

import (
	"context"
	"fmt"


	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/scanner"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// CreateBlockScanner creates the appropriate block scanner based on configuration
func CreateBlockScanner(
	cfg *config.Config,
	client rpc.EthClient,
	db *database.DB,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (BlockScanner, error) {
	// Check if backward sync is enabled via configuration
	useEnhancedScanner := cfg.BlockScanner.Enabled && cfg.BlockScanner.BackwardSync
	
	if useEnhancedScanner {
		// Get the underlying ethclient for scanner
		ethClient, err := client.GetClient()
		if err != nil {
			return nil, fmt.Errorf("failed to get eth client: %w", err)
		}
		
		// Create enhanced scanner with backward sync
		enhancedScanner, err := scanner.NewEnhancedBlockScanner(
			&cfg.BlockScanner,
			&cfg.Source,
			ethClient,
			db,
			eventChan,
			errorChan,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create enhanced block scanner: %w", err)
		}
		return &enhancedScannerAdapter{enhancedScanner}, nil
	}
	
	// Get the underlying ethclient for scanner
	ethClient, err := client.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}
	
	// Create standard scanner
	standardScanner, err := scanner.NewBlockScanner(
		&cfg.BlockScanner,
		&cfg.Source,
		ethClient,
		db,
		eventChan,
		errorChan,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create block scanner: %w", err)
	}
	return &standardScannerAdapter{standardScanner}, nil
}

// BlockScanner interface for both scanner types
type BlockScanner interface {
	Start(ctx context.Context) error
	Stop() error
	GetStats() *bridgeTypes.ScannerStats
}

// Adapter for standard scanner
type standardScannerAdapter struct {
	*scanner.BlockScanner
}

// Adapter for enhanced scanner
type enhancedScannerAdapter struct {
	*scanner.EnhancedBlockScanner
}