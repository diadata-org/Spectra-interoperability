package metrics

import (
	"context"
	"fmt"
	"net/http"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// IntentsCreated tracks the number of intents created
	IntentsCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "attestor_intents_created_total",
			Help: "Total number of intents created",
		},
		[]string{"symbol", "status"},
	)

	// IntentsPublished tracks the number of intents published
	IntentsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "attestor_intents_published_total",
			Help: "Total number of intents published",
		},
		[]string{"symbol", "status"},
	)

	// OracleValueFetchDuration tracks the duration of oracle value fetches
	OracleValueFetchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "attestor_oracle_fetch_duration_seconds",
			Help: "Duration of oracle value fetches",
		},
		[]string{"symbol"},
	)

	// ProcessingDuration tracks the duration of processing
	ProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "attestor_processing_duration_seconds",
			Help: "Duration of attestation processing",
		},
		[]string{"symbol", "type"},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(IntentsCreated)
	prometheus.MustRegister(IntentsPublished)
	prometheus.MustRegister(OracleValueFetchDuration)
	prometheus.MustRegister(ProcessingDuration)
}

// MetricsServer represents a metrics server with graceful shutdown
type MetricsServer struct {
	server *http.Server
}

// NewMetricsServer creates a new metrics server
func NewMetricsServer(port int) *MetricsServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", port)
	return &MetricsServer{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

// Start starts the metrics server
func (m *MetricsServer) Start() error {
	logger.Infof("Starting metrics server on %s", m.server.Addr)
	if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}
	return nil
}

// Stop gracefully stops the metrics server
func (m *MetricsServer) Stop(ctx context.Context) error {
	logger.Info("Stopping metrics server")
	return m.server.Shutdown(ctx)
}

// StartMetricsServer starts the Prometheus metrics server (deprecated - use NewMetricsServer)
func StartMetricsServer(port int) {
	server := NewMetricsServer(port)
	if err := server.Start(); err != nil {
		logger.Errorf("Failed to start metrics server: %v", err)
	}
}
