package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
)

func TestServer_handleHealth(t *testing.T) {
	// Create a minimal server for testing
	server := &Server{
		config: &config.Config{
			API: config.APIConfig{Port: 8081},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", body["status"])
	}

	if _, ok := body["time"]; !ok {
		t.Error("Expected 'time' field in response")
	}
}

func TestServer_handleHealth_InvalidMethod(t *testing.T) {
	server := &Server{
		config: &config.Config{
			API: config.APIConfig{Port: 8081},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}
