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

	started := false
	defer func() {
		if !started {
			s.mu.Lock()
			s.running = false
			s.cancelFunc = nil
			s.mu.Unlock()
		}
	}()

	logger.Info("Starting attestor service")

	if s.config.Attestor.BatchMode {
		if err := s.processBatchAttestation(serviceCtx); err != nil {
			logger.WithError(err).Error("Initial batch attestation failed")
		}
	} else {
		for _, symbol := range s.config.Attestor.Symbols {
			select {
			case <-serviceCtx.Done():
				logger.Info("Context cancelled during initial attestations, stopping")
				return serviceCtx.Err()
			default:
			}
			if err := s.processSingleAttestation(serviceCtx, symbol); err != nil {
				logger.WithError(err).WithField("symbol", symbol).Error("Initial attestation failed")
			}
		}
	}

	ticker := time.NewTicker(s.config.Attestor.PollingTime)
	defer ticker.Stop()

	started = true

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
					select {
					case <-serviceCtx.Done():
						logger.Info("Context cancelled during attestation cycle, stopping")
						return nil
					default:
					}
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

func (s *AttestorService) shouldPublishInReplicaMode(ctx context.Context, symbol string) bool {
	if s.config.Attestor.Mode != config.ModeReplica {
		return true
	}

	latestIntent, err := s.registry.GetLatestIntentByType(ctx, s.config.Attestor.IntentType, symbol)
	if err != nil {
		logger.WithFields(map[string]interface{}{
			"symbol": symbol,
			"error":  err.Error(),
		}).Info("Replica mode: No previous intent found or error, allowing publish")
		return true
	}

	now := time.Now().Unix()
	lastTimestamp := latestIntent.Timestamp.Int64()
	timeDiff := now - lastTimestamp

	backupDelay := int64(s.config.Attestor.ReplicaBackupDelay)

	lastUpdateTime := time.Unix(lastTimestamp, 0).Format(time.RFC3339)
	timeSinceUpdate := time.Duration(timeDiff) * time.Second

	if timeDiff < backupDelay {
		logger.WithFields(map[string]interface{}{
			"symbol":            symbol,
			"last_update_time":  lastUpdateTime,
			"last_timestamp":    lastTimestamp,
			"time_since_update": timeSinceUpdate.String(),
			"backup_delay":      fmt.Sprintf("%ds", backupDelay),
			"remaining_wait":    (time.Duration(backupDelay-timeDiff) * time.Second).String(),
			"last_price":        latestIntent.Price.String(),
		}).Info("Replica mode: Skipping publish, backup delay not exceeded")
		return false
	}

	logger.WithFields(map[string]interface{}{
		"symbol":            symbol,
		"last_update_time":  lastUpdateTime,
		"last_timestamp":    lastTimestamp,
		"time_since_update": timeSinceUpdate.String(),
		"backup_delay":      fmt.Sprintf("%ds", backupDelay),
		"last_price":        latestIntent.Price.String(),
	}).Warn("Replica mode: Backup delay exceeded, TRIGGERING BACKUP PUBLISH")
	return true
}

// processSingleAttestation processes attestation for a single symbol
func (s *AttestorService) processSingleAttestation(ctx context.Context, symbol string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordProcessingDuration(symbol, "single", time.Since(start))
	}()

	logger.WithField("symbol", symbol).Debug("Processing single attestation")

	if !s.shouldPublishInReplicaMode(ctx, symbol) {
		return nil
	}

	// Get guardian params for this symbol
	guardianParams := s.config.Attestor.Guardian.GetParamsForSymbol(symbol)

	// Fetch oracle value
	fetchStart := time.Now()
	price, timestamp, err := s.oracle.GetGuardedValue(ctx, symbol, guardianParams)
	s.metrics.RecordOracleFetchDuration(symbol, time.Since(fetchStart))

	// Log oracle value retrieval with appropriate detail level based on client type
	logFields := map[string]interface{}{
		"symbol":    symbol,
		"price":     "<nil>",
		"timestamp": "<nil>",
	}
	if price != nil {
		logFields["price"] = price.String()
	}
	if timestamp != nil {
		logFields["timestamp"] = timestamp.String()
	}

	if s.config.Oracle.ClientType == config.OracleClientTypeGuarded {
		logFields["MaxDeviationBips"] = guardianParams.MaxDeviationBips
		logFields["MaxTimestampAge"] = guardianParams.MaxTimestampAge
		logFields["MinGuardianMatches"] = guardianParams.MinGuardianMatches
	}

	logger.WithFields(logFields).Info("Retrieving oracle value")

	if err != nil {
		s.metrics.RecordIntentCreated(symbol, false)
		var contractFunction string
		if s.config.Oracle.ClientType == config.OracleClientTypeGuarded {
			contractFunction = "getGuardedValue(string,uint256,uint256,uint256)"
		} else {
			contractFunction = "getValue(string)"
		}
		logger.WithFields(map[string]interface{}{
			"symbol":            symbol,
			"oracle_address":    s.config.Oracle.Address,
			"client_type":       s.config.Oracle.ClientType.String(),
			"contract_function": contractFunction,
			"error":             err.Error(),
		}).Error("Failed to fetch oracle value - check if symbol exists in oracle contract")
		return errors.NewOracleError(symbol, fmt.Sprintf("failed to fetch value (called %s)", contractFunction), err)
	}

	// Default volume
	volume := big.NewInt(1)

	logger.WithFields(map[string]interface{}{
		"symbol":             symbol,
		"price":              price.String(),
		"timestamp":          timestamp.String(),
		"MaxDeviationBips":   guardianParams.MaxDeviationBips,
		"MaxTimestampAge":    guardianParams.MaxTimestampAge,
		"MinGuardianMatches": guardianParams.MinGuardianMatches,
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

symbolLoop:
	for _, symbol := range s.config.Attestor.Symbols {
		// Check context cancellation before processing each symbol
		select {
		case <-ctx.Done():
			logger.Info("Context cancelled during batch attestation, stopping collection")
			// Return error if we haven't collected any data, otherwise proceed with partial batch
			if len(symbolData) == 0 {
				return fmt.Errorf("batch attestation cancelled: %w", ctx.Err())
			}
			// Break out of loop but continue with partial batch
			break symbolLoop
		default:
		}

		if !s.shouldPublishInReplicaMode(ctx, symbol) {
			logger.WithField("symbol", symbol).Debug("Skipping symbol in replica mode")
			continue
		}
		// Get guardian params for this symbol
		guardianParams := s.config.Attestor.Guardian.GetParamsForSymbol(symbol)

		fetchStart := time.Now()
		price, timestamp, err := s.oracle.GetGuardedValue(ctx, symbol, guardianParams)
		s.metrics.RecordOracleFetchDuration(symbol, time.Since(fetchStart))

		if err != nil {
			var contractFunction string
			if s.config.Oracle.ClientType == config.OracleClientTypeGuarded {
				contractFunction = "getGuardedValue(string,uint256,uint256,uint256)"
			} else {
				contractFunction = "getValue(string)"
			}
			logger.WithError(err).WithFields(map[string]interface{}{
				"symbol":            symbol,
				"oracle_address":    s.config.Oracle.Address,
				"client_type":       s.config.Oracle.ClientType.String(),
				"contract_function": contractFunction,
			}).Error("Failed to fetch oracle value - check if symbol exists in oracle contract")
			s.metrics.RecordIntentCreated(symbol, false)
			continue
		}

		volume := big.NewInt(1)

		logFields := map[string]interface{}{
			"symbol":    symbol,
			"price":     price.String(),
			"timestamp": timestamp.String(),
		}
		logger.WithFields(logFields).Debug("Retrieved oracle value")

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
