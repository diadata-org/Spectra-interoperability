package router

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
)

// GenericRouter routes events based on configuration
type GenericRouter struct {
	config        *config.RouterConfig
	triggerEvents map[string]struct{}
	privateKey    *ecdsa.PrivateKey
	address       common.Address

	mu                sync.RWMutex
	stats             GenericRouterStats
	destinationStates map[string]*DestinationState // Tracks state for each destination (key: "chainID-contract-symbol")
}

// DestinationState holds the state for a specific destination
type DestinationState struct {
	mu            sync.Mutex
	lastUpdate    time.Time
	lastPrice     string
	lastTimestamp uint64
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
		destinationStates: make(map[string]*DestinationState),
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

// GetConfigDestinations returns all destinations configured in this router
func (gr *GenericRouter) GetConfigDestinations() []config.RouterDestination {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	destinations := make([]config.RouterDestination, len(gr.config.Destinations))
	copy(destinations, gr.config.Destinations)
	return destinations
}

// GetConfig returns the router configuration
func (gr *GenericRouter) GetConfig() *config.RouterConfig {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return gr.config
}

// GetSymbolsFromConfig extracts symbols from router config trigger conditions
func GetSymbolsFromConfig(routerConfig *config.RouterConfig) []string {
	if routerConfig == nil {
		return nil
	}

	var symbols []string

	for _, condition := range routerConfig.Triggers.Conditions {
		if !strings.Contains(strings.ToLower(condition.Field), "symbol") {
			continue
		}

		switch condition.Operator {
		case "in":
			if valueSlice, ok := condition.Value.([]interface{}); ok {
				for _, val := range valueSlice {
					if symbol, ok := val.(string); ok && symbol != "" {
						symbols = append(symbols, symbol)
					}
				}
			}
		case "eq", "==":
			if symbol, ok := condition.Value.(string); ok && symbol != "" {
				symbols = append(symbols, symbol)
			}
		case "ne", "!=":
			continue
		default:
			continue
		}
	}

	return symbols
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
func (gr *GenericRouter) FilterDestinationsByTimeThreshold(destinations []config.RouterDestination, data *config.ExtractedData, intentHash string) []config.RouterDestination {
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
		timeReason := ""
		priceReason := ""

		if hasTimeThreshold {
			timeThresholdMet, timeReason = gr.checkAndReserveTimeThreshold(dest, data)
		}

		if hasPriceDeviation {
			priceDeviationMet, priceReason = gr.checkAndReservePriceDeviation(dest, data, intentHash)
		}

		if timeThresholdMet || priceDeviationMet {
			currentPrice := gr.GetPriceFromData(data)
			symbol := gr.GetSymbolFromData(data)

			// Important: Time threshold timestamp is ONLY updated when time threshold itself triggers the update
			// NOT when only price deviation triggers it, to preserve the real time-based update schedule

			if timeThresholdMet && priceDeviationMet {
				logger.Infof("Update allowed: router=%s, chain=%d, contract=%s, symbol=%s, currentPrice=%s, reason=BOTH time threshold (%s) AND price deviation (%s) met",
					gr.config.ID, dest.ChainID, dest.Contract, symbol, currentPrice, timeReason, priceReason)
			} else if timeThresholdMet {
				logger.Infof("Update allowed: router=%s, chain=%d, contract=%s, symbol=%s, currentPrice=%s, reason=time threshold met: %s (price deviation: %s)",
					gr.config.ID, dest.ChainID, dest.Contract, symbol, currentPrice, timeReason, priceReason)
			} else {
				logger.Infof("Update allowed: router=%s, chain=%d, contract=%s, symbol=%s, currentPrice=%s, reason=price deviation met: %s (time threshold: %s)",
					gr.config.ID, dest.ChainID, dest.Contract, symbol, currentPrice, priceReason, timeReason)
			}
			filteredDestinations = append(filteredDestinations, dest)
		} else {
			symbol := gr.GetSymbolFromData(data)
			logger.Debugf("Update blocked: router=%s, chain=%d, contract=%s, symbol=%s, reason=NEITHER time threshold (%s) NOR price deviation (%s) met",
				gr.config.ID, dest.ChainID, dest.Contract, symbol, timeReason, priceReason)
		}
	}

	return filteredDestinations
}

// evaluateDestinationCondition evaluates a destination-specific condition
func (gr *GenericRouter) evaluateDestinationCondition(condition string, data *config.ExtractedData) bool {
	return strings.Contains(strings.ToLower(condition), "true") || condition == ""
}

// getOrCreateDestinationState safely retrieves or creates the state for a destination key
func (gr *GenericRouter) getOrCreateDestinationState(key string) *DestinationState {
	gr.mu.RLock()
	state, exists := gr.destinationStates[key]
	gr.mu.RUnlock()

	if exists {
		return state
	}

	gr.mu.Lock()
	defer gr.mu.Unlock()

	// Double-check after acquiring write lock
	state, exists = gr.destinationStates[key]
	if !exists {
		state = &DestinationState{}
		gr.destinationStates[key] = state
	}
	return state
}

// checkAndReserveTimeThreshold atomically checks if threshold is met and reserves the update slot
func (gr *GenericRouter) checkAndReserveTimeThreshold(dest config.RouterDestination, data *config.ExtractedData) (bool, string) {
	symbol := gr.GetSymbolFromData(data)
	destKey := gr.generateDestinationKey(dest, symbol)

	state := gr.getOrCreateDestinationState(destKey)
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.lastUpdate.IsZero() {
		// First time sending to this destination, reserve it now
		state.lastUpdate = time.Now()
		msg := fmt.Sprintf("first update, threshold=%v", dest.TimeThreshold.Duration())
		logger.Infof("Time threshold check: %s, router=%s, chain=%d, contract=%s, symbol=%s",
			msg, gr.config.ID, dest.ChainID, dest.Contract, symbol)
		return true, msg
	}

	// Check if enough time has passed
	timeSinceLastUpdate := time.Since(state.lastUpdate)
	thresholdMet := timeSinceLastUpdate >= dest.TimeThreshold.Duration()

	if thresholdMet {
		// Atomically reserve the slot by updating the time
		state.lastUpdate = time.Now()
		msg := fmt.Sprintf("time passed %v >= threshold %v", timeSinceLastUpdate, dest.TimeThreshold.Duration())
		logger.Infof("Time threshold met and reserved: router=%s, chain=%d, contract=%s, symbol=%s, %s",
			gr.config.ID, dest.ChainID, dest.Contract, symbol, msg)
		return true, msg
	}

	msg := fmt.Sprintf("time passed %v < threshold %v", timeSinceLastUpdate, dest.TimeThreshold.Duration())
	logger.Debugf("Time threshold not met: router=%s, chain=%d, contract=%s, symbol=%s, %s",
		gr.config.ID, dest.ChainID, dest.Contract, symbol, msg)
	return false, msg
}

// checkAndReservePriceDeviation atomically checks if price deviation is met and reserves the update slot
func (gr *GenericRouter) checkAndReservePriceDeviation(dest config.RouterDestination, data *config.ExtractedData, intentHash string) (bool, string) {
	symbol := gr.GetSymbolFromData(data)
	currentPrice := gr.GetPriceFromData(data)

	if currentPrice == "" {
		msg := "no price found in data"
		logger.Debugf("%s for symbol %s, allowing update", msg, symbol)
		return true, msg // Allow if we can't determine price
	}

	var newTimestamp uint64
	if data != nil {
		newTimestamp = gr.GetTimestampFromData(data)
	}

	destKey := gr.generateDestinationKey(dest, symbol)
	state := gr.getOrCreateDestinationState(destKey)

	state.mu.Lock()
	defer state.mu.Unlock()

	// Handle first update
	if state.lastPrice == "" {
		state.lastPrice = currentPrice
		if newTimestamp > 0 {
			state.lastTimestamp = newTimestamp
		}
		msg := fmt.Sprintf("first update, price=%s", currentPrice)
		logger.Infof("Price deviation check: %s, router=%s, chain=%d, contract=%s, symbol=%s, threshold=%s",
			msg, gr.config.ID, dest.ChainID, dest.Contract, symbol, dest.PriceDeviation)
		return true, msg
	}

	// Check timestamp first to prevent stale/duplicate updates (same protection as UpdateDestinationTime)
	if newTimestamp > 0 && state.lastTimestamp > 0 && newTimestamp <= state.lastTimestamp {
		var msg string
		switch {
		case newTimestamp < state.lastTimestamp:
			msg = fmt.Sprintf("REJECTED stale price deviation update: timestamp %d < current %d",
				newTimestamp, state.lastTimestamp)
			logger.Warnf("Price deviation check: %s, router=%s, chain=%d, contract=%s, symbol=%s, intentHash=%s",
				msg, gr.config.ID, dest.ChainID, dest.Contract, symbol, intentHash)
		case newTimestamp == state.lastTimestamp:
			msg = fmt.Sprintf("REJECTED duplicate timestamp: timestamp %d == current %d",
				newTimestamp, state.lastTimestamp)
			logger.Debugf("Price deviation check: %s, router=%s, chain=%d, contract=%s, symbol=%s, intentHash=%s",
				msg, gr.config.ID, dest.ChainID, dest.Contract, symbol, intentHash)
		}
		return false, msg
	}

	// Check deviation
	deviationStr := strings.TrimSuffix(dest.PriceDeviation, "%")
	deviationPercent, err := strconv.ParseFloat(deviationStr, 64)
	if err != nil {
		msg := fmt.Sprintf("invalid deviation format '%s'", dest.PriceDeviation)
		logger.Warnf("%s: %v, allowing update, router=%s, chain=%d, contract=%s, symbol=%s",
			msg, err, gr.config.ID, dest.ChainID, dest.Contract, symbol)
		state.lastPrice = currentPrice
		if newTimestamp > 0 {
			state.lastTimestamp = newTimestamp
		}
		return true, msg
	}

	percentChange := gr.calculatePriceChangePercent(state.lastPrice, currentPrice)
	deviationMet := percentChange >= deviationPercent

	if !deviationMet {
		msg := fmt.Sprintf("change %.2f%% < threshold %.2f%% (last=%s, curr=%s)", percentChange, deviationPercent, state.lastPrice, currentPrice)
		logger.Debugf("Price deviation not met: router=%s, chain=%d, contract=%s, symbol=%s, %s",
			gr.config.ID, dest.ChainID, dest.Contract, symbol, msg)
		return false, msg
	}

	// Deviation met and timestamp is valid
	lastPrice := state.lastPrice
	state.lastPrice = currentPrice
	if newTimestamp > 0 {
		state.lastTimestamp = newTimestamp
	}
	msg := fmt.Sprintf("change %.2f%% >= threshold %.2f%% (last=%s, curr=%s)", percentChange, deviationPercent, lastPrice, currentPrice)
	logger.Infof("Price deviation met and reserved: router=%s, chain=%d, contract=%s, symbol=%s, intentHash=%s, %s",
		gr.config.ID, dest.ChainID, dest.Contract, symbol, intentHash, msg)
	return true, msg
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

// GetTimestampFromData extracts the intent timestamp from enriched data
func (gr *GenericRouter) GetTimestampFromData(data *config.ExtractedData) uint64 {
	if data == nil || data.Enrichment == nil {
		return 0
	}

	// Try to extract from fullIntent structure
	if fullIntentRaw, ok := data.Enrichment["fullIntent"]; ok {
		if intentValue := reflect.ValueOf(fullIntentRaw); intentValue.IsValid() {
			if intentValue.Kind() == reflect.Ptr && !intentValue.IsNil() {
				intentValue = intentValue.Elem()
			}
			if intentValue.Kind() == reflect.Struct {
				timestampField := intentValue.FieldByName("Timestamp")
				if timestampField.IsValid() {
					// Handle *big.Int pointer
					if bigInt, ok := timestampField.Interface().(*big.Int); ok && bigInt != nil {
						return bigInt.Uint64()
					}
					// Handle big.Int value (not pointer)
					if timestampField.Kind() == reflect.Struct && timestampField.Type().String() == "big.Int" {
						if bigInt, ok := timestampField.Addr().Interface().(*big.Int); ok {
							return bigInt.Uint64()
						}
					}
				}
			}
		}
	}

	// Fallback: try timestamp from event data
	if timestamp, ok := data.Event["timestamp"]; ok {
		switch v := timestamp.(type) {
		case *big.Int:
			if v != nil {
				return v.Uint64()
			}
		case uint64:
			return v
		case int64:
			return uint64(v)
		case int:
			return uint64(v)
		}
	}

	return 0
}

// UpdateDestinationTime updates the last update time and price for a destination
func (gr *GenericRouter) UpdateDestinationTime(dest config.RouterDestination, symbol string, data ...*config.ExtractedData) {
	destKey := gr.generateDestinationKey(dest, symbol)

	state := gr.getOrCreateDestinationState(destKey)
	state.mu.Lock()
	defer state.mu.Unlock()

	state.lastUpdate = time.Now()

	if len(data) == 0 || data[0] == nil {
		logger.Debugf("Updated destination time for %s (no data provided)", destKey)
		return
	}

	newPrice := gr.GetPriceFromData(data[0])
	if newPrice == "" {
		logger.Debugf("Updated destination time for %s (no price available)", destKey)
		return
	}

	newTimestamp := gr.GetTimestampFromData(data[0])

	if newTimestamp == 0 {
		state.lastPrice = newPrice
		logger.Debugf("Updated price for %s: %s (no timestamp available, using legacy mode)", destKey, newPrice)
		return
	}

	if state.lastTimestamp > 0 && newTimestamp < state.lastTimestamp {
		logger.Warnf("REJECTED stale price update for %s: timestamp %d <= current %d (price: %s would not replace %s)",
			destKey, newTimestamp, state.lastTimestamp, newPrice, state.lastPrice)
		return
	}

	if state.lastTimestamp > 0 && newTimestamp == state.lastTimestamp {
		logger.Debugf("Price update skipped for %s: same timestamp %d (price: %s)", destKey, newTimestamp, newPrice)
		return
	}

	oldTimestamp := state.lastTimestamp
	state.lastPrice = newPrice
	state.lastTimestamp = newTimestamp

	if oldTimestamp > 0 {
		logger.Infof("Updated price for %s: %s (timestamp: %d > previous: %d)",
			destKey, newPrice, newTimestamp, oldTimestamp)
	} else {
		logger.Infof("First price for %s: %s (timestamp: %d)",
			destKey, newPrice, newTimestamp)
	}
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
		gr.UpdateDestinationTime(dest, symbol, data)
	}
}

// generateDestinationKey creates a unique key for a destination
func (gr *GenericRouter) generateDestinationKey(dest config.RouterDestination, symbol string) string {
	return fmt.Sprintf("%d-%s-%s", dest.ChainID, dest.Contract, symbol)
}
