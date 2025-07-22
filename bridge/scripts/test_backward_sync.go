package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/scanner"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

func main() {
	logger.Init(true, "info")
	
	// Load configuration
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Connect to database
	db, err := database.NewDB(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	
	// Connect to source chain
	client, err := ethclient.Dial(cfg.Source.RPCURL)
	if err != nil {
		log.Fatalf("Failed to connect to chain: %v", err)
	}
	
	// Create channels
	eventChan := make(chan *bridgeTypes.EventData, 100)
	errorChan := make(chan error, 10)
	
	// Get current block
	currentBlock, err := client.BlockNumber(context.Background())
	if err != nil {
		log.Fatalf("Failed to get current block: %v", err)
	}
	
	// Simulate a large gap by setting last scan block way behind
	simulatedGap := uint64(50000)
	if currentBlock > simulatedGap {
		lastScanBlock := currentBlock - simulatedGap
		err = db.UpdateLastScanBlock(cfg.Source.ChainID, lastScanBlock)
		if err != nil {
			log.Fatalf("Failed to update last scan block: %v", err)
		}
		logger.Infof("Simulated gap: Set last scan block to %d (current: %d, gap: %d blocks)", 
			lastScanBlock, currentBlock, simulatedGap)
	}
	
	// Create enhanced scanner
	scanner, err := scanner.NewEnhancedBlockScanner(
		&cfg.BlockScanner,
		&cfg.Source,
		client,
		db,
		eventChan,
		errorChan,
	)
	if err != nil {
		log.Fatalf("Failed to create scanner: %v", err)
	}
	
	// Start event consumer
	go consumeEvents(eventChan)
	go consumeErrors(errorChan)
	
	// Start scanner
	ctx := context.Background()
	if err := scanner.Start(ctx); err != nil {
		log.Fatalf("Failed to start scanner: %v", err)
	}
	
	// Monitor progress
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	startTime := time.Now()
	timeout := 5 * time.Minute
	
	for {
		select {
		case <-ticker.C:
			stats := scanner.GetStats()
			
			logger.Infof("Scanner Progress:")
			logger.Infof("  Forward block: %d", stats.ForwardBlock)
			logger.Infof("  Backward block: %d", stats.BackwardBlock)
			logger.Infof("  Gap: %d blocks", stats.BackwardBlock - stats.ForwardBlock)
			logger.Infof("  Converged: %v", stats.Converged)
			logger.Infof("  Events found: %d (forward), %d (backward)", 
				stats.ForwardEventsFound, stats.BackwardEventsFound)
			logger.Infof("  Total blocks scanned: %d", stats.TotalBlocksScanned)
			
			if stats.Converged {
				logger.Info("SUCCESS: Scanners converged!")
				elapsed := time.Since(startTime)
				logger.Infof("Time taken: %s", elapsed)
				logger.Infof("Blocks per second: %.2f", 
					float64(stats.TotalBlocksScanned)/elapsed.Seconds())
				break
			}
			
		case <-time.After(timeout):
			logger.Error("TIMEOUT: Test exceeded time limit")
			break
		}
	}
	
	// Stop scanner
	if err := scanner.Stop(); err != nil {
		logger.Errorf("Failed to stop scanner: %v", err)
	}
	
	logger.Info("Test completed")
}

func consumeEvents(eventChan <-chan *bridgeTypes.EventData) {
	for event := range eventChan {
		direction := "forward"
		if event.IsBackwardScan {
			direction = "backward"
		}
		logger.Debugf("Event received (%s): %s at block %d", 
			direction, event.EventName, event.BlockNumber)
	}
}

func consumeErrors(errorChan <-chan error) {
	for err := range errorChan {
		logger.Errorf("Scanner error: %v", err)
	}
}