package bridge

import (
	"fmt"
	"math/big"
	"sync"
	"time"

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

// cleanupLifecycle removes old lifecycle data after a delay
func (mt *MetricsTracker) cleanupLifecycle(intentHash string, delay time.Duration) {
	time.Sleep(delay)

	mt.mu.Lock()
	delete(mt.lifecycles, intentHash)
	mt.mu.Unlock()

	logger.Debugf("Cleaned up lifecycle for intent: %s", intentHash)
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
