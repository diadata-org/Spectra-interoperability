package router

import (
	"crypto/ecdsa"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// GenericRouter routes events based on configuration
type GenericRouter struct {
	config        *config.RouterConfig
	triggerEvents map[string]struct{}
	privateKey    *ecdsa.PrivateKey
	address       common.Address

	mu                sync.RWMutex
	stats             GenericRouterStats
	destinationTimes  map[string]time.Time // Tracks last update time for each destination (key: "chainID-contract-symbol")
	destinationPrices map[string]string    // Tracks last price for each destination (key: "chainID-contract-symbol", value: price as string)
}

// GenericRouterStats tracks router statistics
type GenericRouterStats struct {
	EventsReceived uint64
	EventsRouted   uint64
	EventsFiltered uint64
	LastEventTime  time.Time
}

// NewGenericRouter creates a new generic router from configuration
func NewGenericRouter(cfg *config.RouterConfig) (*GenericRouter, error) {
	triggerEvents := make(map[string]struct{}, len(cfg.Triggers.Events))
	for _, event := range cfg.Triggers.Events {
		triggerEvents[event] = struct{}{}
	}

	router := &GenericRouter{
		config:            cfg,
		triggerEvents:     triggerEvents,
		destinationTimes:  make(map[string]time.Time),
		destinationPrices: make(map[string]string),
	}

	if cfg.PrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse router private key: %w", err)
		}
		router.privateKey = key
		router.address = crypto.PubkeyToAddress(key.PublicKey)

		logger.Infof("Router %s initialized with address: %s", cfg.ID, router.address.Hex())
	}

	return router, nil
}

// ID returns the router ID
func (gr *GenericRouter) ID() string {
	return gr.config.ID
}

// Type returns the router type
func (gr *GenericRouter) Type() string {
	return gr.config.Type
}

// IsEnabled returns whether the router is enabled
func (gr *GenericRouter) IsEnabled() bool {
	return gr.config.Enabled
}

// ShouldRoute determines if an event should be routed
func (gr *GenericRouter) ShouldRoute(eventName string, data *config.ExtractedData) (bool, string) {
	gr.mu.Lock()
	gr.stats.EventsReceived++
	gr.stats.LastEventTime = time.Now()
	gr.mu.Unlock()

	if !gr.config.Enabled {
		return false, "router disabled"
	}

	if _, ok := gr.triggerEvents[eventName]; !ok {
		gr.mu.Lock()
		gr.stats.EventsFiltered++
		gr.mu.Unlock()
		return false, fmt.Sprintf("event %s not in trigger list", eventName)
	}

	for _, condition := range gr.config.Triggers.Conditions {
		if !gr.evaluateCondition(condition, data) {
			gr.mu.Lock()
			gr.stats.EventsFiltered++
			gr.mu.Unlock()
			return false, fmt.Sprintf("condition failed: %s %s %v", condition.Field, condition.Operator, condition.Value)
		}
	}

	gr.mu.Lock()
	gr.stats.EventsRouted++
	gr.mu.Unlock()

	return true, "all conditions met"
}

// evaluateCondition evaluates a trigger condition
func (gr *GenericRouter) evaluateCondition(condition config.TriggerCondition, data *config.ExtractedData) bool {
	value, err := gr.getFieldValue(condition.Field, data)
	if err != nil {
		logger.Debugf("Failed to get field value for condition: %v", err)
		return false
	}

	switch condition.Operator {
	case "==", "eq":
		return compareEqual(value, condition.Value)
	case "!=", "ne":
		return !compareEqual(value, condition.Value)
	case ">", "gt":
		return compareGreater(value, condition.Value)
	case "<", "lt":
		return compareLess(value, condition.Value)
	case ">=", "gte":
		return !compareLess(value, condition.Value)
	case "<=", "lte":
		return !compareGreater(value, condition.Value)
	case "contains":
		return compareContains(value, condition.Value)
	case "in":
		return compareIn(value, condition.Value)
	default:
		logger.Warnf("[Router %s] Unknown operator '%s' for condition: field=%s, value=%v (check YAML config - operators like != must be quoted)",
			gr.config.ID, condition.Operator, condition.Field, condition.Value)
		return false
	}
}

// getFieldValue extracts a field value using template syntax
func (gr *GenericRouter) getFieldValue(field string, data *config.ExtractedData) (interface{}, error) {
	if !strings.HasPrefix(field, "${") || !strings.HasSuffix(field, "}") {
		return nil, fmt.Errorf("invalid field syntax: %s", field)
	}

	path := field[2 : len(field)-1]
	parts := strings.Split(path, ".")

	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid field path: %s", path)
	}

	var source map[string]interface{}
	switch parts[0] {
	case "event":
		source = data.Event
	case "enrichment":
		source = data.Enrichment
	case "processed":
		source = data.Processed
	default:
		return nil, fmt.Errorf("unknown source: %s", parts[0])
	}

	var current interface{} = source
	for i := 1; i < len(parts); i++ {
		// Try map first
		if m, ok := current.(map[string]interface{}); ok {
			current, ok = m[parts[i]]
			if !ok {
				return nil, fmt.Errorf("field not found: %s", parts[i])
			}
		} else {
			// Use reflection for structured types
			value := reflect.ValueOf(current)
			if value.Kind() == reflect.Ptr && !value.IsNil() {
				value = value.Elem()
			}
			if value.Kind() != reflect.Struct {
				return nil, fmt.Errorf("cannot navigate through non-map/struct at %s", parts[i])
			}

			fieldValue := value.FieldByName(parts[i])
			if !fieldValue.IsValid() {
				return nil, fmt.Errorf("field not found: %s", parts[i])
			}

			current = fieldValue.Interface()
		}
	}

	return current, nil
}

// Comparison functions
func compareEqual(a, b interface{}) bool {
	aFloat, aIsNum := toFloat64(a)
	bFloat, bIsNum := toFloat64(b)

	if aIsNum && bIsNum {
		return aFloat == bFloat
	}

	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func compareGreater(a, b interface{}) bool {
	aFloat, aIsNum := toFloat64(a)
	bFloat, bIsNum := toFloat64(b)

	if aIsNum && bIsNum {
		return aFloat > bFloat
	}

	return fmt.Sprintf("%v", a) > fmt.Sprintf("%v", b)
}

func compareLess(a, b interface{}) bool {
	aFloat, aIsNum := toFloat64(a)
	bFloat, bIsNum := toFloat64(b)

	if aIsNum && bIsNum {
		return aFloat < bFloat
	}

	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

func compareContains(a, b interface{}) bool {
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.Contains(aStr, bStr)
}

func compareIn(a, b interface{}) bool {
	bSlice, ok := b.([]interface{})
	if !ok {
		return false
	}

	for _, item := range bSlice {
		if compareEqual(a, item) {
			return true
		}
	}

	return false
}

// toFloat64 attempts to convert an interface{} to a float64 for numeric comparisons.
func toFloat64(v interface{}) (float64, bool) {
	val := reflect.ValueOf(v)

	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(val.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(val.Uint()), true
	case reflect.Float32, reflect.Float64:
		return val.Float(), true
	case reflect.String:
		if f, err := strconv.ParseFloat(val.String(), 64); err == nil {
			return f, true
		}
	}

	return 0, false
}

// GetDestinations returns the router's destinations with evaluated conditions
func (gr *GenericRouter) GetDestinations(data *config.ExtractedData) []config.RouterDestination {
	var destinations []config.RouterDestination

	for _, dest := range gr.config.Destinations {
		if dest.Condition != "" {
			if !gr.evaluateDestinationCondition(dest.Condition, data) {
				continue
			}
		}

		destinations = append(destinations, dest)
	}

	return destinations
}

// FilterDestinationsByTimeThreshold filters destinations based on time thresholds after enrichment
// Uses OR logic: update if EITHER time_threshold is met OR price_deviation is met
func (gr *GenericRouter) FilterDestinationsByTimeThreshold(destinations []config.RouterDestination, data *config.ExtractedData) []config.RouterDestination {
	var filteredDestinations []config.RouterDestination

	for _, dest := range destinations {
		hasTimeThreshold := dest.TimeThreshold.Duration() > 0
		hasPriceDeviation := dest.PriceDeviation != ""

		// If neither threshold is configured, allow the update
		if !hasTimeThreshold && !hasPriceDeviation {
			filteredDestinations = append(filteredDestinations, dest)
			continue
		}

		// Check both conditions
		timeThresholdMet := false
		priceDeviationMet := false

		if hasTimeThreshold {
			timeThresholdMet = gr.checkTimeThreshold(dest, data)
			if timeThresholdMet {
				logger.Debugf("Time threshold met for destination %s (symbol: %s)",
					dest.Contract, gr.GetSymbolFromData(data))
			} else {
				logger.Debugf("Time threshold not met for destination %s (symbol: %s)",
					dest.Contract, gr.GetSymbolFromData(data))
			}
		}

		if hasPriceDeviation {
			priceDeviationMet = gr.checkPriceDeviation(dest, data)
			if priceDeviationMet {
				logger.Debugf("Price deviation threshold met for destination %s (symbol: %s)",
					dest.Contract, gr.GetSymbolFromData(data))
			} else {
				logger.Debugf("Price deviation threshold not met for destination %s (symbol: %s)",
					dest.Contract, gr.GetSymbolFromData(data))
			}
		}

		if timeThresholdMet || priceDeviationMet {
			if timeThresholdMet && priceDeviationMet {
				logger.Infof("Update allowed for %s (symbol: %s): BOTH time threshold AND price deviation met",
					dest.Contract, gr.GetSymbolFromData(data))
			} else if timeThresholdMet {
				logger.Infof("Update allowed for %s (symbol: %s): time threshold met (price deviation not required or not met)",
					dest.Contract, gr.GetSymbolFromData(data))
			} else {
				logger.Infof("Update allowed for %s (symbol: %s): price deviation met (time threshold not required or not met)",
					dest.Contract, gr.GetSymbolFromData(data))
			}
			filteredDestinations = append(filteredDestinations, dest)
		} else {
			logger.Debugf("Update blocked for %s (symbol: %s): NEITHER time threshold NOR price deviation met",
				dest.Contract, gr.GetSymbolFromData(data))
		}
	}

	return filteredDestinations
}

// evaluateDestinationCondition evaluates a destination-specific condition
func (gr *GenericRouter) evaluateDestinationCondition(condition string, data *config.ExtractedData) bool {
	return strings.Contains(strings.ToLower(condition), "true") || condition == ""
}

// checkTimeThreshold checks if enough time has passed since the last update to this destination
func (gr *GenericRouter) checkTimeThreshold(dest config.RouterDestination, data *config.ExtractedData) bool {
	symbol := gr.GetSymbolFromData(data)

	// Create unique key for this destination + symbol
	destKey := fmt.Sprintf("%d-%s-%s", dest.ChainID, dest.Contract, symbol)

	gr.mu.RLock()
	lastUpdate, exists := gr.destinationTimes[destKey]
	gr.mu.RUnlock()

	if !exists {
		// First time sending to this destination, allow it
		return true
	}

	// Check if enough time has passed
	timeSinceLastUpdate := time.Since(lastUpdate)
	thresholdMet := timeSinceLastUpdate >= dest.TimeThreshold.Duration()

	if thresholdMet {
		logger.Debugf("Time threshold met for destination %s: %v >= %v",
			destKey, timeSinceLastUpdate, dest.TimeThreshold.Duration())
	} else {
		logger.Debugf("Time threshold not met for destination %s: %v < %v",
			destKey, timeSinceLastUpdate, dest.TimeThreshold.Duration())
	}

	return thresholdMet
}

// checkPriceDeviation checks if price has changed by the configured deviation percentage
func (gr *GenericRouter) checkPriceDeviation(dest config.RouterDestination, data *config.ExtractedData) bool {
	symbol := gr.GetSymbolFromData(data)
	currentPrice := gr.GetPriceFromData(data)

	if currentPrice == "" {
		logger.Debugf("No price found in data for symbol %s, allowing update", symbol)
		return true // Allow if we can't determine price
	}

	// Create unique key for this destination + symbol
	destKey := fmt.Sprintf("%d-%s-%s", dest.ChainID, dest.Contract, symbol)

	gr.mu.RLock()
	lastPrice, exists := gr.destinationPrices[destKey]
	gr.mu.RUnlock()

	if !exists {
		// First time, allow it
		logger.Debugf("No previous price for %s, allowing update", destKey)
		return true
	}

	// Parse deviation percentage (e.g., "0.5%" -> 0.5)
	deviationStr := strings.TrimSuffix(dest.PriceDeviation, "%")
	deviationPercent, err := strconv.ParseFloat(deviationStr, 64)
	if err != nil {
		logger.Warnf("Invalid price_deviation format '%s': %v, allowing update", dest.PriceDeviation, err)
		return true
	}

	// Calculate percentage change
	percentChange := gr.calculatePriceChangePercent(lastPrice, currentPrice)
	deviationMet := percentChange >= deviationPercent

	if deviationMet {
		logger.Debugf("Price deviation met for %s: %.2f%% >= %.2f%% (last: %s, current: %s)",
			destKey, percentChange, deviationPercent, lastPrice, currentPrice)
	} else {
		logger.Debugf("Price deviation not met for %s: %.2f%% < %.2f%% (last: %s, current: %s)",
			destKey, percentChange, deviationPercent, lastPrice, currentPrice)
	}

	return deviationMet
}

// calculatePriceChangePercent calculates the percentage change between two price strings
func (gr *GenericRouter) calculatePriceChangePercent(oldPriceStr, newPriceStr string) float64 {
	oldPrice, err1 := strconv.ParseFloat(oldPriceStr, 64)
	newPrice, err2 := strconv.ParseFloat(newPriceStr, 64)

	if err1 != nil || err2 != nil || oldPrice == 0 {
		return 0
	}

	change := ((newPrice - oldPrice) / oldPrice) * 100
	if change < 0 {
		change = -change // Return absolute value
	}
	return change
}

// Helper to get map keys for debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// GetPriceFromData extracts price from enriched data
func (gr *GenericRouter) GetPriceFromData(data *config.ExtractedData) string {
	if data == nil || data.Enrichment == nil {
		return ""
	}

	// Try to extract from fullIntent structure
	if fullIntentRaw, ok := data.Enrichment["fullIntent"]; ok {
		// Try as struct with reflection
		if intentValue := reflect.ValueOf(fullIntentRaw); intentValue.IsValid() {
			if intentValue.Kind() == reflect.Ptr && !intentValue.IsNil() {
				intentValue = intentValue.Elem()
			}
			if intentValue.Kind() == reflect.Struct {
				priceField := intentValue.FieldByName("Price")
				if priceField.IsValid() {
					// Convert big.Int to string if that's the type
					return fmt.Sprintf("%v", priceField.Interface())
				}
			}
		}
	}

	// Try direct price key from enrichment
	if price, ok := data.Enrichment["price"]; ok {
		return fmt.Sprintf("%v", price)
	}

	return ""
}

// GetSymbolFromData extracts symbol from enriched data
func (gr *GenericRouter) GetSymbolFromData(data *config.ExtractedData) string {
	if data == nil || data.Enrichment == nil {
		return "unknown"
	}

	// Direct symbol key from enrichment
	if symbol, ok := data.Enrichment["symbol"].(string); ok && symbol != "" {
		return symbol
	}

	// Extract from fullIntent structure
	if fullIntentRaw, ok := data.Enrichment["fullIntent"]; ok {
		// Try as map[string]interface{} (legacy format)
		if fullIntent, ok := fullIntentRaw.(map[string]interface{}); ok {
			if symbol, ok := fullIntent["symbol"].(string); ok && symbol != "" {
				return symbol
			}
		}

		// Try as structured type using reflection
		if intentValue := reflect.ValueOf(fullIntentRaw); intentValue.IsValid() {
			if intentValue.Kind() == reflect.Ptr && !intentValue.IsNil() {
				intentValue = intentValue.Elem()
			}
			if intentValue.Kind() == reflect.Struct {
				symbolField := intentValue.FieldByName("Symbol")
				if symbolField.IsValid() && symbolField.Kind() == reflect.String {
					if symbol := symbolField.String(); symbol != "" {
						return symbol
					}
				}
			}
		}
	}

	logger.Debugf("No symbol found in enrichment data, using fallback 'unknown'")
	return "unknown"
}

// UpdateDestinationTime updates the last update time for a destination
func (gr *GenericRouter) UpdateDestinationTime(dest config.RouterDestination, symbol string, data ...*config.ExtractedData) {
	destKey := fmt.Sprintf("%d-%s-%s", dest.ChainID, dest.Contract, symbol)

	gr.mu.Lock()
	gr.destinationTimes[destKey] = time.Now()

	// Also update price if data is provided
	if len(data) > 0 && data[0] != nil {
		if price := gr.GetPriceFromData(data[0]); price != "" {
			gr.destinationPrices[destKey] = price
			logger.Debugf("Updated destination time and price for %s (price: %s)", destKey, price)
		} else {
			logger.Debugf("Updated destination time for %s (no price available)", destKey)
		}
	} else {
		logger.Debugf("Updated destination time for %s", destKey)
	}
	gr.mu.Unlock()
}

// GetPrivateKey returns the router's private key
func (gr *GenericRouter) GetPrivateKey() *ecdsa.PrivateKey {
	return gr.privateKey
}

// GetAddress returns the router's address
func (gr *GenericRouter) GetAddress() common.Address {
	return gr.address
}

// GetStats returns router statistics
func (gr *GenericRouter) GetStats() GenericRouterStats {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.stats
}

// BuildUpdateRequest builds an update request for a destination
func (gr *GenericRouter) BuildUpdateRequest(
	eventName string,
	data *config.ExtractedData,
	dest config.RouterDestination,
) (*types.UpdateRequest, error) {
	updateReq := &types.UpdateRequest{
		ID:            fmt.Sprintf("%s-%s-%d", gr.config.ID, eventName, time.Now().Unix()),
		CreatedAt:     time.Now(),
		Priority:      1,
		RouterID:      gr.config.ID,
		ExtractedData: data, // Store original extracted data for OnRouted callback
	}

	return updateReq, nil
}

// ProcessingConfig returns the router's processing configuration
func (gr *GenericRouter) ProcessingConfig() *config.ProcessingConfig {
	return &gr.config.Processing
}

// OnRouted is called after an event is successfully routed
func (gr *GenericRouter) OnRouted(eventName string, data *config.ExtractedData) {
	logger.Debugf("Router %s successfully routed event %s", gr.config.ID, eventName)

	// Update destination times for all destinations that were used
	symbol := gr.GetSymbolFromData(data)
	destinations := gr.GetDestinations(data)

	for _, dest := range destinations {
		gr.UpdateDestinationTime(dest, symbol)
	}
}
