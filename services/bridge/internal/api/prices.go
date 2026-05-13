package api

import (
	"encoding/json"
	"net/http"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	"github.com/gorilla/mux"
)

// handleGetPrices returns all prices from the in-memory cache
func (s *Server) handleGetPrices(w http.ResponseWriter, r *http.Request) {
	priceCache, ok := s.priceCache.(*processor.PriceCache)
	if !ok || priceCache == nil {
		logger.Warn("Price cache not available")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Price cache not available",
		})
		return
	}

	prices := priceCache.GetAllPrices()

	response := make(map[string]interface{})
	for symbol, entry := range prices {
		response[symbol] = map[string]interface{}{
			"price":       entry.Price,
			"timestamp":   entry.Timestamp,
			"intent_hash": entry.IntentHash,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":  len(prices),
		"prices": response,
	})
}

// handleGetPrice returns the price for a specific symbol
func (s *Server) handleGetPrice(w http.ResponseWriter, r *http.Request) {
	priceCache, ok := s.priceCache.(*processor.PriceCache)
	if !ok || priceCache == nil {
		logger.Warn("Price cache not available")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Price cache not available",
		})
		return
	}

	vars := mux.Vars(r)
	symbol := vars["symbol"]

	if symbol == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Symbol is required",
		})
		return
	}

	priceEntry, exists := priceCache.GetPrice(symbol)
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Price not found for symbol: " + symbol,
		})
		return
	}

	response := map[string]interface{}{
		"symbol":      symbol,
		"price":       priceEntry.Price,
		"timestamp":   priceEntry.Timestamp,
		"intent_hash": priceEntry.IntentHash,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
