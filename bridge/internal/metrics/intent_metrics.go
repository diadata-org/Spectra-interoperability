package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// IntentMetrics tracks oracle intent lifecycle metrics
type IntentMetrics struct {
	// Latency metrics - tracks time between different stages
	intentToRegistrationLatency    *prometheus.HistogramVec // Time from intent creation to registration on chain
	registrationToScanLatency      *prometheus.HistogramVec // Time from registration to scanner detection
	scanToProcessingLatency        *prometheus.HistogramVec // Time from scan to processing start
	processingToSubmissionLatency  *prometheus.HistogramVec // Time from processing to submission
	submissionToConfirmationLatency *prometheus.HistogramVec // Time from submission to confirmation
	endToEndLatency                *prometheus.HistogramVec // Total time from intent creation to confirmation
	
	// Stage timestamps - tracks when each stage occurred
	intentTimestamp         *prometheus.GaugeVec
	registrationTimestamp   *prometheus.GaugeVec
	scanTimestamp           *prometheus.GaugeVec
	processingTimestamp     *prometheus.GaugeVec
	submissionTimestamp     *prometheus.GaugeVec
	confirmationTimestamp   *prometheus.GaugeVec
	
	// Price deviation metrics
	priceDeviation          *prometheus.HistogramVec // Percentage deviation between intent and submission
	priceAge               *prometheus.HistogramVec // Age of price at submission time
	
	// Intent counts by stage
	intentsCreated          *prometheus.CounterVec
	intentsRegistered       *prometheus.CounterVec
	intentsScanned          *prometheus.CounterVec
	intentsProcessed        *prometheus.CounterVec
	intentsSubmitted        *prometheus.CounterVec
	intentsConfirmed        *prometheus.CounterVec
	intentsFailed           *prometheus.CounterVec
	
	// Router metrics
	routerDecisions         *prometheus.CounterVec // Count of router decisions
	routerLatency          *prometheus.HistogramVec // Time taken for routing decision
	
	// Gas metrics per symbol
	gasUsedPerSymbol       *prometheus.CounterVec
	gasPricePerSymbol      *prometheus.HistogramVec
}

// NewIntentMetrics creates a new intent metrics collector
func NewIntentMetrics() *IntentMetrics {
	return &IntentMetrics{
		// Latency metrics with fine-grained buckets for subsecond measurements
		intentToRegistrationLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_intent_to_registration_latency_seconds",
			Help:    "Time from intent creation to on-chain registration",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"symbol", "chain_id", "source"}),
		
		registrationToScanLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_registration_to_scan_latency_seconds",
			Help:    "Time from on-chain registration to scanner detection",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"symbol", "chain_id", "scanner_type"}),
		
		scanToProcessingLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_scan_to_processing_latency_seconds",
			Help:    "Time from scanner detection to processing start",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
		}, []string{"symbol", "chain_id", "priority"}),
		
		processingToSubmissionLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_processing_to_submission_latency_seconds",
			Help:    "Time from processing start to transaction submission",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		}, []string{"symbol", "destination_chain", "router_id"}),
		
		submissionToConfirmationLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_submission_to_confirmation_latency_seconds",
			Help:    "Time from transaction submission to confirmation",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"symbol", "destination_chain", "gas_price_tier"}),
		
		endToEndLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_end_to_end_latency_seconds",
			Help:    "Total time from intent creation to on-chain confirmation",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1200},
		}, []string{"symbol", "source_chain", "destination_chain"}),
		
		// Timestamp gauges
		intentTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_intent_timestamp",
			Help: "Unix timestamp when intent was created",
		}, []string{"intent_hash", "symbol"}),
		
		registrationTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_registration_timestamp",
			Help: "Unix timestamp when intent was registered on-chain",
		}, []string{"intent_hash", "symbol"}),
		
		scanTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_scan_timestamp",
			Help: "Unix timestamp when intent was detected by scanner",
		}, []string{"intent_hash", "symbol", "scanner_type"}),
		
		processingTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_processing_timestamp",
			Help: "Unix timestamp when intent processing started",
		}, []string{"intent_hash", "symbol"}),
		
		submissionTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_submission_timestamp",
			Help: "Unix timestamp when transaction was submitted",
		}, []string{"intent_hash", "symbol", "tx_hash"}),
		
		confirmationTimestamp: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_confirmation_timestamp",
			Help: "Unix timestamp when transaction was confirmed",
		}, []string{"intent_hash", "symbol", "tx_hash"}),
		
		// Price metrics
		priceDeviation: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_price_deviation_percent",
			Help:    "Percentage deviation between intent price and submission price",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
		}, []string{"symbol"}),
		
		priceAge: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_price_age_seconds",
			Help:    "Age of price data at submission time",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"symbol"}),
		
		// Counters
		intentsCreated: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_created_total",
			Help: "Total number of intents created",
		}, []string{"symbol", "source"}),
		
		intentsRegistered: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_registered_total",
			Help: "Total number of intents registered on-chain",
		}, []string{"symbol", "chain_id"}),
		
		intentsScanned: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_scanned_total",
			Help: "Total number of intents detected by scanner",
		}, []string{"symbol", "scanner_type", "priority"}),
		
		intentsProcessed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_processed_total",
			Help: "Total number of intents processed",
		}, []string{"symbol", "router_id"}),
		
		intentsSubmitted: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_submitted_total",
			Help: "Total number of intents submitted to destination chains",
		}, []string{"symbol", "destination_chain"}),
		
		intentsConfirmed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_confirmed_total",
			Help: "Total number of intents confirmed on-chain",
		}, []string{"symbol", "destination_chain"}),
		
		intentsFailed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_intents_failed_total",
			Help: "Total number of failed intents",
		}, []string{"symbol", "stage", "error_type"}),
		
		// Router metrics
		routerDecisions: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_router_decisions_total",
			Help: "Total number of router decisions",
		}, []string{"router_id", "decision", "reason"}),
		
		routerLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_router_decision_latency_seconds",
			Help:    "Time taken for router to make decision",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		}, []string{"router_id"}),
		
		// Gas metrics
		gasUsedPerSymbol: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_gas_used_total",
			Help: "Total gas used per symbol",
		}, []string{"symbol", "destination_chain"}),
		
		gasPricePerSymbol: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_gas_price_gwei",
			Help:    "Gas price in gwei per symbol",
			Buckets: []float64{1, 5, 10, 20, 50, 100, 200, 500, 1000},
		}, []string{"symbol", "destination_chain"}),
	}
}

// IntentLifecycle tracks the complete lifecycle of an intent
type IntentLifecycle struct {
	IntentHash       string
	Symbol           string
	SourceChain      string
	DestinationChain string
	
	// Timestamps
	IntentTime       time.Time
	RegistrationTime time.Time
	ScanTime         time.Time
	ProcessingTime   time.Time
	SubmissionTime   time.Time
	ConfirmationTime time.Time
	
	// Additional metadata
	ScannerType      string
	RouterID         string
	TxHash           string
	GasPrice         float64
	Priority         string
}

// RecordIntentCreated records when an intent is created
func (m *IntentMetrics) RecordIntentCreated(intentHash, symbol, source string, timestamp time.Time) {
	m.intentsCreated.WithLabelValues(symbol, source).Inc()
	m.intentTimestamp.WithLabelValues(intentHash, symbol).Set(float64(timestamp.Unix()))
}

// RecordIntentRegistered records when an intent is registered on-chain
func (m *IntentMetrics) RecordIntentRegistered(lifecycle *IntentLifecycle) {
	chainID := lifecycle.SourceChain
	
	m.intentsRegistered.WithLabelValues(lifecycle.Symbol, chainID).Inc()
	m.registrationTimestamp.WithLabelValues(lifecycle.IntentHash, lifecycle.Symbol).Set(float64(lifecycle.RegistrationTime.Unix()))
	
	// Calculate latency from creation to registration
	if !lifecycle.IntentTime.IsZero() {
		latency := lifecycle.RegistrationTime.Sub(lifecycle.IntentTime).Seconds()
		m.intentToRegistrationLatency.WithLabelValues(lifecycle.Symbol, chainID, "oracle").Observe(latency)
	}
}

// RecordIntentScanned records when an intent is detected by scanner
func (m *IntentMetrics) RecordIntentScanned(lifecycle *IntentLifecycle) {
	m.intentsScanned.WithLabelValues(lifecycle.Symbol, lifecycle.ScannerType, lifecycle.Priority).Inc()
	m.scanTimestamp.WithLabelValues(lifecycle.IntentHash, lifecycle.Symbol, lifecycle.ScannerType).Set(float64(lifecycle.ScanTime.Unix()))
	
	// Calculate latency from registration to scan
	if !lifecycle.RegistrationTime.IsZero() {
		latency := lifecycle.ScanTime.Sub(lifecycle.RegistrationTime).Seconds()
		m.registrationToScanLatency.WithLabelValues(lifecycle.Symbol, lifecycle.SourceChain, lifecycle.ScannerType).Observe(latency)
	}
}

// RecordIntentProcessing records when intent processing starts
func (m *IntentMetrics) RecordIntentProcessing(lifecycle *IntentLifecycle) {
	m.intentsProcessed.WithLabelValues(lifecycle.Symbol, lifecycle.RouterID).Inc()
	m.processingTimestamp.WithLabelValues(lifecycle.IntentHash, lifecycle.Symbol).Set(float64(lifecycle.ProcessingTime.Unix()))
	
	// Calculate latency from scan to processing
	if !lifecycle.ScanTime.IsZero() {
		latency := lifecycle.ProcessingTime.Sub(lifecycle.ScanTime).Seconds()
		m.scanToProcessingLatency.WithLabelValues(lifecycle.Symbol, lifecycle.SourceChain, lifecycle.Priority).Observe(latency)
	}
}

// RecordIntentSubmitted records when a transaction is submitted
func (m *IntentMetrics) RecordIntentSubmitted(lifecycle *IntentLifecycle) {
	m.intentsSubmitted.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain).Inc()
	m.submissionTimestamp.WithLabelValues(lifecycle.IntentHash, lifecycle.Symbol, lifecycle.TxHash).Set(float64(lifecycle.SubmissionTime.Unix()))
	
	// Calculate latency from processing to submission
	if !lifecycle.ProcessingTime.IsZero() {
		latency := lifecycle.SubmissionTime.Sub(lifecycle.ProcessingTime).Seconds()
		m.processingToSubmissionLatency.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain, lifecycle.RouterID).Observe(latency)
	}
	
	// Record gas metrics
	if lifecycle.GasPrice > 0 {
		m.gasPricePerSymbol.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain).Observe(lifecycle.GasPrice)
	}
}

// RecordIntentConfirmed records when a transaction is confirmed
func (m *IntentMetrics) RecordIntentConfirmed(lifecycle *IntentLifecycle, gasUsed uint64) {
	m.intentsConfirmed.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain).Inc()
	m.confirmationTimestamp.WithLabelValues(lifecycle.IntentHash, lifecycle.Symbol, lifecycle.TxHash).Set(float64(lifecycle.ConfirmationTime.Unix()))
	
	// Calculate submission to confirmation latency
	if !lifecycle.SubmissionTime.IsZero() {
		latency := lifecycle.ConfirmationTime.Sub(lifecycle.SubmissionTime).Seconds()
		gasPriceTier := getGasPriceTier(lifecycle.GasPrice)
		m.submissionToConfirmationLatency.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain, gasPriceTier).Observe(latency)
	}
	
	// Calculate end-to-end latency
	if !lifecycle.IntentTime.IsZero() {
		e2eLatency := lifecycle.ConfirmationTime.Sub(lifecycle.IntentTime).Seconds()
		m.endToEndLatency.WithLabelValues(lifecycle.Symbol, lifecycle.SourceChain, lifecycle.DestinationChain).Observe(e2eLatency)
	}
	
	// Record gas used
	if gasUsed > 0 {
		m.gasUsedPerSymbol.WithLabelValues(lifecycle.Symbol, lifecycle.DestinationChain).Add(float64(gasUsed))
	}
	
	// Calculate price age
	if !lifecycle.IntentTime.IsZero() {
		priceAge := lifecycle.ConfirmationTime.Sub(lifecycle.IntentTime).Seconds()
		m.priceAge.WithLabelValues(lifecycle.Symbol).Observe(priceAge)
	}
}

// RecordIntentFailed records when an intent fails at any stage
func (m *IntentMetrics) RecordIntentFailed(symbol, stage, errorType string) {
	m.intentsFailed.WithLabelValues(symbol, stage, errorType).Inc()
}

// RecordRouterDecision records router decision metrics
func (m *IntentMetrics) RecordRouterDecision(routerID string, decision bool, reason string, latency float64) {
	decisionStr := "rejected"
	if decision {
		decisionStr = "approved"
	}
	m.routerDecisions.WithLabelValues(routerID, decisionStr, reason).Inc()
	m.routerLatency.WithLabelValues(routerID).Observe(latency)
}

// RecordPriceDeviation records price deviation between intent and submission
func (m *IntentMetrics) RecordPriceDeviation(symbol string, deviationPercent float64) {
	m.priceDeviation.WithLabelValues(symbol).Observe(deviationPercent)
}

// Helper function to categorize gas prices
func getGasPriceTier(gasPrice float64) string {
	switch {
	case gasPrice < 10:
		return "low"
	case gasPrice < 50:
		return "medium"
	case gasPrice < 200:
		return "high"
	default:
		return "very_high"
	}
}