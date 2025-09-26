package metrics

import (
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
)

// PrometheusCollector implements the MetricsCollector interface using Prometheus
type PrometheusCollector struct{}

// NewPrometheusCollector creates a new Prometheus metrics collector
func NewPrometheusCollector() interfaces.MetricsCollector {
	return &PrometheusCollector{}
}

// RecordIntentCreated records when an intent is created
func (c *PrometheusCollector) RecordIntentCreated(symbol string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	IntentsCreated.WithLabelValues(symbol, status).Inc()
}

// RecordIntentPublished records when an intent is published
func (c *PrometheusCollector) RecordIntentPublished(symbol string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	IntentsPublished.WithLabelValues(symbol, status).Inc()
}

// RecordProcessingDuration records the duration of processing
func (c *PrometheusCollector) RecordProcessingDuration(symbol string, mode string, duration time.Duration) {
	ProcessingDuration.WithLabelValues(symbol, mode).Observe(duration.Seconds())
}

// RecordOracleFetchDuration records the duration of oracle fetches
func (c *PrometheusCollector) RecordOracleFetchDuration(symbol string, duration time.Duration) {
	OracleValueFetchDuration.WithLabelValues(symbol).Observe(duration.Seconds())
}