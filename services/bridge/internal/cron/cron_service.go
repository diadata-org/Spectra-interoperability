package cron

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/robfig/cron/v3"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/processor"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/utils"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

// Service manages cron-based price updates from price cache to on-chain
type Service struct {
	priceCache            *processor.PriceCache
	routerRegistry        *router.GenericRegistry
	writeClients          map[int64]rpc.EthClient
	updateChan            chan *types.UpdateRequest
	config                config.CronServiceConfig
	cron                  *cron.Cron
	ctx                   context.Context
	cancel                context.CancelFunc
	monitoredDestinations map[string]*DestinationMonitor // key -> monitor
	routerSchedules       map[string]string              // routerID -> cron schedule
	mu                    sync.RWMutex
}

// DestinationMonitor tracks a destination for cron updates
type DestinationMonitor struct {
	RouterID         string
	ChainID          int64
	ContractAddress  common.Address
	Symbol           string
	Client           rpc.EthClient
	TimeThreshold    time.Duration                   // Per-destination time threshold
	PriceDeviation   float64                         // Per-destination price deviation
	MethodConfig     *config.DestinationMethodConfig // Destination method config for updates
	LastOnChainTime  uint64
	LastOnChainValue *big.Int
	LastUpdateTime   time.Time
}

// NewService creates a new cron service
func NewService(
	priceCache *processor.PriceCache,
	routerRegistry *router.GenericRegistry,
	writeClients map[int64]rpc.EthClient,
	updateChan chan *types.UpdateRequest,
	cfg config.CronServiceConfig,
) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		priceCache:            priceCache,
		routerRegistry:        routerRegistry,
		writeClients:          writeClients,
		updateChan:            updateChan,
		config:                cfg,
		ctx:                   ctx,
		cancel:                cancel,
		monitoredDestinations: make(map[string]*DestinationMonitor),
		routerSchedules:       make(map[string]string),
	}

	return service
}

// Start initializes and starts the cron service
func (s *Service) Start() error {
	if !s.config.Enabled {
		logger.Info("Cron service is disabled in configuration")
		return nil
	}

	logger.Info("Starting cron service")

	// Build monitor list from routers with cron: true
	s.buildMonitorList()

	if len(s.monitoredDestinations) == 0 {
		logger.Warn("No destinations configured for cron monitoring (no routers with cron: true found)")
		return nil
	}

	// Initialize cron scheduler
	s.cron = cron.New(cron.WithSeconds(), cron.WithLocation(time.UTC))

	// Add cron jobs for each router with its own schedule
	for routerID, schedule := range s.routerSchedules {
		if schedule == "" {
			schedule = s.config.Schedule
			if schedule == "" {
				schedule = "0 */5 * * * *" // Default: every 5 minutes
			}
		}

		// Create a closure to capture routerID
		routerID := routerID
		schedule := schedule

		_, err := s.cron.AddFunc(schedule, func() {
			s.runCronJob(routerID)
		})
		if err != nil {
			return fmt.Errorf("failed to add cron job for router %s: %w", routerID, err)
		}

		logger.Infof("Cron job added for router=%s with schedule: %s", routerID, schedule)
	}

	// Start the cron scheduler
	s.cron.Start()

	logger.Infof("Cron service started with %d routers and %d destination-symbol combinations",
		len(s.routerSchedules), len(s.monitoredDestinations))

	return nil
}

// Stop gracefully stops the cron service
func (s *Service) Stop() {
	if s.cron != nil {
		logger.Info("Stopping cron service")
		s.cron.Stop()
	}
	s.cancel()
}

// buildMonitorList builds the list of destinations to monitor
func (s *Service) buildMonitorList() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing monitors and schedules
	s.monitoredDestinations = make(map[string]*DestinationMonitor)
	s.routerSchedules = make(map[string]string)

	// Get active routers
	activeRouters := s.routerRegistry.GetActiveRouters()

	for _, routerInstance := range activeRouters {
		routerID := routerInstance.ID()
		routerConfig := routerInstance.GetConfig()
		if routerConfig == nil {
			continue
		}

		// Check if any destination has cron enabled
		hasCronDestinations := false
		var routerSchedule string

		// Get symbols from router
		symbols := router.GetSymbolsFromConfig(routerConfig)

		// Process each destination
		for _, dest := range routerConfig.Destinations {
			// Check if cron is enabled for this destination
			if !dest.Cron {
				continue
			}

			hasCronDestinations = true

			// Check if we have a client for this chain
			client, exists := s.writeClients[dest.ChainID]
			if !exists {
				logger.Warnf("No write client for chain %d, skipping cron monitoring for router %s", dest.ChainID, routerID)
				continue
			}

			// Parse time_threshold to create cron schedule
			timeThreshold := dest.TimeThreshold.Duration()
			if timeThreshold == 0 {
				timeThreshold = 5 * time.Minute // Default
			}

			// Convert time_threshold to cron expression
			// For example: 1m -> "*/1 * * * *", 5m -> "*/5 * * * *"
			minutes := int(timeThreshold.Minutes())
			if minutes < 1 {
				minutes = 1
			}
			cronSchedule := fmt.Sprintf("0 */%d * * * *", minutes)

			// Store the schedule for this router
			if routerSchedule == "" || routerSchedule == cronSchedule {
				routerSchedule = cronSchedule
			} else {
				logger.Warnf("Router %s has destinations with different time_thresholds, using first schedule: %s", routerID, routerSchedule)
			}

			// Parse price deviation if available, otherwise use config default
			priceDeviation := s.config.PriceDeviation
			if dest.PriceDeviation != "" {
				// Try to parse percentage (e.g., "0.10%" -> 0.10)
				var pd float64
				_, err := fmt.Sscanf(dest.PriceDeviation, "%f%%", &pd)
				if err == nil {
					priceDeviation = pd
				}
			}

			contractAddress := common.HexToAddress(dest.Contract)

			// Create a monitor for each symbol
			for _, symbol := range symbols {
				key := utils.GenerateDestinationKey(dest.ChainID, dest.Contract, symbol)
				s.monitoredDestinations[key] = &DestinationMonitor{
					RouterID:        routerID,
					ChainID:         dest.ChainID,
					ContractAddress: contractAddress,
					Symbol:          symbol,
					Client:          client,
					TimeThreshold:   timeThreshold,
					PriceDeviation:  priceDeviation,
					MethodConfig:    &dest.Method, // Store method config for transaction execution
				}
				logger.Debugf("Added cron monitoring for router=%s chain=%d contract=%s symbol=%s time_threshold=%v price_deviation=%.2f%%",
					routerID, dest.ChainID, dest.Contract, symbol, timeThreshold, priceDeviation)
			}
		}

		// Store the router schedule if it has cron destinations
		if hasCronDestinations && routerSchedule != "" {
			s.routerSchedules[routerID] = routerSchedule
			logger.Infof("Router %s has cron enabled with schedule: %s", routerID, routerSchedule)
		}
	}
}

// runCronJob executes the cron job for a specific router
func (s *Service) runCronJob(routerID string) {
	logger.Debugf("Cron job started for router=%s", routerID)

	s.mu.RLock()
	destinations := make([]*DestinationMonitor, 0, len(s.monitoredDestinations))
	for _, monitor := range s.monitoredDestinations {
		if monitor.RouterID == routerID {
			destinations = append(destinations, monitor)
		}
	}
	s.mu.RUnlock()

	// Check each destination for this router
	for _, monitor := range destinations {
		go s.checkDestination(monitor)
	}
}

// checkDestination checks a single destination and triggers update if needed
func (s *Service) checkDestination(monitor *DestinationMonitor) {
	// Get on-chain value and timestamp
	onChainValue, onChainTimestamp, err := s.getOnChainValue(monitor)
	if err != nil {
		logger.Errorf("Failed to get on-chain value for router=%s chain=%d contract=%s symbol=%s: %v",
			monitor.RouterID, monitor.ChainID, monitor.ContractAddress.Hex(), monitor.Symbol, err)
		return
	}

	// Update monitor state
	monitor.LastOnChainValue = onChainValue
	monitor.LastOnChainTime = onChainTimestamp
	monitor.LastUpdateTime = time.Now()

	// Get cached price
	priceEntry, exists := s.priceCache.GetPrice(monitor.Symbol)
	if !exists {
		logger.Debugf("No cached price for symbol=%s", monitor.Symbol)
		return
	}

	// Parse cached price
	cachedPrice, ok := new(big.Int).SetString(priceEntry.Price, 10)
	if !ok {
		logger.Errorf("Failed to parse cached price for symbol=%s: %s", monitor.Symbol, priceEntry.Price)
		return
	}

	// Check if update is needed
	shouldUpdate := s.shouldUpdate(monitor, cachedPrice, onChainValue, onChainTimestamp, priceEntry.Timestamp)

	if shouldUpdate {
		logger.Infof("Cron triggering update for router=%s chain=%d contract=%s symbol=%s: cached=%s on-chain=%s cached_ts=%d on-chain_ts=%d",
			monitor.RouterID, monitor.ChainID, monitor.ContractAddress.Hex(), monitor.Symbol,
			cachedPrice.String(), onChainValue.String(), priceEntry.Timestamp, onChainTimestamp)

		s.triggerUpdate(monitor, priceEntry)
	} else {
		logger.Debugf("No update needed for router=%s chain=%d contract=%s symbol=%s",
			monitor.RouterID, monitor.ChainID, monitor.ContractAddress.Hex(), monitor.Symbol)
	}
}

// shouldUpdate determines if an update should be triggered
func (s *Service) shouldUpdate(monitor *DestinationMonitor, cachedPrice, onChainValue *big.Int, onChainTimestamp, cachedTimestamp uint64) bool {
	// If there's no on-chain value, always update
	if onChainValue == nil || onChainValue.Sign() == 0 {
		logger.Debugf("No on-chain value for %s, triggering update", monitor.Symbol)
		return true
	}

	// If cached price is different from on-chain, check deviation
	if cachedPrice.Cmp(onChainValue) != 0 {
		if monitor.PriceDeviation > 0 {
			// Calculate percentage deviation
			diff := new(big.Int).Sub(cachedPrice, onChainValue)
			oldFloat := new(big.Float).SetInt(onChainValue)
			diffFloat := new(big.Float).SetInt(diff)
			percentageChange := new(big.Float).Quo(diffFloat, oldFloat)
			percentageChange.Mul(percentageChange, big.NewFloat(100))
			absChange := new(big.Float).Abs(percentageChange)

			threshold := big.NewFloat(monitor.PriceDeviation)
			if absChange.Cmp(threshold) > 0 {
				logger.Debugf("Price deviation for %s: %.2f%% > %.2f%%, triggering update",
					monitor.Symbol, percentageChange, monitor.PriceDeviation)
				return true
			}
		} else {
			// Any deviation triggers update
			logger.Debugf("Price changed for %s: %s -> %s, triggering update",
				monitor.Symbol, onChainValue.String(), cachedPrice.String())
			return true
		}
	}

	// Check time threshold (use per-destination threshold)
	if monitor.TimeThreshold > 0 {
		timeSinceUpdate := time.Since(time.Unix(int64(onChainTimestamp), 0))
		if timeSinceUpdate > monitor.TimeThreshold {
			logger.Debugf("Time threshold exceeded for %s: %v > %v, triggering update",
				monitor.Symbol, timeSinceUpdate, monitor.TimeThreshold)
			return true
		}
	}

	return false
}

// triggerUpdate creates and sends an update request
func (s *Service) triggerUpdate(monitor *DestinationMonitor, priceEntry *processor.PriceEntry) {
	// Parse cached price
	cachedPrice, ok := new(big.Int).SetString(priceEntry.Price, 10)
	if !ok {
		logger.Errorf("Failed to parse cached price for symbol=%s: %s", monitor.Symbol, priceEntry.Price)
		return
	}

	// Create OracleIntent entirely cache entry
	intent := &types.OracleIntent{
		Symbol:     priceEntry.Symbol,
		Price:      cachedPrice,
		Timestamp:  big.NewInt(int64(priceEntry.Timestamp)),
		Nonce:      priceEntry.Nonce,
		Expiry:     priceEntry.Expiry,
		Signer:     priceEntry.Signer,
		Signature:  priceEntry.Signature,
		Source:     priceEntry.Source,
		ChainID:    priceEntry.ChainID,
		IntentType: priceEntry.IntentType,
		Version:    priceEntry.Version,
	}

	// Create update request
	updateReq := &types.UpdateRequest{
		ID:     fmt.Sprintf("cron-%s-%d", monitor.Symbol, time.Now().Unix()),
		Intent: intent,
		DestinationChain: &config.DestinationConfig{
			ChainID: monitor.ChainID,
			Name:    fmt.Sprintf("Chain %d", monitor.ChainID),
		},
		Contract: &config.ContractConfig{
			Address: monitor.ContractAddress.Hex(),
		},
		RouterID:                monitor.RouterID,
		DestinationMethodConfig: monitor.MethodConfig,
		IsCronTriggered:         true, // Mark as cron-triggered
		CreatedAt:               time.Now(),
	}

	updateReq.ExtractedData = &config.ExtractedData{
		Enrichment: map[string]interface{}{
			"fullIntent": intent,
		},
	}

	// Send to update channel
	select {
	case s.updateChan <- updateReq:
		logger.Infof("Cron update request sent for router=%s symbol=%s", monitor.RouterID, monitor.Symbol)
	default:
		logger.Warnf("Cron update channel full, dropping update request for router=%s symbol=%s", monitor.RouterID, monitor.Symbol)
	}
}

// getOnChainValue retrieves the current value and timestamp from on-chain
func (s *Service) getOnChainValue(monitor *DestinationMonitor) (*big.Int, uint64, error) {
	const getValueABI = `[{
		"inputs": [{"internalType": "string", "name": "key", "type": "string"}],
		"name": "getValue",
		"outputs": [
			{"internalType": "uint128", "name": "value", "type": "uint128"},
			{"internalType": "uint128", "name": "timestamp", "type": "uint128"}
		],
		"stateMutability": "view",
		"type": "function"
	}]`

	parsedABI, err := abi.JSON(strings.NewReader(getValueABI))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack("getValue", monitor.Symbol)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to pack call data: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &monitor.ContractAddress,
		Data: data,
	}

	result, err := monitor.Client.CallContract(context.Background(), msg, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to call contract: %w", err)
	}

	// Unpack result
	values := struct {
		Value     *big.Int
		Timestamp *big.Int
	}{}

	err = parsedABI.UnpackIntoInterface(&values, "getValue", result)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to unpack result: %w", err)
	}

	return values.Value, values.Timestamp.Uint64(), nil
}
