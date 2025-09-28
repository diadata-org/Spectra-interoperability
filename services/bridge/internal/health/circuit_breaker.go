package health

import (
	"fmt"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name             string
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenMaxCalls int

	mu              sync.Mutex
	state           State
	failures        int
	lastFailureTime time.Time
	successCount    int
	lastStateChange time.Time
}

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// String returns the string representation of the state
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		halfOpenMaxCalls: 3, // Allow 3 calls in half-open state
		state:            StateClosed,
		lastStateChange:  time.Now(),
	}
}

// Call executes the function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()

	// Check if we should transition from open to half-open
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.resetTimeout {
		cb.changeState(StateHalfOpen)
	}

	state := cb.state
	cb.mu.Unlock()

	// Check if circuit is open
	if state == StateOpen {
		return fmt.Errorf("circuit breaker %s is open", cb.name)
	}

	// Execute the function
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

// recordFailure records a failure
func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.maxFailures {
			cb.changeState(StateOpen)
		}
	case StateHalfOpen:
		cb.changeState(StateOpen)
	}
}

// recordSuccess records a success
func (cb *CircuitBreaker) recordSuccess() {
	switch cb.state {
	case StateClosed:
		cb.failures = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.changeState(StateClosed)
		}
	}
}

// changeState changes the circuit breaker state
func (cb *CircuitBreaker) changeState(newState State) {
	if cb.state != newState {
		oldState := cb.state
		cb.state = newState
		cb.lastStateChange = time.Now()

		// Reset counters based on new state
		switch newState {
		case StateClosed:
			cb.failures = 0
			cb.successCount = 0
		case StateHalfOpen:
			cb.successCount = 0
		}

		logger.Infof("Circuit breaker %s: %s -> %s", cb.name, oldState, newState)
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return map[string]interface{}{
		"name":              cb.name,
		"state":             cb.state.String(),
		"failures":          cb.failures,
		"success_count":     cb.successCount,
		"last_failure_time": cb.lastFailureTime,
		"last_state_change": cb.lastStateChange,
	}
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.changeState(StateClosed)
	cb.failures = 0
	cb.successCount = 0
}
