package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/utils"
)

// APIServer represents the API server
type APIServer struct {
	bridge *bridge.Bridge
	port   int
}

// NewAPIServer creates a new API server
func NewAPIServer(bridgeService *bridge.Bridge, port int) *APIServer {
	return &APIServer{
		bridge: bridgeService,
		port:   port,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status          string                 `json:"status"`
	Timestamp       time.Time              `json:"timestamp"`
	Uptime          string                 `json:"uptime"`
	UptimeFormatted string                 `json:"uptime_formatted"`
	Chains          map[string]interface{} `json:"chains"`
}

// StatsResponse represents the stats response
type StatsResponse struct {
	Status          string                 `json:"status"`
	Timestamp       time.Time              `json:"timestamp"`
	Uptime          string                 `json:"uptime"`
	UptimeFormatted string                 `json:"uptime_formatted"`
	Operations      map[string]interface{} `json:"operations"`
	Chains          map[string]interface{} `json:"chains"`
	Performance     map[string]interface{} `json:"performance"`
}

// healthHandler handles health check requests
func (s *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats := s.bridge.GetStats()
	
	response := HealthResponse{
		Status:          "healthy",
		Timestamp:       time.Now(),
		Uptime:          stats.Uptime.String(),
		UptimeFormatted: utils.FormatDurationVerbose(stats.Uptime),
		Chains:          make(map[string]interface{}),
	}
	
	// Add chain status
	for chainID, chainStats := range stats.ChainStats {
		response.Chains[chainStats.Name] = map[string]interface{}{
			"chain_id":          chainID,
			"connected":         chainStats.Connected,
			"latest_block":      chainStats.LatestBlock,
			"synced_block":      chainStats.SyncedBlock,
			"last_health_check": chainStats.LastHealthCheck,
			"pending_tx":        chainStats.PendingTxCount,
			"successful_tx":     chainStats.SuccessfulTxCount,
			"failed_tx":         chainStats.FailedTxCount,
		}
	}
	
	// Set appropriate HTTP status
	statusCode := http.StatusOK
	for _, chainStats := range stats.ChainStats {
		if !chainStats.Connected {
			response.Status = "degraded"
			statusCode = http.StatusServiceUnavailable
			break
		}
	}
	
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// statsHandler handles statistics requests
func (s *APIServer) statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats := s.bridge.GetStats()
	
	response := StatsResponse{
		Status:          "ok",
		Timestamp:       time.Now(),
		Uptime:          stats.Uptime.String(),
		UptimeFormatted: utils.FormatDurationVerbose(stats.Uptime),
		Operations: map[string]interface{}{
			"total":      stats.TotalOperations,
			"successful": stats.SuccessfulOps,
			"failed":     stats.FailedOps,
			"pending":    stats.PendingOps,
			"processing": stats.ProcessingOps,
			"retrying":   stats.RetryingOps,
		},
		Chains: make(map[string]interface{}),
		Performance: map[string]interface{}{
			"last_processed_block": stats.LastProcessedBlock,
			"start_time":          stats.StartTime,
			"uptime_seconds":      stats.Uptime.Seconds(),
		},
	}
	
	// Add detailed chain statistics
	for chainID, chainStats := range stats.ChainStats {
		response.Chains[chainStats.Name] = map[string]interface{}{
			"chain_id":          chainID,
			"connected":         chainStats.Connected,
			"latest_block":      chainStats.LatestBlock,
			"synced_block":      chainStats.SyncedBlock,
			"last_health_check": chainStats.LastHealthCheck,
			"last_error":        chainStats.LastError,
			"pending_tx":        chainStats.PendingTxCount,
			"successful_tx":     chainStats.SuccessfulTxCount,
			"failed_tx":         chainStats.FailedTxCount,
		}
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// metricsHandler handles Prometheus metrics requests
func (s *APIServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	
	stats := s.bridge.GetStats()
	
	// Generate Prometheus metrics
	metrics := ""
	
	// Bridge uptime
	metrics += "# HELP bridge_uptime_seconds Bridge uptime in seconds\n"
	metrics += "# TYPE bridge_uptime_seconds gauge\n"
	metrics += "bridge_uptime_seconds " + formatFloat(stats.Uptime.Seconds()) + "\n"
	
	// Operations metrics
	metrics += "# HELP bridge_operations_total Total number of bridge operations\n"
	metrics += "# TYPE bridge_operations_total counter\n"
	metrics += "bridge_operations_total{status=\"total\"} " + formatInt(stats.TotalOperations) + "\n"
	metrics += "bridge_operations_total{status=\"successful\"} " + formatInt(stats.SuccessfulOps) + "\n"
	metrics += "bridge_operations_total{status=\"failed\"} " + formatInt(stats.FailedOps) + "\n"
	metrics += "bridge_operations_total{status=\"pending\"} " + formatInt(stats.PendingOps) + "\n"
	metrics += "bridge_operations_total{status=\"processing\"} " + formatInt(stats.ProcessingOps) + "\n"
	metrics += "bridge_operations_total{status=\"retrying\"} " + formatInt(stats.RetryingOps) + "\n"
	
	// Chain metrics
	metrics += "# HELP bridge_chain_connected Chain connection status\n"
	metrics += "# TYPE bridge_chain_connected gauge\n"
	metrics += "# HELP bridge_chain_latest_block Latest block number for each chain\n"
	metrics += "# TYPE bridge_chain_latest_block gauge\n"
	metrics += "# HELP bridge_chain_transactions_total Total transactions per chain\n"
	metrics += "# TYPE bridge_chain_transactions_total counter\n"
	
	for chainID, chainStats := range stats.ChainStats {
		chainLabel := "chain_id=\"" + formatInt(chainID) + "\",name=\"" + chainStats.Name + "\""
		
		connected := "0"
		if chainStats.Connected {
			connected = "1"
		}
		
		metrics += "bridge_chain_connected{" + chainLabel + "} " + connected + "\n"
		metrics += "bridge_chain_latest_block{" + chainLabel + "} " + formatInt(int64(chainStats.LatestBlock)) + "\n"
		metrics += "bridge_chain_transactions_total{" + chainLabel + ",status=\"successful\"} " + formatInt(int64(chainStats.SuccessfulTxCount)) + "\n"
		metrics += "bridge_chain_transactions_total{" + chainLabel + ",status=\"failed\"} " + formatInt(int64(chainStats.FailedTxCount)) + "\n"
		metrics += "bridge_chain_transactions_total{" + chainLabel + ",status=\"pending\"} " + formatInt(int64(chainStats.PendingTxCount)) + "\n"
	}
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metrics))
}

// Start starts the API server
func (s *APIServer) Start() error {
	mux := http.NewServeMux()
	
	// Register handlers
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/stats", s.statsHandler)
	mux.HandleFunc("/metrics", s.metricsHandler)
	
	// Add CORS middleware
	handler := corsMiddleware(mux)
	
	logger.Infof("Starting API server on port %d", s.port)
	logger.Infof("Endpoints available:")
	logger.Infof("  - GET /health  - Health check with uptime")
	logger.Infof("  - GET /stats   - Detailed statistics")
	logger.Infof("  - GET /metrics - Prometheus metrics")
	
	return http.ListenAndServe(":"+formatInt(int64(s.port)), handler)
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// Helper functions
func formatInt(i int64) string {
	return fmt.Sprintf("%d", i)
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}