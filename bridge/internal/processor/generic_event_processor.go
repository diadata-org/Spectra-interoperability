package processor

import (
	"context"
	"fmt"
	"math/big"
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
	
	return &GenericEventProcessor{
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
	}, nil
}

// Start begins processing events
func (gep *GenericEventProcessor) Start(ctx context.Context) error {
	logger.Info("Starting generic event processor")
	
	gep.wg.Add(1)
	go gep.processLoop(ctx)
	
	gep.wg.Add(1)
	go gep.statsReporter(ctx)
	
	return nil
}

// Stop gracefully stops the processor
func (gep *GenericEventProcessor) Stop() error {
	logger.Info("Stopping generic event processor")
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
	
	if gep.dedupCache.Has(eventID) {
		atomic.AddUint64(&gep.stats.EventsDuplicate, 1)
		logger.Debugf("Event already in cache: %s", eventID)
		return nil
	}
	
	processed, err := gep.db.IsEventProcessed(eventID)
	if err != nil {
		return fmt.Errorf("failed to check event status: %w", err)
	}
	if processed {
		gep.dedupCache.Add(eventID)
		atomic.AddUint64(&gep.stats.EventsDuplicate, 1)
		logger.Debugf("Event already processed: %s", eventID)
		return nil
	}
	
	log, ok := event.Raw.(ethtypes.Log)
	if !ok {
		return fmt.Errorf("event.Raw is not of type types.Log")
	}
	extractedData, err := gep.extractor.ExtractEventData(event.EventName, log)
	if err != nil {
		return fmt.Errorf("failed to extract event data: %w", err)
	}
	
	logger.Debugf("Extracted data for %s: %+v", event.EventName, extractedData)
	
	eventDef, exists := gep.eventDefs[event.EventName]
	if exists && eventDef.Enrichment != nil {
		if err := gep.enricher.EnrichEventData(ctx, event.EventName, extractedData); err != nil {
			logger.Warnf("Failed to enrich event data: %v", err)
		} else {
			logger.Debugf("Enriched data: %+v", extractedData.Enrichment)
		}
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
	
	processedEvent := &database.ProcessedEvent{
		EventID:         eventID,
		EventName:       event.EventName,
		BlockNumber:     event.BlockNumber,
		TransactionHash: event.TxHash.Hex(),
		LogIndex:        event.LogIndex,
		ProcessedAt:     time.Now(),
	}
	
	if symbol, ok := extractedData.Event["symbol"].(string); ok {
		processedEvent.Symbol = symbol
	}
	
	if priceValue, ok := extractedData.Event["price"]; ok && priceValue != nil {
		switch v := priceValue.(type) {
		case *big.Int:
			processedEvent.Price = v.String()
		case string:
			processedEvent.Price = v
		default:
			processedEvent.Price = fmt.Sprintf("%v", v)
		}
	} else {
		processedEvent.Price = "0"
	}
	
	if err := gep.db.SaveProcessedEvent(processedEvent); err != nil {
		return fmt.Errorf("failed to save processed event: %w", err)
	}
	
	gep.dedupCache.Add(eventID)
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


