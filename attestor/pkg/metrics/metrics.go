package metrics

import (
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/logger"
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

// StartMetricsServer starts the Prometheus metrics server
func StartMetricsServer(port string) {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	logger.Infof("Starting metrics server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Errorf("Failed to start metrics server: %v", err)
	}
}