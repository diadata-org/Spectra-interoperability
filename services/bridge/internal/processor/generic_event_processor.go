package processor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/pipeline"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// GenericEventProcessor processes events using the generic pipeline
type GenericEventProcessor struct {
	config         *config.EventProcessorConfig
	eventDefs      map[string]*config.EventDefinition
	destinations   map[int64]*config.DestinationConfig
	db             *database.DB
	routerRegistry *router.GenericRegistry
	sourceClient   *ethclient.Client
	destClients    map[int64]*ethclient.Client

	extractor   *pipeline.DataExtractor
	enricher    *pipeline.DataEnricher
	transformer *pipeline.DataTransformer
	// txBuilder   *pipeline.TransactionBuilder // UNUSED - removed to eliminate duplicate NonceManager

	eventChan  <-chan *types.EventData
	errorChan  chan<- error
	updateChan chan<- *types.UpdateRequest

	dedupCache       *DedupCache
	metricsCollector *metrics.Collector
	reportQueueSize  func() // Callback to report queue size after enqueue

	stats types.ProcessorStats

	// Event processing worker pool
	eventWorkerPool *EventWorkerPool
	useParallelMode bool

	// Parallel pipeline processing
	parallelPipeline    *ParallelPipeline
	useParallelPipeline bool

	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewGenericEventProcessor creates a new generic event processor
func NewGenericEventProcessor(
	cfg *config.EventProcessorConfig,
	eventDefs map[string]*config.EventDefinition,
	destinations map[int64]*config.DestinationConfig,
	db *database.DB,
	routerRegistry *router.GenericRegistry,
	sourceClient *ethclient.Client,
	destClients map[int64]*ethclient.Client,
	eventChan <-chan *types.EventData,
	errorChan chan<- error,
	updateChan chan<- *types.UpdateRequest,
	metricsCollector *metrics.Collector,
	reportQueueSize func(), // Callback to report queue size after enqueue
) (*GenericEventProcessor, error) {
	extractor, err := pipeline.NewDataExtractor(eventDefs)
	if err != nil {
		return nil, fmt.Errorf("failed to create data extractor: %w", err)
	}

	enricher, err := pipeline.NewDataEnricher(sourceClient, eventDefs)
	if err != nil {
		return nil, fmt.Errorf("failed to create data enricher: %w", err)
	}

	transformer := pipeline.NewDataTransformer()

	// txBuilder removed - unused dead code that created duplicate NonceManager
	// All transactions now go through transaction.Client → contracts.NonceManager

	gep := &GenericEventProcessor{
		config:         cfg,
		eventDefs:      eventDefs,
		destinations:   destinations,
		db:             db,
		routerRegistry: routerRegistry,
		sourceClient:   sourceClient,
		destClients:    destClients,
		extractor:      extractor,
		enricher:       enricher,
		transformer:    transformer,
		// txBuilder:     nil, // removed
		eventChan:        eventChan,
		errorChan:        errorChan,
		updateChan:       updateChan,
		dedupCache:       NewDedupCache(cfg.DedupCacheSize, cfg.DedupCacheTTL.Duration()),
		metricsCollector: metricsCollector,
		reportQueueSize:  reportQueueSize,
		stopChan:         make(chan struct{}),
	}

	// Initialize event worker pool for parallel processing
	gep.useParallelMode = cfg.EnableParallelMode
	if gep.useParallelMode {
		eventWorkerConfig := DefaultEventWorkerPoolConfig()

		// Use configuration settings if provided
		if cfg.ParallelWorkerCount > 0 {
			eventWorkerConfig.WorkerCount = cfg.ParallelWorkerCount
		}
		if cfg.ParallelQueueSize > 0 {
			eventWorkerConfig.EventQueueSize = cfg.ParallelQueueSize
		}
		if cfg.ParallelTimeout.Duration() > 0 {
			eventWorkerConfig.ProcessingTimeout = cfg.ParallelTimeout.Duration()
		}

		gep.eventWorkerPool = NewEventWorkerPool(eventWorkerConfig, gep)
		logger.Infof("Event worker pool enabled: %d workers, queue size %d",
			eventWorkerConfig.WorkerCount, eventWorkerConfig.EventQueueSize)
	} else {
		logger.Info("Event worker pool disabled, using sequential processing")
	}

	// Initialize parallel pipeline for enrichment and gas estimation
	gep.useParallelPipeline = cfg.EnableParallelMode // Use same flag for now
	if gep.useParallelPipeline {
		// Create service adapters
		enrichmentService := NewEnrichmentServiceAdapter(enricher)
		routingService := NewRoutingServiceAdapter(routerRegistry)
		gasEstimationService := NewGasEstimationService(destClients)

		// Create parallel pipeline
		parallelConfig := DefaultParallelPipelineConfig()
		if cfg.ParallelTimeout.Duration() > 0 {
			// Use the same timeout for both enrichment and gas estimation
			parallelConfig.EnrichmentTimeout = cfg.ParallelTimeout.Duration()
			parallelConfig.GasEstimationTimeout = cfg.ParallelTimeout.Duration() / 2 // Shorter timeout for gas
		}

		gep.parallelPipeline = NewParallelPipeline(
			parallelConfig,
			enrichmentService,
			gasEstimationService,
			routingService,
		)

		logger.Info("Parallel pipeline enabled for enrichment and gas estimation")
	} else {
		logger.Info("Parallel pipeline disabled, using sequential processing")
	}

	return gep, nil
}

// Start begins processing events
func (gep *GenericEventProcessor) Start(ctx context.Context) error {
	logger.Info("Starting generic event processor")

	// Start event worker pool if enabled
	if gep.useParallelMode && gep.eventWorkerPool != nil {
		if err := gep.eventWorkerPool.Start(ctx); err != nil {
			return fmt.Errorf("failed to start event worker pool: %w", err)
		}

		// Start event dispatcher (feeds events to worker pool)
		gep.wg.Add(1)
		go gep.eventDispatcher(ctx)
	} else {
		// Use traditional sequential processing
		gep.wg.Add(1)
		go gep.processLoop(ctx)
	}

	gep.wg.Add(1)
	go gep.statsReporter(ctx)

	return nil
}

// Stop gracefully stops the processor
func (gep *GenericEventProcessor) Stop() error {
	logger.Info("Stopping generic event processor")

	// Stop event worker pool if enabled
	if gep.useParallelMode && gep.eventWorkerPool != nil {
		if err := gep.eventWorkerPool.Stop(); err != nil {
			logger.Errorf("Error stopping event worker pool: %v", err)
		}
	}

	close(gep.stopChan)
	gep.wg.Wait()
	return nil
}

// processLoop is the main event processing loop
func (gep *GenericEventProcessor) processLoop(ctx context.Context) {
	defer gep.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gep.stopChan:
			return
		case event := <-gep.eventChan:
			if event == nil {
				continue
			}

			atomic.AddUint64(&gep.stats.EventsReceived, 1)
			if gep.metricsCollector != nil {
				gep.metricsCollector.IncEventsReceived()
			}

			if err := gep.processEvent(ctx, event); err != nil {
				logger.Errorf("Failed to process event: %v", err)
				atomic.AddUint64(&gep.stats.EventsFailed, 1)
				if gep.metricsCollector != nil {
					gep.metricsCollector.IncEventsFailed()
				}
				gep.errorChan <- fmt.Errorf("event processing failed: %w", err)
			} else {
				atomic.AddUint64(&gep.stats.EventsProcessed, 1)
				if gep.metricsCollector != nil {
					gep.metricsCollector.IncEventsProcessed()
				}
			}
		}
	}
}

// processEvent processes a single event through the pipeline
func (gep *GenericEventProcessor) processEvent(ctx context.Context, event *types.EventData) error {
	eventID := fmt.Sprintf("%s-%d-%d", event.TxHash.Hex(), event.BlockNumber, event.LogIndex)

	log, ok := event.Raw.(ethtypes.Log)
	if !ok {
		return fmt.Errorf("event.Raw is not of type types.Log")
	}
	extractedData, err := gep.extractor.ExtractEventData(event.EventName, log)
	if err != nil {
		return fmt.Errorf("failed to extract event data: %w", err)
	}

	logger.Debugf("Extracted data for %s: %+v", event.EventName, extractedData)

	// Handle enrichment (with optional parallel processing)
	if err := gep.handleEnrichment(ctx, event.EventName, extractedData); err != nil {
		logger.Warnf("Failed to enrich event data: %v", err)
	} else {
		logger.Debugf("Enriched data: %+v", extractedData.Enrichment)
	}

	// Use new router system - route events directly
	routingResults := gep.routerRegistry.RouteEvent(event.EventName, extractedData)

	routersUsed := 0
	for _, result := range routingResults {
		if result.Routed {
			logger.Infof("Router %s approved event %s: %s", result.RouterID, event.EventName, result.Reason)

			// Get the router to apply time threshold filtering after enrichment
			router := gep.routerRegistry.GetRouterByID(result.RouterID)
			if router == nil {
				logger.Warnf("Router %s not found for time threshold filtering", result.RouterID)
				continue
			}

			intentHashStr := fmt.Sprintf("%x", event.IntentHash)
			filteredDestinations := router.FilterDestinationsByTimeThreshold(result.Destinations, extractedData, intentHashStr)
			if len(filteredDestinations) == 0 {
				logger.Debugf("All destinations filtered out by time threshold for router %s", result.RouterID)
				continue
			}

			// Process filtered destinations for this router
			// Note: Time threshold and price deviation checks are now atomic (check-and-reserve)
			// in FilterDestinationsByTimeThreshold, so no need for pre-emptive update here
			for _, dest := range filteredDestinations {
				destConfig, exists := gep.destinations[dest.ChainID]
				if !exists || destConfig == nil {
					logger.Warnf("Destination config not found for chain %d", dest.ChainID)
					continue
				}

				// Find contract config
				var contractConfig *config.LegacyContractConfig
				for i := range destConfig.Contracts {
					if destConfig.Contracts[i].Address == dest.Contract {
						contractConfig = &destConfig.Contracts[i]
						break
					}
				}

				if contractConfig == nil {
					logger.Debugf("Contract config not found for %s, creating minimal config from router destination", dest.Contract)
					contractConfig = &config.LegacyContractConfig{
						Address:       dest.Contract,
						Enabled:       true,
						GasLimit:      dest.GasLimit,
						GasMultiplier: dest.GasMultiplier,
						MaxGasPrice:   dest.MaxGasPrice,
					}
				}

				// Create update request using the router's method configuration
				// Extract Intent from enrichment if available (this is what gets passed to handleIntentUpdate)
				var intent *types.OracleIntent
				var createdAt time.Time

				if extractedData.Enrichment != nil {
					if fullIntentValue, exists := extractedData.Enrichment["fullIntent"]; exists {
						if intentFromEnrichment, ok := fullIntentValue.(*types.OracleIntent); ok {
							intent = intentFromEnrichment
							// Use Intent timestamp - this is the timestamp passed to handleIntentUpdate
							if intent.Timestamp != nil {
								createdAt = time.Unix(intent.Timestamp.Int64(), 0)
							}
						}
					}
				}

				// Fallback to event timestamp if Intent timestamp not available
				if createdAt.IsZero() && event.Timestamp != nil {
					createdAt = time.Unix(event.Timestamp.Int64(), 0)
				}
				// Final fallback to current time
				if createdAt.IsZero() {
					createdAt = time.Now()
				}

				updateReq := &types.UpdateRequest{
					ID:                      fmt.Sprintf("%s-%s-%d", result.RouterID, event.EventName, time.Now().Unix()),
					Event:                   event,
					Intent:                  intent, // Set Intent so it's available later
					DestinationChain:        destConfig,
					Contract:                contractConfig,
					Priority:                1,
					Retries:                 0,
					CreatedAt:               createdAt, // Use timestamp from Intent (same as passed to handleIntentUpdate)
					RouterID:                result.RouterID,
					DestinationMethodConfig: &dest.Method,
					ExtractedData:           extractedData,
				}

				// For IntArraySet events, create a minimal Intent structure for compatibility
				if event.EventName == "IntArraySet" && updateReq.Intent == nil {
					updateReq.Intent = &types.OracleIntent{
						Symbol: fmt.Sprintf("RandomRequest-%s", event.RequestId.String()),
						Signer: common.Address{},                                  // No signer required for randomness
						Expiry: big.NewInt(time.Now().Add(24 * time.Hour).Unix()), // 24h expiry
					}
				}

				select {
				case gep.updateChan <- updateReq:
					routersUsed++
					symbol := router.GetSymbolFromData(extractedData)
					logger.Infof("Queued update: event=%s, router=%s, symbol=%s, chain=%d, contract=%s",
						event.EventName, result.RouterID, symbol, dest.ChainID, dest.Contract)
					// Report queue size immediately after enqueueing
					if gep.reportQueueSize != nil {
						gep.reportQueueSize()
					}
				case <-ctx.Done():
					return ctx.Err()
				default:
					symbol := router.GetSymbolFromData(extractedData)
					logger.Errorf("CRITICAL: Update channel full (%d/%d), DROPPING request for router %s, symbol %s",
						len(gep.updateChan), cap(gep.updateChan), result.RouterID, symbol)
				}
			}
		} else {
			logger.Debugf("Router %s skipped event %s: %s", result.RouterID, event.EventName, result.Reason)
		}
	}

	if routersUsed == 0 {
		logger.Debugf("No routers handled event %s", event.EventName)
	}

	// Collect all routing destinations to generate composite IntentHashes
	var routingDestinations []string
	for _, result := range routingResults {
		if result.Routed {
			for _, dest := range result.Destinations {
				routingDestinations = append(routingDestinations, fmt.Sprintf("%d-%s", dest.ChainID, dest.Contract))
			}
		}
	}

	// If no routing destinations, use a single generic destination ID
	if len(routingDestinations) == 0 {
		routingDestinations = append(routingDestinations, "no-routing")
	}

	// Create ProcessedEvent records for each routing destination
	for _, destID := range routingDestinations {
		// Create composite IntentHash
		hashInput := fmt.Sprintf("0x%x-%s-%s", event.IntentHash, eventID, destID)
		hash := sha256.Sum256([]byte(hashInput))
		compositeIntentHash := fmt.Sprintf("0x%x", hash)

		// Check deduplication for this specific destination
		if gep.dedupCache.Has(compositeIntentHash) {
			atomic.AddUint64(&gep.stats.EventsDuplicate, 1)
			logger.Debugf("Event already in cache for destination %s: %s", destID, compositeIntentHash)
			continue
		}

		processed, err := gep.db.IsEventProcessed(compositeIntentHash)
		if err != nil {
			logger.Errorf("Failed to check processed status for %s: %v", compositeIntentHash, err)
			continue
		}
		if processed {
			gep.dedupCache.Add(compositeIntentHash)
			atomic.AddUint64(&gep.stats.EventsDuplicate, 1)
			logger.Debugf("Event already processed for destination %s: %s", destID, compositeIntentHash)
			continue
		}

		processedEvent := &database.ProcessedEvent{
			EventID:         eventID,
			EventName:       event.EventName,
			IntentHash:      compositeIntentHash,
			BlockNumber:     event.BlockNumber,
			TransactionHash: event.TxHash.Hex(),
			LogIndex:        event.LogIndex,
			ProcessedAt:     time.Now(),
		}

		if symbol, ok := extractedData.Event["symbol"].(string); ok {
			processedEvent.Symbol = symbol
		}

		if priceValue, ok := extractedData.Event["price"]; ok {
			processedEvent.Price = parsePrice(priceValue)
		}

		if timestampValue, ok := extractedData.Event["timestamp"]; ok {
			processedEvent.Timestamp = parseTimestamp(timestampValue)
		}

		logger.Infof("Saving ProcessedEvent with composite IntentHash: %s (len=%d) for destination: %s", compositeIntentHash, len(compositeIntentHash), destID)
		if err := gep.db.SaveProcessedEvent(processedEvent); err != nil {
			logger.Errorf("Failed to save processed event for destination %s: %v", destID, err)
			continue
		}

		gep.dedupCache.Add(compositeIntentHash)
		logger.Debugf("Added composite IntentHash to dedup cache: %s", compositeIntentHash)
	}

	gep.stats.LastProcessedTime = time.Now()
	return nil
}

// statsReporter periodically reports statistics
func (gep *GenericEventProcessor) statsReporter(ctx context.Context) {
	defer gep.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gep.stopChan:
			return
		case <-ticker.C:
			logger.Infof("Event processor stats: received=%d, processed=%d, duplicate=%d, failed=%d, updates=%d",
				gep.stats.EventsReceived,
				gep.stats.EventsProcessed,
				gep.stats.EventsDuplicate,
				gep.stats.EventsFailed,
				gep.stats.UpdatesCreated,
			)
		}
	}
}

// GetStats returns processor statistics
func (gep *GenericEventProcessor) GetStats() types.ProcessorStats {
	return gep.stats
}

// eventDispatcher feeds events from the main channel to the worker pool
func (gep *GenericEventProcessor) eventDispatcher(ctx context.Context) {
	defer gep.wg.Done()

	logger.Info("Starting event dispatcher for parallel processing")

	for {
		select {
		case <-ctx.Done():
			return
		case <-gep.stopChan:
			return
		case event := <-gep.eventChan:
			if event == nil {
				continue
			}

			// Update main stats
			atomic.AddUint64(&gep.stats.EventsReceived, 1)
			if gep.metricsCollector != nil {
				gep.metricsCollector.IncEventsReceived()
			}

			// Submit to worker pool for parallel processing
			if err := gep.eventWorkerPool.SubmitEvent(event); err != nil {
				logger.Warnf("Failed to submit event to worker pool: %v", err)
			}
		}
	}
}

// ProcessEvent implements EventProcessor interface for the worker pool
func (gep *GenericEventProcessor) ProcessEvent(ctx context.Context, event *types.EventData) error {
	if err := gep.processEvent(ctx, event); err != nil {
		atomic.AddUint64(&gep.stats.EventsFailed, 1)
		if gep.metricsCollector != nil {
			gep.metricsCollector.IncEventsFailed()
		}
		return err
	}

	atomic.AddUint64(&gep.stats.EventsProcessed, 1)
	if gep.metricsCollector != nil {
		gep.metricsCollector.IncEventsProcessed()
	}
	return nil
}

// handleEnrichment handles event enrichment with optional parallel processing
func (gep *GenericEventProcessor) handleEnrichment(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	eventDef, exists := gep.eventDefs[eventName]
	if !exists || eventDef.Enrichment == nil {
		return nil // No enrichment needed
	}

	if gep.useParallelPipeline && gep.parallelPipeline != nil {
		return gep.enrichParallel(ctx, eventName, extractedData)
	}

	return gep.enricher.EnrichEventData(ctx, eventName, extractedData)
}

// enrichParallel performs enrichment using the parallel pipeline
func (gep *GenericEventProcessor) enrichParallel(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	enrichCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	dummyEvent := &types.EventData{
		EventName: eventName,
	}

	result, err := gep.parallelPipeline.ProcessEventParallel(enrichCtx, dummyEvent, extractedData)
	if err != nil {
		logger.Warnf("Parallel enrichment failed, falling back to sequential: %v", err)
		return gep.enricher.EnrichEventData(ctx, eventName, extractedData)
	}

	if result.EnrichmentError != nil {
		return result.EnrichmentError
	}

	logger.Debugf("Parallel enrichment completed in %v", result.ProcessingTime)
	return nil
}

// parsePrice converts a price value from an interface to a string representation.
func parsePrice(priceValue interface{}) string {
	if priceValue == nil {
		return "0"
	}

	switch v := priceValue.(type) {
	case *big.Int:
		return v.String()
	case string:
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			if bigInt, success := new(big.Int).SetString(v, 0); success {
				return bigInt.String()
			} else {
				logger.Warnf("Failed to parse hex price value: %s", v)
				return "0"
			}
		} else {
			return v
		}
	default:
		valueStr := fmt.Sprintf("%v", v)
		if strings.HasPrefix(valueStr, "0x") || strings.HasPrefix(valueStr, "0X") {
			if bigInt, success := new(big.Int).SetString(valueStr, 0); success {
				return bigInt.String()
			} else {
				logger.Warnf("Failed to parse default case hex price value: %s", valueStr)
				return "0"
			}
		} else {
			return valueStr
		}
	}
}

// parseTimestamp converts a timestamp value from an interface to a uint64.
func parseTimestamp(timestampValue interface{}) uint64 {
	if timestampValue == nil {
		return 0
	}

	switch v := timestampValue.(type) {
	case uint64:
		return v
	case *big.Int:
		return v.Uint64()
	case float64:
		return uint64(v)
	case float32:
		return uint64(v)
	case string:
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			if bigInt, success := new(big.Int).SetString(v, 0); success {
				return bigInt.Uint64()
			} else {
				logger.Warnf("Failed to parse hex timestamp value: %s", v)
				return 0
			}
		} else {
			if ts, err := strconv.ParseUint(v, 10, 64); err == nil {
				return ts
			} else {
				logger.Warnf("Failed to parse timestamp string: %s", v)
				return 0
			}
		}
	default:
		valueStr := fmt.Sprintf("%v", v)
		if strings.HasPrefix(valueStr, "0x") || strings.HasPrefix(valueStr, "0X") {
			if bigInt, success := new(big.Int).SetString(valueStr, 0); success {
				return bigInt.Uint64()
			} else {
				logger.Warnf("Failed to parse default case hex timestamp value: %s", valueStr)
				return 0
			}
		} else {
			if ts, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
				return ts
			} else {
				logger.Warnf("Failed to parse default case timestamp: %s", valueStr)
				return 0
			}
		}
	}
}
