package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/attestor/pkg/service"
)

// Server represents the API server
type Server struct {
	config   *config.Config
	attestor *service.AttestorService
	server   *http.Server
	mux      *http.ServeMux
}

// NewServer creates a new API server
func NewServer(cfg *config.Config, attestor *service.AttestorService) *Server {
	mux := http.NewServeMux()
	
	server := &Server{
		config:   cfg,
		attestor: attestor,
		mux:      mux,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.API.Port),
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
	
	server.setupRoutes()
	return server
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/ready", s.handleReady)
	s.mux.HandleFunc("/status", s.handleStatus)
}

// Start starts the API server
func (s *Server) Start() error {
	logger.WithField("port", s.config.API.Port).Info("Starting API server")
	
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("API server error: %w", err)
	}
	
	return nil
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	logger.Info("Stopping API server")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown API server: %w", err)
	}
	
	return nil
}

// handleHealth handles the /health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	response := map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleReady handles the /ready endpoint
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Check if attestor service is running
	if !s.attestor.IsRunning() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"reason": "attestor service not running",
		})
		return
	}
	
	response := map[string]interface{}{
		"status": "ready",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStatus handles the /status endpoint
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	health := s.attestor.Health()
	
	response := map[string]interface{}{
		"status":   "operational",
		"time":     time.Now().UTC().Format(time.RFC3339),
		"attestor": health,
		"config": map[string]interface{}{
			"api_port":     s.config.API.Port,
			"metrics_port": s.config.Metrics.Port,
			"log_level":    s.config.Logging.Level,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}