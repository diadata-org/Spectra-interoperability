package metrics

import (
	"fmt"
	
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Collector holds all Prometheus metrics
type Collector struct {
	// Event metrics
	eventsReceived   prometheus.Counter
	eventsProcessed  prometheus.Counter
	eventsDuplicate  prometheus.Counter
	eventsInvalid    prometheus.Counter
	eventsFailed     prometheus.Counter
	eventProcessingDuration prometheus.Histogram
	
	// Transaction metrics
	transactionsSent      prometheus.Counter
	transactionsConfirmed prometheus.Counter
	transactionsFailed    prometheus.Counter
	transactionGasUsed    prometheus.Counter
	transactionDuration   prometheus.Histogram
	insufficientBalance   prometheus.Counter
	
	// Chain metrics
	blockLag        *prometheus.GaugeVec
	chainHealth     *prometheus.GaugeVec
	lastBlockNumber *prometheus.GaugeVec
	
	// Worker pool metrics
	workerPoolSize    prometheus.Gauge
	activeWorkers     prometheus.Gauge
	taskQueueSize     prometheus.Gauge
	taskProcessingDuration prometheus.Histogram
	
	// HTTP metrics
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	
	// Database metrics
	dbConnections    prometheus.Gauge
	dbQueryDuration  prometheus.Histogram
	
	// Health metrics
	componentHealth  *prometheus.GaugeVec
	recoveryAttempts *prometheus.CounterVec
	
	// Intent lifecycle metrics
	IntentMetrics *IntentMetrics
	
	// Failover metrics (shared instance)
	FailoverMetrics *Metrics
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		// Event metrics
		eventsReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_events_received_total",
			Help: "Total number of events received",
		}),
		eventsProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_events_processed_total",
			Help: "Total number of events processed",
		}),
		eventsDuplicate: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_events_duplicate_total",
			Help: "Total number of duplicate events",
		}),
		eventsInvalid: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_events_invalid_total",
			Help: "Total number of invalid events",
		}),
		eventsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_events_failed_total",
			Help: "Total number of failed events",
		}),
		eventProcessingDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_event_processing_duration_seconds",
			Help:    "Duration of event processing",
			Buckets: prometheus.DefBuckets,
		}),
		
		// Transaction metrics
		transactionsSent: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_transactions_sent_total",
			Help: "Total number of transactions sent",
		}),
		transactionsConfirmed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_transactions_confirmed_total",
			Help: "Total number of transactions confirmed",
		}),
		transactionsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_transactions_failed_total",
			Help: "Total number of transactions failed",
		}),
		transactionGasUsed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_transaction_gas_used_total",
			Help: "Total gas used by transactions",
		}),
		transactionDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_transaction_duration_seconds",
			Help:    "Duration of transaction execution",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		}),
		insufficientBalance: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bridge_insufficient_balance_total",
			Help: "Total number of transactions failed due to insufficient balance",
		}),
		
		// Chain metrics
		blockLag: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_chain_block_lag",
			Help: "Block lag for each chain",
		}, []string{"chain_id", "chain_name"}),
		chainHealth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_chain_health",
			Help: "Health status of each chain (1=healthy, 0=unhealthy)",
		}, []string{"chain_id", "chain_name"}),
		lastBlockNumber: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_chain_last_block_number",
			Help: "Last processed block number for each chain",
		}, []string{"chain_id", "chain_name"}),
		
		// Worker pool metrics
		workerPoolSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bridge_worker_pool_size",
			Help: "Number of workers in the pool",
		}),
		activeWorkers: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bridge_active_workers",
			Help: "Number of active workers",
		}),
		taskQueueSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bridge_task_queue_size",
			Help: "Number of tasks in the queue",
		}),
		taskProcessingDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_task_processing_duration_seconds",
			Help:    "Duration of task processing",
			Buckets: prometheus.DefBuckets,
		}),
		
		// HTTP metrics
		httpRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_http_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"method", "path", "status"}),
		httpRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		
		// Database metrics
		dbConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bridge_db_connections",
			Help: "Number of database connections",
		}),
		dbQueryDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_db_query_duration_seconds",
			Help:    "Duration of database queries",
			Buckets: prometheus.DefBuckets,
		}),
		
		// Health metrics
		componentHealth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_component_health",
			Help: "Health status of bridge components (1=healthy, 0=unhealthy)",
		}, []string{"component", "type"}),
		recoveryAttempts: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_recovery_attempts_total",
			Help: "Total number of recovery attempts",
		}, []string{"component", "result"}),
		
		// Initialize intent metrics
		IntentMetrics: NewIntentMetrics(),
		
		// Initialize failover metrics (shared instance)
		FailoverMetrics: NewMetrics(),
	}
}

// Event metric methods

func (c *Collector) IncEventsReceived() {
	c.eventsReceived.Inc()
}

func (c *Collector) IncEventsProcessed() {
	c.eventsProcessed.Inc()
}

func (c *Collector) IncEventsDuplicate() {
	c.eventsDuplicate.Inc()
}

func (c *Collector) IncEventsInvalid() {
	c.eventsInvalid.Inc()
}

func (c *Collector) IncEventsFailed() {
	c.eventsFailed.Inc()
}

func (c *Collector) ObserveEventProcessingDuration(seconds float64) {
	c.eventProcessingDuration.Observe(seconds)
}

// Transaction metric methods

func (c *Collector) IncTransactionsSent() {
	c.transactionsSent.Inc()
}

func (c *Collector) IncTransactionsConfirmed() {
	c.transactionsConfirmed.Inc()
}

func (c *Collector) IncTransactionsFailed() {
	c.transactionsFailed.Inc()
}

func (c *Collector) AddTransactionGasUsed(gas uint64) {
	c.transactionGasUsed.Add(float64(gas))
}

func (c *Collector) ObserveTransactionDuration(seconds float64) {
	c.transactionDuration.Observe(seconds)
}

func (c *Collector) IncInsufficientBalance() {
	c.insufficientBalance.Inc()
}

// Chain metric methods

func (c *Collector) SetBlockLag(chainID int64, chainName string, lag uint64) {
	c.blockLag.WithLabelValues(
		formatChainID(chainID),
		chainName,
	).Set(float64(lag))
}

func (c *Collector) SetChainHealth(chainID int64, chainName string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	c.chainHealth.WithLabelValues(
		formatChainID(chainID),
		chainName,
	).Set(value)
}

func (c *Collector) SetLastBlockNumber(chainID int64, chainName string, blockNumber uint64) {
	c.lastBlockNumber.WithLabelValues(
		formatChainID(chainID),
		chainName,
	).Set(float64(blockNumber))
}

// Worker pool metric methods

func (c *Collector) SetWorkerPoolSize(size int) {
	c.workerPoolSize.Set(float64(size))
}

func (c *Collector) SetActiveWorkers(count int32) {
	c.activeWorkers.Set(float64(count))
}

func (c *Collector) SetTaskQueueSize(size int32) {
	c.taskQueueSize.Set(float64(size))
}

func (c *Collector) ObserveTaskProcessingDuration(seconds float64) {
	c.taskProcessingDuration.Observe(seconds)
}

// HTTP metric methods

func (c *Collector) RecordHTTPRequest(method, path string, status int, duration float64) {
	statusStr := formatHTTPStatus(status)
	c.httpRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	c.httpRequestDuration.WithLabelValues(method, path).Observe(duration)
}

// Database metric methods

func (c *Collector) SetDBConnections(count int) {
	c.dbConnections.Set(float64(count))
}

func (c *Collector) ObserveDBQueryDuration(seconds float64) {
	c.dbQueryDuration.Observe(seconds)
}

// Health metric methods

func (c *Collector) SetComponentHealth(component, componentType string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	c.componentHealth.WithLabelValues(component, componentType).Set(value)
}

func (c *Collector) IncRecoveryAttempts(component, result string) {
	c.recoveryAttempts.WithLabelValues(component, result).Inc()
}

// Helper functions

func formatChainID(chainID int64) string {
	return fmt.Sprintf("%d", chainID)
}

func formatHTTPStatus(status int) string {
	return fmt.Sprintf("%d", status)
}

// GetMetrics returns all metrics for external monitoring
func (c *Collector) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// Add all counter values
	metrics["events_received"] = c.eventsReceived
	metrics["events_processed"] = c.eventsProcessed
	metrics["events_duplicate"] = c.eventsDuplicate
	metrics["events_invalid"] = c.eventsInvalid
	metrics["events_failed"] = c.eventsFailed
	metrics["transactions_sent"] = c.transactionsSent
	metrics["transactions_confirmed"] = c.transactionsConfirmed
	metrics["transactions_failed"] = c.transactionsFailed
	metrics["transaction_gas_used"] = c.transactionGasUsed
	metrics["insufficient_balance"] = c.insufficientBalance
	
	return metrics
}