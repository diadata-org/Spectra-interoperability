package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// BlockScanner scans blockchain for missed events
type BlockScanner struct {
	config           *config.BlockScannerConfig
	sourceConfig     *config.SourceConfig
	eventDefinitions map[string]*config.EventDefinition
	client           *ethclient.Client
	db               *database.DB
	eventChan        chan<- *bridgeTypes.EventData
	errorChan        chan<- error
	
	contractAddresses []common.Address
	eventSignatures   []common.Hash
	
	mu              sync.RWMutex
	scanning        bool
	lastScanBlock   uint64
	
	stopChan        chan struct{}
	stoppedChan     chan struct{}
}

// NewBlockScanner creates a new block scanner
func NewBlockScanner(
	cfg *config.BlockScannerConfig,
	sourceConfig *config.SourceConfig,
	eventDefinitions map[string]*config.EventDefinition,
	client *ethclient.Client,
	db *database.DB,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (*BlockScanner, error) {
	scanner := &BlockScanner{
		config:           cfg,
		sourceConfig:     sourceConfig,
		eventDefinitions: eventDefinitions,
		client:           client,
		db:               db,
		eventChan:        eventChan,
		errorChan:        errorChan,
		stopChan:         make(chan struct{}),
		stoppedChan:      make(chan struct{}),
	}

	// Extract contract addresses and event signatures
	if err := scanner.extractContractInfo(); err != nil {
		return nil, fmt.Errorf("failed to extract contract info: %w", err)
	}

	return scanner, nil
}

// Start begins scanning blocks
func (bs *BlockScanner) Start(ctx context.Context) error {
	if !bs.config.Enabled {
		logger.Info("Block scanner disabled")
		return nil
	}

	logger.Info("Starting block scanner")
	
	// Initialize chain state if needed
	if err := bs.db.InitializeChainState(bs.sourceConfig.ChainID, bs.sourceConfig.Name, bs.sourceConfig.StartBlock); err != nil {
		logger.Warnf("Failed to initialize chain state: %v", err)
	}
	
	// Get initial state from database
	chainState, err := bs.db.GetChainState(bs.sourceConfig.ChainID)
	if err != nil {
		return fmt.Errorf("failed to get chain state: %w", err)
	}
	
	bs.mu.Lock()
	bs.lastScanBlock = chainState.LastScanBlock
	bs.mu.Unlock()

	// Start scanning in a goroutine
	go bs.scanLoop(ctx)

	// Start gap detection in a goroutine
	go bs.gapDetectionLoop(ctx)

	return nil
}

// Stop gracefully stops the block scanner
func (bs *BlockScanner) Stop() error {
	logger.Info("Stopping block scanner")
	
	close(bs.stopChan)
	
	// Wait for scanner to stop with timeout
	select {
	case <-bs.stoppedChan:
		logger.Info("Block scanner stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Block scanner stop timeout")
	}

	return nil
}

// scanLoop is the main scanning loop
func (bs *BlockScanner) scanLoop(ctx context.Context) {
	defer close(bs.stoppedChan)
	
	ticker := time.NewTicker(bs.config.ScanInterval.Duration())
	defer ticker.Stop()

	// Initial scan
	if err := bs.scan(ctx); err != nil {
		logger.Errorf("Initial scan error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			if err := bs.scan(ctx); err != nil {
				logger.Errorf("Block scan error: %v", err)
				bs.errorChan <- err
			}
		}
	}
}

// scan performs a single scan iteration
func (bs *BlockScanner) scan(ctx context.Context) error {
	bs.mu.Lock()
	if bs.scanning {
		bs.mu.Unlock()
		logger.Debug("Scan already in progress, skipping")
		return nil
	}
	bs.scanning = true
	startBlock := bs.lastScanBlock + 1
	bs.mu.Unlock()

	defer func() {
		bs.mu.Lock()
		bs.scanning = false
		bs.mu.Unlock()
	}()

	// Get current block number
	currentBlock, err := bs.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current block: %w", err)
	}

	// Check if we're too far behind
	gap := currentBlock - startBlock
	if gap > bs.config.MaxBlockGap {
		logger.Warnf("Large block gap detected: %d blocks behind (from %d to %d)", 
			gap, startBlock, currentBlock)
		
		// Send alert
		bs.errorChan <- fmt.Errorf("block scanner %d blocks behind", gap)
		
		// Limit scan range to prevent overwhelming the system
		currentBlock = startBlock + bs.config.MaxBlockGap
	}

	// Calculate end block
	endBlock := startBlock + bs.config.BlockRange - 1
	if endBlock > currentBlock {
		endBlock = currentBlock
	}

	// Skip if nothing to scan
	if startBlock > endBlock {
		logger.Debugf("No new blocks to scan (last: %d, current: %d)", bs.lastScanBlock, currentBlock)
		return nil
	}

	logger.Infof("Scanning blocks %d to %d (current: %d)", startBlock, endBlock, currentBlock)

	// Scan in smaller chunks for better performance
	const chunkSize = 100
	for chunkStart := startBlock; chunkStart <= endBlock; chunkStart += chunkSize {
		chunkEnd := chunkStart + chunkSize - 1
		if chunkEnd > endBlock {
			chunkEnd = endBlock
		}

		events, err := bs.scanBlockRange(ctx, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("failed to scan blocks %d-%d: %w", chunkStart, chunkEnd, err)
		}

		// Process events
		for _, event := range events {
			// Check if already processed
			intentHashHex := common.BytesToHash(event.IntentHash[:]).Hex()
			processed, err := bs.db.IsEventProcessed(intentHashHex)
			if err != nil {
				logger.Errorf("Failed to check if event processed: %v", err)
				continue
			}
			if processed {
				logger.Debugf("Event already processed: %s", intentHashHex)
				continue
			}

			// Send to event channel
			select {
			case bs.eventChan <- event:
				logger.Infof("Block scanner found event: %s at block %d", 
					event.EventName, event.BlockNumber)
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				logger.Warn("Timeout sending event to channel")
			}
		}

		// Update scan progress
		if err := bs.db.UpdateLastScanBlock(bs.sourceConfig.ChainID, chunkEnd); err != nil {
			logger.Errorf("Failed to update last scan block: %v", err)
		}
		
		bs.mu.Lock()
		bs.lastScanBlock = chunkEnd
		bs.mu.Unlock()
	}

	logger.Infof("Scan complete, processed blocks %d to %d", startBlock, endBlock)
	return nil
}

// scanBlockRange scans a specific range of blocks for events
func (bs *BlockScanner) scanBlockRange(ctx context.Context, startBlock, endBlock uint64) ([]*bridgeTypes.EventData, error) {
	// Build filter query
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(startBlock)),
		ToBlock:   big.NewInt(int64(endBlock)),
		Addresses: bs.contractAddresses,
		Topics:    [][]common.Hash{bs.eventSignatures},
	}

	// Get logs
	logs, err := bs.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to filter logs: %w", err)
	}

	// Convert logs to events
	var events []*bridgeTypes.EventData
	for _, log := range logs {
		event, err := bs.parseLog(log)
		if err != nil {
			logger.Errorf("Failed to parse log at block %d, tx %s: %v", 
				log.BlockNumber, log.TxHash.Hex(), err)
			continue
		}
		
		// Apply filters
		if bs.shouldProcessEvent(event) {
			events = append(events, event)
		}
	}

	return events, nil
}

// gapDetectionLoop periodically checks for gaps in processed blocks
func (bs *BlockScanner) gapDetectionLoop(ctx context.Context) {
	// Run less frequently than main scan
	ticker := time.NewTicker(bs.config.ScanInterval.Duration() * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			if err := bs.detectAndFillGaps(ctx); err != nil {
				logger.Errorf("Gap detection error: %v", err)
			}
		}
	}
}

// detectAndFillGaps finds and fills gaps in processed blocks
func (bs *BlockScanner) detectAndFillGaps(ctx context.Context) error {
	// Query processed events to find gaps
	const lookback = 10000 // Check last 10k blocks
	
	bs.mu.RLock()
	currentScanBlock := bs.lastScanBlock
	bs.mu.RUnlock()
	
	startBlock := currentScanBlock - lookback
	if startBlock < bs.sourceConfig.StartBlock {
		startBlock = bs.sourceConfig.StartBlock
	}

	// Get all processed blocks in range
	events, err := bs.db.GetProcessedEventsByBlockRange(startBlock, currentScanBlock)
	if err != nil {
		return fmt.Errorf("failed to get processed events: %w", err)
	}

	// Build block map
	blockMap := make(map[uint64]bool)
	for _, event := range events {
		blockMap[event.BlockNumber] = true
	}

	// Find gaps
	var gaps []struct{ start, end uint64 }
	gapStart := uint64(0)
	inGap := false

	for block := startBlock; block <= currentScanBlock; block++ {
		if !blockMap[block] {
			if !inGap {
				gapStart = block
				inGap = true
			}
		} else if inGap {
			gaps = append(gaps, struct{ start, end uint64 }{gapStart, block - 1})
			inGap = false
		}
	}

	// Handle gap at the end
	if inGap {
		gaps = append(gaps, struct{ start, end uint64 }{gapStart, currentScanBlock})
	}

	// Fill gaps
	for _, gap := range gaps {
		if gap.end-gap.start > 100 {
			logger.Warnf("Found large gap in blocks %d-%d (%d blocks)", 
				gap.start, gap.end, gap.end-gap.start+1)
		}

		logger.Infof("Filling gap in blocks %d-%d", gap.start, gap.end)
		events, err := bs.scanBlockRange(ctx, gap.start, gap.end)
		if err != nil {
			logger.Errorf("Failed to fill gap %d-%d: %v", gap.start, gap.end, err)
			continue
		}

		// Process gap events with higher priority
		for _, event := range events {
			event.IsGapFill = true
			select {
			case bs.eventChan <- event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if len(gaps) > 0 {
		logger.Infof("Filled %d gaps in block scanning", len(gaps))
	}

	return nil
}

// parseLog converts a raw log to EventData
func (bs *BlockScanner) parseLog(log types.Log) (*bridgeTypes.EventData, error) {
	// This is a simplified version - in production, use the same parsing logic as EventMonitor
	event := &bridgeTypes.EventData{
		EventName:       "IntentRegistered", // Determine from log.Topics[0]
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Raw:             log,
	}

	// Extract data based on event type
	if len(log.Topics) > 1 {
		event.IntentHash = [32]byte(log.Topics[1])
	}

	// Parse additional fields from log.Data
	// This would use ABI decoding in production

	return event, nil
}

// shouldProcessEvent applies filters to determine if event should be processed
func (bs *BlockScanner) shouldProcessEvent(event *bridgeTypes.EventData) bool {
	// For now, process all events since new config doesn't have filters
	// Filtering is done at router level
	return true
}

// extractContractInfo extracts addresses and event signatures from config
func (bs *BlockScanner) extractContractInfo() error {
	if bs.eventDefinitions == nil || len(bs.eventDefinitions) == 0 {
		return fmt.Errorf("no event definitions provided")
	}
	
	// Extract unique contract addresses and event signatures
	contractMap := make(map[common.Address]bool)
	
	for eventName, eventDef := range bs.eventDefinitions {
		// Add contract address
		contractAddr := common.HexToAddress(eventDef.Contract)
		if !contractMap[contractAddr] {
			bs.contractAddresses = append(bs.contractAddresses, contractAddr)
			contractMap[contractAddr] = true
		}
		
		// Calculate event signature from ABI
		// Parse the event ABI to get the signature
		var event struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Inputs []struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				Indexed bool   `json:"indexed"`
			} `json:"inputs"`
		}
		
		if err := json.Unmarshal([]byte(eventDef.ABI), &event); err != nil {
			logger.Warnf("Failed to parse ABI for event %s: %v", eventName, err)
			continue
		}
		
		// Build event signature string
		var types []string
		for _, input := range event.Inputs {
			types = append(types, input.Type)
		}
		sigStr := fmt.Sprintf("%s(%s)", event.Name, strings.Join(types, ","))
		
		// Calculate signature hash
		sigHash := crypto.Keccak256Hash([]byte(sigStr))
		bs.eventSignatures = append(bs.eventSignatures, sigHash)
		
		logger.Infof("Event %s: signature=%s, hash=%s", eventName, sigStr, sigHash.Hex())
	}
	
	logger.Infof("Monitoring %d contracts for %d events on chain %d", 
		len(bs.contractAddresses), len(bs.eventSignatures), bs.sourceConfig.ChainID)
	for _, addr := range bs.contractAddresses {
		logger.Infof("  - Contract: %s", addr.Hex())
	}
	
	return nil
}

// GetStats returns scanner statistics
func (bs *BlockScanner) GetStats() *bridgeTypes.ScannerStats {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	currentBlock, _ := bs.client.BlockNumber(context.Background())

	return &bridgeTypes.ScannerStats{
		LastScanBlock:  bs.lastScanBlock,
		CurrentBlock:   currentBlock,
		BlocksBehind:   currentBlock - bs.lastScanBlock,
		IsScanning:     bs.scanning,
	}
}