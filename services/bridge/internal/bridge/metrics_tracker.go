package bridge

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// MetricsTracker tracks intent lifecycle metrics
type MetricsTracker struct {
	collector *metrics.Collector

	// In-memory tracking of intent lifecycles
	mu         sync.RWMutex
	lifecycles map[string]*metrics.IntentLifecycle // intentHash -> lifecycle
}

// NewMetricsTracker creates a new metrics tracker
func NewMetricsTracker(collector *metrics.Collector) *MetricsTracker {
	return &MetricsTracker{
		collector:  collector,
		lifecycles: make(map[string]*metrics.IntentLifecycle),
	}
}

// StartIntentLifecycle starts tracking a new intent
func (mt *MetricsTracker) StartIntentLifecycle(intent *bridgetypes.OracleIntent, sourceChain string) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := fmt.Sprintf("%x", getIntentHash(intent))
	intentTime := intent.GetTimestamp()

	// Record intent creation
	mt.collector.IntentMetrics.RecordIntentCreated(
		intentHash,
		intent.Symbol,
		intent.Source,
		intentTime,
	)

	// Store lifecycle for tracking
	mt.mu.Lock()
	mt.lifecycles[intentHash] = &metrics.IntentLifecycle{
		IntentHash:  intentHash,
		Symbol:      intent.Symbol,
		SourceChain: sourceChain,
		IntentTime:  intentTime,
	}
	mt.mu.Unlock()

	logger.Debugf("Started tracking intent lifecycle: %s for %s", intentHash, intent.Symbol)
}

// RecordIntentRegistered records when an intent is registered on-chain
func (mt *MetricsTracker) RecordIntentRegistered(event *bridgetypes.IntentRegisteredEvent, chainID string) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := event.IntentHash.Hex()

	// Handle nil timestamp
	var registrationTime time.Time
	if event.Timestamp != nil {
		registrationTime = time.Unix(event.Timestamp.Int64(), 0)
	} else {
		registrationTime = time.Now()
	}

	mt.mu.Lock()
	lifecycle, exists := mt.lifecycles[intentHash]
	if !exists {
		// Create new lifecycle if we don't have it
		lifecycle = &metrics.IntentLifecycle{
			IntentHash:  intentHash,
			Symbol:      event.Symbol,
			SourceChain: chainID,
		}
		mt.lifecycles[intentHash] = lifecycle
	}
	lifecycle.RegistrationTime = registrationTime
	mt.mu.Unlock()

	mt.collector.IntentMetrics.RecordIntentRegistered(lifecycle)
	logger.Debugf("Recorded intent registration: %s at %v", intentHash, registrationTime)
}

// RecordIntentScanned records when an intent is detected by scanner
func (mt *MetricsTracker) RecordIntentScanned(event *bridgetypes.EventData, scannerType string) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := common.BytesToHash(event.IntentHash[:]).Hex()
	scanTime := time.Now()

	priority := "normal"
	if event.Priority > 1 {
		priority = "high"
	}

	mt.mu.Lock()
	lifecycle, exists := mt.lifecycles[intentHash]
	if !exists {
		lifecycle = &metrics.IntentLifecycle{
			IntentHash: intentHash,
			Symbol:     event.Symbol,
		}
		mt.lifecycles[intentHash] = lifecycle
	}
	lifecycle.ScanTime = scanTime
	lifecycle.ScannerType = scannerType
	lifecycle.Priority = priority
	mt.mu.Unlock()

	mt.collector.IntentMetrics.RecordIntentScanned(lifecycle)
	logger.Debugf("Recorded intent scan: %s by %s scanner", intentHash, scannerType)
}

// RecordIntentProcessing records when intent processing starts
func (mt *MetricsTracker) RecordIntentProcessing(intent *bridgetypes.OracleIntent, routerID string) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := fmt.Sprintf("%x", getIntentHash(intent))
	processingTime := time.Now()

	mt.mu.Lock()
	lifecycle, exists := mt.lifecycles[intentHash]
	if !exists {
		lifecycle = &metrics.IntentLifecycle{
			IntentHash: intentHash,
			Symbol:     intent.Symbol,
		}
		mt.lifecycles[intentHash] = lifecycle
	}
	lifecycle.ProcessingTime = processingTime
	lifecycle.RouterID = routerID
	mt.mu.Unlock()

	mt.collector.IntentMetrics.RecordIntentProcessing(lifecycle)
}

// RecordIntentSubmitted records when a transaction is submitted
func (mt *MetricsTracker) RecordIntentSubmitted(intent *bridgetypes.OracleIntent, destChainID string, txHash string, gasPrice *big.Int) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := fmt.Sprintf("%x", getIntentHash(intent))
	submissionTime := time.Now()

	mt.mu.Lock()
	lifecycle, exists := mt.lifecycles[intentHash]
	if !exists {
		lifecycle = &metrics.IntentLifecycle{
			IntentHash: intentHash,
			Symbol:     intent.Symbol,
		}
		mt.lifecycles[intentHash] = lifecycle
	}
	lifecycle.SubmissionTime = submissionTime
	lifecycle.DestinationChain = destChainID
	lifecycle.TxHash = txHash
	if gasPrice != nil {
		// Convert to gwei
		gasPriceGwei := new(big.Float).SetInt(gasPrice)
		gasPriceGwei.Quo(gasPriceGwei, big.NewFloat(1e9))
		lifecycle.GasPrice, _ = gasPriceGwei.Float64()
	}
	mt.mu.Unlock()

	mt.collector.IntentMetrics.RecordIntentSubmitted(lifecycle)
	logger.Debugf("Recorded intent submission: %s tx=%s", intentHash, txHash)
}

// RecordIntentConfirmed records when a transaction is confirmed
func (mt *MetricsTracker) RecordIntentConfirmed(intent *bridgetypes.OracleIntent, txHash string, gasUsed uint64) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	intentHash := fmt.Sprintf("%x", getIntentHash(intent))
	confirmationTime := time.Now()

	mt.mu.Lock()
	lifecycle, exists := mt.lifecycles[intentHash]
	if !exists {
		logger.Warnf("No lifecycle found for confirmed intent: %s", intentHash)
		return
	}
	lifecycle.ConfirmationTime = confirmationTime
	mt.mu.Unlock()

	mt.collector.IntentMetrics.RecordIntentConfirmed(lifecycle, gasUsed)

	// Calculate and log total latency
	if !lifecycle.IntentTime.IsZero() {
		totalLatency := confirmationTime.Sub(lifecycle.IntentTime)
		logger.Infof("Intent %s completed end-to-end in %v", intentHash, totalLatency)
	}

	// Clean up old lifecycles after some time
	go mt.cleanupLifecycle(intentHash, 5*time.Minute)
}

// RecordIntentFailed records when an intent fails
func (mt *MetricsTracker) RecordIntentFailed(intent *bridgetypes.OracleIntent, stage, errorType string) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	mt.collector.IntentMetrics.RecordIntentFailed(intent.Symbol, stage, errorType)

	// Clean up lifecycle
	intentHash := fmt.Sprintf("%x", getIntentHash(intent))
	go mt.cleanupLifecycle(intentHash, 1*time.Minute)
}

// RecordRouterDecision records a router decision
func (mt *MetricsTracker) RecordRouterDecision(routerID string, intent *bridgetypes.OracleIntent, decision bool, reason string, startTime time.Time) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil {
		return
	}

	latency := time.Since(startTime).Seconds()
	mt.collector.IntentMetrics.RecordRouterDecision(routerID, decision, reason, latency)
}

// RecordPriceDeviation records price deviation
func (mt *MetricsTracker) RecordPriceDeviation(symbol string, originalPrice, currentPrice *big.Int) {
	if mt.collector == nil || mt.collector.IntentMetrics == nil || originalPrice == nil || currentPrice == nil {
		return
	}

	// Calculate percentage deviation
	original := new(big.Float).SetInt(originalPrice)
	current := new(big.Float).SetInt(currentPrice)

	diff := new(big.Float).Sub(current, original)
	diff.Abs(diff)

	deviation := new(big.Float).Quo(diff, original)
	deviation.Mul(deviation, big.NewFloat(100))

	deviationPercent, _ := deviation.Float64()
	mt.collector.IntentMetrics.RecordPriceDeviation(symbol, deviationPercent)
}

// cleanupLifecycle removes old lifecycle data after a delay
func (mt *MetricsTracker) cleanupLifecycle(intentHash string, delay time.Duration) {
	time.Sleep(delay)

	mt.mu.Lock()
	delete(mt.lifecycles, intentHash)
	mt.mu.Unlock()

	logger.Debugf("Cleaned up lifecycle for intent: %s", intentHash)
}

// GetLifecycleStats returns current lifecycle statistics
func (mt *MetricsTracker) GetLifecycleStats() map[string]interface{} {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	stats := map[string]interface{}{
		"active_lifecycles": len(mt.lifecycles),
		"symbols":           make(map[string]int),
	}

	// Count by symbol
	symbolCounts := stats["symbols"].(map[string]int)
	for _, lifecycle := range mt.lifecycles {
		symbolCounts[lifecycle.Symbol]++
	}

	return stats
}

// Helper function to compute intent hash
func getIntentHash(intent *bridgetypes.OracleIntent) []byte {
	// This should match the actual intent hash computation
	// For now, use a simple hash of key fields
	data := fmt.Sprintf("%s-%s-%s-%s-%s",
		intent.Symbol,
		intent.Price.String(),
		intent.Timestamp.String(),
		intent.Nonce.String(),
		intent.Signer.Hex(),
	)
	return []byte(data)[:32] // Simplified - use proper hashing in production
}
