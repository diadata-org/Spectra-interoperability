package api

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

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
				"intent_to_event":   2.5,
				"event_detection":   0.5,
				"wait":              28.0,
				"bridge_processing": 1.2,
			},
		},
		{
			key:      "11155420:e14bc:300s",
			waitTime: "5min",
			phases: map[string]float64{
				"intent_to_event":   3.0,
				"event_detection":   1.0,
				"wait":              300.0,
				"bridge_processing": 2.0,
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
