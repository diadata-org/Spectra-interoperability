package router

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	
	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// GenericRouter routes events based on configuration
type GenericRouter struct {
	config     *config.RouterConfig
	privateKey *ecdsa.PrivateKey
	address    common.Address
	
	mu         sync.RWMutex
	stats      GenericRouterStats
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
	router := &GenericRouter{
		config: cfg,
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
	
	eventMatches := false
	for _, triggerEvent := range gr.config.Triggers.Events {
		if triggerEvent == eventName {
			eventMatches = true
			break
		}
	}
	
	if !eventMatches {
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
		return gr.compareEqual(value, condition.Value)
	case "!=", "ne":
		return !gr.compareEqual(value, condition.Value)
	case ">", "gt":
		return gr.compareGreater(value, condition.Value)
	case "<", "lt":
		return gr.compareLess(value, condition.Value)
	case ">=", "gte":
		return !gr.compareLess(value, condition.Value)
	case "<=", "lte":
		return !gr.compareGreater(value, condition.Value)
	case "contains":
		return gr.compareContains(value, condition.Value)
	case "in":
		return gr.compareIn(value, condition.Value)
	default:
		logger.Warnf("Unknown operator: %s", condition.Operator)
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
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot navigate through non-map at %s", parts[i])
		}
		current, ok = m[parts[i]]
		if !ok {
			return nil, fmt.Errorf("field not found: %s", parts[i])
		}
	}
	
	return current, nil
}

// Comparison functions
func (gr *GenericRouter) compareEqual(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func (gr *GenericRouter) compareGreater(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) > fmt.Sprintf("%v", b)
}

func (gr *GenericRouter) compareLess(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b)
}

func (gr *GenericRouter) compareContains(a, b interface{}) bool {
	return strings.Contains(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
}

func (gr *GenericRouter) compareIn(a, b interface{}) bool {
	if arr, ok := b.([]interface{}); ok {
		aStr := fmt.Sprintf("%v", a)
		for _, item := range arr {
			if fmt.Sprintf("%v", item) == aStr {
				return true
			}
		}
	}
	return false
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

// evaluateDestinationCondition evaluates a destination-specific condition
func (gr *GenericRouter) evaluateDestinationCondition(condition string, data *config.ExtractedData) bool {
	return strings.Contains(strings.ToLower(condition), "true") || condition == ""
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
		ID:        fmt.Sprintf("%s-%s-%d", gr.config.ID, eventName, time.Now().Unix()),
		CreatedAt: time.Now(),
		Priority:  1,
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
}