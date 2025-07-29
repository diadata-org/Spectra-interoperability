package scanner

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// EnhancedBlockScanner implements both forward and backward scanning
type EnhancedBlockScanner struct {
	config          *config.BlockScannerConfig
	sourceConfig    *config.SourceConfig
	client          *ethclient.Client
	db              *database.DB
	eventChan       chan<- *bridgeTypes.EventData
	errorChan       chan<- error
	
	contractAddresses []common.Address
	eventSignatures   []common.Hash
	
	mu              sync.RWMutex
	scanning        bool
	lastScanBlock   uint64
	
	// Backward scanning state
	backwardScanning bool
	backwardStartBlock uint64
	backwardEndBlock   uint64
	
	// Convergence tracking
	forwardBlock    uint64
	backwardBlock   uint64
	converged       bool
	
	// Statistics
	forwardEventsFound  uint64
	backwardEventsFound uint64
	totalBlocksScanned  uint64
	
	stopChan        chan struct{}
	stoppedChan     chan struct{}
}

// NewEnhancedBlockScanner creates a new enhanced block scanner
func NewEnhancedBlockScanner(
	cfg *config.BlockScannerConfig,
	sourceConfig *config.SourceConfig,
	client *ethclient.Client,
	db *database.DB,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (*EnhancedBlockScanner, error) {
	scanner := &EnhancedBlockScanner{
		config:       cfg,
		sourceConfig: sourceConfig,
		client:       client,
		db:           db,
		eventChan:    eventChan,
		errorChan:    errorChan,
		stopChan:     make(chan struct{}),
		stoppedChan:  make(chan struct{}),
	}

	// Extract contract addresses and event signatures
	if err := scanner.extractContractInfo(); err != nil {
		return nil, fmt.Errorf("failed to extract contract info: %w", err)
	}

	return scanner, nil
}

// Start begins scanning blocks
func (bs *EnhancedBlockScanner) Start(ctx context.Context) error {
	if !bs.config.Enabled {
		logger.Info("Block scanner disabled")
		return nil
	}

	logger.Info("Starting enhanced block scanner with backward sync")
	
	// Initialize chain state if needed
	if err := bs.db.InitializeChainState(bs.sourceConfig.ChainID, bs.sourceConfig.Name, bs.sourceConfig.StartBlock); err != nil {
		logger.Warnf("Failed to initialize chain state: %v", err)
	}
	
	// Get initial state from database
	chainState, err := bs.db.GetChainState(bs.sourceConfig.ChainID)
	if err != nil {
		return fmt.Errorf("failed to get chain state: %w", err)
	}
	
	// Get current block number
	currentBlock, err := bs.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current block: %w", err)
	}
	
	bs.mu.Lock()
	bs.lastScanBlock = chainState.LastScanBlock
	bs.forwardBlock = bs.lastScanBlock
	bs.backwardBlock = currentBlock
	
	// Check if we need backward scanning
	gap := currentBlock - bs.lastScanBlock
	needBackwardScan := gap > bs.config.MaxBlockGap
	
	if needBackwardScan {
		logger.Warnf("Large gap detected: %d blocks behind. Starting dual-direction scanning", gap)
		bs.backwardScanning = true
		bs.backwardStartBlock = currentBlock
		bs.backwardEndBlock = bs.lastScanBlock + 1
	}
	bs.mu.Unlock()

	// Start forward scanning
	go bs.forwardScanLoop(ctx)

	// Start backward scanning if needed
	if needBackwardScan {
		go bs.backwardScanLoop(ctx)
	}

	// Start convergence monitor
	go bs.convergenceMonitor(ctx)

	// Start gap detection
	go bs.gapDetectionLoop(ctx)

	// Try to start WebSocket subscription for real-time events
	go bs.startWebSocketSubscription(ctx)

	return nil
}

// forwardScanLoop scans forward from last processed block
func (bs *EnhancedBlockScanner) forwardScanLoop(ctx context.Context) {
	defer func() {
		logger.Info("Forward scanner stopped")
	}()
	
	ticker := time.NewTicker(bs.config.ScanInterval.Duration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			if err := bs.forwardScan(ctx); err != nil {
				logger.Errorf("Forward scan error: %v", err)
				bs.errorChan <- err
			}
		}
	}
}

// backwardScanLoop scans backward from current block
func (bs *EnhancedBlockScanner) backwardScanLoop(ctx context.Context) {
	defer func() {
		logger.Info("Backward scanner stopped")
	}()
	
	// Backward scan runs continuously without ticker
	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		default:
			bs.mu.RLock()
			if bs.converged || !bs.backwardScanning {
				bs.mu.RUnlock()
				time.Sleep(5 * time.Second)
				continue
			}
			bs.mu.RUnlock()
			
			if err := bs.backwardScan(ctx); err != nil {
				logger.Errorf("Backward scan error: %v", err)
				time.Sleep(time.Second)
			}
		}
	}
}

// forwardScan performs a forward scan iteration
func (bs *EnhancedBlockScanner) forwardScan(ctx context.Context) error {
	bs.mu.Lock()
	if bs.scanning {
		bs.mu.Unlock()
		return nil
	}
	bs.scanning = true
	startBlock := bs.forwardBlock + 1
	bs.mu.Unlock()

	defer func() {
		bs.mu.Lock()
		bs.scanning = false
		bs.mu.Unlock()
	}()

	// Get current block or convergence point
	var endBlock uint64
	currentBlock, err := bs.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current block: %w", err)
	}

	bs.mu.RLock()
	if bs.backwardScanning && !bs.converged {
		// If backward scanning, only go up to backward scanner position
		endBlock = bs.backwardBlock
	} else {
		endBlock = currentBlock
	}
	bs.mu.RUnlock()

	// Limit scan range
	if endBlock-startBlock > uint64(bs.config.BlockRange) {
		endBlock = startBlock + uint64(bs.config.BlockRange) - 1
	}

	if startBlock > endBlock {
		return nil
	}

	logger.Debugf("Forward scanning blocks %d to %d", startBlock, endBlock)

	events, err := bs.scanBlockRange(ctx, startBlock, endBlock, false)
	if err != nil {
		return fmt.Errorf("forward scan failed: %w", err)
	}

	// Process events
	for _, event := range events {
		if err := bs.processEvent(ctx, event); err != nil {
			logger.Errorf("Failed to process event: %v", err)
		}
	}

	// Update progress
	bs.mu.Lock()
	bs.forwardBlock = endBlock
	atomic.AddUint64(&bs.forwardEventsFound, uint64(len(events)))
	atomic.AddUint64(&bs.totalBlocksScanned, endBlock-startBlock+1)
	
	// Check convergence
	if bs.backwardScanning && bs.forwardBlock >= bs.backwardBlock {
		bs.converged = true
		bs.backwardScanning = false
		logger.Info("Forward and backward scanners converged!")
	}
	bs.mu.Unlock()

	// Update database
	if err := bs.db.UpdateLastScanBlock(bs.sourceConfig.ChainID, endBlock); err != nil {
		logger.Errorf("Failed to update last scan block: %v", err)
	}

	return nil
}

// backwardScan performs a backward scan iteration
func (bs *EnhancedBlockScanner) backwardScan(ctx context.Context) error {
	bs.mu.Lock()
	if bs.converged {
		bs.mu.Unlock()
		return nil
	}
	
	endBlock := bs.backwardBlock
	targetBlock := bs.forwardBlock + 1
	bs.mu.Unlock()

	// Calculate start block for this iteration
	startBlock := endBlock
	if endBlock > uint64(bs.config.BlockRange) {
		startBlock = endBlock - uint64(bs.config.BlockRange) + 1
	}
	
	// Don't go below target
	if startBlock < targetBlock {
		startBlock = targetBlock
	}

	if startBlock > endBlock {
		return nil
	}

	logger.Debugf("Backward scanning blocks %d to %d", startBlock, endBlock)

	events, err := bs.scanBlockRange(ctx, startBlock, endBlock, true)
	if err != nil {
		return fmt.Errorf("backward scan failed: %w", err)
	}

	// Process events with higher priority
	for _, event := range events {
		event.Priority = 2 // Higher priority for recent events
		if err := bs.processEvent(ctx, event); err != nil {
			logger.Errorf("Failed to process event: %v", err)
		}
	}

	// Update progress
	bs.mu.Lock()
	bs.backwardBlock = startBlock - 1
	atomic.AddUint64(&bs.backwardEventsFound, uint64(len(events)))
	atomic.AddUint64(&bs.totalBlocksScanned, endBlock-startBlock+1)
	
	// Check convergence
	if bs.backwardBlock <= bs.forwardBlock {
		bs.converged = true
		bs.backwardScanning = false
		logger.Info("Backward scanner reached forward scanner position - converged!")
	}
	bs.mu.Unlock()

	return nil
}

// scanBlockRange scans a specific range of blocks for events
func (bs *EnhancedBlockScanner) scanBlockRange(ctx context.Context, startBlock, endBlock uint64, isBackward bool) ([]*bridgeTypes.EventData, error) {
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
		
		event.IsBackwardScan = isBackward
		
		// Apply filters
		if bs.shouldProcessEvent(event) {
			events = append(events, event)
			
			// Log individual event discovery with method
			scanMethod := "FORWARD"
			if isBackward {
				scanMethod = "BACKFILL"
			}
			logger.Infof("[%s] Discovered %s event at block %d, tx %s, symbol: %s", 
				scanMethod, event.EventName, log.BlockNumber, log.TxHash.Hex(), event.Symbol)
		}
	}

	if len(events) > 0 {
		scanType := "forward scan"
		if isBackward {
			scanType = "backfill"
		}
		logger.Infof("Found %d events via %s in blocks %d-%d", 
			len(events), scanType, startBlock, endBlock)
	}

	return events, nil
}

// processEvent processes a single event
func (bs *EnhancedBlockScanner) processEvent(ctx context.Context, event *bridgeTypes.EventData) error {
	// Check if already processed
	intentHashHex := common.BytesToHash(event.IntentHash[:]).Hex()
	processed, err := bs.db.IsEventProcessed(intentHashHex)
	if err != nil {
		return fmt.Errorf("failed to check if event processed: %w", err)
	}
	if processed {
		logger.Debugf("Event already processed: %s", intentHashHex)
		return nil
	}

	// Send to event channel
	select {
	case bs.eventChan <- event:
		scanMethod := "FORWARD"
		if event.IsBackwardScan {
			scanMethod = "BACKFILL"
		}
		logger.Infof("[%s] Processing event: %s at block %d (intent: %s)", 
			scanMethod, event.EventName, event.BlockNumber, intentHashHex)
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout sending event to channel")
	}

	return nil
}

// convergenceMonitor monitors and logs convergence progress
func (bs *EnhancedBlockScanner) convergenceMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			bs.logProgress()
		}
	}
}

// logProgress logs scanning progress
func (bs *EnhancedBlockScanner) logProgress() {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if !bs.backwardScanning && !bs.converged {
		return
	}

	var gap uint64
	if bs.backwardBlock > bs.forwardBlock {
		gap = bs.backwardBlock - bs.forwardBlock
	} else {
		// Already converged or invalid state
		gap = 0
	}
	
	if gap > 0 {
		blocksPerSec := float64(bs.totalBlocksScanned) / time.Since(time.Now().Add(-30 * time.Second)).Seconds()
		if blocksPerSec > 0 {
			eta := time.Duration(float64(gap) / blocksPerSec * float64(time.Second))
			
			logger.Infof("Enhanced Block Scanner Progress:")
			logger.Infof("  - Forward Scanner: Block %d (found %d events)", bs.forwardBlock, bs.forwardEventsFound)
			logger.Infof("  - Backward Scanner: Block %d (found %d events)", bs.backwardBlock, bs.backwardEventsFound)
			logger.Infof("  - Gap: %d blocks, ETA: %s", gap, eta)
			logger.Infof("  - Total blocks scanned: %d", bs.totalBlocksScanned)
		}
	} else if bs.converged {
		logger.Info("Scanners have converged - no gap remaining")
	}
}

// gapDetectionLoop periodically checks for gaps in processed blocks
func (bs *EnhancedBlockScanner) gapDetectionLoop(ctx context.Context) {
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
			// Only run gap detection after convergence
			bs.mu.RLock()
			if bs.backwardScanning || !bs.converged {
				bs.mu.RUnlock()
				continue
			}
			bs.mu.RUnlock()
			
			if err := bs.detectAndFillGaps(ctx); err != nil {
				logger.Errorf("Gap detection error: %v", err)
			}
		}
	}
}

// detectAndFillGaps finds and fills gaps in processed blocks
func (bs *EnhancedBlockScanner) detectAndFillGaps(ctx context.Context) error {
	// Query processed events to find gaps
	const lookback = 10000 // Check last 10k blocks
	
	bs.mu.RLock()
	currentScanBlock := bs.forwardBlock
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
		events, err := bs.scanBlockRange(ctx, gap.start, gap.end, false)
		if err != nil {
			logger.Errorf("Failed to fill gap %d-%d: %v", gap.start, gap.end, err)
			continue
		}

		// Process gap events with higher priority
		for _, event := range events {
			event.IsGapFill = true
			event.Priority = 3 // Highest priority for gap fills
			if err := bs.processEvent(ctx, event); err != nil {
				logger.Errorf("Failed to process gap event: %v", err)
			}
		}
	}

	if len(gaps) > 0 {
		logger.Infof("Filled %d gaps in block scanning", len(gaps))
	}

	return nil
}

// Stop gracefully stops the block scanner
func (bs *EnhancedBlockScanner) Stop() error {
	logger.Info("Stopping enhanced block scanner")
	
	close(bs.stopChan)
	
	// Wait for scanner to stop with timeout
	select {
	case <-bs.stoppedChan:
		logger.Info("Enhanced block scanner stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Enhanced block scanner stop timeout")
	}

	// Log final statistics
	logger.Infof("Scanner statistics: Forward events: %d, Backward events: %d, Total blocks: %d",
		bs.forwardEventsFound, bs.backwardEventsFound, bs.totalBlocksScanned)

	return nil
}

// parseLog converts a raw log to EventData
func (bs *EnhancedBlockScanner) parseLog(log types.Log) (*bridgeTypes.EventData, error) {
	event := &bridgeTypes.EventData{
		EventName:       "IntentRegistered",
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Raw:             log,
	}

	// Extract indexed data
	if len(log.Topics) > 1 {
		event.IntentHash = [32]byte(log.Topics[1])
	}

	// Parse non-indexed data from log.Data
	// The data contains: price (uint256), timestamp (uint256), signer (address)
	if len(log.Data) >= 96 { // 32 bytes each for price, timestamp, and 20 bytes for address (padded to 32)
		event.Price = new(big.Int).SetBytes(log.Data[0:32])
		event.Timestamp = new(big.Int).SetBytes(log.Data[32:64])
		event.Signer = common.BytesToAddress(log.Data[64:96])
	}

	// Symbol is indexed but as a string hash, we'll need to get it from the contract or leave empty
	// For now, leave it empty and let the bridge fetch full event details if needed

	return event, nil
}

// shouldProcessEvent applies filters to determine if event should be processed
func (bs *EnhancedBlockScanner) shouldProcessEvent(event *bridgeTypes.EventData) bool {
	// Apply the same filters as EventMonitor
	filters := bs.sourceConfig.EventFilters

	// Check symbol filter
	if len(filters.Symbols) > 0 && event.Symbol != "" {
		found := false
		for _, symbol := range filters.Symbols {
			if event.Symbol == symbol {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Additional filters...
	
	return true
}

// extractContractInfo extracts addresses and event signatures from config
func (bs *EnhancedBlockScanner) extractContractInfo() error {
	for _, contractConfig := range bs.sourceConfig.Contracts {
		if addr, ok := contractConfig["address"].(string); ok {
			bs.contractAddresses = append(bs.contractAddresses, common.HexToAddress(addr))
		}

		// Extract event signatures
		if events, ok := contractConfig["events"].(map[string]interface{}); ok {
			for _, eventConfig := range events {
				if cfg, ok := eventConfig.(map[string]interface{}); ok {
					if enabled, ok := cfg["enabled"].(bool); ok && enabled {
						if sig, ok := cfg["signature"].(string); ok {
							bs.eventSignatures = append(bs.eventSignatures, common.HexToHash(sig))
						}
					}
				}
			}
		}
	}

	if len(bs.contractAddresses) == 0 {
		return fmt.Errorf("no contract addresses found")
	}

	if len(bs.eventSignatures) == 0 {
		return fmt.Errorf("no event signatures found")
	}

	return nil
}

// GetStats returns scanner statistics
func (bs *EnhancedBlockScanner) GetStats() *bridgeTypes.ScannerStats {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	currentBlock, _ := bs.client.BlockNumber(context.Background())

	return &bridgeTypes.ScannerStats{
		LastScanBlock:       bs.forwardBlock,
		CurrentBlock:        currentBlock,
		BlocksBehind:        currentBlock - bs.forwardBlock,
		IsScanning:          bs.scanning,
		BackwardScanning:    bs.backwardScanning,
		Converged:           bs.converged,
		ForwardBlock:        bs.forwardBlock,
		BackwardBlock:       bs.backwardBlock,
		ForwardEventsFound:  bs.forwardEventsFound,
		BackwardEventsFound: bs.backwardEventsFound,
		TotalBlocksScanned:  bs.totalBlocksScanned,
	}
}

// startWebSocketSubscription attempts to subscribe to real-time events via WebSocket
func (bs *EnhancedBlockScanner) startWebSocketSubscription(ctx context.Context) {
	logger.Info("Attempting to start WebSocket subscription for real-time events")
	
	// Build filter query
	query := ethereum.FilterQuery{
		Addresses: bs.contractAddresses,
		Topics:    [][]common.Hash{bs.eventSignatures},
	}
	
	// Create log channel
	logs := make(chan types.Log, 100)
	
	// Subscribe to logs
	sub, err := bs.client.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		logger.Warnf("WebSocket subscription not available, falling back to polling: %v", err)
		return
	}
	
	logger.Info("[WEBSOCKET] Real-time event subscription active")
	
	// Process incoming logs
	for {
		select {
		case <-ctx.Done():
			sub.Unsubscribe()
			return
			
		case <-bs.stopChan:
			sub.Unsubscribe()
			return
			
		case err := <-sub.Err():
			logger.Errorf("[WEBSOCKET] Subscription error: %v", err)
			// Try to reconnect after a delay
			time.Sleep(10 * time.Second)
			go bs.startWebSocketSubscription(ctx)
			return
			
		case log := <-logs:
			// Process the log
			event, err := bs.parseLog(log)
			if err != nil {
				logger.Errorf("[WEBSOCKET] Failed to parse log: %v", err)
				continue
			}
			
			// Mark as real-time event
			event.Priority = 3 // Highest priority for real-time events
			
			// Apply filters
			if bs.shouldProcessEvent(event) {
				logger.Infof("[WEBSOCKET] Real-time event detected: %s at block %d, tx %s, symbol: %s", 
					event.EventName, log.BlockNumber, log.TxHash.Hex(), event.Symbol)
				
				// Send to processing
				if err := bs.processEvent(ctx, event); err != nil {
					logger.Errorf("[WEBSOCKET] Failed to process event: %v", err)
				}
			}
		}
	}
}