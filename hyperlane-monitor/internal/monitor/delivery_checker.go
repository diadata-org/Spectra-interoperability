package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/blockchain"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/database"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/failover"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// DeliveryChecker checks if intents have been delivered to destination chains
type DeliveryChecker struct {
	db               *database.Repository
	destClients      map[int]*blockchain.ChainClient
	pairReceivers    map[string]map[string]*types.ReceiverConfig
	bridgeClient     *failover.BridgeClient
	metrics          *metrics.Metrics
	checkInterval    time.Duration
	batchSize        int
	mu               sync.RWMutex
}

// NewDeliveryChecker creates a new delivery checker
func NewDeliveryChecker(
	db *database.Repository,
	destClients map[int]*blockchain.ChainClient,
	bridgeClient *failover.BridgeClient,
	serviceMetrics *metrics.Metrics,
	checkInterval time.Duration,
) *DeliveryChecker {
	return &DeliveryChecker{
		db:            db,
		destClients:   destClients,
		pairReceivers: make(map[string]map[string]*types.ReceiverConfig),
		bridgeClient:  bridgeClient,
		metrics:       serviceMetrics,
		checkInterval: checkInterval,
		batchSize:     100,
	}
}

// AddPairReceivers adds receiver configurations for a monitoring pair
func (d *DeliveryChecker) AddPairReceivers(pairID string, receivers []types.ReceiverConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.pairReceivers[pairID] == nil {
		d.pairReceivers[pairID] = make(map[string]*types.ReceiverConfig)
	}

	for i := range receivers {
		d.pairReceivers[pairID][receivers[i].Address] = &receivers[i]
	}
}

// Start begins the delivery checking loop
func (d *DeliveryChecker) Start(ctx context.Context) error {
	logger.Info("Starting delivery checker")

	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	// Initial check immediately
	if err := d.checkDeliveries(ctx); err != nil {
		logger.WithError(err).Error("Initial delivery check failed")
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Delivery checker stopping")
			return ctx.Err()
		case <-ticker.C:
			if err := d.checkDeliveries(ctx); err != nil {
				logger.WithError(err).Error("Delivery check failed")
			}
		}
	}
}

// checkDeliveries checks pending messages for delivery
func (d *DeliveryChecker) checkDeliveries(ctx context.Context) error {
	// Get pending messages
	messages, err := d.db.GetPendingMessages(d.batchSize)
	if err != nil {
		return fmt.Errorf("failed to get pending messages: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	logger.Debugf("Checking delivery status for %d messages", len(messages))

	// Process messages concurrently but with a limit
	sem := make(chan struct{}, 10) // Max 10 concurrent checks
	var wg sync.WaitGroup

	for _, msg := range messages {
		wg.Add(1)
		go func(msg database.HyperlaneMessage) {
			defer wg.Done()
			
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := d.checkMessageDelivery(ctx, &msg); err != nil {
				logger.WithError(err).WithFields(logger.Fields{
					"message_id": msg.MessageID,
					"intent_hash": msg.IntentHash,
				}).Error("Failed to check message delivery")
			}
		}(msg)
	}

	wg.Wait()
	return nil
}

// checkMessageDelivery checks if a specific message has been delivered
func (d *DeliveryChecker) checkMessageDelivery(ctx context.Context, msg *database.HyperlaneMessage) error {
	startTime := time.Now()
	
	logger.WithFields(logger.Fields{
		"message_id": msg.MessageID,
		"intent_hash": msg.IntentHash,
		"receiver": msg.ReceiverAddress,
		"check_count": msg.DeliveryChecks + 1,
	}).Debug("Checking message delivery")
	
	// Get receiver configuration
	d.mu.RLock()
	receiverConfig := d.pairReceivers[msg.PairID][msg.ReceiverAddress]
	d.mu.RUnlock()

	if receiverConfig == nil {
		return fmt.Errorf("no receiver config found for %s in pair %s", msg.ReceiverAddress, msg.PairID)
	}

	// Get destination client
	destClient, exists := d.destClients[msg.DestinationChainID]
	if !exists {
		return fmt.Errorf("no client for destination chain %d", msg.DestinationChainID)
	}

	// Check if intent was processed
	intentHash := common.HexToHash(msg.IntentHash)
	receiverAddr := common.HexToAddress(msg.ReceiverAddress)
	
	logger.Debugf("Calling IsIntentProcessed for intent %s on receiver %s", intentHash.Hex(), receiverAddr.Hex())
	
	processed, err := destClient.IsIntentProcessed(ctx, receiverAddr, intentHash)
	if err != nil {
		// Network error - schedule retry
		d.metrics.RecordDeliveryCheck("error", time.Since(startTime).Seconds())
		d.metrics.RecordRPCError(fmt.Sprintf("%d", msg.DestinationChainID), "is_intent_processed")
		nextCheck := time.Now().Add(time.Minute)
		d.db.UpdateMessageCheck(msg.MessageID, nextCheck)
		return fmt.Errorf("failed to check intent status: %w", err)
	}

	logger.Debugf("Intent %s processed status: %v", intentHash.Hex(), processed)

	if processed {
		// Message was delivered!
		d.metrics.RecordDeliveryCheck("confirmed", time.Since(startTime).Seconds())
		d.metrics.RecordMessageAge(fmt.Sprintf("%d", msg.DestinationChainID), time.Since(msg.CreatedAt).Seconds())
		
		logger.WithFields(logger.Fields{
			"message_id":  msg.MessageID,
			"intent_hash": msg.IntentHash,
			"receiver":    msg.ReceiverName,
			"delivery_time": time.Since(msg.CreatedAt),
		}).Info("Message delivered via Hyperlane")

		return d.db.UpdateMessageDelivered(msg.MessageID)
	}

	// Not delivered yet - check if we should trigger failover
	timeSinceDispatch := time.Since(msg.CreatedAt)
	if timeSinceDispatch > receiverConfig.MaxDeliveryWait {
		// Trigger failover
		d.metrics.RecordDeliveryCheck("timeout", time.Since(startTime).Seconds())
		
		logger.WithFields(logger.Fields{
			"message_id":    msg.MessageID,
			"intent_hash":   msg.IntentHash,
			"receiver":      msg.ReceiverName,
			"wait_time":     timeSinceDispatch,
			"max_wait_time": receiverConfig.MaxDeliveryWait,
		}).Warn("Message delivery timeout - triggering failover")

		return d.triggerFailover(ctx, msg, receiverConfig)
	}

	// Still within delivery window - schedule next check
	d.metrics.RecordDeliveryCheck("pending", time.Since(startTime).Seconds())
	
	// Record wait phase duration
	waitDuration := time.Since(msg.CreatedAt).Seconds()
	d.metrics.RecordTimelinePhase("wait", waitDuration, 
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.DestinationChainID))
	
	nextCheck := d.calculateNextCheck(msg, receiverConfig)
	return d.db.UpdateMessageCheck(msg.MessageID, nextCheck)
}

// calculateNextCheck determines when to check again
func (d *DeliveryChecker) calculateNextCheck(msg *database.HyperlaneMessage, config *types.ReceiverConfig) time.Time {
	// Simple linear backoff for now
	// Could implement exponential backoff based on config
	baseInterval := config.CheckInterval
	
	// Increase interval based on number of checks
	multiplier := 1
	if msg.DeliveryChecks > 5 {
		multiplier = 2
	}
	if msg.DeliveryChecks > 10 {
		multiplier = 3
	}

	interval := time.Duration(multiplier) * baseInterval
	return time.Now().Add(interval)
}

// triggerFailover sends the message to Bridge service for direct delivery
func (d *DeliveryChecker) triggerFailover(ctx context.Context, msg *database.HyperlaneMessage, config *types.ReceiverConfig) error {
	startTime := time.Now()
	
	logger.WithFields(logger.Fields{
		"message_id": msg.MessageID,
		"intent_hash": msg.IntentHash,
		"msg_symbol": msg.Symbol,
		"msg_price": msg.Price,
		"msg_timestamp": msg.Timestamp,
	}).Info("Triggering failover for message with stored data")
	
	// Record wait phase completion and start of bridge processing
	waitDuration := time.Since(msg.CreatedAt).Seconds()
	d.metrics.RecordTimelinePhase("wait", waitDuration,
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.DestinationChainID))
	
	// Extract intent from JSONB - it's stored with key "intent"
	var intentData types.OracleIntent
	if msg.IntentData != nil {
		// IntentData is already a map[string]interface{} (JSONB type)
		if intentRaw, exists := msg.IntentData["intent"]; exists {
			// Marshal and unmarshal to convert to our struct
			intentBytes, err := json.Marshal(intentRaw)
			if err == nil {
				if err := json.Unmarshal(intentBytes, &intentData); err != nil {
					logger.WithError(err).Error("Failed to unmarshal intent data")
				} else {
					logger.WithFields(logger.Fields{
						"intent_type": intentData.IntentType,
						"symbol": intentData.Symbol,
						"price": intentData.Price,
						"signature_len": len(intentData.Signature),
					}).Info("Successfully extracted intent from JSONB")
				}
			}
		} else {
			logger.Warn("No 'intent' key found in JSONB data")
		}
	}
	
	// If we couldn't extract from JSONB, create from message fields
	if intentData.IntentType == "" {
		logger.Info("Creating intent from message fields as fallback")
		price := new(big.Int)
		if _, ok := price.SetString(msg.Price, 10); !ok {
			logger.Warnf("Failed to parse price %s, using 0", msg.Price)
			price = big.NewInt(0)
		}
		
		intentData = types.OracleIntent{
			IntentType: "PriceUpdate",
			Version:    "1.0",
			Symbol:     msg.Symbol,
			Price:      price,
			Timestamp:  big.NewInt(msg.Timestamp),
			ChainID:    big.NewInt(int64(msg.SourceChainID)),
			Nonce:      big.NewInt(0),
			Expiry:     big.NewInt(0),
			Source:     "hyperlane-failover",
			Signature:  []byte{},
			Signer:     common.Address{},
		}
	}
	
	// Debug log the unmarshaled data
	logger.WithFields(logger.Fields{
		"intent_type": intentData.IntentType,
		"symbol": intentData.Symbol,
		"price": intentData.Price,
		"chainId": intentData.ChainID,
		"signature_len": len(intentData.Signature),
		"signature_nil": intentData.Signature == nil,
		"signer": intentData.Signer.Hex(),
	}).Info("Unmarshaled intent data for failover")

	// Create failover request
	request := &types.FailoverRequest{
		MessageID:          msg.MessageID,
		IntentHash:         msg.IntentHash,
		PairID:             msg.PairID,
		SourceChainID:      msg.SourceChainID,
		DestinationChainID: msg.DestinationChainID,
		ReceiverAddress:    msg.ReceiverAddress,
		IntentData:         &intentData,
		Reason:             fmt.Sprintf("Hyperlane delivery timeout after %v", time.Since(msg.CreatedAt)),
	}
	
	// Log the full request object before sending
	logger.WithFields(logger.Fields{
		"intent_data_nil": request.IntentData == nil,
		"full_request": fmt.Sprintf("%+v", request),
	}).Info("Full failover request object before sending")

	// Send to Bridge API
	response, err := d.bridgeClient.TriggerFailover(ctx, request)
	if err != nil {
		d.metrics.RecordFailoverAttempt(false, time.Since(startTime).Seconds())
		return fmt.Errorf("failed to trigger failover: %w", err)
	}

	// Update message status
	if err := d.db.UpdateMessageFailover(msg.MessageID, response.RequestID); err != nil {
		d.metrics.RecordFailoverAttempt(false, time.Since(startTime).Seconds())
		return fmt.Errorf("failed to update message failover status: %w", err)
	}

	d.metrics.RecordFailoverAttempt(true, time.Since(startTime).Seconds())
	
	// Record bridge processing phase
	bridgeProcessingDuration := time.Since(startTime).Seconds()
	d.metrics.RecordTimelinePhase("bridge_processing", bridgeProcessingDuration,
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.SourceChainID),
		fmt.Sprintf("%d", msg.DestinationChainID))
	
	logger.WithFields(logger.Fields{
		"message_id":  msg.MessageID,
		"request_id":  response.RequestID,
		"intent_hash": msg.IntentHash,
		"receiver":    msg.ReceiverName,
	}).Info("Failover triggered successfully")

	// Send alert if configured
	if config.AlertOnFailure && config.AlertWebhook != "" {
		// TODO: Send webhook notification
		logger.Debugf("Would send alert to %s", config.AlertWebhook)
	}

	return nil
}