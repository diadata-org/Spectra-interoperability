package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

type EventCache struct {
	sigToName map[common.Hash]string
	sigToDef  map[common.Hash]*config.EventDefinition
	mu        sync.RWMutex
}

func newEventCache(definitions map[string]*config.EventDefinition) *EventCache {
	cache := &EventCache{
		sigToName: make(map[common.Hash]string),
		sigToDef:  make(map[common.Hash]*config.EventDefinition),
	}

	for eventName, eventDef := range definitions {
		sig := cache.calculateSignature(eventDef.ABI)
		if sig != (common.Hash{}) {
			if _, exists := cache.sigToName[sig]; !exists {
				cache.sigToName[sig] = eventName
				cache.sigToDef[sig] = eventDef
			}
		}
	}

	return cache
}

func (c *EventCache) findEvent(eventSig common.Hash) (string, *config.EventDefinition) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if eventName, exists := c.sigToName[eventSig]; exists {
		return eventName, c.sigToDef[eventSig]
	}
	return "", nil
}

func (c *EventCache) calculateSignature(eventABI string) common.Hash {
	var event struct {
		Name   string `json:"name"`
		Inputs []struct {
			Type string `json:"type"`
		} `json:"inputs"`
	}

	if err := json.Unmarshal([]byte(eventABI), &event); err != nil {
		return common.Hash{}
	}

	var types []string
	for _, input := range event.Inputs {
		types = append(types, input.Type)
	}
	sigStr := fmt.Sprintf("%s(%s)", event.Name, strings.Join(types, ","))

	return crypto.Keccak256Hash([]byte(sigStr))
}

// EnhancedBlockScanner implements both forward and backward scanning
type EnhancedBlockScanner struct {
	config           *config.BlockScannerConfig
	sourceConfig     *config.SourceConfig
	eventDefinitions map[string]*config.EventDefinition
	client           EthereumClient
	db               DatabaseInterface
	eventChan        chan<- *bridgeTypes.EventData
	errorChan        chan<- error

	contractAddresses []common.Address
	eventSignatures   []common.Hash
	eventCache        *EventCache

	mu            sync.RWMutex
	scanning      bool
	lastScanBlock uint64

	// Backward scanning state
	backwardScanning   bool
	backwardStartBlock uint64
	backwardEndBlock   uint64

	// Convergence tracking
	forwardBlock  uint64
	backwardBlock uint64
	converged     bool

	// Head tracking for real-time processing
	headBlock       uint64
	lastHeadUpdate  time.Time
	headEventsFound uint64

	// Statistics
	forwardEventsFound  uint64
	backwardEventsFound uint64
	totalBlocksScanned  uint64

	stopChan    chan struct{}
	stoppedChan chan struct{}
	wg          sync.WaitGroup
}

// NewEnhancedBlockScanner creates a new enhanced block scanner
func NewEnhancedBlockScanner(
	cfg *config.BlockScannerConfig,
	sourceConfig *config.SourceConfig,
	eventDefinitions map[string]*config.EventDefinition,
	client EthereumClient,
	db DatabaseInterface,
	eventChan chan<- *bridgeTypes.EventData,
	errorChan chan<- error,
) (*EnhancedBlockScanner, error) {
	scanner := &EnhancedBlockScanner{
		config:           cfg,
		sourceConfig:     sourceConfig,
		eventDefinitions: eventDefinitions,
		client:           client,
		db:               db,
		eventChan:        eventChan,
		errorChan:        errorChan,
		stopChan:         make(chan struct{}),
		stoppedChan:      make(chan struct{}),
		eventCache:       newEventCache(eventDefinitions),
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

	// PRIORITY 1: Start real-time head tracker for new blocks
	bs.wg.Add(1)
	go bs.headTrackerLoop(ctx)

	// PRIORITY 2: Start backward scanning to catch recent events quickly
	if needBackwardScan {
		bs.wg.Add(1)
		go bs.backwardScanLoop(ctx)
	}

	// PRIORITY 3: Start forward scanning from last known position
	bs.wg.Add(1)
	go bs.forwardScanLoop(ctx)

	// Start convergence monitor
	bs.wg.Add(1)
	go bs.convergenceMonitor(ctx)

	// Start gap detection
	bs.wg.Add(1)
	go bs.gapDetectionLoop(ctx)

	// Try to start WebSocket subscription for real-time events
	bs.wg.Add(1)
	go bs.startWebSocketSubscription(ctx)

	// Goroutine to wait for all workers to stop and then close the stoppedChan
	go func() {
		bs.wg.Wait()
		close(bs.stoppedChan)
	}()

	return nil
}

// forwardScanLoop scans forward from last processed block
func (bs *EnhancedBlockScanner) forwardScanLoop(ctx context.Context) {
	defer bs.wg.Done()
	defer func() {
		logger.Info("Forward scanner stopped")
	}()

	// Use longer interval for forward scan since head tracker handles new blocks
	interval := bs.config.ScanInterval
	if interval <= 0 {
		interval = config.Duration(30 * time.Second) // Default if not set
	}
	ticker := time.NewTicker(time.Duration(interval))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			// Only scan if we're not too far behind head tracker
			bs.mu.RLock()
			headBlock := bs.headBlock
			forwardBlock := bs.forwardBlock
			bs.mu.RUnlock()

			// Skip if head tracker has already processed recent blocks
			if headBlock > 0 && headBlock-forwardBlock < 100 {
				logger.Debugf("Forward scanner skipping - head tracker is handling recent blocks")
				continue
			}

			if err := bs.forwardScan(ctx); err != nil {
				logger.Errorf("Forward scan error: %v", err)
				bs.errorChan <- err
			}
		}
	}
}

// backwardScanLoop scans backward from current block
func (bs *EnhancedBlockScanner) backwardScanLoop(ctx context.Context) {
	defer bs.wg.Done()
	defer func() {
		logger.Info("Backward scanner stopped")
	}()

	// Backward scan runs continuously without ticker
	// Use smaller sleep time for faster processing
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
				time.Sleep(2 * time.Second)
				continue
			}
			bs.mu.RUnlock()

			if err := bs.backwardScan(ctx); err != nil {
				logger.Errorf("Backward scan error: %v", err)
				time.Sleep(500 * time.Millisecond) // Shorter retry delay
			}
			// Small delay between batches to avoid overloading
			time.Sleep(100 * time.Millisecond)
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
	// Use larger batch size for backward scanning to catch up faster
	const backwardBatchSize = 5000 // Process 5000 blocks at a time
	var startBlock uint64
	if endBlock > backwardBatchSize {
		startBlock = endBlock - backwardBatchSize + 1
	} else {
		startBlock = 1
	}

	// Don't go below target
	if startBlock < targetBlock {
		startBlock = targetBlock
	}

	if startBlock > endBlock {
		return nil
	}

	logger.Infof("[BACKFILL] Scanning blocks %d to %d (batch size: %d)", startBlock, endBlock, endBlock-startBlock+1)

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
		// After a successful backfill, update the database to the top of the scanned range.
		if err := bs.db.UpdateLastScanBlock(bs.sourceConfig.ChainID, bs.backwardStartBlock); err != nil {
			logger.Errorf("Failed to update last scan block on convergence: %v", err)
		}
	}
	bs.mu.Unlock()

	if len(events) > 0 {
		logger.Infof("[BACKFILL] Found %d events in blocks %d-%d", len(events), startBlock, endBlock)
	}

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

	// Get logs for the block range, fallback to per-block on error
	logs, err := bs.client.FilterLogs(ctx, query)
	if err != nil {
		logger.Errorf("RPC error filtering logs for blocks %d-%d: %v", startBlock, endBlock, err)
		// Notify error but continue processing other blocks
		select {
		case bs.errorChan <- fmt.Errorf("RPC error filtering logs for blocks %d-%d: %w", startBlock, endBlock, err):
		default:
		}
		// Fallback: scan each block individually
		var allEvents []*bridgeTypes.EventData
		for block := startBlock; block <= endBlock; block++ {
			subQuery := ethereum.FilterQuery{
				FromBlock: big.NewInt(int64(block)),
				ToBlock:   big.NewInt(int64(block)),
				Addresses: bs.contractAddresses,
				Topics:    [][]common.Hash{bs.eventSignatures},
			}
			blockLogs, err2 := bs.client.FilterLogs(ctx, subQuery)
			if err2 != nil {
				logger.Errorf("RPC error filtering logs for block %d: %v", block, err2)
				select {
				case bs.errorChan <- fmt.Errorf("RPC error filtering logs for block %d: %w", block, err2):
				default:
				}
				continue
			}
			for _, logEntry := range blockLogs {
				event, err3 := bs.parseLog(logEntry)
				if err3 != nil {
					logger.Errorf("Failed to parse log at block %d, tx %s: %v", logEntry.BlockNumber, logEntry.TxHash.Hex(), err3)
					continue
				}
				event.IsBackwardScan = isBackward
				if bs.shouldProcessEvent(event) {
					allEvents = append(allEvents, event)
				}
			}
		}
		return allEvents, nil
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
	defer bs.wg.Done()
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

	logger.Infof("Enhanced Block Scanner Progress:")
	logger.Infof("  - Head Tracker: Block %d (found %d events) [Last Update: %s ago]",
		bs.headBlock, bs.headEventsFound, time.Since(bs.lastHeadUpdate).Round(time.Second))

	if bs.backwardScanning || bs.converged {
		var gap uint64
		if bs.backwardBlock > bs.forwardBlock {
			gap = bs.backwardBlock - bs.forwardBlock
		} else {
			// Already converged or invalid state
			gap = 0
		}

		logger.Infof("  - Forward Scanner: Block %d (found %d events)", bs.forwardBlock, bs.forwardEventsFound)
		logger.Infof("  - Backward Scanner: Block %d (found %d events)", bs.backwardBlock, bs.backwardEventsFound)

		if gap > 0 {
			blocksPerSec := float64(bs.totalBlocksScanned) / time.Since(time.Now().Add(-30*time.Second)).Seconds()
			if blocksPerSec > 0 {
				eta := time.Duration(float64(gap) / blocksPerSec * float64(time.Second))
				logger.Infof("  - Gap: %d blocks, ETA: %s", gap, eta)
			}
		} else if bs.converged {
			logger.Info("  - Scanners have converged - no gap remaining")
		}
	} else {
		logger.Infof("  - Forward Scanner: Block %d (found %d events)", bs.forwardBlock, bs.forwardEventsFound)
	}

	logger.Infof("  - Total blocks scanned: %d", bs.totalBlocksScanned)
	totalEvents := bs.forwardEventsFound + bs.backwardEventsFound + bs.headEventsFound
	logger.Infof("  - Total events found: %d (Forward: %d, Backward: %d, Head: %d, WebSocket: active)",
		totalEvents, bs.forwardEventsFound, bs.backwardEventsFound, bs.headEventsFound)

	// Add health status
	if time.Since(bs.lastHeadUpdate) > 30*time.Second {
		logger.Warnf("  WARNING: Head tracker hasn't updated in %s - may be stuck", time.Since(bs.lastHeadUpdate).Round(time.Second))
	}
}

// gapDetectionLoop periodically checks for gaps in processed blocks
func (bs *EnhancedBlockScanner) gapDetectionLoop(ctx context.Context) {
	defer bs.wg.Done()
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

	// Determine start block for gap detection without underflow and skip the very first block
	minGapStart := bs.sourceConfig.StartBlock + 1
	var startBlock uint64
	// If lookback range extends before start, clamp to first possible gap start
	if currentScanBlock < lookback+bs.sourceConfig.StartBlock {
		startBlock = minGapStart
	} else {
		// Safe subtraction
		startBlock = currentScanBlock - lookback
		if startBlock <= bs.sourceConfig.StartBlock {
			startBlock = minGapStart
		}
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
	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}

	// Get event signature from first topic
	eventSig := log.Topics[0]

	// Find matching event definition
	eventName, _ := bs.findEventDefinition(eventSig)
	if eventName == "" {
		return nil, fmt.Errorf("unknown event signature: %s", eventSig.Hex())
	}

	detectionTime := time.Now()
	event := &bridgeTypes.EventData{
		EventName:       eventName,
		ContractAddress: log.Address,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Raw:             log,
		DetectedAt:      detectionTime,
	}

	// Record event detection metrics
	metricsInstance := metrics.NewMetrics()
	metricsInstance.RecordEventDetected(eventName, log.Address.Hex())

	// Parse event data based on event type
	switch eventName {
	case "IntentRegistered":
		return bs.parseIntentRegisteredEvent(event, log)
	case "IntArraySet":
		return bs.parseIntArraySetEvent(event, log)
	default:
		return nil, fmt.Errorf("unsupported event type: %s", eventName)
	}
}

func (bs *EnhancedBlockScanner) findEventDefinition(eventSig common.Hash) (string, *config.EventDefinition) {
	// Lazy initialize eventCache from eventDefinitions
	if bs.eventCache == nil {
		bs.eventCache = newEventCache(bs.eventDefinitions)
	}
	// Lookup event signature in cache
	return bs.eventCache.findEvent(eventSig)
}

// parseIntentRegisteredEvent parses an IntentRegistered event
func (bs *EnhancedBlockScanner) parseIntentRegisteredEvent(event *bridgeTypes.EventData, log types.Log) (*bridgeTypes.EventData, error) {
	// Extract indexed data: intentHash (topics[1]), symbol (topics[2])
	if len(log.Topics) > 1 {
		event.IntentHash = [32]byte(log.Topics[1])
	}

	// Symbol is indexed but as a string hash - we'll extract it later via enrichment
	// For now, leave it empty

	// Parse non-indexed data from log.Data
	// The data contains: price (uint256), timestamp (uint256), signer (address)
	if len(log.Data) >= 96 { // 32 bytes each for price, timestamp, and 20 bytes for address (padded to 32)
		event.Price = new(big.Int).SetBytes(log.Data[0:32])
		event.Timestamp = new(big.Int).SetBytes(log.Data[32:64])
		event.Signer = common.BytesToAddress(log.Data[64:96])
	}

	return event, nil
}

// parseIntArraySetEvent parses an IntArraySet event
func (bs *EnhancedBlockScanner) parseIntArraySetEvent(event *bridgeTypes.EventData, log types.Log) (*bridgeTypes.EventData, error) {
	logger.Infof("[DEBUG] parseIntArraySetEvent called for tx %s at block %d", log.TxHash.Hex(), log.BlockNumber)
	logger.Infof("[DEBUG] IntArraySet event topics: %v", log.Topics)
	logger.Infof("[DEBUG] IntArraySet event data length: %d", len(log.Data))

	// IntArraySet event structure:
	// - requestId (uint256) - non-indexed
	// - round (int256) - indexed (topics[1])
	// - seed (string) - non-indexed
	// - signature (string) - non-indexed

	// Extract indexed data: round (topics[1])
	if len(log.Topics) > 1 {
		// Round is indexed as topics[1]
		roundBytes := log.Topics[1][:]
		event.Round = new(big.Int).SetBytes(roundBytes)
		logger.Infof("[DEBUG] IntArraySet extracted round: %s", event.Round.String())
	}

	// Parse non-indexed data: requestId, seed, signature
	// This requires proper ABI decoding since strings have dynamic length
	if len(log.Data) > 0 {
		// For now, we'll store the raw data and let the enrichment process decode it properly
		// The enrichment will call getIntArray to get the full structured data
		event.RawData = log.Data

		// Try to extract requestId from the beginning (first 32 bytes)
		if len(log.Data) >= 32 {
			event.RequestId = new(big.Int).SetBytes(log.Data[0:32])
			logger.Infof("[DEBUG] IntArraySet extracted requestId: %s", event.RequestId.String())
		}
	}

	// CRITICAL: IntArraySet events don't have IntentHash, use RequestId as unique identifier
	if event.RequestId != nil {
		// Convert RequestId to a hash-like format for database compatibility
		requestIdBytes := event.RequestId.Bytes()
		// Pad to 32 bytes if needed
		if len(requestIdBytes) < 32 {
			padded := make([]byte, 32)
			copy(padded[32-len(requestIdBytes):], requestIdBytes)
			copy(event.IntentHash[:], padded)
		} else {
			copy(event.IntentHash[:], requestIdBytes[:32])
		}
		logger.Infof("[DEBUG] IntArraySet using RequestId %s as IntentHash %x", event.RequestId.String(), event.IntentHash)
	}

	logger.Infof("[DEBUG] parseIntArraySetEvent completed successfully for RequestId: %s",
		func() string {
			if event.RequestId != nil {
				return event.RequestId.String()
			}
			return "nil"
		}())

	return event, nil
}

// shouldProcessEvent applies filters to determine if event should be processed
func (bs *EnhancedBlockScanner) shouldProcessEvent(event *bridgeTypes.EventData) bool {
	// For now, process all events since new config doesn't have filters
	// Filtering is done at router level
	return true
}

// extractContractInfo extracts addresses and event signatures from config
func (bs *EnhancedBlockScanner) extractContractInfo() error {
	if len(bs.eventDefinitions) == 0 {
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

	// Build event name list for logging
	eventNames := make([]string, 0, len(bs.eventDefinitions))
	for eventName := range bs.eventDefinitions {
		eventNames = append(eventNames, eventName)
	}

	contractToEvents := make(map[common.Address][]string)
	for eventName, eventDef := range bs.eventDefinitions {
		contractAddr := common.HexToAddress(eventDef.Contract)
		contractToEvents[contractAddr] = append(contractToEvents[contractAddr], eventName)
	}

	logger.Infof("Block scanner initialized: monitoring %d contracts for %d events on chain %d (chain_name=%s)",
		len(bs.contractAddresses), len(bs.eventSignatures), bs.sourceConfig.ChainID, bs.sourceConfig.Name)
	logger.Infof("  Events being monitored: %v", eventNames)
	for _, addr := range bs.contractAddresses {
		eventsForContract := contractToEvents[addr]
		logger.Infof("  - Contract: %s (events: %v)", addr.Hex(), eventsForContract)
	}
	if bs.config.ScanInterval > 0 {
		logger.Infof("  Scan interval: %v", bs.config.ScanInterval.Duration())
	}
	if bs.config.BlockRange > 0 {
		logger.Infof("  Block range per scan: %d", bs.config.BlockRange)
	}
	if bs.config.MaxBlockGap > 0 {
		logger.Infof("  Max block gap: %d", bs.config.MaxBlockGap)
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
		HeadBlock:           bs.headBlock,
		ForwardEventsFound:  bs.forwardEventsFound,
		BackwardEventsFound: bs.backwardEventsFound,
		HeadEventsFound:     bs.headEventsFound,
		TotalBlocksScanned:  bs.totalBlocksScanned,
		LastHeadUpdate:      bs.lastHeadUpdate,
	}
}

// startWebSocketSubscription attempts to subscribe to real-time events via WebSocket
func (bs *EnhancedBlockScanner) startWebSocketSubscription(ctx context.Context) {
	defer bs.wg.Done()
	logger.Infof("Starting WebSocket subscription manager for chain %d (%s)", bs.sourceConfig.ChainID, bs.sourceConfig.Name)

ReconnectLoop:
	for {
		select {
		case <-bs.stopChan:
			logger.Info("Stopping WebSocket subscription manager.")
			return
		case <-ctx.Done():
			logger.Info("Context cancelled, stopping WebSocket subscription manager.")
			return
		default:
		}

		logger.Infof("Attempting to start WebSocket subscription for real-time events on chain %d (%s)", bs.sourceConfig.ChainID, bs.sourceConfig.Name)
		query := ethereum.FilterQuery{
			Addresses: bs.contractAddresses,
			Topics:    [][]common.Hash{bs.eventSignatures},
		}
		logs := make(chan types.Log, 100)

		sub, err := bs.client.SubscribeFilterLogs(ctx, query, logs)
		if err != nil {
			logger.Warnf("WebSocket subscription not available for chain %d (%s), will retry in 10s: %v", bs.sourceConfig.ChainID, bs.sourceConfig.Name, err)
			// Wait before retrying
			select {
			case <-time.After(10 * time.Second):
				continue ReconnectLoop
			case <-bs.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}

		logger.Infof("[WEBSOCKET] Real-time event subscription active for chain %d (%s)", bs.sourceConfig.ChainID, bs.sourceConfig.Name)

		// Process incoming logs
	ProcessingLoop:
		for {
			select {
			case <-ctx.Done():
				sub.Unsubscribe()
				return

			case <-bs.stopChan:
				sub.Unsubscribe()
				return

			case err := <-sub.Err():
				logger.Errorf("[WEBSOCKET] Subscription error for chain %d (%s): %v. Reconnecting...", bs.sourceConfig.ChainID, bs.sourceConfig.Name, err)
				sub.Unsubscribe()
				break ProcessingLoop // Exit inner loop to trigger reconnect

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
		// If we broke out of ProcessingLoop due to an error, the outer ReconnectLoop will continue.
	}
}

// headTrackerLoop continuously monitors and processes new blocks in real-time
func (bs *EnhancedBlockScanner) headTrackerLoop(ctx context.Context) {
	defer bs.wg.Done()
	defer func() {
		logger.Info("Head tracker stopped")
	}()

	// Use a shorter interval for head tracking
	interval := bs.config.HeadTrackerInterval
	if interval <= 0 {
		interval = config.Duration(2 * time.Second) // Default if not set
	}
	ticker := time.NewTicker(time.Duration(interval))
	defer ticker.Stop()

	var lastProcessedHead uint64

	// Initialize with current head
	if currentBlock, err := bs.client.BlockNumber(ctx); err == nil {
		lastProcessedHead = currentBlock
		bs.mu.Lock()
		bs.headBlock = currentBlock
		bs.lastHeadUpdate = time.Now()
		bs.mu.Unlock()
	}

	logger.Info("Starting head tracker for real-time block processing")

	for {
		select {
		case <-ctx.Done():
			return
		case <-bs.stopChan:
			return
		case <-ticker.C:
			// Get current block
			currentBlock, err := bs.client.BlockNumber(ctx)
			if err != nil {
				logger.Errorf("Head tracker: failed to get current block: %v", err)
				continue
			}

			// Check if there are new blocks
			if currentBlock > lastProcessedHead {
				logger.Infof("[HEAD TRACKER] New blocks detected: %d to %d", lastProcessedHead+1, currentBlock)

				// Process new blocks immediately
				startBlock := lastProcessedHead + 1
				endBlock := currentBlock

				// Limit batch size to prevent overwhelming
				const maxBatchSize = 50
				if endBlock-startBlock > maxBatchSize {
					endBlock = startBlock + maxBatchSize - 1
				}

				// Scan new blocks with highest priority
				events, err := bs.scanBlockRange(ctx, startBlock, endBlock, false)
				if err != nil {
					logger.Errorf("[HEAD TRACKER] Failed to scan blocks %d-%d: %v", startBlock, endBlock, err)
					continue
				}

				// Process events with highest priority
				for _, event := range events {
					event.Priority = 4 // Highest priority for head tracker events
					if err := bs.processEvent(ctx, event); err != nil {
						logger.Errorf("[HEAD TRACKER] Failed to process event: %v", err)
					}
				}

				// Update statistics
				bs.mu.Lock()
				bs.headBlock = endBlock
				bs.lastHeadUpdate = time.Now()
				bs.headEventsFound += uint64(len(events))
				atomic.AddUint64(&bs.totalBlocksScanned, endBlock-startBlock+1)
				bs.mu.Unlock()

				// Update last processed head
				lastProcessedHead = endBlock

				if len(events) > 0 {
					logger.Infof("[HEAD TRACKER] Processed %d events from blocks %d-%d",
						len(events), startBlock, endBlock)
				}

				// Update database with latest block if it's ahead of forward scanner
				bs.mu.RLock()
				if endBlock > bs.forwardBlock {
					bs.mu.RUnlock()
					if err := bs.db.UpdateLastScanBlock(bs.sourceConfig.ChainID, endBlock); err != nil {
						logger.Errorf("[HEAD TRACKER] Failed to update last scan block: %v", err)
					}
				} else {
					bs.mu.RUnlock()
				}
			}
		}
	}
}

// calculateEventSignature returns the Keccak256 event signature for the given event ABI
func (bs *EnhancedBlockScanner) calculateEventSignature(eventABI string) common.Hash {
	if bs.eventCache == nil {
		// Fallback: calculate directly
		var event struct {
			Name   string `json:"name"`
			Inputs []struct {
				Type string `json:"type"`
			} `json:"inputs"`
		}
		if err := json.Unmarshal([]byte(eventABI), &event); err != nil {
			return common.Hash{}
		}
		var typesList []string
		for _, input := range event.Inputs {
			typesList = append(typesList, input.Type)
		}
		sigStr := fmt.Sprintf("%s(%s)", event.Name, strings.Join(typesList, ","))
		return crypto.Keccak256Hash([]byte(sigStr))
	}
	return bs.eventCache.calculateSignature(eventABI)
}
