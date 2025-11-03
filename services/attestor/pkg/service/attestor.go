package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/errors"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
)

// AttestorService is the main service for attestation
type AttestorService struct {
	config   *config.Config
	oracle   interfaces.OracleReader
	registry interfaces.RegistryClient
	signer   interfaces.IntentSigner
	metrics  interfaces.MetricsCollector

	mu         sync.RWMutex
	running    bool
	cancelFunc context.CancelFunc
}

// NewAttestorService creates a new attestor service
func NewAttestorService(
	cfg *config.Config,
	oracle interfaces.OracleReader,
	registry interfaces.RegistryClient,
	signer interfaces.IntentSigner,
	metrics interfaces.MetricsCollector,
) *AttestorService {
	return &AttestorService{
		config:   cfg,
		oracle:   oracle,
		registry: registry,
		signer:   signer,
		metrics:  metrics,
	}
}

// Start starts the attestor service
func (s *AttestorService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("service already running")
	}

	serviceCtx, cancel := context.WithCancel(ctx)
	// Set both cancelFunc and running atomically under lock
	s.cancelFunc = cancel
	s.running = true
	s.mu.Unlock()

	// Defer cleanup in case of early return
	defer func() {
		s.mu.Lock()
		s.running = false
		s.cancelFunc = nil
		s.mu.Unlock()
	}()

	logger.Info("Starting attestor service")

	// Process initial attestations
	if s.config.Attestor.BatchMode {
		if err := s.processBatchAttestation(serviceCtx); err != nil {
			logger.WithError(err).Error("Initial batch attestation failed")
		}
	} else {
		for _, symbol := range s.config.Attestor.Symbols {
			if err := s.processSingleAttestation(serviceCtx, symbol); err != nil {
				logger.WithError(err).WithField("symbol", symbol).Error("Initial attestation failed")
			}
		}
	}

	// Start periodic attestation
	ticker := time.NewTicker(s.config.Attestor.PollingTime)
	defer ticker.Stop()

	for {
		select {
		case <-serviceCtx.Done():
			logger.Info("Attestor service stopped")
			return nil
		case <-ticker.C:
			if s.config.Attestor.BatchMode {
				if err := s.processBatchAttestation(serviceCtx); err != nil {
					logger.WithError(err).Error("Batch attestation failed")
				}
			} else {
				for _, symbol := range s.config.Attestor.Symbols {
					if err := s.processSingleAttestation(serviceCtx, symbol); err != nil {
						logger.WithError(err).WithField("symbol", symbol).Error("Attestation failed")
					}
				}
			}
		}
	}
}

// Stop stops the attestor service
func (s *AttestorService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("service not running")
	}

	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	s.running = false
	return nil
}

// IsRunning returns whether the service is running
func (s *AttestorService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// processSingleAttestation processes attestation for a single symbol
func (s *AttestorService) processSingleAttestation(ctx context.Context, symbol string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordProcessingDuration(symbol, "single", time.Since(start))
	}()

	logger.WithField("symbol", symbol).Debug("Processing single attestation")

	// Get guardian params for this symbol
	guardianParams := s.config.Attestor.Guardian.GetParamsForSymbol(symbol)

	// Fetch oracle value
	fetchStart := time.Now()
	price, timestamp, err := s.oracle.GetGuardedValue(ctx, symbol, guardianParams)
	s.metrics.RecordOracleFetchDuration(symbol, time.Since(fetchStart))

	if err != nil {
		s.metrics.RecordIntentCreated(symbol, false)
		return errors.NewOracleError(symbol, "failed to fetch value", err)
	}

	// Default volume
	volume := big.NewInt(1)

	logger.WithFields(map[string]interface{}{
		"symbol":    symbol,
		"price":     price.String(),
		"timestamp": timestamp.String(),
	}).Debug("Retrieved oracle value")

	// Sign intent
	signedIntent, err := s.signer.SignIntent(ctx, price, volume, symbol)
	if err != nil {
		s.metrics.RecordIntentCreated(symbol, false)
		return errors.NewSignerError("sign intent", symbol, err)
	}
	s.metrics.RecordIntentCreated(symbol, true)

	// Publish intent
	txHash, err := s.registry.PublishIntent(ctx, signedIntent)
	if err != nil {
		s.metrics.RecordIntentPublished(symbol, false)
		return errors.NewRegistryError("publish", "", err)
	}
	s.metrics.RecordIntentPublished(symbol, true)

	logger.WithFields(map[string]interface{}{
		"symbol":   symbol,
		"tx_hash":  txHash,
		"duration": time.Since(start).String(),
	}).Info("Successfully published intent")

	return nil
}

// processBatchAttestation processes attestation for multiple symbols
func (s *AttestorService) processBatchAttestation(ctx context.Context) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordProcessingDuration("batch", "batch", time.Since(start))
	}()

	logger.WithField("symbol_count", len(s.config.Attestor.Symbols)).Info("Processing batch attestation")

	// Collect symbol data
	symbolData := make([]interfaces.SymbolData, 0, len(s.config.Attestor.Symbols))

	for _, symbol := range s.config.Attestor.Symbols {
		// Get guardian params for this symbol
		guardianParams := s.config.Attestor.Guardian.GetParamsForSymbol(symbol)

		fetchStart := time.Now()
		price, timestamp, err := s.oracle.GetGuardedValue(ctx, symbol, guardianParams)
		s.metrics.RecordOracleFetchDuration(symbol, time.Since(fetchStart))

		if err != nil {
			logger.WithError(err).WithField("symbol", symbol).Error("Failed to fetch oracle value")
			s.metrics.RecordIntentCreated(symbol, false)
			continue
		}

		volume := big.NewInt(1)

		logger.WithFields(map[string]interface{}{
			"symbol":    symbol,
			"price":     price.String(),
			"timestamp": timestamp.String(),
		}).Debug("Retrieved oracle value")

		symbolData = append(symbolData, interfaces.SymbolData{
			Symbol: symbol,
			Price:  price,
			Volume: volume,
		})
		s.metrics.RecordIntentCreated(symbol, true)
	}

	if len(symbolData) == 0 {
		return fmt.Errorf("no valid symbol data collected")
	}

	// Sign batch intent
	signedIntent, err := s.signer.SignBatchIntent(ctx, symbolData)
	if err != nil {
		for _, data := range symbolData {
			s.metrics.RecordIntentPublished(data.Symbol, false)
		}
		return errors.NewSignerError("sign batch intent", fmt.Sprintf("%d symbols", len(symbolData)), err)
	}

	// Publish batch intent
	txHash, err := s.registry.PublishBatchIntents(ctx, signedIntent)
	if err != nil {
		for _, data := range symbolData {
			s.metrics.RecordIntentPublished(data.Symbol, false)
		}
		return errors.NewRegistryError("publish batch", "", err)
	}

	for _, data := range symbolData {
		s.metrics.RecordIntentPublished(data.Symbol, true)
	}

	logger.WithFields(map[string]interface{}{
		"symbol_count": len(symbolData),
		"tx_hash":      txHash,
		"duration":     time.Since(start).String(),
	}).Info("Successfully published batch intent")

	return nil
}

// Health returns the health status of the service
func (s *AttestorService) Health() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"running": s.running,
		"config": map[string]interface{}{
			"symbols":      s.config.Attestor.Symbols,
			"batch_mode":   s.config.Attestor.BatchMode,
			"polling_time": s.config.Attestor.PollingTime.String(),
		},
	}
}
