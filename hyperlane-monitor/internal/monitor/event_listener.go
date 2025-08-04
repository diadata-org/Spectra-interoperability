package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/blockchain"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/database"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

type EventListener struct {
	pair            *database.MonitoringPair
	receivers       map[string]*types.ReceiverConfig
	sourceClient    *blockchain.ChainClient
	db              *database.Repository
	decoder         *blockchain.HyperlaneMessageDecoder
	metrics         *metrics.Metrics
	lastBlock       uint64
	scanInterval    time.Duration
	
	// Head tracking for real-time processing
	headBlock      uint64
	lastHeadUpdate time.Time
	headTracking   bool
}

func NewEventListener(
	pair *database.MonitoringPair,
	receivers []types.ReceiverConfig,
	sourceClient *blockchain.ChainClient,
	db *database.Repository,
	serviceMetrics *metrics.Metrics,
	scanInterval time.Duration,
) (*EventListener, error) {
	decoder, err := blockchain.NewHyperlaneMessageDecoder()
	if err != nil {
		return nil, fmt.Errorf("failed to create message decoder: %w", err)
	}

	receiverMap := make(map[string]*types.ReceiverConfig)
	for i := range receivers {
		receiverMap[receivers[i].Address] = &receivers[i]
	}

	return &EventListener{
		pair:         pair,
		receivers:    receiverMap,
		sourceClient: sourceClient,
		db:           db,
		decoder:      decoder,
		metrics:      serviceMetrics,
		lastBlock:    pair.LastProcessedBlock,
		scanInterval: scanInterval,
	}, nil
}

func (l *EventListener) Start(ctx context.Context) error {
	logger.WithFields(logger.Fields{
		"pair_id":     l.pair.PairID,
		"source":      l.pair.SourceChainName,
		"destination": l.pair.DestinationChainName,
		"start_block": l.lastBlock,
	}).Info("Starting event listener")

	// Get current block to determine if we should enable head tracking
	currentBlock, err := l.sourceClient.GetLatestBlock(ctx)
	if err == nil {
		// Enable head tracking if we're far behind
		if currentBlock > l.lastBlock && currentBlock-l.lastBlock > 1000 {
			l.headTracking = true
			l.headBlock = currentBlock
			l.lastHeadUpdate = time.Now()
			
			// Start head tracker goroutine for real-time processing
			go l.headTrackerLoop(ctx)
			
			logger.WithFields(logger.Fields{
				"pair_id": l.pair.PairID,
				"head_block": currentBlock,
				"last_block": l.lastBlock,
				"blocks_behind": currentBlock - l.lastBlock,
			}).Info("Head tracking enabled - scanning from head backwards")
		}
	}

	ticker := time.NewTicker(l.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Event listener stopping")
			return ctx.Err()
		case <-ticker.C:
			if err := l.scanForEvents(ctx); err != nil {
				logger.WithError(err).Error("Failed to scan for events")
			}
		}
	}
}

// scanForEvents scans for new MessageDispatched events
func (l *EventListener) scanForEvents(ctx context.Context) error {
	// Get current block
	currentBlock, err := l.sourceClient.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	// Don't scan if we're already caught up
	if l.lastBlock >= currentBlock {
		return nil
	}

	// Limit scan range to avoid timeouts
	fromBlock := l.lastBlock + 1
	toBlock := fromBlock + 1000 // Max 1000 blocks per scan
	if toBlock > currentBlock {
		toBlock = currentBlock
	}

	logger.Debugf("Scanning blocks %d to %d on %s", fromBlock, toBlock, l.pair.SourceChainName)

	// Filter for MessageDispatched events
	triggerAddr := common.HexToAddress(l.pair.OracleTriggerAddress)
	events, err := l.sourceClient.FilterMessageDispatchedEvents(ctx, triggerAddr, fromBlock, toBlock)
	if err != nil {
		l.metrics.RecordRPCError(fmt.Sprintf("%d", l.pair.SourceChainID), "filter_events")
		return fmt.Errorf("failed to filter events: %w", err)
	}

	// Process each event
	for _, event := range events {
		l.metrics.RecordEventDetected()
		startTime := time.Now()
		
		logger.WithFields(logger.Fields{
			"message_id": event.MessageId.Hex(),
			"tx_hash":    event.Raw.TxHash.Hex(),
			"recipient":  event.RecipientAddress.Hex(),
			"chain_id":   event.ChainId,
			"intent_hash": event.IntentHash.Hex(),
			"symbol":     event.Symbol,
		}).Debug("Processing MessageDispatched event")
		
		if err := l.processMessageDispatchedEvent(ctx, &event); err != nil {
			l.metrics.RecordEventProcessingError()
			logger.WithError(err).WithFields(logger.Fields{
				"message_id": event.MessageId.Hex(),
				"tx_hash":    event.Raw.TxHash.Hex(),
			}).Error("Failed to process event")
			// Continue processing other events
		} else {
			l.metrics.RecordEventProcessed(time.Since(startTime).Seconds())
			
			// Record detection phase metric
			detectionDuration := time.Since(startTime).Seconds()
			l.metrics.RecordTimelinePhase("detection", detectionDuration,
				fmt.Sprintf("%d", l.pair.SourceChainID),
				fmt.Sprintf("%d", l.pair.SourceChainID),
				fmt.Sprintf("%d", l.pair.DestinationChainID))
		}
	}

	// Update last processed block
	l.lastBlock = toBlock
	if err := l.db.UpdateLastProcessedBlock(l.pair.PairID, toBlock); err != nil {
		logger.WithError(err).Error("Failed to update last processed block")
	}

	if len(events) > 0 {
		logger.WithFields(logger.Fields{
			"pair_id": l.pair.PairID,
			"events":  len(events),
			"blocks":  fmt.Sprintf("%d-%d", fromBlock, toBlock),
		}).Info("Processed MessageDispatched events")
	}

	return nil
}

// processMessageDispatchedEvent processes a single MessageDispatched event
func (l *EventListener) processMessageDispatchedEvent(ctx context.Context, event *blockchain.MessageDispatchedEvent) error {
	// Log all configured receivers for debugging
	logger.WithFields(logger.Fields{
		"configured_receivers": l.receivers,
		"event_recipient": event.RecipientAddress.Hex(),
		"pair_id": l.pair.PairID,
	}).Debug("Checking receiver configuration")
	
	// Check if this receiver is monitored (case-insensitive comparison)
	var receiverConfig *types.ReceiverConfig
	for addr, cfg := range l.receivers {
		logger.Debugf("Comparing %s with %s", addr, event.RecipientAddress.Hex())
		if strings.EqualFold(addr, event.RecipientAddress.Hex()) {
			receiverConfig = cfg
			break
		}
	}
	
	if receiverConfig == nil {
		logger.Debugf("Receiver %s not found in configured receivers for pair %s", event.RecipientAddress.Hex(), l.pair.PairID)
		return nil
	}
	
	if !receiverConfig.Enabled {
		logger.Debugf("Receiver %s found but not enabled in pair %s", event.RecipientAddress.Hex(), l.pair.PairID)
		return nil
	}

	// Now we have the intent hash and symbol directly from the event
	symbol := event.Symbol
	intentHash := event.IntentHash

	// Get the full intent data from the registry using the intent hash
	registryAddr := common.HexToAddress(l.pair.OracleRegistryAddress)
	intent, err := l.sourceClient.GetOracleIntent(ctx, registryAddr, intentHash)
	if err != nil {
		return fmt.Errorf("failed to get intent for hash %s: %w", intentHash.Hex(), err)
	}

	logger.WithFields(logger.Fields{
		"message_id": event.MessageId.Hex(),
		"symbol": symbol,
		"intent_hash": intentHash.Hex(),
		"price": intent.Price.String(),
	}).Info("Processing MessageDispatched event with intent data")

	// Create message record
	now := time.Now()
	nextCheckAt := now.Add(receiverConfig.InitialWait)
	
	msg := &database.HyperlaneMessage{
		MessageID:          event.MessageId.Hex(),
		IntentHash:         intentHash.Hex(),
		PairID:             l.pair.PairID,
		SourceChainID:      l.pair.SourceChainID,
		SourceTxHash:       event.Raw.TxHash.Hex(),
		SourceBlockNumber:  event.Raw.BlockNumber,
		DestinationChainID: l.pair.DestinationChainID,
		ReceiverAddress:    receiverConfig.Address,
		ReceiverName:       receiverConfig.Name,
		Symbol:             symbol,
		Price:              intent.Price.String(),
		Timestamp:          intent.Timestamp.Int64(),
		IntentData:         database.JSONB{"intent": intent},
		Status:             types.StatusDispatched,
		Priority:           receiverConfig.Priority,
		NextCheckAt:        &nextCheckAt,
	}

	// Save to database
	if err := l.db.SaveMessage(msg); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	logger.WithFields(logger.Fields{
		"message_id": event.MessageId.Hex(),
		"intent_hash": intentHash.Hex(),
		"receiver":    receiverConfig.Name,
		"chain":       l.pair.DestinationChainName,
	}).Info("New Hyperlane message detected")

	return nil
}

// headTrackerLoop continuously monitors and processes new blocks in real-time
func (l *EventListener) headTrackerLoop(ctx context.Context) {
	logger.WithField("pair_id", l.pair.PairID).Info("[HEAD TRACKER] Starting head tracker for real-time block processing")
	
	// Use a shorter interval for head tracking
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	var lastProcessedHead uint64 = l.headBlock
	
	for {
		select {
		case <-ctx.Done():
			logger.Info("[HEAD TRACKER] Head tracker stopping")
			return
		case <-ticker.C:
			// Get current block
			currentBlock, err := l.sourceClient.GetLatestBlock(ctx)
			if err != nil {
				logger.WithError(err).Error("[HEAD TRACKER] Failed to get current block")
				continue
			}
			
			// Check if there are new blocks
			if currentBlock > lastProcessedHead {
				logger.WithFields(logger.Fields{
					"pair_id": l.pair.PairID,
					"from_block": lastProcessedHead + 1,
					"to_block": currentBlock,
				}).Info("[HEAD TRACKER] New blocks detected")
				
				// Process new blocks immediately
				startBlock := lastProcessedHead + 1
				endBlock := currentBlock
				
				// Limit batch size
				if endBlock-startBlock > 100 {
					endBlock = startBlock + 100
				}
				
				// Scan new blocks with highest priority
				triggerAddr := common.HexToAddress(l.pair.OracleTriggerAddress)
				events, err := l.sourceClient.FilterMessageDispatchedEvents(ctx, triggerAddr, startBlock, endBlock)
				if err != nil {
					logger.WithError(err).WithFields(logger.Fields{
						"from_block": startBlock,
						"to_block": endBlock,
					}).Error("[HEAD TRACKER] Failed to scan blocks")
					continue
				}
				
				// Process events immediately
				for _, event := range events {
					l.metrics.RecordEventDetected()
					startTime := time.Now()
					
					logger.WithFields(logger.Fields{
						"message_id": event.MessageId.Hex(),
						"tx_hash":    event.Raw.TxHash.Hex(),
						"block":      event.Raw.BlockNumber,
					}).Info("[HEAD TRACKER] Found MessageDispatched event")
					
					if err := l.processMessageDispatchedEvent(ctx, &event); err != nil {
						l.metrics.RecordEventProcessingError()
						logger.WithError(err).Error("[HEAD TRACKER] Failed to process event")
					} else {
						l.metrics.RecordEventProcessed(time.Since(startTime).Seconds())
					}
				}
				
				// Update head position
				l.headBlock = endBlock
				l.lastHeadUpdate = time.Now()
				lastProcessedHead = endBlock
				
				if len(events) > 0 {
					logger.WithFields(logger.Fields{
						"pair_id": l.pair.PairID,
						"events": len(events),
						"head_block": endBlock,
					}).Info("[HEAD TRACKER] Processed events from head")
				}
			}
		}
	}
}