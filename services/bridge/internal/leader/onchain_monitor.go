package leader

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/utils"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/router"
)

type OnChainMonitor struct {
	routers map[string]*RouterMonitor

	enabled bool

	timeThresholdOffset  time.Duration
	priceDeviationOffset *big.Float

	checkInterval time.Duration

	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// RouterMonitor tracks one router and its destinations
type RouterMonitor struct {
	RouterID     string
	destinations map[string]*DestinationMonitor
	mu           sync.RWMutex
}

// DestinationMonitor tracks one destination with its router config
type DestinationMonitor struct {
	config.RouterDestination

	ContractAddress      common.Address
	Client               rpc.EthClient
	mu                   sync.RWMutex
	lastValue            *big.Int
	lastTimestamp        uint64
	lastCheck            time.Time
	lastPercentageChange *big.Float
}

// MonitorConfig for offsets
type MonitorConfig struct {
	Enabled              bool
	TimeThresholdOffset  time.Duration
	PriceDeviationOffset *big.Float
	CheckInterval        time.Duration
}

// DefaultMonitorConfig returns default config
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		Enabled:              false,
		TimeThresholdOffset:  1 * time.Minute,
		PriceDeviationOffset: big.NewFloat(0.10), // 10%
		CheckInterval:        10 * time.Second,
	}
}

// NewOnChainMonitor creates monitor from router registry
func NewOnChainMonitor(
	routerRegistry *router.GenericRegistry,
	ethClients map[int64]rpc.EthClient,
	monitorConfig MonitorConfig,
) *OnChainMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	monitor := &OnChainMonitor{
		routers:              make(map[string]*RouterMonitor),
		enabled:              monitorConfig.Enabled,
		timeThresholdOffset:  monitorConfig.TimeThresholdOffset,
		priceDeviationOffset: monitorConfig.PriceDeviationOffset,
		checkInterval:        monitorConfig.CheckInterval,
		ctx:                  ctx,
		cancel:               cancel,
	}

	activeRouters := routerRegistry.GetActiveRouters()
	for _, routerInstance := range activeRouters {
		routerID := routerInstance.ID()
		routerConfig := routerInstance.GetConfig()
		if routerConfig == nil {
			continue
		}

		routerMonitor := &RouterMonitor{
			RouterID:     routerID,
			destinations: make(map[string]*DestinationMonitor),
		}

		symbols := router.GetSymbolsFromConfig(routerConfig)
		for _, dest := range routerConfig.Destinations {
			ethClient, exists := ethClients[dest.ChainID]
			if !exists {
				continue
			}

			for _, symbol := range symbols {
				key := utils.GenerateDestinationKey(dest.ChainID, dest.Contract, symbol)
				routerMonitor.destinations[key] = &DestinationMonitor{
					RouterDestination: dest,
					ContractAddress:   common.HexToAddress(dest.Contract),
					Client:            ethClient,
				}
			}
		}

		if len(routerMonitor.destinations) > 0 {
			monitor.routers[routerID] = routerMonitor
		}
	}

	return monitor
}

func (m *OnChainMonitor) Start() {
	totalDestinations := 0
	m.mu.RLock()
	routerIDs := make([]string, 0, len(m.routers))
	for routerID, routerMonitor := range m.routers {
		totalDestinations += len(routerMonitor.destinations)
		routerIDs = append(routerIDs, routerID)
	}
	m.mu.RUnlock()

	logger.Infof("Starting on-chain monitor: tracking %d routers (%d destination-symbol combinations), check interval: %v",
		len(m.routers), totalDestinations, m.checkInterval)
	logger.Debugf("Tracking routers: %v", routerIDs)

	m.checkAllRouters()

	go m.monitoringLoop()
}

func (m *OnChainMonitor) Stop() {
	m.cancel()
}

// monitoringLoop periodically checks activity
func (m *OnChainMonitor) monitoringLoop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAllRouters()
		}
	}
}

func (m *OnChainMonitor) checkAllRouters() {
	m.mu.RLock()
	routers := make([]*RouterMonitor, 0, len(m.routers))
	for _, routerMonitor := range m.routers {
		routers = append(routers, routerMonitor)
	}
	m.mu.RUnlock()

	for _, routerMonitor := range routers {
		go m.checkRouter(routerMonitor)
	}
}

func (m *OnChainMonitor) checkRouter(routerMonitor *RouterMonitor) {
	routerMonitor.mu.RLock()
	destinations := make(map[string]*DestinationMonitor)
	for key, dest := range routerMonitor.destinations {
		destinations[key] = dest
	}
	routerMonitor.mu.RUnlock()

	for key, dest := range destinations {
		parts := strings.SplitN(key, "-", 3)
		if len(parts) != 3 {
			logger.Debugf("Invalid destination key format: %s", key)
			continue
		}
		symbol := parts[2]

		value, timestamp, err := m.getValueFromContract(dest, symbol)
		if err != nil {
			logger.Errorf("Failed to get value for router %s destination %s and symbol %s: %v",
				routerMonitor.RouterID, key, symbol, err)
			continue
		}

		dest.mu.Lock()
		previousValue := dest.lastValue
		previousTimestamp := dest.lastTimestamp

		if timestamp > dest.lastTimestamp {
			if dest.lastValue != nil && dest.lastValue.Sign() != 0 {
				diff := new(big.Int).Sub(value, dest.lastValue)
				oldFloat := new(big.Float).SetInt(dest.lastValue)
				diffFloat := new(big.Float).SetInt(diff)
				percentageChange := new(big.Float).Quo(diffFloat, oldFloat)
				percentageChange.Mul(percentageChange, big.NewFloat(100))
				dest.lastPercentageChange = percentageChange

				priceDevThreshold := m.GetPriceDeviationThreshold(dest.ChainID, dest.ContractAddress, symbol)
				if priceDevThreshold != nil {
					thresholdPercent := new(big.Float).Mul(priceDevThreshold, big.NewFloat(100))
					absChange := new(big.Float).Abs(percentageChange)
					if absChange.Cmp(thresholdPercent) > 0 {
						logger.Infof("Monitoring triggered: price deviation for router=%s chain=%d contract=%s symbol=%s, "+
							"previous=%s current=%s change=%.2f%% threshold=%.2f%%",
							routerMonitor.RouterID, dest.ChainID, dest.ContractAddress.Hex(), symbol,
							previousValue.String(), value.String(), percentageChange, thresholdPercent)
					}
				}
			} else {
				dest.lastPercentageChange = nil
				logger.Debugf("Monitoring: first value for router=%s chain=%d contract=%s symbol=%s value=%s",
					routerMonitor.RouterID, dest.ChainID, dest.ContractAddress.Hex(), symbol, value.String())
			}
			dest.lastValue = value
			dest.lastTimestamp = timestamp
		}

		if previousTimestamp > 0 {
			timeThreshold := dest.TimeThreshold.Duration()
			if timeThreshold == 0 {
				timeThreshold = 5 * time.Minute
			}
			totalThreshold := timeThreshold + m.timeThresholdOffset
			timeSinceUpdate := time.Since(time.Unix(int64(previousTimestamp), 0))

			if timeSinceUpdate > totalThreshold {
				logger.Infof("Monitoring triggered: time threshold for router=%s chain=%d contract=%s symbol=%s, "+
					"time_since_update=%v threshold=%v value=%s",
					routerMonitor.RouterID, dest.ChainID, dest.ContractAddress.Hex(), symbol,
					timeSinceUpdate, totalThreshold, formatValue(previousValue))
			}
		}

		dest.lastCheck = time.Now()
		dest.mu.Unlock()
	}
}

func (m *OnChainMonitor) findDestination(key string) *DestinationMonitor {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, routerMonitor := range m.routers {
		routerMonitor.mu.RLock()
		if dest, exists := routerMonitor.destinations[key]; exists {
			routerMonitor.mu.RUnlock()
			return dest
		}
		routerMonitor.mu.RUnlock()
	}
	return nil
}

func (m *OnChainMonitor) getValueFromContract(dest *DestinationMonitor, symbol string) (*big.Int, uint64, error) {

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

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(getValueABI)))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack("getValue", symbol)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to pack input data: %w", err)
	}

	callMsg := ethereum.CallMsg{
		To:   &dest.ContractAddress,
		Data: data,
	}

	resultBytes, err := dest.Client.CallContract(m.ctx, callMsg, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("contract call failed: %w", err)
	}

	outputs, err := parsedABI.Unpack("getValue", resultBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to unpack result: %w", err)
	}

	if len(outputs) != 2 {
		return nil, 0, fmt.Errorf("unexpected number of outputs: got %d, want 2", len(outputs))
	}

	value, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, 0, fmt.Errorf("failed to convert value to big.Int, got type %T: %v", outputs[0], outputs[0])
	}

	timestamp, ok := outputs[1].(*big.Int)
	if !ok {
		return nil, 0, fmt.Errorf("failed to convert timestamp to big.Int, got type %T: %v", outputs[1], outputs[1])
	}

	return value, timestamp.Uint64(), nil
}

func formatValue(v *big.Int) string {
	if v == nil {
		return "nil"
	}
	return v.String()
}

// ShouldProcess checks if a destination should be processed
func (m *OnChainMonitor) ShouldProcess(chainID int64, contractAddress common.Address, symbol string, incomingPrice *big.Int) bool {
	if !m.enabled {
		return true
	}

	key := utils.GenerateDestinationKey(chainID, contractAddress.Hex(), symbol)
	dest := m.findDestination(key)
	if dest == nil {
		return true
	}

	dest.mu.RLock()
	lastTimestamp := dest.lastTimestamp
	lastCheck := dest.lastCheck
	timeThreshold := dest.TimeThreshold.Duration()
	currentValue := dest.lastValue
	lastPercentageChange := dest.lastPercentageChange
	dest.mu.RUnlock()

	if lastTimestamp == 0 {
		return true
	}
	if !lastCheck.IsZero() && time.Since(lastCheck) > m.checkInterval*2 {
		logger.Warnf("Monitor check stale for chain=%d contract=%s symbol=%s, allowing processing",
			chainID, contractAddress.Hex(), symbol)
		return true
	}

	if lastPercentageChange != nil {
		priceDevThreshold := m.GetPriceDeviationThreshold(chainID, contractAddress, symbol)
		if priceDevThreshold != nil {
			thresholdPercent := new(big.Float).Mul(priceDevThreshold, big.NewFloat(100))
			absChange := new(big.Float).Abs(lastPercentageChange)
			if absChange.Cmp(thresholdPercent) > 0 {
				logger.Infof("Monitoring triggered: price deviation threshold exceeded for chain=%d contract=%s symbol=%s, "+
					"change=%.2f%% threshold=%.2f%% value=%s",
					chainID, contractAddress.Hex(), symbol, lastPercentageChange, thresholdPercent, formatValue(currentValue))
				return true
			}
		}
	}

	if incomingPrice != nil && currentValue != nil && currentValue.Sign() != 0 {
		priceDevThreshold := m.GetPriceDeviationThreshold(chainID, contractAddress, symbol)
		if priceDevThreshold != nil {
			diff := new(big.Int).Sub(incomingPrice, currentValue)
			oldFloat := new(big.Float).SetInt(currentValue)
			diffFloat := new(big.Float).SetInt(diff)
			percentageChange := new(big.Float).Quo(diffFloat, oldFloat)
			percentageChange.Mul(percentageChange, big.NewFloat(100))
			thresholdPercent := new(big.Float).Mul(priceDevThreshold, big.NewFloat(100))
			absChange := new(big.Float).Abs(percentageChange)
			if absChange.Cmp(thresholdPercent) > 0 {
				logger.Infof("Monitoring triggered: price deviation threshold exceeded (incoming vs on-chain) for chain=%d contract=%s symbol=%s, "+
					"change=%.2f%% threshold=%.2f%% onchain_value=%s incoming_value=%s",
					chainID, contractAddress.Hex(), symbol, percentageChange, thresholdPercent, formatValue(currentValue), incomingPrice.String())
				return true
			}
		}
	}

	if timeThreshold == 0 {
		timeThreshold = 5 * time.Minute
	}
	totalThreshold := timeThreshold + m.timeThresholdOffset

	timeSinceUpdate := time.Since(time.Unix(int64(lastTimestamp), 0))
	if timeSinceUpdate > totalThreshold {
		logger.Infof("Monitoring triggered: time threshold exceeded for chain=%d contract=%s symbol=%s, "+
			"time_since_update=%v threshold=%v value=%s",
			chainID, contractAddress.Hex(), symbol, timeSinceUpdate, totalThreshold, formatValue(currentValue))
		return true
	}

	return false
}

// GetPriceDeviationThreshold returns the price deviation threshold for a destination
func (m *OnChainMonitor) GetPriceDeviationThreshold(chainID int64, contractAddress common.Address, symbol string) *big.Float {
	key := utils.GenerateDestinationKey(chainID, contractAddress.Hex(), symbol)
	dest := m.findDestination(key)

	if dest == nil || dest.PriceDeviation == "" {
		return big.NewFloat(0.10)
	}

	priceDeviation := parsePriceDeviation(dest.PriceDeviation)
	return new(big.Float).Add(priceDeviation, m.priceDeviationOffset)
}

// GetInactivityThreshold returns the inactivity threshold for a destination
func (m *OnChainMonitor) GetInactivityThreshold(chainID int64, contractAddress common.Address, symbol string) time.Duration {
	key := utils.GenerateDestinationKey(chainID, contractAddress.Hex(), symbol)
	dest := m.findDestination(key)

	timeThreshold := 5 * time.Minute
	if dest != nil {
		timeThreshold = dest.TimeThreshold.Duration()
		if timeThreshold == 0 {
			timeThreshold = 5 * time.Minute
		}
	}

	return timeThreshold + m.timeThresholdOffset
}

func ParsePriceDeviation(s string) *big.Float {
	return parsePriceDeviation(s)
}

func parsePriceDeviation(s string) *big.Float {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	s = strings.TrimSpace(s)

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		logger.Warnf("Failed to parse price deviation '%s', using default 10%%", s)
		return big.NewFloat(0.10) // Default 10%
	}

	// Convert percentage to decimal (0.5% -> 0.005, 10% -> 0.10)
	return big.NewFloat(val / 100.0)
}
