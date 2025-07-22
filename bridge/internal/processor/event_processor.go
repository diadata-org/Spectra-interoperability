package processor

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/router"
)

// EventProcessor processes blockchain events and creates update tasks
type EventProcessor struct {
	config             *config.EventProcessorConfig
	db                 *database.DB
	registryClient     contracts.RegistryClient
	destinations       []*config.DestinationConfig
	eventChan          <-chan *types.EventData
	updateChan         chan<- *types.UpdateRequest
	errorChan          chan<- error

	dedupCache         *DedupCache
	stats              *ProcessingStats
	routerRegistry     *router.Registry
	
	stopChan           chan struct{}
	stoppedChan        chan struct{}
}

// ProcessingStats tracks processing statistics
type ProcessingStats struct {
	EventsReceived     uint64
	EventsProcessed    uint64
	EventsDuplicate    uint64
	EventsInvalid      uint64
	EventsFailed       uint64
	UpdatesCreated     uint64
	LastProcessedTime  time.Time
	mu                 sync.RWMutex
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	cfg *config.EventProcessorConfig,
	db *database.DB,
	registryClient contracts.RegistryClient,
	destinations []*config.DestinationConfig,
	eventChan <-chan *types.EventData,
	updateChan chan<- *types.UpdateRequest,
	errorChan chan<- error,
	routerRegistry *router.Registry,
) (*EventProcessor, error) {
	dedupCache := NewDedupCache(cfg.DedupCacheSize, cfg.DedupCacheTTL)

	return &EventProcessor{
		config:         cfg,
		db:             db,
		registryClient: registryClient,
		destinations:   destinations,
		eventChan:      eventChan,
		updateChan:     updateChan,
		errorChan:      errorChan,
		dedupCache:     dedupCache,
		stats:          &ProcessingStats{},
		routerRegistry: routerRegistry,
		stopChan:       make(chan struct{}),
		stoppedChan:    make(chan struct{}),
	}, nil
}

// Start begins processing events
func (ep *EventProcessor) Start(ctx context.Context) error {
	logger.Info("Starting event processor")

	// Start processing loop
	go ep.processLoop(ctx)

	// Start stats reporter
	go ep.statsReporter(ctx)

	// Start cache cleaner
	go ep.dedupCache.StartCleaner(ctx)

	return nil
}

// Stop gracefully stops the event processor
func (ep *EventProcessor) Stop() error {
	logger.Info("Stopping event processor")
	
	close(ep.stopChan)
	
	// Wait for processor to stop with timeout
	select {
	case <-ep.stoppedChan:
		logger.Info("Event processor stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Event processor stop timeout")
	}

	return nil
}

// processLoop is the main event processing loop
func (ep *EventProcessor) processLoop(ctx context.Context) {
	defer close(ep.stoppedChan)

	// Process events with configurable batch size
	batch := make([]*types.EventData, 0, ep.config.BatchSize)
	batchTimer := time.NewTimer(time.Second)
	batchTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			ep.processBatch(ctx, batch)
			return
			
		case <-ep.stopChan:
			ep.processBatch(ctx, batch)
			return
			
		case event := <-ep.eventChan:
			atomic.AddUint64(&ep.stats.EventsReceived, 1)
			
			batch = append(batch, event)
			if len(batch) == 1 {
				batchTimer.Reset(time.Second)
			}
			
			if len(batch) >= ep.config.BatchSize {
				batchTimer.Stop()
				ep.processBatch(ctx, batch)
				batch = batch[:0]
			}
			
		case <-batchTimer.C:
			if len(batch) > 0 {
				ep.processBatch(ctx, batch)
				batch = batch[:0]
			}
		}
	}
}

// processBatch processes a batch of events
func (ep *EventProcessor) processBatch(ctx context.Context, events []*types.EventData) {
	if len(events) == 0 {
		return
	}

	logger.Debugf("Processing batch of %d events", len(events))

	for _, event := range events {
		if err := ep.processEvent(ctx, event); err != nil {
			logger.Errorf("Failed to process event: %v", err)
			atomic.AddUint64(&ep.stats.EventsFailed, 1)
			ep.errorChan <- fmt.Errorf("event processing failed: %w", err)
		}
	}
}

// processEvent processes a single event
func (ep *EventProcessor) processEvent(ctx context.Context, event *types.EventData) error {
	intentHashHex := hex.EncodeToString(event.IntentHash[:])
	
	// Check deduplication cache
	if ep.dedupCache.Has(intentHashHex) {
		atomic.AddUint64(&ep.stats.EventsDuplicate, 1)
		logger.Debugf("Event already in cache: %s", intentHashHex)
		return nil
	}

	// Check database for processed events
	processed, err := ep.db.IsEventProcessed(intentHashHex)
	if err != nil {
		return fmt.Errorf("failed to check event status: %w", err)
	}
	if processed {
		ep.dedupCache.Add(intentHashHex)
		atomic.AddUint64(&ep.stats.EventsDuplicate, 1)
		logger.Debugf("Event already processed: %s", intentHashHex)
		return nil
	}

	// Fetch full intent data from registry
	intent, err := ep.registryClient.GetIntent(ctx, event.IntentHash)
	if err != nil {
		return fmt.Errorf("failed to get intent: %w", err)
	}

	// Validate intent
	if err := ep.validateIntent(intent); err != nil {
		atomic.AddUint64(&ep.stats.EventsInvalid, 1)
		logger.Warnf("Invalid intent %s: %v", intentHashHex, err)
		return nil // Don't retry invalid intents
	}

	// Save to database
	processedEvent := &database.ProcessedEvent{
		IntentHash:      intentHashHex,
		BlockNumber:     event.BlockNumber,
		TransactionHash: event.TxHash.Hex(),
		LogIndex:        event.LogIndex,
		Symbol:          intent.Symbol,
		Price:           intent.Price.String(),
		Timestamp:       intent.Timestamp.Uint64(),
		Signer:          intent.Signer,
		ProcessedAt:     time.Now(),
	}

	if err := ep.db.SaveProcessedEvent(processedEvent); err != nil {
		return fmt.Errorf("failed to save processed event: %w", err)
	}

	// Add to dedup cache
	ep.dedupCache.Add(intentHashHex)

	// Route using routers
	updatesCreated := 0
	
	if ep.routerRegistry == nil {
		logger.Error("No router registry configured")
		return fmt.Errorf("router registry not initialized")
	}

	// Get all active routers
	routers := ep.routerRegistry.GetActiveRouters()
	
	if len(routers) == 0 {
		logger.Warn("No active routers configured")
		return nil
	}

	// Check each router
	for _, r := range routers {
		shouldRoute, reason := r.ShouldRoute(intent)
		if !shouldRoute {
			logger.Debugf("Router %s skipped: %s", r.ID(), reason)
			continue
		}
		
		logger.Infof("Router %s approved: %s", r.ID(), reason)
		
		// Create update requests for router's destinations
		for _, dest := range r.GetDestinations() {
			// Find destination config
			destConfig := ep.getDestinationConfig(dest.ChainID)
			if destConfig == nil {
				logger.Warnf("Destination chain %d not found for router %s", dest.ChainID, r.ID())
				continue
			}
			
			// Create update for each contract
			for _, contractAddr := range dest.Contracts {
				// Find contract config
				contractConfig := ep.findContractConfig(destConfig, contractAddr)
				if contractConfig == nil {
					logger.Warnf("Contract %s not found on chain %d", contractAddr, dest.ChainID)
					continue
				}
				
				request := ep.createUpdateRequest(event, intent, destConfig, contractConfig)
				
				select {
				case ep.updateChan <- request:
					updatesCreated++
					logger.Infof("Router %s: Created update for %s on %s contract %s", 
						r.ID(), intent.Symbol, destConfig.Name, contractConfig.Name)
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					logger.Warn("Timeout sending update request")
				}
			}
		}
		
		// Notify router that intent was processed
		r.OnRouted(intent)
	}

	atomic.AddUint64(&ep.stats.EventsProcessed, 1)
	atomic.AddUint64(&ep.stats.UpdatesCreated, uint64(updatesCreated))
	
	ep.stats.mu.Lock()
	ep.stats.LastProcessedTime = time.Now()
	ep.stats.mu.Unlock()

	logger.Infof("Processed event %s, created %d updates", intentHashHex, updatesCreated)
	return nil
}


// createUpdateRequest creates an update request for a specific contract
func (ep *EventProcessor) createUpdateRequest(
	event *types.EventData,
	intent *types.OracleIntent,
	destination *config.DestinationConfig,
	contract *config.ContractConfig,
) *types.UpdateRequest {
	return &types.UpdateRequest{
		ID:               fmt.Sprintf("%s-%d-%s", common.BytesToHash(event.IntentHash[:]).Hex(), destination.ChainID, contract.Address),
		IntentHash:       event.IntentHash,
		Intent:           intent,
		Event:            event,
		DestinationChain: destination,
		Contract:         contract,
		Priority:         contract.Priority,
		CreatedAt:        time.Now(),
	}
}

// validateIntent validates an oracle intent
func (ep *EventProcessor) validateIntent(intent *types.OracleIntent) error {
	// Expiry check removed - bridge processes all intents regardless of expiry

	// Check required fields
	if intent.Symbol == "" {
		return fmt.Errorf("missing symbol")
	}
	if intent.Price == nil || intent.Price.Sign() <= 0 {
		return fmt.Errorf("invalid price")
	}
	if intent.Timestamp == nil || intent.Timestamp.Uint64() == 0 {
		return fmt.Errorf("invalid timestamp")
	}
	if intent.Signer == (common.Address{}) {
		return fmt.Errorf("missing signer")
	}

	// Verify signature if configured
	if ep.config.ValidationTimeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), ep.config.ValidationTimeout)
		defer cancel()
		
		// In production, implement proper EIP-712 signature verification
		_ = ctx
	}

	return nil
}

// statsReporter periodically logs processing statistics
func (ep *EventProcessor) statsReporter(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ep.stopChan:
			return
		case <-ticker.C:
			ep.logStats()
		}
	}
}

// logStats logs current processing statistics
func (ep *EventProcessor) logStats() {
	ep.stats.mu.RLock()
	lastProcessed := ep.stats.LastProcessedTime
	ep.stats.mu.RUnlock()

	received := atomic.LoadUint64(&ep.stats.EventsReceived)
	processed := atomic.LoadUint64(&ep.stats.EventsProcessed)
	duplicate := atomic.LoadUint64(&ep.stats.EventsDuplicate)
	invalid := atomic.LoadUint64(&ep.stats.EventsInvalid)
	failed := atomic.LoadUint64(&ep.stats.EventsFailed)
	updates := atomic.LoadUint64(&ep.stats.UpdatesCreated)

	logger.Infof("Event processor stats - Received: %d, Processed: %d, Duplicate: %d, Invalid: %d, Failed: %d, Updates: %d, Last: %s ago",
		received, processed, duplicate, invalid, failed, updates, time.Since(lastProcessed))
}

// GetStats returns current processing statistics
func (ep *EventProcessor) GetStats() types.ProcessorStats {
	ep.stats.mu.RLock()
	lastProcessed := ep.stats.LastProcessedTime
	ep.stats.mu.RUnlock()

	return types.ProcessorStats{
		EventsReceived:    atomic.LoadUint64(&ep.stats.EventsReceived),
		EventsProcessed:   atomic.LoadUint64(&ep.stats.EventsProcessed),
		EventsDuplicate:   atomic.LoadUint64(&ep.stats.EventsDuplicate),
		EventsInvalid:     atomic.LoadUint64(&ep.stats.EventsInvalid),
		EventsFailed:      atomic.LoadUint64(&ep.stats.EventsFailed),
		UpdatesCreated:    atomic.LoadUint64(&ep.stats.UpdatesCreated),
		LastProcessedTime: lastProcessed,
		CacheSize:         ep.dedupCache.Size(),
	}
}

// getDestinationConfig finds a destination configuration by chain ID
func (ep *EventProcessor) getDestinationConfig(chainID int64) *config.DestinationConfig {
	for _, dest := range ep.destinations {
		if dest.ChainID == chainID {
			return dest
		}
	}
	return nil
}

// findContractConfig finds a contract configuration by address
func (ep *EventProcessor) findContractConfig(destConfig *config.DestinationConfig, address string) *config.ContractConfig {
	for i := range destConfig.Contracts {
		if destConfig.Contracts[i].Address == address {
			return &destConfig.Contracts[i]
		}
	}
	return nil
}