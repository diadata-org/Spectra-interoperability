package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// RecoveryManager handles automatic recovery from failures
type RecoveryManager struct {
	config         *config.RecoveryConfig
	healthMonitor  *HealthMonitor
	
	mu             sync.Mutex
	recoveryActions map[string]RecoveryAction
	activeRecoveries map[string]*RecoveryAttempt
	
	stopChan       chan struct{}
}

// RecoveryAction defines a recovery action for a component
type RecoveryAction func(ctx context.Context, component string, error error) error

// RecoveryAttempt tracks an ongoing recovery attempt
type RecoveryAttempt struct {
	Component    string
	StartTime    time.Time
	Attempts     int
	LastAttempt  time.Time
	Success      bool
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(
	cfg *config.RecoveryConfig,
	healthMonitor *HealthMonitor,
) *RecoveryManager {
	rm := &RecoveryManager{
		config:           cfg,
		healthMonitor:    healthMonitor,
		recoveryActions:  make(map[string]RecoveryAction),
		activeRecoveries: make(map[string]*RecoveryAttempt),
		stopChan:         make(chan struct{}),
	}
	
	// Register default recovery actions
	rm.registerDefaultActions()
	
	return rm
}

// Start begins the recovery manager
func (rm *RecoveryManager) Start(ctx context.Context) error {
	logger.Info("Starting recovery manager")
	
	// Monitor health alerts
	go rm.monitorAlerts(ctx)
	
	return nil
}

// Stop gracefully stops the recovery manager
func (rm *RecoveryManager) Stop() error {
	logger.Info("Stopping recovery manager")
	close(rm.stopChan)
	return nil
}

// RegisterRecoveryAction registers a custom recovery action
func (rm *RecoveryManager) RegisterRecoveryAction(componentType string, action RecoveryAction) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.recoveryActions[componentType] = action
}

// registerDefaultActions registers default recovery actions
func (rm *RecoveryManager) registerDefaultActions() {
	// Database recovery
	rm.recoveryActions["database"] = rm.recoverDatabase
	
	// Blockchain connection recovery
	rm.recoveryActions["blockchain"] = rm.recoverBlockchainConnection
	
	// Service recovery
	rm.recoveryActions["service"] = rm.recoverService
}

// monitorAlerts monitors health alerts and triggers recovery
func (rm *RecoveryManager) monitorAlerts(ctx context.Context) {
	alerts := rm.healthMonitor.GetAlerts()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stopChan:
			return
		case alert := <-alerts:
			go rm.handleAlert(ctx, alert)
		}
	}
}

// handleAlert handles a health alert
func (rm *RecoveryManager) handleAlert(ctx context.Context, alert *Alert) {
	logger.Infof("Handling alert for component: %s", alert.Component)
	
	// Check if recovery is already in progress
	rm.mu.Lock()
	if recovery, exists := rm.activeRecoveries[alert.Component]; exists {
		if time.Since(recovery.LastAttempt) < rm.config.RetryInterval.Duration() {
			rm.mu.Unlock()
			logger.Debugf("Recovery already in progress for %s", alert.Component)
			return
		}
	}
	rm.mu.Unlock()
	
	// Get component status
	status := rm.healthMonitor.GetStatus()
	componentStatus, exists := status[alert.Component]
	if !exists {
		logger.Errorf("Unknown component: %s", alert.Component)
		return
	}
	
	// Check if component should be recovered
	if !rm.shouldRecover(componentStatus) {
		logger.Debugf("Component %s does not meet recovery criteria", alert.Component)
		return
	}
	
	// Start recovery
	rm.startRecovery(ctx, alert.Component, componentStatus.Type, alert.Error)
}

// shouldRecover determines if a component should be recovered
func (rm *RecoveryManager) shouldRecover(status *ComponentStatus) bool {
	// Don't recover if healthy
	if status.Healthy {
		return false
	}
	
	// Check consecutive failures threshold
	if status.ConsecutiveFails < rm.config.MinFailures {
		return false
	}
	
	return true
}

// startRecovery starts a recovery attempt
func (rm *RecoveryManager) startRecovery(ctx context.Context, component, componentType string, err error) {
	rm.mu.Lock()
	recovery, exists := rm.activeRecoveries[component]
	if !exists {
		recovery = &RecoveryAttempt{
			Component: component,
			StartTime: time.Now(),
			Attempts:  0,
		}
		rm.activeRecoveries[component] = recovery
	}
	
	recovery.Attempts++
	recovery.LastAttempt = time.Now()
	rm.mu.Unlock()
	
	// Check max attempts
	if recovery.Attempts > rm.config.MaxAttempts {
		logger.Errorf("Max recovery attempts reached for %s", component)
		rm.sendRecoveryFailedAlert(component, fmt.Errorf("max recovery attempts exceeded"))
		return
	}
	
	logger.Infof("Starting recovery attempt %d for %s", recovery.Attempts, component)
	
	// Get recovery action
	action, exists := rm.recoveryActions[componentType]
	if !exists {
		logger.Warnf("No recovery action for component type: %s", componentType)
		return
	}
	
	// Execute recovery with timeout
	recoveryCtx, cancel := context.WithTimeout(ctx, rm.config.RecoveryTimeout.Duration())
	defer cancel()
	
	if err := action(recoveryCtx, component, err); err != nil {
		logger.Errorf("Recovery failed for %s: %v", component, err)
		
		// Schedule retry
		time.AfterFunc(rm.config.RetryInterval.Duration(), func() {
			rm.startRecovery(ctx, component, componentType, err)
		})
	} else {
		logger.Infof("Recovery successful for %s", component)
		
		rm.mu.Lock()
		recovery.Success = true
		delete(rm.activeRecoveries, component)
		rm.mu.Unlock()
	}
}

// recoverDatabase attempts to recover database connection
func (rm *RecoveryManager) recoverDatabase(ctx context.Context, component string, err error) error {
	logger.Info("Attempting database recovery")
	
	// In a real implementation, this might:
	// 1. Close existing connections
	// 2. Clear connection pool
	// 3. Attempt reconnection
	// 4. Run health check query
	
	// For now, just log
	logger.Info("Database recovery placeholder")
	
	return nil
}

// recoverBlockchainConnection attempts to recover blockchain connection
func (rm *RecoveryManager) recoverBlockchainConnection(ctx context.Context, component string, err error) error {
	logger.Infof("Attempting blockchain recovery for %s", component)
	
	// In a real implementation, this might:
	// 1. Close existing WebSocket/HTTP connections
	// 2. Try alternate RPC endpoints
	// 3. Reconnect with exponential backoff
	// 4. Verify connection with block number query
	
	// For now, just log
	logger.Info("Blockchain recovery placeholder")
	
	return nil
}

// recoverService attempts to recover a service
func (rm *RecoveryManager) recoverService(ctx context.Context, component string, err error) error {
	logger.Infof("Attempting service recovery for %s", component)
	
	// In a real implementation, this might:
	// 1. Restart the service
	// 2. Clear any stuck state
	// 3. Reinitialize connections
	// 4. Verify service is operational
	
	// For now, just log
	logger.Info("Service recovery placeholder")
	
	return nil
}

// sendRecoveryFailedAlert sends an alert when recovery fails
func (rm *RecoveryManager) sendRecoveryFailedAlert(component string, err error) {
	alert := &Alert{
		Component:      component,
		Severity:       "critical",
		Message:        "Recovery failed after maximum attempts",
		Error:          err,
		Timestamp:      time.Now(),
		RecoveryAction: "Manual intervention required",
	}
	
	// Send through health monitor
	select {
	case rm.healthMonitor.alerts <- alert:
	default:
		logger.Error("Failed to send recovery failed alert")
	}
}

// GetActiveRecoveries returns currently active recovery attempts
func (rm *RecoveryManager) GetActiveRecoveries() map[string]*RecoveryAttempt {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	// Deep copy
	active := make(map[string]*RecoveryAttempt)
	for k, v := range rm.activeRecoveries {
		active[k] = &RecoveryAttempt{
			Component:   v.Component,
			StartTime:   v.StartTime,
			Attempts:    v.Attempts,
			LastAttempt: v.LastAttempt,
			Success:     v.Success,
		}
	}
	
	return active
}