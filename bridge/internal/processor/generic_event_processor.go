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

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/pipeline"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/bridge/pkg/router"
)

// GenericEventProcessor processes events using the generic pipeline
type GenericEventProcessor struct {
	config          *config.EventProcessorConfig
	eventDefs       map[string]*config.EventDefinition
	destinations    map[int64]*config.DestinationConfig
	db              *database.DB
	routerRegistry  *router.GenericRegistry
	sourceClient    *ethclient.Client
	destClients     map[int64]*ethclient.Client
	
	extractor       *pipeline.DataExtractor
	enricher        *pipeline.DataEnricher
	transformer     *pipeline.DataTransformer
	txBuilder       *pipeline.TransactionBuilder
	
	eventChan       <-chan *types.EventData
	errorChan       chan<- error
	updateChan      chan<- *types.UpdateRequest
	
	dedupCache      *DedupCache
	metricsCollector *metrics.Collector
	
	stats           types.ProcessorStats
	
	// Event processing worker pool
	eventWorkerPool  *EventWorkerPool
	useParallelMode  bool
	
	// Parallel pipeline processing
	parallelPipeline *ParallelPipeline
	useParallelPipeline bool
	
	stopChan        chan struct{}
	wg              sync.WaitGroup
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
	
	txBuilder, err := pipeline.NewTransactionBuilder(destClients)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction builder: %w", err)
	}
	
	gep := &GenericEventProcessor{
		config:           cfg,
		eventDefs:        eventDefs,
		destinations:     destinations,
		db:               db,
		routerRegistry:   routerRegistry,
		sourceClient:     sourceClient,
		destClients:      destClients,
		extractor:        extractor,
		enricher:         enricher,
		transformer:      transformer,
		txBuilder:        txBuilder,
		eventChan:        eventChan,
		errorChan:        errorChan,
		updateChan:       updateChan,
		dedupCache:       NewDedupCache(cfg.DedupCacheSize, cfg.DedupCacheTTL.Duration()),
		metricsCollector: metricsCollector,
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
	
	// Note: We'll handle deduplication after routing to check against composite IntentHashes
	// that include destination information for proper uniqueness across multiple destinations
	
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
			
			// Process destinations for this router
			for _, dest := range result.Destinations {
				destConfig, exists := gep.destinations[dest.ChainID]
				if !exists || destConfig == nil {
					logger.Warnf("Destination config not found for chain %d", dest.ChainID)
					continue
				}
				
				// Find contract config
				var contractConfig *config.ContractConfig
				for i := range destConfig.Contracts {
					if destConfig.Contracts[i].Address == dest.Contract {
						contractConfig = &destConfig.Contracts[i]
						break
					}
				}
				
				if contractConfig == nil {
					logger.Warnf("Contract config not found for %s", dest.Contract)
					continue
				}
				
				// Create update request using the router's method configuration
				updateReq := &types.UpdateRequest{
					ID:        fmt.Sprintf("%s-%s-%d", result.RouterID, event.EventName, time.Now().Unix()),
					Event:     event,
					DestinationChain: destConfig,
					Contract:         contractConfig,
					Priority:         1,
					Retries:          0,
					CreatedAt:        time.Now(),
					RouterID:         result.RouterID,
					DestinationMethodConfig: &dest.Method,
					ExtractedData:    extractedData,
				}
				
				// For IntArraySet events, create a minimal Intent structure for compatibility
				if event.EventName == "IntArraySet" && updateReq.Intent == nil {
					updateReq.Intent = &types.OracleIntent{
						Symbol:    fmt.Sprintf("RandomRequest-%s", event.RequestId.String()),
						Signer:    common.Address{}, // No signer required for randomness
						Expiry:    big.NewInt(time.Now().Add(24*time.Hour).Unix()), // 24h expiry
					}
				}
				
				select {
				case gep.updateChan <- updateReq:
					routersUsed++
					logger.Infof("Queued update for event %s via router %s to chain %d contract %s", 
						event.EventName, result.RouterID, dest.ChainID, dest.Contract)
				case <-ctx.Done():
					return ctx.Err()
				default:
					logger.Warnf("Update channel full, dropping request for router %s", result.RouterID)
				}
			}
		} else {
			logger.Debugf("Router %s skipped event %s: %s", result.RouterID, event.EventName, result.Reason)
		}
	}
	
	if routersUsed == 0 {
		logger.Debugf("No routers handled event %s", event.EventName)
	}
	
	// For events with multiple routing destinations, we need to save separate ProcessedEvent records
	// for each destination to avoid constraint violations. We'll create a composite IntentHash 
	// that includes the original IntentHash + destination info for uniqueness.
	
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
		// Create composite IntentHash: hash(originalIntentHash + eventID + destID + contractAddress)
		// Use SHA256 to ensure it fits in VARCHAR(66) database column and includes destination contract
		hashInput := fmt.Sprintf("0x%x-%s-%s", event.IntentHash, eventID, destID)
		hash := sha256.Sum256([]byte(hashInput))
		compositeIntentHash := fmt.Sprintf("0x%x", hash)
		logger.Infof("Generated composite IntentHash - Input: %s (len=%d), Hash: %s (len=%d)", hashInput, len(hashInput), compositeIntentHash, len(compositeIntentHash))
		
		// Check deduplication for this specific destination
		if gep.dedupCache.Has(compositeIntentHash) {
			atomic.AddUint64(&gep.stats.EventsDuplicate, 1)
			logger.Debugf("Event already in cache for destination %s: %s", destID, compositeIntentHash)
			continue
		}
		
		// Check if this specific composite IntentHash was already processed
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
		
		if priceValue, ok := extractedData.Event["price"]; ok && priceValue != nil {
			logger.Infof("Processing price value: %v (type: %T)", priceValue, priceValue)
			switch v := priceValue.(type) {
			case *big.Int:
				processedEvent.Price = v.String()
			case string:
				// Handle hex strings by converting to big.Int first, then to decimal string
				if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
					logger.Infof("Converting hex price value %s to decimal", v)
					if bigInt, success := new(big.Int).SetString(v, 0); success {
						processedEvent.Price = bigInt.String()
						logger.Infof("Successfully converted hex %s to decimal %s", v, processedEvent.Price)
					} else {
						logger.Warnf("Failed to parse hex price value: %s", v)
						processedEvent.Price = "0"
					}
				} else {
					processedEvent.Price = v
				}
			default:
				// Convert any other type to string, handle hex values
				valueStr := fmt.Sprintf("%v", v)
				if strings.HasPrefix(valueStr, "0x") || strings.HasPrefix(valueStr, "0X") {
					logger.Infof("Converting default case hex price value %s to decimal", valueStr)
					if bigInt, success := new(big.Int).SetString(valueStr, 0); success {
						processedEvent.Price = bigInt.String()
						logger.Infof("Successfully converted default case hex %s to decimal %s", valueStr, processedEvent.Price)
					} else {
						logger.Warnf("Failed to parse default case hex price value: %s", valueStr)
						processedEvent.Price = "0"
					}
				} else {
					processedEvent.Price = valueStr
				}
			}
		} else {
			processedEvent.Price = "0"
		}
		
		// Handle timestamp field
		if timestampValue, ok := extractedData.Event["timestamp"]; ok && timestampValue != nil {
			logger.Infof("Processing timestamp value: %v (type: %T)", timestampValue, timestampValue)
			switch v := timestampValue.(type) {
			case uint64:
				processedEvent.Timestamp = v
			case *big.Int:
				processedEvent.Timestamp = v.Uint64()
			case string:
				if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
					logger.Infof("Converting hex timestamp value %s to uint64", v)
					if bigInt, success := new(big.Int).SetString(v, 0); success {
						processedEvent.Timestamp = bigInt.Uint64()
						logger.Infof("Successfully converted hex timestamp %s to uint64 %d", v, processedEvent.Timestamp)
					} else {
						logger.Warnf("Failed to parse hex timestamp value: %s", v)
						processedEvent.Timestamp = 0
					}
				} else {
					if ts, err := strconv.ParseUint(v, 10, 64); err == nil {
						processedEvent.Timestamp = ts
					} else {
						logger.Warnf("Failed to parse timestamp string: %s", v)
						processedEvent.Timestamp = 0
					}
				}
			default:
				// Convert any other type, handle hex values
				valueStr := fmt.Sprintf("%v", v)
				if strings.HasPrefix(valueStr, "0x") || strings.HasPrefix(valueStr, "0X") {
					logger.Infof("Converting default case hex timestamp value %s to uint64", valueStr)
					if bigInt, success := new(big.Int).SetString(valueStr, 0); success {
						processedEvent.Timestamp = bigInt.Uint64()
						logger.Infof("Successfully converted default case hex timestamp %s to uint64 %d", valueStr, processedEvent.Timestamp)
					} else {
						logger.Warnf("Failed to parse default case hex timestamp value: %s", valueStr)
						processedEvent.Timestamp = 0
					}
				} else {
					if ts, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
						processedEvent.Timestamp = ts
					} else {
						logger.Warnf("Failed to parse default case timestamp: %s", valueStr)
						processedEvent.Timestamp = 0
					}
				}
			}
		} else {
			processedEvent.Timestamp = 0
		}
		
		logger.Infof("Saving ProcessedEvent with composite IntentHash: %s (len=%d) for destination: %s", compositeIntentHash, len(compositeIntentHash), destID)
		if err := gep.db.SaveProcessedEvent(processedEvent); err != nil {
			logger.Errorf("Failed to save processed event for destination %s: %v", destID, err)
			continue
		}
		
		// Use composite IntentHash for dedup cache as well
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
				// Continue processing to avoid blocking
			}
		}
	}
}

// ProcessEvent implements EventProcessor interface for the worker pool
// This method will be called by worker pool workers in parallel
func (gep *GenericEventProcessor) ProcessEvent(ctx context.Context, event *types.EventData) error {
	// This is the same logic as processEvent but called from worker pool
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
	
	// Use parallel enrichment if enabled, otherwise fall back to sequential
	if gep.useParallelPipeline && gep.parallelPipeline != nil {
		return gep.enrichParallel(ctx, eventName, extractedData)
	}
	
	// Sequential enrichment (original implementation)
	return gep.enricher.EnrichEventData(ctx, eventName, extractedData)
}

// enrichParallel performs enrichment using the parallel pipeline
func (gep *GenericEventProcessor) enrichParallel(ctx context.Context, eventName string, extractedData *config.ExtractedData) error {
	// Create a timeout context for parallel enrichment
	enrichCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Shorter timeout for parallel
	defer cancel()
	
	// Create a dummy event for the parallel pipeline (we only need enrichment)
	dummyEvent := &types.EventData{
		EventName: eventName,
		// Other fields aren't needed for enrichment
	}
	
	result, err := gep.parallelPipeline.ProcessEventParallel(enrichCtx, dummyEvent, extractedData)
	if err != nil {
		// Fall back to sequential processing
		logger.Warnf("Parallel enrichment failed, falling back to sequential: %v", err)
		return gep.enricher.EnrichEventData(ctx, eventName, extractedData)
	}
	
	// The enrichment result is already stored in extractedData
	if result.EnrichmentError != nil {
		return result.EnrichmentError
	}
	
	logger.Debugf("Parallel enrichment completed in %v", result.ProcessingTime)
	return nil
}


