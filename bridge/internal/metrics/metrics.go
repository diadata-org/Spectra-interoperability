package metrics

import (
	"fmt"
	
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the bridge service
type Metrics struct {
	// API request metrics
	HTTPRequestsTotal         *prometheus.CounterVec
	HTTPRequestDuration       *prometheus.HistogramVec
	HTTPResponseSizeBytes     *prometheus.HistogramVec
	
	// Failover metrics
	FailoverRequestsTotal     *prometheus.CounterVec
	FailoverProcessingTime    *prometheus.HistogramVec
	FailoverSuccess           *prometheus.CounterVec
	FailoverErrors            *prometheus.CounterVec
	
	// Transaction metrics
	TransactionsSubmitted     *prometheus.CounterVec
	TransactionConfirmations  *prometheus.CounterVec
	TransactionFailures       *prometheus.CounterVec
	TransactionGasUsed        *prometheus.HistogramVec
	TransactionFees           *prometheus.HistogramVec
	
	// Timeline metrics for Grafana dashboard
	BridgeProcessingDuration    *prometheus.HistogramVec
	TransactionConfirmationTime *prometheus.HistogramVec
	TotalDeliveryTime          *prometheus.HistogramVec
	
	// Intent processing metrics
	IntentValidations         *prometheus.CounterVec
	IntentValidationErrors    *prometheus.CounterVec
	IntentProcessingTime      *prometheus.HistogramVec
	
	// Chain connection metrics
	ChainConnectionStatus     *prometheus.GaugeVec
	ChainRPCLatency          *prometheus.HistogramVec
	ChainRPCErrors           *prometheus.CounterVec
	
	// Database metrics
	DBOperations             *prometheus.CounterVec
	DBOperationTime          *prometheus.HistogramVec
	DBConnectionStatus       prometheus.Gauge
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// API request metrics
		HTTPRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_http_requests_total",
			Help: "Total number of HTTP requests",
		}, []string{"method", "endpoint", "status"}),
		HTTPRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "endpoint"}),
		HTTPResponseSizeBytes: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		}, []string{"method", "endpoint"}),
		
		// Failover metrics
		FailoverRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_failover_requests_total",
			Help: "Total number of failover requests received",
		}, []string{"source_chain", "destination_chain"}),
		FailoverProcessingTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_failover_processing_duration_seconds",
			Help:    "Time taken to process failover request",
			Buckets: prometheus.DefBuckets,
		}, []string{"source_chain", "destination_chain"}),
		FailoverSuccess: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_failover_success_total",
			Help: "Total number of successful failover operations",
		}, []string{"source_chain", "destination_chain"}),
		FailoverErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_failover_errors_total",
			Help: "Total number of failover errors",
		}, []string{"source_chain", "destination_chain", "error_type"}),
		
		// Transaction metrics
		TransactionsSubmitted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_transactions_submitted_total",
			Help: "Total number of transactions submitted",
		}, []string{"chain", "contract_type"}),
		TransactionConfirmations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_transaction_confirmations_total",
			Help: "Total number of transaction confirmations",
		}, []string{"chain", "contract_type"}),
		TransactionFailures: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_transaction_failures_total",
			Help: "Total number of transaction failures",
		}, []string{"chain", "contract_type", "error_type"}),
		TransactionGasUsed: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_transaction_gas_used",
			Help:    "Gas used by transactions",
			Buckets: prometheus.ExponentialBuckets(21000, 2, 10),
		}, []string{"chain", "contract_type"}),
		TransactionFees: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_transaction_fees_wei",
			Help:    "Transaction fees in wei",
			Buckets: prometheus.ExponentialBuckets(1e15, 10, 8),
		}, []string{"chain", "contract_type"}),
		
		// Timeline metrics for Grafana dashboard
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
		
		// Intent processing metrics
		IntentValidations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intent_validations_total",
			Help: "Total number of intent validations",
		}, []string{"result"}),
		IntentValidationErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intent_validation_errors_total",
			Help: "Total number of intent validation errors",
		}, []string{"error_type"}),
		IntentProcessingTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_intent_processing_duration_seconds",
			Help:    "Time taken to process intents",
			Buckets: prometheus.DefBuckets,
		}, []string{"intent_type"}),
		
		// Chain connection metrics
		ChainConnectionStatus: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_chain_connection_status",
			Help: "Chain connection status (1 = connected, 0 = disconnected)",
		}, []string{"chain_id", "chain_name"}),
		ChainRPCLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_chain_rpc_latency_seconds",
			Help:    "RPC call latency by chain",
			Buckets: prometheus.DefBuckets,
		}, []string{"chain_id", "method"}),
		ChainRPCErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_chain_rpc_errors_total",
			Help: "Total number of RPC errors by chain",
		}, []string{"chain_id", "error_type"}),
		
		// Database metrics
		DBOperations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_db_operations_total",
			Help: "Total number of database operations",
		}, []string{"operation", "status"}),
		DBOperationTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_db_operation_duration_seconds",
			Help:    "Database operation duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		DBConnectionStatus: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bridge_db_connection_status",
			Help: "Database connection status (1 = connected, 0 = disconnected)",
		}),
	}
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, endpoint, status string, duration float64, responseSize int) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration)
	m.HTTPResponseSizeBytes.WithLabelValues(method, endpoint).Observe(float64(responseSize))
}

// RecordFailoverRequest records failover request metrics
func (m *Metrics) RecordFailoverRequest(sourceChain, destChain string) {
	m.FailoverRequestsTotal.WithLabelValues(sourceChain, destChain).Inc()
}

// RecordFailoverProcessing records failover processing metrics
func (m *Metrics) RecordFailoverProcessing(sourceChain, destChain string, duration float64, success bool, errorType string) {
	m.FailoverProcessingTime.WithLabelValues(sourceChain, destChain).Observe(duration)
	
	if success {
		m.FailoverSuccess.WithLabelValues(sourceChain, destChain).Inc()
	} else {
		m.FailoverErrors.WithLabelValues(sourceChain, destChain, errorType).Inc()
	}
}

// RecordTransaction records transaction metrics
func (m *Metrics) RecordTransaction(chain, contractType string, gasUsed uint64, fee uint64) {
	m.TransactionsSubmitted.WithLabelValues(chain, contractType).Inc()
	m.TransactionGasUsed.WithLabelValues(chain, contractType).Observe(float64(gasUsed))
	m.TransactionFees.WithLabelValues(chain, contractType).Observe(float64(fee))
}

// RecordTransactionConfirmation records transaction confirmation
func (m *Metrics) RecordTransactionConfirmation(chain, contractType string, duration float64) {
	m.TransactionConfirmations.WithLabelValues(chain, contractType).Inc()
	m.TransactionConfirmationTime.WithLabelValues(chain, fmt.Sprintf("%s", chain)).Observe(duration)
}

// RecordTransactionFailure records transaction failure
func (m *Metrics) RecordTransactionFailure(chain, contractType, errorType string) {
	m.TransactionFailures.WithLabelValues(chain, contractType, errorType).Inc()
}

// RecordTimelinePhase records metrics for a specific phase in the delivery timeline
func (m *Metrics) RecordTimelinePhase(phase string, duration float64, chain, sourceDomain, destDomain string) {
	switch phase {
	case "bridge_processing":
		m.BridgeProcessingDuration.WithLabelValues(chain, destDomain).Observe(duration)
	case "confirmation":
		m.TransactionConfirmationTime.WithLabelValues(chain, destDomain).Observe(duration)
	}
}

// RecordTotalDeliveryTime records the total end-to-end delivery time
func (m *Metrics) RecordTotalDeliveryTime(duration float64, chain, sourceDomain, destDomain, deliveryMethod string) {
	m.TotalDeliveryTime.WithLabelValues(chain, sourceDomain, destDomain, deliveryMethod).Observe(duration)
}

// RecordIntentValidation records intent validation metrics
func (m *Metrics) RecordIntentValidation(success bool, errorType string) {
	if success {
		m.IntentValidations.WithLabelValues("success").Inc()
	} else {
		m.IntentValidations.WithLabelValues("failure").Inc()
		m.IntentValidationErrors.WithLabelValues(errorType).Inc()
	}
}

// RecordIntentProcessing records intent processing time
func (m *Metrics) RecordIntentProcessing(intentType string, duration float64) {
	m.IntentProcessingTime.WithLabelValues(intentType).Observe(duration)
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