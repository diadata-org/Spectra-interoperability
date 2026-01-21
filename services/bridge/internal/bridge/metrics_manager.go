package bridge

import (
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

// MetricsManager
type MetricsManager struct {
	collector *metrics.Collector
	tracker   *MetricsTracker
	failover  *metrics.Metrics
}

// NewMetricsManager creates a new MetricsManager
func NewMetricsManager(collector *metrics.Collector) *MetricsManager {
	if collector == nil {
		return &MetricsManager{}
	}

	return &MetricsManager{
		collector: collector,
		tracker:   NewMetricsTracker(collector),
		failover:  collector.FailoverMetrics,
	}
}

// ReportUpdateQueueSize reports the current update queue size
func (m *MetricsManager) ReportUpdateQueueSize(size int) {
	if m.collector != nil {
		m.collector.SetUpdateChanSize(size)
	}
}

// GetTracker returns the MetricsTracker for transaction metrics
func (m *MetricsManager) GetTracker() *MetricsTracker {
	return m.tracker
}

// GetCollector returns the metrics Collector
func (m *MetricsManager) GetCollector() *metrics.Collector {
	return m.collector
}

// GetFailoverMetrics returns the failover metrics instance
func (m *MetricsManager) GetFailoverMetrics() *metrics.Metrics {
	return m.failover
}
