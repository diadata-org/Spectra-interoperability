package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/health"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

const (
	// Version of the bridge service
	Version = "1.0.0"
)

// Server represents the API server
type Server struct {
	config         *config.APIConfig
	cfg            *config.Config // Full config for failover handler
	db             *database.DB
	healthMonitor  *health.HealthMonitor
	metrics        *metrics.Collector
	routerRegistry interface{} // Will be *router.Registry when available

	router          *mux.Router
	httpServer      *http.Server
	failoverHandler *FailoverHandler
}

// NewServer creates a new API server
func NewServer(
	cfg *config.Config,
	db *database.DB,
	healthMonitor *health.HealthMonitor,
	metricsCollector *metrics.Collector,
	routerRegistry interface{}, // Pass as interface{} to avoid import cycle
) *Server {
	s := &Server{
		config:         &cfg.API,
		cfg:            cfg,
		db:             db,
		healthMonitor:  healthMonitor,
		metrics:        metricsCollector,
		routerRegistry: routerRegistry,
		router:         mux.NewRouter(),
	}

	logrus.Info("Creating failover handler")

	var failoverMetrics *metrics.Metrics
	var intentMetrics *metrics.IntentMetrics
	if metricsCollector != nil {
		if metricsCollector.FailoverMetrics != nil {
			failoverMetrics = metricsCollector.FailoverMetrics
			logrus.Info("Using shared metrics instance for failover handler")
		}
		if metricsCollector.IntentMetrics != nil {
			intentMetrics = metricsCollector.IntentMetrics
			logrus.Info("Using shared intent metrics instance for failover handler")
		}
	} else {
		logrus.Warn("Metrics collector not available, failover handler will run without metrics")
	}

	failoverHandler, err := NewFailoverHandler(cfg, db, failoverMetrics, intentMetrics)
	if err != nil {
		logrus.WithError(err).Error("Failed to create failover handler")
	} else {
		s.failoverHandler = failoverHandler
		if failoverMetrics != nil && intentMetrics != nil {
			logrus.Info("Failover handler created successfully with integrated metrics and intent metrics")
		} else if failoverMetrics != nil {
			logrus.Info("Failover handler created successfully with integrated metrics only")
		} else {
			logrus.Info("Failover handler created successfully without metrics")
		}
	}

	logrus.Info("Setting up routes")
	s.setupRoutes()
	logrus.Info("Routes setup complete")

	s.httpServer = &http.Server{
		Addr:         s.config.ListenAddr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the API server
func (s *Server) Start(ctx context.Context) error {
	logger.Infof("Starting API server on %s", s.config.ListenAddr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("API server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	logger.Info("Stopping API server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}

// GetFailoverHandler returns the failover handler instance
func (s *Server) GetFailoverHandler() *FailoverHandler {
	return s.failoverHandler
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/health/ready", s.handleReadiness).Methods("GET")
	s.router.HandleFunc("/health/live", s.handleLiveness).Methods("GET")

	// Metrics endpoint
	s.router.Handle("/metrics", promhttp.Handler())

	// Debug endpoint
	s.router.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":                 "Debug endpoint working",
			"failover_handler_exists": s.failoverHandler != nil,
		})
	}).Methods("GET")

	// API v1 routes
	v1 := s.router.PathPrefix("/api/v1").Subrouter()

	// Status endpoints
	v1.HandleFunc("/status", s.handleStatus).Methods("GET")
	v1.HandleFunc("/status/components", s.handleComponentStatus).Methods("GET")

	// Event endpoints
	v1.HandleFunc("/events", s.handleGetEvents).Methods("GET")
	v1.HandleFunc("/events/names", s.handleGetEventNames).Methods("GET")
	v1.HandleFunc("/events/{hash}", s.handleGetEvent).Methods("GET")

	// Transaction endpoints
	v1.HandleFunc("/transactions", s.handleGetTransactions).Methods("GET")
	v1.HandleFunc("/transactions/{hash}", s.handleGetTransaction).Methods("GET")

	// Chain endpoints
	v1.HandleFunc("/chains", s.handleGetChains).Methods("GET")
	v1.HandleFunc("/chains/{id}/status", s.handleGetChainStatus).Methods("GET")

	// Symbol endpoints
	v1.HandleFunc("/symbols", s.handleGetSymbols).Methods("GET")
	v1.HandleFunc("/symbols/{symbol}/updates", s.handleGetSymbolUpdates).Methods("GET")

	// Failover endpoints (if available)
	if s.failoverHandler != nil {
		logrus.Info("Registering failover routes - handler is NOT nil")
		s.failoverHandler.RegisterRoutes(v1)
		logrus.Info("Failover routes registered with v1 subrouter")
	} else {
		logrus.Warn("Failover handler is nil, not registering failover routes")
	}

	// Middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	if s.config.EnableCORS {
		s.router.Use(s.corsMiddleware)
	}
}

// Health check handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := s.healthMonitor.IsHealthy()

	response := map[string]interface{}{
		"status":    "ok",
		"healthy":   health,
		"timestamp": time.Now().UTC(),
	}

	if !health {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	s.writeJSON(w, response)
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Check if all components are ready
	status := s.healthMonitor.GetStatus()
	ready := true

	for _, component := range status {
		if !component.Healthy {
			ready = false
			break
		}
	}

	if ready {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	// Simple liveness check
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("alive"))
}

// Status handlers

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Get overall system status
	componentStatus := s.healthMonitor.GetStatus()

	// Get basic statistics
	stats, err := s.getSystemStats()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get system stats", err)
		return
	}

	response := map[string]interface{}{
		"status":     "operational",
		"version":    Version,
		"uptime":     s.getUptime(),
		"components": componentStatus,
		"statistics": stats,
	}

	s.writeJSON(w, response)
}

func (s *Server) handleComponentStatus(w http.ResponseWriter, r *http.Request) {
	status := s.healthMonitor.GetStatus()
	s.writeJSON(w, status)
}

// Event handlers

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	startBlock := s.parseUint64Param(r, "start_block", 0)
	endBlock := s.parseUint64Param(r, "end_block", 0)
	limit := s.parseIntParam(r, "limit", 100)
	offset := s.parseIntParam(r, "offset", 0)
	eventName := r.URL.Query().Get("eventName")

	// Validate parameters
	if limit > 1000 {
		limit = 1000
	}

	// Query events
	events, err := s.queryEvents(startBlock, endBlock, limit, offset, eventName)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to query events", err)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"events": events,
		"count":  len(events),
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleGetEventNames(w http.ResponseWriter, r *http.Request) {
	eventNames, err := s.getEventNames()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get event names", err)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"eventNames": eventNames,
		"count":      len(eventNames),
	})
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hash := vars["hash"]

	event, err := s.getEventByHash(hash)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get event", err)
		return
	}

	if event == nil {
		s.writeError(w, http.StatusNotFound, "Event not found", nil)
		return
	}

	s.writeJSON(w, event)
}

// Transaction handlers

func (s *Server) handleGetTransactions(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	chainID := s.parseInt64Param(r, "chain_id", 0)
	status := r.URL.Query().Get("status")
	limit := s.parseIntParam(r, "limit", 100)
	offset := s.parseIntParam(r, "offset", 0)

	// Validate parameters
	if limit > 1000 {
		limit = 1000
	}

	// Query transactions
	transactions, err := s.queryTransactions(chainID, status, limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to query transactions", err)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"transactions": transactions,
		"count":        len(transactions),
		"limit":        limit,
		"offset":       offset,
	})
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	hash := vars["hash"]

	transaction, err := s.getTransactionByHash(hash)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get transaction", err)
		return
	}

	if transaction == nil {
		s.writeError(w, http.StatusNotFound, "Transaction not found", nil)
		return
	}

	s.writeJSON(w, transaction)
}

// Chain handlers

func (s *Server) handleGetChains(w http.ResponseWriter, r *http.Request) {
	chains, err := s.getConfiguredChains()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get chains", err)
		return
	}

	s.writeJSON(w, chains)
}

func (s *Server) handleGetChainStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainID := s.parseChainID(vars["id"])
	if chainID == 0 {
		s.writeError(w, http.StatusBadRequest, "Invalid chain ID", nil)
		return
	}

	status, err := s.db.GetChainState(chainID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get chain status", err)
		return
	}

	s.writeJSON(w, status)
}

// Symbol handlers

func (s *Server) handleGetSymbols(w http.ResponseWriter, r *http.Request) {
	symbols, err := s.getSupportedSymbols()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get symbols", err)
		return
	}

	s.writeJSON(w, symbols)
}

func (s *Server) handleGetSymbolUpdates(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]

	chainID := s.parseInt64Param(r, "chain_id", 0)
	contractAddr := r.URL.Query().Get("contract")
	limit := s.parseIntParam(r, "limit", 100)

	updates, err := s.getSymbolUpdates(symbol, chainID, contractAddr, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get symbol updates", err)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"symbol":  symbol,
		"updates": updates,
		"count":   len(updates),
	})
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		logger.Infof("%s %s %d %s", r.Method, r.URL.Path, wrapped.statusCode, duration)
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		s.metrics.RecordHTTPRequest(r.Method, r.URL.Path, wrapped.statusCode, duration)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
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

// Helper methods

func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Errorf("Failed to encode JSON response: %v", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, code int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	if err != nil {
		response["details"] = err.Error()
	}

	json.NewEncoder(w).Encode(response)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
