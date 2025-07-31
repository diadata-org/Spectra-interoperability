package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/config"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/blockchain"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/database"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/failover"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

type Service struct {
	config          *config.Config
	db              *database.Repository
	sourceClients   map[int]*blockchain.ChainClient
	destClients     map[int]*blockchain.ChainClient
	eventListeners  []*EventListener
	deliveryChecker *DeliveryChecker
	bridgeClient    *failover.BridgeClient
	metrics         *metrics.Metrics
	wg              sync.WaitGroup
	cancel          context.CancelFunc
}

func NewService(cfg *config.Config, db *database.Repository) (*Service, error) {
	sourceClients := make(map[int]*blockchain.ChainClient)
	destClients := make(map[int]*blockchain.ChainClient)

	for _, pair := range cfg.MonitoringPairs {
		if _, exists := sourceClients[pair.Source.ChainID]; !exists {
			chainCfg, exists := cfg.GetChainConfig(pair.Source.ChainID)
			if !exists {
				return nil, fmt.Errorf("chain config not found for source chain %d", pair.Source.ChainID)
			}
			client, err := blockchain.NewChainClient(
				pair.Source.ChainID,
				chainCfg.Name,
				chainCfg.RPCURLs,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create source client for chain %d: %w", pair.Source.ChainID, err)
			}
			sourceClients[pair.Source.ChainID] = client
		}

		if _, exists := destClients[pair.Destination.ChainID]; !exists {
			chainCfg, exists := cfg.GetChainConfig(pair.Destination.ChainID)
			if !exists {
				return nil, fmt.Errorf("chain config not found for destination chain %d", pair.Destination.ChainID)
			}
			client, err := blockchain.NewChainClient(
				pair.Destination.ChainID,
				chainCfg.Name,
				chainCfg.RPCURLs,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create destination client for chain %d: %w", pair.Destination.ChainID, err)
			}
			destClients[pair.Destination.ChainID] = client
		}
	}

	bridgeClient := failover.NewBridgeClient(
		cfg.BridgeAPI.BaseURL,
		config.GetDuration(cfg.BridgeAPI.Timeout, "30s"),
		cfg.BridgeAPI.RetryAttempts,
		config.GetDuration(cfg.BridgeAPI.RetryDelay, "5s"),
	)

	serviceMetrics := metrics.NewMetrics()

	deliveryChecker := NewDeliveryChecker(
		db,
		destClients,
		bridgeClient,
		serviceMetrics,
		30*time.Second,
	)

	return &Service{
		config:          cfg,
		db:              db,
		sourceClients:   sourceClients,
		destClients:     destClients,
		eventListeners:  make([]*EventListener, 0),
		deliveryChecker: deliveryChecker,
		bridgeClient:    bridgeClient,
		metrics:         serviceMetrics,
	}, nil
}
func (s *Service) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	logger.Info("Starting Hyperlane monitoring service")

	// Initialize database (ensure pairs and receivers are saved)
	if err := s.initializeDatabase(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Check Bridge API health
	if err := s.bridgeClient.CheckHealth(ctx); err != nil {
		logger.WithError(err).Warn("Bridge API health check failed - failover may not work")
	}

	// Create event listeners for each monitoring pair
	for _, pairCfg := range s.config.MonitoringPairs {
		if err := s.createPairMonitor(pairCfg); err != nil {
			logger.WithError(err).WithField("pair", config.GetPairID(
				pairCfg.Source.ChainID,
				pairCfg.Destination.ChainID,
			)).Error("Failed to create pair monitor")
			continue
		}
	}

	// Start event listeners
	for _, listener := range s.eventListeners {
		s.wg.Add(1)
		go func(l *EventListener) {
			defer s.wg.Done()
			if err := l.Start(ctx); err != nil && err != context.Canceled {
				logger.WithError(err).Error("Event listener failed")
			}
		}(listener)
	}

	// Start delivery checker
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.deliveryChecker.Start(ctx); err != nil && err != context.Canceled {
			logger.WithError(err).Error("Delivery checker failed")
		}
	}()

	// Start metrics updater
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.updateMetrics(ctx)
	}()

	logger.Info("Hyperlane monitoring service started")
	return nil
}

// Stop gracefully stops the monitoring service
func (s *Service) Stop() {
	logger.Info("Stopping Hyperlane monitoring service")

	if s.cancel != nil {
		s.cancel()
	}

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Close blockchain clients
	for _, client := range s.sourceClients {
		client.Close()
	}
	for _, client := range s.destClients {
		client.Close()
	}

	logger.Info("Hyperlane monitoring service stopped")
}

// initializeDatabase ensures all configuration is saved to database
func (s *Service) initializeDatabase() error {
	for _, pairCfg := range s.config.MonitoringPairs {
		pairID := config.GetPairID(pairCfg.Source.ChainID, pairCfg.Destination.ChainID)

		// Save monitoring pair
		pair := &database.MonitoringPair{
			PairID:                pairID,
			SourceChainID:         pairCfg.Source.ChainID,
			SourceChainName:       s.getChainName(pairCfg.Source.ChainID),
			OracleTriggerAddress:  pairCfg.Source.OracleTrigger,
			OracleRegistryAddress: pairCfg.Source.OracleRegistry,
			DestinationChainID:    pairCfg.Destination.ChainID,
			DestinationChainName:  s.getChainName(pairCfg.Destination.ChainID),
			Enabled:               true,
			LastProcessedBlock:    pairCfg.Source.StartBlock,
		}

		if err := s.db.SaveOrUpdatePair(pair); err != nil {
			return fmt.Errorf("failed to save pair %s: %w", pairID, err)
		}

		// Save receivers
		for _, receiverCfg := range pairCfg.Destination.Receivers {
			if !receiverCfg.Monitoring.Enabled {
				continue
			}

			// Apply profile settings if specified
			profile := s.getMonitoringProfile(&receiverCfg)

			receiver := &database.PairReceiver{
				PairID:                 pairID,
				ReceiverAddress:        receiverCfg.Address,
				ReceiverName:           receiverCfg.Name,
				Enabled:                receiverCfg.Monitoring.Enabled,
				MonitoringProfile:      receiverCfg.Monitoring.Profile,
				CheckIntervalSeconds:   int(profile.CheckInterval.Seconds()),
				InitialWaitSeconds:     int(profile.InitialWait.Seconds()),
				MaxDeliveryWaitSeconds: int(profile.MaxDeliveryWait.Seconds()),
				MaxCheckAttempts:       profile.MaxCheckAttempts,
				Priority:               profile.Priority,
				AlertOnFailure:         receiverCfg.Monitoring.AlertOnFailure,
				AlertWebhook:           receiverCfg.Monitoring.AlertWebhook,
			}

			if err := s.db.SaveOrUpdateReceiver(receiver); err != nil {
				return fmt.Errorf("failed to save receiver %s: %w", receiverCfg.Address, err)
			}
		}
	}

	return nil
}

// createPairMonitor creates monitoring for a source-destination pair
func (s *Service) createPairMonitor(pairCfg config.MonitoringPairConfig) error {
	pairID := config.GetPairID(pairCfg.Source.ChainID, pairCfg.Destination.ChainID)

	// Get pair from database
	pairs, err := s.db.GetMonitoringPairs()
	if err != nil {
		return err
	}

	var pair *database.MonitoringPair
	for _, p := range pairs {
		if p.PairID == pairID {
			pair = &p
			break
		}
	}

	if pair == nil {
		return fmt.Errorf("pair %s not found in database", pairID)
	}

	// Get receivers
	dbReceivers, err := s.db.GetPairReceivers(pairID)
	if err != nil {
		return err
	}

	// Convert to types.ReceiverConfig
	receivers := make([]types.ReceiverConfig, 0, len(dbReceivers))
	for _, dbRcv := range dbReceivers {
		receivers = append(receivers, types.ReceiverConfig{
			Address:          dbRcv.ReceiverAddress,
			Name:             dbRcv.ReceiverName,
			Enabled:          dbRcv.Enabled,
			Profile:          dbRcv.MonitoringProfile,
			CheckInterval:    time.Duration(dbRcv.CheckIntervalSeconds) * time.Second,
			InitialWait:      time.Duration(dbRcv.InitialWaitSeconds) * time.Second,
			MaxDeliveryWait:  time.Duration(dbRcv.MaxDeliveryWaitSeconds) * time.Second,
			MaxCheckAttempts: dbRcv.MaxCheckAttempts,
			Priority:         dbRcv.Priority,
			AlertOnFailure:   dbRcv.AlertOnFailure,
			AlertWebhook:     dbRcv.AlertWebhook,
		})
	}

	// Add receivers to delivery checker
	s.deliveryChecker.AddPairReceivers(pairID, receivers)

	// Create event listener
	sourceClient := s.sourceClients[pairCfg.Source.ChainID]
	chainCfg, exists := s.config.GetChainConfig(pairCfg.Source.ChainID)
	if !exists {
		return fmt.Errorf("chain config not found for source chain %d", pairCfg.Source.ChainID)
	}
	scanInterval := config.GetDuration(chainCfg.ScanInterval, "10s")

	listener, err := NewEventListener(
		pair,
		receivers,
		sourceClient,
		s.db,
		s.metrics,
		scanInterval,
	)
	if err != nil {
		return err
	}

	s.eventListeners = append(s.eventListeners, listener)

	logger.WithFields(logger.Fields{
		"pair_id":   pairID,
		"source":    pair.SourceChainName,
		"dest":      pair.DestinationChainName,
		"receivers": len(receivers),
	}).Info("Created pair monitor")

	return nil
}

// getMonitoringProfile returns the effective monitoring profile for a receiver
func (s *Service) getMonitoringProfile(receiverCfg *config.ReceiverConfig) *types.ReceiverConfig {
	// Start with defaults
	result := &types.ReceiverConfig{
		CheckInterval:    30 * time.Second,
		InitialWait:      2 * time.Minute,
		MaxDeliveryWait:  10 * time.Minute,
		MaxCheckAttempts: 20,
		Priority:         "medium",
	}

	// Apply profile if specified
	if receiverCfg.Monitoring.Profile != "" {
		if profile, exists := s.config.MonitoringProfiles[receiverCfg.Monitoring.Profile]; exists {
			result.CheckInterval = config.GetDuration(profile.CheckInterval, "30s")
			result.InitialWait = config.GetDuration(profile.InitialWait, "2m")
			result.MaxDeliveryWait = config.GetDuration(profile.MaxDeliveryWait, "10m")
			result.MaxCheckAttempts = profile.MaxCheckAttempts
			result.Priority = profile.Priority
		}
	}

	// Override with specific settings
	if receiverCfg.Monitoring.CheckInterval != "" {
		result.CheckInterval = config.GetDuration(receiverCfg.Monitoring.CheckInterval, "30s")
	}
	if receiverCfg.Monitoring.InitialWait != "" {
		result.InitialWait = config.GetDuration(receiverCfg.Monitoring.InitialWait, "2m")
	}
	if receiverCfg.Monitoring.MaxDeliveryWait != "" {
		result.MaxDeliveryWait = config.GetDuration(receiverCfg.Monitoring.MaxDeliveryWait, "10m")
	}
	if receiverCfg.Monitoring.MaxCheckAttempts > 0 {
		result.MaxCheckAttempts = receiverCfg.Monitoring.MaxCheckAttempts
	}

	result.AlertOnFailure = receiverCfg.Monitoring.AlertOnFailure
	result.AlertWebhook = receiverCfg.Monitoring.AlertWebhook

	return result
}

// getChainName returns the chain name for a given chain ID
func (s *Service) getChainName(chainID int) string {
	if cfg, exists := s.config.GetChainConfig(chainID); exists {
		return cfg.Name
	}
	return fmt.Sprintf("Chain_%d", chainID)
}

// updateMetrics periodically updates metrics
func (s *Service) updateMetrics(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Update chain connection status
			for chainID, client := range s.sourceClients {
				chainName := s.getChainName(chainID)
				connected := client.IsConnected()
				s.metrics.UpdateChainConnectionStatus(fmt.Sprintf("%d", chainID), chainName, connected)
			}
			
			for chainID, client := range s.destClients {
				chainName := s.getChainName(chainID)
				connected := client.IsConnected()
				s.metrics.UpdateChainConnectionStatus(fmt.Sprintf("%d", chainID), chainName, connected)
			}

			// Update database connection status
			if err := s.db.Ping(); err == nil {
				s.metrics.UpdateDBConnectionStatus(true)
			} else {
				s.metrics.UpdateDBConnectionStatus(false)
			}

			// Update message queue depth
			if queueStats, err := s.db.GetQueueStats(); err == nil {
				s.metrics.UpdateMessageQueueDepth("pending", float64(queueStats.PendingMessages))
				s.metrics.UpdateMessageQueueDepth("checking", float64(queueStats.CheckingMessages))
				s.metrics.UpdateMessageQueueDepth("delivered", float64(queueStats.DeliveredMessages))
				s.metrics.UpdateMessageQueueDepth("failed", float64(queueStats.FailedMessages))
			}
		}
	}
}