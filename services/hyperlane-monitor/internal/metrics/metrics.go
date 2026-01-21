package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the hyperlane monitor
type Metrics struct {
	// Event monitoring metrics
	EventsDetected        prometheus.Counter
	EventsProcessed       prometheus.Counter
	EventProcessingErrors prometheus.Counter
	EventProcessingTime   prometheus.Histogram

	// Delivery checking metrics
	DeliveryChecks        prometheus.Counter
	DeliveryConfirmed     prometheus.Counter
	DeliveryPending       prometheus.Counter
	DeliveryTimeout       prometheus.Counter
	DeliveryCheckErrors   prometheus.Counter
	DeliveryCheckTime     prometheus.Histogram

	// Failover metrics
	FailoverAttempts      prometheus.Counter
	FailoverSuccess       prometheus.Counter
	FailoverErrors        prometheus.Counter
	FailoverTime          prometheus.Histogram

	// Chain connectivity metrics
	ChainConnectionStatus *prometheus.GaugeVec
	ChainRPCLatency       *prometheus.HistogramVec
	ChainRPCErrors        *prometheus.CounterVec

	// Database metrics
	DBOperations          *prometheus.CounterVec
	DBOperationTime       *prometheus.HistogramVec
	DBConnectionStatus    prometheus.Gauge

	// Message queue metrics
	MessageQueueDepth     *prometheus.GaugeVec
	MessageAge            *prometheus.HistogramVec

	// Timeline metrics for Grafana dashboard
	MessageDetectionDuration     *prometheus.HistogramVec
	HyperlaneWaitDuration       *prometheus.HistogramVec
	BridgeProcessingDuration    *prometheus.HistogramVec
	TransactionConfirmationTime *prometheus.HistogramVec
	TotalDeliveryTime          *prometheus.HistogramVec
	MessagesPerPhase           *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// Event monitoring metrics
		EventsDetected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_events_detected_total",
			Help: "Total number of MessageDispatched events detected",
		}),
		EventsProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_events_processed_total",
			Help: "Total number of events successfully processed",
		}),
		EventProcessingErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_event_processing_errors_total",
			Help: "Total number of event processing errors",
		}),
		EventProcessingTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_event_processing_duration_seconds",
			Help:    "Time taken to process an event",
			Buckets: prometheus.DefBuckets,
		}),

		// Delivery checking metrics
		DeliveryChecks: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_delivery_checks_total",
			Help: "Total number of delivery checks performed",
		}),
		DeliveryConfirmed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_delivery_confirmed_total",
			Help: "Total number of confirmed deliveries",
		}),
		DeliveryPending: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_delivery_pending_total",
			Help: "Total number of pending deliveries",
		}),
		DeliveryTimeout: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_delivery_timeout_total",
			Help: "Total number of delivery timeouts",
		}),
		DeliveryCheckErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_delivery_check_errors_total",
			Help: "Total number of delivery check errors",
		}),
		DeliveryCheckTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_delivery_check_duration_seconds",
			Help:    "Time taken to check delivery status",
			Buckets: prometheus.DefBuckets,
		}),

		// Failover metrics
		FailoverAttempts: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_failover_attempts_total",
			Help: "Total number of failover attempts",
		}),
		FailoverSuccess: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_failover_success_total",
			Help: "Total number of successful failovers",
		}),
		FailoverErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "hyperlane_monitor_failover_errors_total",
			Help: "Total number of failover errors",
		}),
		FailoverTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_failover_duration_seconds",
			Help:    "Time taken to complete failover",
			Buckets: prometheus.DefBuckets,
		}),

		// Chain connectivity metrics
		ChainConnectionStatus: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hyperlane_monitor_chain_connection_status",
			Help: "Chain connection status (1 = connected, 0 = disconnected)",
		}, []string{"chain_id", "chain_name"}),
		ChainRPCLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_chain_rpc_latency_seconds",
			Help:    "RPC call latency by chain",
			Buckets: prometheus.DefBuckets,
		}, []string{"chain_id", "method"}),
		ChainRPCErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hyperlane_monitor_chain_rpc_errors_total",
			Help: "Total number of RPC errors by chain",
		}, []string{"chain_id", "error_type"}),

		// Database metrics
		DBOperations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hyperlane_monitor_db_operations_total",
			Help: "Total number of database operations",
		}, []string{"operation", "status"}),
		DBOperationTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_db_operation_duration_seconds",
			Help:    "Database operation duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		DBConnectionStatus: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "hyperlane_monitor_db_connection_status",
			Help: "Database connection status (1 = connected, 0 = disconnected)",
		}),

		// Message queue metrics
		MessageQueueDepth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hyperlane_monitor_message_queue_depth",
			Help: "Current depth of message queue",
		}, []string{"status"}),
		MessageAge: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_monitor_message_age_seconds",
			Help:    "Age of messages when processed",
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 28800, 86400},
		}, []string{"chain_id"}),

		// Timeline metrics for Grafana dashboard
		MessageDetectionDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_message_detection_duration_seconds",
			Help:    "Time taken to detect a new Hyperlane message",
			Buckets: prometheus.DefBuckets,
		}, []string{"chain", "source_domain", "destination_domain"}),
		HyperlaneWaitDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_delivery_wait_duration_seconds",
			Help:    "Time spent waiting for Hyperlane to deliver the message",
			Buckets: []float64{1, 5, 10, 15, 30, 60, 120, 300},
		}, []string{"chain", "source_domain", "destination_domain", "status"}),
		BridgeProcessingDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_processing_duration_seconds",
			Help:    "Time taken by the Bridge to process failover request",
			Buckets: prometheus.DefBuckets,
		}, []string{"chain", "destination_domain"}),
		TransactionConfirmationTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "transaction_confirmation_duration_seconds",
			Help:    "Time taken for transaction to be confirmed on-chain",
			Buckets: prometheus.DefBuckets,
		}, []string{"chain", "destination_domain"}),
		TotalDeliveryTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hyperlane_total_delivery_time_seconds",
			Help:    "Total time from message dispatch to final delivery",
			Buckets: []float64{5, 10, 15, 30, 60, 120, 300, 600},
		}, []string{"chain", "source_domain", "destination_domain", "delivery_method"}),
		MessagesPerPhase: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hyperlane_messages_per_phase_total",
			Help: "Total number of messages in each phase",
		}, []string{"phase", "chain", "source_domain", "destination_domain"}),
	}
}

// RecordEventDetected increments the events detected counter
func (m *Metrics) RecordEventDetected() {
	m.EventsDetected.Inc()
}

// RecordEventProcessed records a successfully processed event
func (m *Metrics) RecordEventProcessed(duration float64) {
	m.EventsProcessed.Inc()
	m.EventProcessingTime.Observe(duration)
}

// RecordEventProcessingError increments the event processing error counter
func (m *Metrics) RecordEventProcessingError() {
	m.EventProcessingErrors.Inc()
}

// RecordDeliveryCheck records a delivery check result
func (m *Metrics) RecordDeliveryCheck(status string, duration float64) {
	m.DeliveryChecks.Inc()
	m.DeliveryCheckTime.Observe(duration)

	switch status {
	case "confirmed":
		m.DeliveryConfirmed.Inc()
	case "pending":
		m.DeliveryPending.Inc()
	case "timeout":
		m.DeliveryTimeout.Inc()
	case "error":
		m.DeliveryCheckErrors.Inc()
	}
}

// RecordFailoverAttempt records a failover attempt
func (m *Metrics) RecordFailoverAttempt(success bool, duration float64) {
	m.FailoverAttempts.Inc()
	m.FailoverTime.Observe(duration)

	if success {
		m.FailoverSuccess.Inc()
	} else {
		m.FailoverErrors.Inc()
	}
}

// UpdateChainConnectionStatus updates the connection status for a chain
func (m *Metrics) UpdateChainConnectionStatus(chainID, chainName string, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	m.ChainConnectionStatus.WithLabelValues(chainID, chainName).Set(value)
}

// RecordRPCLatency records RPC call latency
func (m *Metrics) RecordRPCLatency(chainID, method string, duration float64) {
	m.ChainRPCLatency.WithLabelValues(chainID, method).Observe(duration)
}

// RecordRPCError increments the RPC error counter
func (m *Metrics) RecordRPCError(chainID, errorType string) {
	m.ChainRPCErrors.WithLabelValues(chainID, errorType).Inc()
}

// RecordDBOperation records a database operation
func (m *Metrics) RecordDBOperation(operation, status string, duration float64) {
	m.DBOperations.WithLabelValues(operation, status).Inc()
	m.DBOperationTime.WithLabelValues(operation).Observe(duration)
}

// UpdateDBConnectionStatus updates the database connection status
func (m *Metrics) UpdateDBConnectionStatus(connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	m.DBConnectionStatus.Set(value)
}

// UpdateMessageQueueDepth updates the message queue depth
func (m *Metrics) UpdateMessageQueueDepth(status string, depth float64) {
	m.MessageQueueDepth.WithLabelValues(status).Set(depth)
}

// RecordMessageAge records the age of a message when processed
func (m *Metrics) RecordMessageAge(chainID string, age float64) {
	m.MessageAge.WithLabelValues(chainID).Observe(age)
}

// RecordTimelinePhase records metrics for a specific phase in the delivery timeline
func (m *Metrics) RecordTimelinePhase(phase string, duration float64, chain, sourceDomain, destDomain string) {
	labels := prometheus.Labels{
		"chain":              chain,
		"source_domain":      sourceDomain,
		"destination_domain": destDomain,
	}

	switch phase {
	case "detection":
		m.MessageDetectionDuration.With(labels).Observe(duration)
		m.MessagesPerPhase.WithLabelValues("detection", chain, sourceDomain, destDomain).Inc()
	case "wait":
		waitLabels := prometheus.Labels{
			"chain":              chain,
			"source_domain":      sourceDomain,
			"destination_domain": destDomain,
			"status":            "timeout",
		}
		m.HyperlaneWaitDuration.With(waitLabels).Observe(duration)
		m.MessagesPerPhase.WithLabelValues("wait", chain, sourceDomain, destDomain).Inc()
	case "bridge_processing":
		bridgeLabels := prometheus.Labels{
			"chain":              chain,
			"destination_domain": destDomain,
		}
		m.BridgeProcessingDuration.With(bridgeLabels).Observe(duration)
		m.MessagesPerPhase.WithLabelValues("bridge_processing", chain, sourceDomain, destDomain).Inc()
	case "confirmation":
		confirmLabels := prometheus.Labels{
			"chain":              chain,
			"destination_domain": destDomain,
		}
		m.TransactionConfirmationTime.With(confirmLabels).Observe(duration)
		m.MessagesPerPhase.WithLabelValues("confirmation", chain, sourceDomain, destDomain).Inc()
	}
}

// RecordTotalDeliveryTime records the total end-to-end delivery time
func (m *Metrics) RecordTotalDeliveryTime(duration float64, chain, sourceDomain, destDomain, deliveryMethod string) {
	labels := prometheus.Labels{
		"chain":              chain,
		"source_domain":      sourceDomain,
		"destination_domain": destDomain,
		"delivery_method":    deliveryMethod,
	}
	m.TotalDeliveryTime.With(labels).Observe(duration)
}