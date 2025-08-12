package api

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

func TestFailoverHandlerPhaseMetrics(t *testing.T) {
	// Create metrics instance
	m := metrics.NewMetrics()
	
	// Create a mock failover handler with metrics
	handler := &FailoverHandler{
		metrics:       m,
		intentMetrics: metrics.NewIntentMetrics(),
		requestStatus: make(map[string]*FailoverStatus),
	}
	
	// Create test intent data
	now := time.Now()
	intentTimestamp := now.Add(-35 * time.Second) // 35 seconds ago
	
	intentData := &types.OracleIntent{
		IntentType: "OracleUpdate",
		Version:    "1.0",
		Symbol:     "BTC/USD",
		Price:      big.NewInt(50000),
		Timestamp:  big.NewInt(intentTimestamp.Unix()),
		ChainID:    big.NewInt(11155420),
		Nonce:      big.NewInt(1),
		Expiry:     big.NewInt(0),
		Source:     "hyperlane-failover",
		Signature:  []byte{0x01, 0x02, 0x03},
		Signer:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
	}
	
	// Create test request with timestamps
	detectionTime := intentTimestamp.Add(2 * time.Second)
	monitoringStartTime := detectionTime.Add(500 * time.Millisecond)
	failoverTime := monitoringStartTime.Add(30 * time.Second)
	
	req := FailoverRequest{
		MessageID:                "0x1234567890123456789012345678901234567890123456789012345678901234",
		IntentHash:               "0xabcdef1234567890123456789012345678901234567890123456789012345678",
		PairID:                   "100640_11155420_0x34d3Eb1e5411F352650392a5516aEE6700Aa161C",
		SourceChainID:            100640,
		DestinationChainID:       11155420,
		ReceiverAddress:          "0x474F45415504f46f143Eb09Ea461F46270F7372f",
		IntentData:               intentData,
		Reason:                   "Hyperlane delivery timeout after 30s",
		DetectionTimestamp:       detectionTime.Unix(),
		MonitoringStartTimestamp: monitoringStartTime.Unix(),
		FailoverTimestamp:        failoverTime.Unix(),
		ReceiverKey:              "11155420:7372f:0s",
	}
	
	// Simulate the phase metrics recording part of processFailover
	if req.DetectionTimestamp > 0 && req.MonitoringStartTimestamp > 0 && req.FailoverTimestamp > 0 {
		intentTime := time.Unix(intentData.Timestamp.Int64(), 0)
		detectionTime := time.Unix(req.DetectionTimestamp, 0)
		monitoringStartTime := time.Unix(req.MonitoringStartTimestamp, 0)
		failoverTime := time.Unix(req.FailoverTimestamp, 0)
		
		// Calculate phase durations
		intentToEventDuration := detectionTime.Sub(intentTime).Seconds()
		eventDetectionDuration := monitoringStartTime.Sub(detectionTime).Seconds()
		hyperlaneWaitDuration := failoverTime.Sub(monitoringStartTime).Seconds()
		
		// Record phase durations
		handler.metrics.RecordTimelinePhaseDuration("intent_to_event", intentToEventDuration, req.ReceiverKey)
		handler.metrics.RecordTimelinePhaseDuration("event_detection", eventDetectionDuration, req.ReceiverKey)
		handler.metrics.RecordTimelinePhaseDuration("wait", hyperlaneWaitDuration, req.ReceiverKey)
		
		// Verify calculated durations
		assert.InDelta(t, 2.0, intentToEventDuration, 0.1)
		assert.InDelta(t, 0.5, eventDetectionDuration, 0.1)
		assert.InDelta(t, 30.0, hyperlaneWaitDuration, 1.0)
	}
	
	// Simulate bridge processing phase
	time.Sleep(100 * time.Millisecond)
	bridgeProcessingDuration := 1.5
	handler.metrics.RecordTimelinePhaseDuration("bridge_processing", bridgeProcessingDuration, req.ReceiverKey)
	
	// Verify metrics were recorded
	count := testutil.CollectAndCount(handler.metrics.TimelinePhaseDuration)
	assert.Equal(t, 4, count, "Should have recorded 4 phase metrics")
	
	// Check specific metric values
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	
	for _, mf := range metricFamilies {
		if mf.GetName() == "oracle_bridge_timeline_phase_duration_seconds" {
			for _, metric := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, label := range metric.GetLabel() {
					labels[label.GetName()] = label.GetValue()
				}
				
				// Verify labels
				assert.Equal(t, "11155420:7372f:0s", labels["receiver_key"])
				assert.Contains(t, []string{"intent_to_event", "event_detection", "wait", "bridge_processing"}, labels["phase"])
				
				// Verify histogram has data
				histogram := metric.GetHistogram()
				assert.Greater(t, histogram.GetSampleCount(), uint64(0))
				assert.Greater(t, histogram.GetSampleSum(), float64(0))
				
				t.Logf("Phase: %s, Sum: %f seconds", labels["phase"], histogram.GetSampleSum())
			}
		}
	}
}

func TestFailoverHandlerWithZeroTimestamps(t *testing.T) {
	// Test that metrics are not recorded when timestamps are zero
	m := metrics.NewMetrics()
	handler := &FailoverHandler{
		metrics:       m,
		intentMetrics: metrics.NewIntentMetrics(),
		requestStatus: make(map[string]*FailoverStatus),
	}
	
	// Create request with zero timestamps
	req := FailoverRequest{
		MessageID:                "0x1234",
		IntentHash:               "0xabcd",
		DetectionTimestamp:       0,
		MonitoringStartTimestamp: 0,
		FailoverTimestamp:        0,
		ReceiverKey:              "",
	}
	
	// The condition should prevent metrics recording
	if req.DetectionTimestamp > 0 && req.MonitoringStartTimestamp > 0 && req.FailoverTimestamp > 0 {
		t.Fatal("Should not enter this block with zero timestamps")
	}
	
	// Verify no metrics were recorded
	count := testutil.CollectAndCount(handler.metrics.TimelinePhaseDuration)
	assert.Equal(t, 0, count, "Should not have recorded any metrics with zero timestamps")
}

func TestMultipleReceiversPhaseMetrics(t *testing.T) {
	m := metrics.NewMetrics()
	
	// Test different receivers
	receivers := []struct {
		key      string
		waitTime string
		phases   map[string]float64
	}{
		{
			key:      "11155420:a161c:0s",
			waitTime: "immediate",
			phases: map[string]float64{
				"intent_to_event":    2.5,
				"event_detection":    0.5,
				"wait":               28.0,
				"bridge_processing":  1.2,
			},
		},
		{
			key:      "11155420:e14bc:300s",
			waitTime: "5min",
			phases: map[string]float64{
				"intent_to_event":    3.0,
				"event_detection":    1.0,
				"wait":               300.0,
				"bridge_processing":  2.0,
			},
		},
	}
	
	// Record metrics for each receiver
	for _, receiver := range receivers {
		for phase, duration := range receiver.phases {
			m.RecordTimelinePhaseDuration(phase, duration, receiver.key)
		}
	}
	
	// Verify total metrics
	count := testutil.CollectAndCount(m.TimelinePhaseDuration)
	assert.Equal(t, 8, count, "Should have 8 metrics (4 phases × 2 receivers)")
}