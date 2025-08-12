package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordTimelinePhaseDuration(t *testing.T) {
	// Create a new registry for testing
	reg := prometheus.NewRegistry()
	
	// Create metrics with the test registry
	m := &Metrics{
		TimelinePhaseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "oracle_bridge_timeline_phase_duration_seconds",
			Help:    "Duration of each phase in the oracle intent lifecycle",
			Buckets: prometheus.DefBuckets,
		}, []string{"phase", "receiver_key"}),
	}
	
	// Register the metric
	reg.MustRegister(m.TimelinePhaseDuration)
	
	// Test recording different phases
	testCases := []struct {
		phase       string
		duration    float64
		receiverKey string
	}{
		{"intent_to_event", 2.5, "11155420:a161c:0s"},
		{"event_detection", 0.5, "11155420:a161c:0s"},
		{"wait", 5.0, "11155420:a161c:0s"},
		{"bridge_processing", 1.2, "11155420:a161c:0s"},
		{"intent_to_event", 15.0, "11155420:e14bc:300s"},
		{"event_detection", 1.0, "11155420:e14bc:300s"},
		{"wait", 300.0, "11155420:e14bc:300s"},
		{"bridge_processing", 2.0, "11155420:e14bc:300s"},
	}
	
	// Record metrics
	for _, tc := range testCases {
		m.RecordTimelinePhaseDuration(tc.phase, tc.duration, tc.receiverKey)
	}
	
	// Verify metrics were recorded
	metricFamily, err := reg.Gather()
	assert.NoError(t, err)
	assert.Len(t, metricFamily, 1)
	
	// Check metric name
	assert.Equal(t, "oracle_bridge_timeline_phase_duration_seconds", metricFamily[0].GetName())
	
	// Check that we have metrics for all combinations
	metrics := metricFamily[0].GetMetric()
	assert.Greater(t, len(metrics), 0)
	
	// Verify specific metric values
	for _, metric := range metrics {
		labels := metric.GetLabel()
		labelMap := make(map[string]string)
		for _, label := range labels {
			labelMap[label.GetName()] = label.GetValue()
		}
		
		// Check that phase and receiver_key labels exist
		phase, hasPhase := labelMap["phase"]
		receiverKey, hasReceiverKey := labelMap["receiver_key"]
		assert.True(t, hasPhase, "phase label should exist")
		assert.True(t, hasReceiverKey, "receiver_key label should exist")
		
		// Verify histogram has samples
		histogram := metric.GetHistogram()
		assert.NotNil(t, histogram)
		assert.Greater(t, histogram.GetSampleCount(), uint64(0))
		
		t.Logf("Phase: %s, ReceiverKey: %s, Count: %d, Sum: %f", 
			phase, receiverKey, histogram.GetSampleCount(), histogram.GetSampleSum())
	}
}

func TestPhaseMetricsIntegration(t *testing.T) {
	// Create full metrics instance
	m := NewMetrics()
	
	// Simulate a complete intent lifecycle
	receiverKey := "11155420:a161c:0s"
	
	// Record each phase with realistic durations
	phases := []struct {
		name     string
		duration float64
	}{
		{"intent_to_event", 2.5},      // 2.5 seconds from intent to blockchain event
		{"event_detection", 0.5},       // 0.5 seconds to detect the event
		{"wait", 28.0},                 // 28 seconds waiting for Hyperlane
		{"bridge_processing", 1.5},     // 1.5 seconds for bridge to process
	}
	
	for _, phase := range phases {
		m.RecordTimelinePhaseDuration(phase.name, phase.duration, receiverKey)
	}
	
	// Test with a slow receiver
	slowReceiverKey := "11155420:e14bc:300s"
	slowPhases := []struct {
		name     string
		duration float64
	}{
		{"intent_to_event", 3.0},
		{"event_detection", 1.0},
		{"wait", 300.0},  // 5 minutes wait
		{"bridge_processing", 2.0},
	}
	
	for _, phase := range slowPhases {
		m.RecordTimelinePhaseDuration(phase.name, phase.duration, slowReceiverKey)
	}
	
	// Verify metrics can be collected
	count := testutil.CollectAndCount(m.TimelinePhaseDuration)
	assert.Equal(t, 8, count, "Should have 8 metrics (4 phases × 2 receivers)")
}

func TestRecordTimelinePhaseDurationConcurrent(t *testing.T) {
	m := NewMetrics()
	
	// Test concurrent recording
	done := make(chan bool)
	
	// Start multiple goroutines recording metrics
	for i := 0; i < 10; i++ {
		go func(id int) {
			receiverKey := "11155420:test:0s"
			for j := 0; j < 100; j++ {
				m.RecordTimelinePhaseDuration("intent_to_event", float64(j)*0.1, receiverKey)
				m.RecordTimelinePhaseDuration("event_detection", float64(j)*0.05, receiverKey)
				m.RecordTimelinePhaseDuration("wait", float64(j)*1.0, receiverKey)
				m.RecordTimelinePhaseDuration("bridge_processing", float64(j)*0.2, receiverKey)
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify metrics were recorded without panic
	count := testutil.CollectAndCount(m.TimelinePhaseDuration)
	assert.Greater(t, count, 0, "Should have recorded metrics")
}

func BenchmarkRecordTimelinePhaseDuration(b *testing.B) {
	m := NewMetrics()
	receiverKey := "11155420:a161c:0s"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordTimelinePhaseDuration("intent_to_event", 2.5, receiverKey)
	}
}