package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/pkg/rpc"
)

// HealthMonitor monitors the health of bridge components
type HealthMonitor struct {
	config         *config.HealthCheckConfig
	db             *database.DB
	sourceClient   rpc.EthClient
	destClients    map[int64]rpc.EthClient
	
	mu             sync.RWMutex
	componentStatus map[string]*ComponentStatus
	alerts         chan *Alert
	
	stopChan       chan struct{}
	stoppedChan    chan struct{}
}

// ComponentStatus represents the health status of a component
type ComponentStatus struct {
	Name           string
	Type           string
	Healthy        bool
	LastCheck      time.Time
	LastError      error
	ConsecutiveFails int
	Metadata       map[string]interface{}
}

// Alert represents a health alert
type Alert struct {
	Component      string
	Severity       string
	Message        string
	Error          error
	Timestamp      time.Time
	RecoveryAction string
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(
	cfg *config.HealthCheckConfig,
	db *database.DB,
	sourceClient rpc.EthClient,
	destClients map[int64]rpc.EthClient,
) *HealthMonitor {
	return &HealthMonitor{
		config:          cfg,
		db:              db,
		sourceClient:    sourceClient,
		destClients:     destClients,
		componentStatus: make(map[string]*ComponentStatus),
		alerts:          make(chan *Alert, 100),
		stopChan:        make(chan struct{}),
		stoppedChan:     make(chan struct{}),
	}
}

// Start begins health monitoring
func (hm *HealthMonitor) Start(ctx context.Context) error {
	logger.Info("Starting health monitor")
	
	// Initialize component statuses
	hm.initializeComponents()
	
	// Start monitoring loops
	go hm.monitorLoop(ctx)
	go hm.alertProcessor(ctx)
	
	return nil
}

// Stop gracefully stops the health monitor
func (hm *HealthMonitor) Stop() error {
	logger.Info("Stopping health monitor")
	
	close(hm.stopChan)
	
	select {
	case <-hm.stoppedChan:
		logger.Info("Health monitor stopped")
	case <-time.After(10 * time.Second):
		logger.Warn("Health monitor stop timeout")
	}
	
	return nil
}

// GetAlerts returns the alerts channel
func (hm *HealthMonitor) GetAlerts() <-chan *Alert {
	return hm.alerts
}

// GetStatus returns the current health status
func (hm *HealthMonitor) GetStatus() map[string]*ComponentStatus {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	
	// Deep copy the status map
	status := make(map[string]*ComponentStatus)
	for k, v := range hm.componentStatus {
		status[k] = &ComponentStatus{
			Name:             v.Name,
			Type:             v.Type,
			Healthy:          v.Healthy,
			LastCheck:        v.LastCheck,
			LastError:        v.LastError,
			ConsecutiveFails: v.ConsecutiveFails,
			Metadata:         v.Metadata,
		}
	}
	
	return status
}

// IsHealthy returns overall system health
func (hm *HealthMonitor) IsHealthy() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	
	for _, status := range hm.componentStatus {
		if !status.Healthy {
			return false
		}
	}
	
	return true
}

// initializeComponents sets up initial component statuses
func (hm *HealthMonitor) initializeComponents() {
	// Database
	hm.componentStatus["database"] = &ComponentStatus{
		Name:     "Database",
		Type:     "infrastructure",
		Healthy:  true,
		Metadata: make(map[string]interface{}),
	}
	
	// Source chain
	hm.componentStatus["source_chain"] = &ComponentStatus{
		Name:     "Source Chain",
		Type:     "blockchain",
		Healthy:  true,
		Metadata: make(map[string]interface{}),
	}
	
	// Destination chains
	for chainID := range hm.destClients {
		key := fmt.Sprintf("dest_chain_%d", chainID)
		hm.componentStatus[key] = &ComponentStatus{
			Name:     fmt.Sprintf("Destination Chain %d", chainID),
			Type:     "blockchain",
			Healthy:  true,
			Metadata: map[string]interface{}{"chain_id": chainID},
		}
	}
	
	// Event processing
	hm.componentStatus["event_processor"] = &ComponentStatus{
		Name:     "Event Processor",
		Type:     "service",
		Healthy:  true,
		Metadata: make(map[string]interface{}),
	}
	
	// Worker pool
	hm.componentStatus["worker_pool"] = &ComponentStatus{
		Name:     "Worker Pool",
		Type:     "service",
		Healthy:  true,
		Metadata: make(map[string]interface{}),
	}
}

// monitorLoop is the main monitoring loop
func (hm *HealthMonitor) monitorLoop(ctx context.Context) {
	defer close(hm.stoppedChan)
	
	ticker := time.NewTicker(hm.config.CheckInterval.Duration())
	defer ticker.Stop()
	
	// Initial check
	hm.performHealthChecks(ctx)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		case <-ticker.C:
			hm.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks runs all health checks
func (hm *HealthMonitor) performHealthChecks(ctx context.Context) {
	// Check database
	hm.checkDatabase(ctx)
	
	// Check source chain
	hm.checkSourceChain(ctx)
	
	// Check destination chains
	hm.checkDestinationChains(ctx)
	
	// Check services
	hm.checkServices(ctx)
}

// checkDatabase checks database connectivity
func (hm *HealthMonitor) checkDatabase(ctx context.Context) {
	componentKey := "database"
	
	checkCtx, cancel := context.WithTimeout(ctx, hm.config.Timeout.Duration())
	defer cancel()
	
	err := hm.db.Ping(checkCtx)
	hm.updateComponentStatus(componentKey, err)
	
	if err != nil {
		hm.sendAlert(&Alert{
			Component:      "Database",
			Severity:       "critical",
			Message:        "Database connection failed",
			Error:          err,
			Timestamp:      time.Now(),
			RecoveryAction: "Check database connection and credentials",
		})
	}
}

// checkSourceChain checks source blockchain connectivity
func (hm *HealthMonitor) checkSourceChain(ctx context.Context) {
	componentKey := "source_chain"
	
	checkCtx, cancel := context.WithTimeout(ctx, hm.config.Timeout.Duration())
	defer cancel()
	
	blockNumber, err := hm.sourceClient.BlockNumber(checkCtx)
	
	hm.mu.Lock()
	if status, exists := hm.componentStatus[componentKey]; exists {
		status.Metadata["latest_block"] = blockNumber
	}
	hm.mu.Unlock()
	
	hm.updateComponentStatus(componentKey, err)
	
	if err != nil {
		hm.sendAlert(&Alert{
			Component:      "Source Chain",
			Severity:       "high",
			Message:        "Source chain connection failed",
			Error:          err,
			Timestamp:      time.Now(),
			RecoveryAction: "Check source chain RPC endpoint",
		})
	}
}

// checkDestinationChains checks destination blockchain connectivity
func (hm *HealthMonitor) checkDestinationChains(ctx context.Context) {
	for chainID, client := range hm.destClients {
		componentKey := fmt.Sprintf("dest_chain_%d", chainID)
		
		checkCtx, cancel := context.WithTimeout(ctx, hm.config.Timeout.Duration())
		
		blockNumber, err := client.BlockNumber(checkCtx)
		
		hm.mu.Lock()
		if status, exists := hm.componentStatus[componentKey]; exists {
			status.Metadata["latest_block"] = blockNumber
		}
		hm.mu.Unlock()
		
		hm.updateComponentStatus(componentKey, err)
		
		if err != nil {
			hm.sendAlert(&Alert{
				Component:      fmt.Sprintf("Destination Chain %d", chainID),
				Severity:       "high",
				Message:        "Destination chain connection failed",
				Error:          err,
				Timestamp:      time.Now(),
				RecoveryAction: fmt.Sprintf("Check chain %d RPC endpoint", chainID),
			})
		}
		
		cancel()
	}
}

// checkServices checks internal service health
func (hm *HealthMonitor) checkServices(ctx context.Context) {
	// Check event processor lag
	hm.checkEventProcessorLag(ctx)
	
	// Check worker pool health
	hm.checkWorkerPoolHealth(ctx)
}

// checkEventProcessorLag checks if event processing is lagging
func (hm *HealthMonitor) checkEventProcessorLag(ctx context.Context) {
	componentKey := "event_processor"
	
	// Get latest processed event
	lastEvent, err := hm.db.GetLastProcessedEvent()
	if err != nil {
		hm.updateComponentStatus(componentKey, err)
		return
	}
	
	if lastEvent != nil {
		lag := time.Since(lastEvent.ProcessedAt)
		
		hm.mu.Lock()
		if status, exists := hm.componentStatus[componentKey]; exists {
			status.Metadata["processing_lag"] = lag.String()
			status.Metadata["last_event_time"] = lastEvent.ProcessedAt
		}
		hm.mu.Unlock()
		
		// Alert if lag is too high
		if lag > hm.config.MaxProcessingLag.Duration() {
			err = fmt.Errorf("event processing lag: %s", lag)
			hm.updateComponentStatus(componentKey, err)
			
			hm.sendAlert(&Alert{
				Component:      "Event Processor",
				Severity:       "medium",
				Message:        fmt.Sprintf("Event processing lagging by %s", lag),
				Error:          err,
				Timestamp:      time.Now(),
				RecoveryAction: "Check event processor logs and performance",
			})
		} else {
			hm.updateComponentStatus(componentKey, nil)
		}
	}
}

// checkWorkerPoolHealth checks worker pool statistics
func (hm *HealthMonitor) checkWorkerPoolHealth(ctx context.Context) {
	componentKey := "worker_pool"
	
	// Get worker pool stats from database
	stats, err := hm.db.GetWorkerPoolStats()
	if err != nil {
		hm.updateComponentStatus(componentKey, err)
		return
	}
	
	hm.mu.Lock()
	if status, exists := hm.componentStatus[componentKey]; exists {
		status.Metadata["queue_size"] = stats.QueueSize
		status.Metadata["active_workers"] = stats.ActiveWorkers
		status.Metadata["success_rate"] = stats.SuccessRate
	}
	hm.mu.Unlock()
	
	// Alert if queue is too large
	if stats.QueueSize > hm.config.MaxQueueSize {
		err = fmt.Errorf("worker queue size: %d", stats.QueueSize)
		hm.updateComponentStatus(componentKey, err)
		
		hm.sendAlert(&Alert{
			Component:      "Worker Pool",
			Severity:       "medium",
			Message:        fmt.Sprintf("Worker queue size high: %d", stats.QueueSize),
			Error:          err,
			Timestamp:      time.Now(),
			RecoveryAction: "Consider scaling workers or checking destination chains",
		})
	} else {
		hm.updateComponentStatus(componentKey, nil)
	}
}

// updateComponentStatus updates a component's health status
func (hm *HealthMonitor) updateComponentStatus(componentKey string, err error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	
	status, exists := hm.componentStatus[componentKey]
	if !exists {
		return
	}
	
	status.LastCheck = time.Now()
	
	if err != nil {
		status.Healthy = false
		status.LastError = err
		status.ConsecutiveFails++
	} else {
		status.Healthy = true
		status.LastError = nil
		status.ConsecutiveFails = 0
	}
}

// sendAlert sends an alert
func (hm *HealthMonitor) sendAlert(alert *Alert) {
	select {
	case hm.alerts <- alert:
		logger.Warnf("Health alert: %s - %s", alert.Component, alert.Message)
	default:
		logger.Error("Alert channel full, dropping alert")
	}
}

// alertProcessor processes and potentially sends alerts
func (hm *HealthMonitor) alertProcessor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-hm.stopChan:
			return
		case alert := <-hm.alerts:
			hm.processAlert(alert)
		}
	}
}

// processAlert processes an alert
func (hm *HealthMonitor) processAlert(alert *Alert) {
	// Log alert
	logger.Errorf("[%s] %s: %s - %v", alert.Severity, alert.Component, alert.Message, alert.Error)
	
	// Store alert in database
	if err := hm.db.StoreHealthAlert(alert); err != nil {
		logger.Errorf("Failed to store alert: %v", err)
	}
	
	// In production, send to monitoring service (e.g., PagerDuty, Slack)
}