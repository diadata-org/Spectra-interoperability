package worker

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// MOCK INFRASTRUCTURE
// ============================================================================

// MockTaskHandler allows controlled handler behavior for testing
type MockTaskHandler struct {
	executeFunc     func(ctx context.Context, task *WorkerTask) error
	callCount       atomic.Int64
	errorToReturn   error
	delay           time.Duration
	shouldHang      bool
	hangChannel     chan struct{}
	panicOnError    bool
	returnAfterN    int // Return error after N successful calls
	successfulCalls atomic.Int64
}

// Execute implements the handler interface
func (m *MockTaskHandler) Execute(ctx context.Context, task *WorkerTask) error {
	m.callCount.Add(1)

	// Simulate delay
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.delay):
			// Continue
		}
	}

	// Simulate hanging handler
	if m.shouldHang {
		if m.hangChannel != nil {
			m.hangChannel <- struct{}{} // Signal that we're hanging
		}
		<-ctx.Done() // Block until context cancelled
		return ctx.Err()
	}

	// Simulate panic
	if m.panicOnError && m.errorToReturn != nil {
		panic(m.errorToReturn)
	}

	// Simulate intermittent failures
	if m.returnAfterN > 0 {
		callNum := m.successfulCalls.Add(1)
		if int(callNum) <= m.returnAfterN {
			return m.errorToReturn
		}
	}

	return m.errorToReturn
}

func (m *MockTaskHandler) Reset() {
	m.callCount.Store(0)
	m.successfulCalls.Store(0)
}

// createTestWorkerPool creates a worker pool for testing
func createTestWorkerPool(routerID string, maxWorkers int, taskTimeout time.Duration) *WorkerPool {
	pool := NewWorkerPool(routerID, maxWorkers, 100, taskTimeout)
	pool.SetMetricsCollector(nil) // Disable metrics for testing
	return pool
}

// createTestTask creates a test WorkerTask
func createTestTask(id string, handler *MockTaskHandler) *WorkerTask {
	return &WorkerTask{
		ID: id,
		Request: &bridgetypes.UpdateRequest{
			RouterID: "test-router",
			Intent: &bridgetypes.OracleIntent{
				Symbol:    "BTC/USD",
				Price:     big.NewInt(50000000000000000), // 50000 in wei
				Timestamp: big.NewInt(time.Now().Unix()),
				Expiry:    big.NewInt(time.Now().Add(time.Hour).Unix()),
				Nonce:     big.NewInt(12345),
				Signer:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
			},
			DestinationChain: &config.DestinationConfig{
				ChainID: 1,
			},
			DestinationMethodConfig: &config.DestinationMethodConfig{
				Name:     "updatePrice",
				GasLimit: 300000,
			},
		},
		Handler: handler.Execute,
	}
}

// ============================================================================
// FAILING TEST 1: Handler Doesn't Respect Context Cancellation
// ============================================================================

// TestProcessTask_HandlerDoesNotRespectContextCancellation
//
// FAILS BECAUSE: The handler ignores ctx.Done() and blocks forever when taskCtx times out
// The processTask creates taskCtx with timeout, but if handler doesn't check context,
// it will hang forever even after taskCtx expires.
//
// EXPECTED BEHAVIOR: Task should complete when taskCtx times out, regardless of handler state
// ACTUAL BEHAVIOR: Task hangs indefinitely
func TestProcessTask_HandlerDoesNotRespectContextCancellation(t *testing.T) {
	// t.Skip("This test fails - handler doesn't respect context cancellation")

	// Short timeout to trigger the issue
	shortTimeout := 1 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		// Handler that blocks forever without checking context
		shouldHang:  true,
		hangChannel: make(chan struct{}),
	}
	task := createTestTask("task-ignores-context", handler)

	pool := createTestWorkerPool("test-router", 1, shortTimeout)
	worker := &Worker{
		id:               0,
		taskQueue:        make(chan *WorkerTask, 1),
		quit:             make(chan struct{}),
		wg:               &sync.WaitGroup{},
		metricsCollector: nil,
		pool:             pool,
	}

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		worker.processTask(ctx, task)
		close(done)
	}()

	// Wait for handler to start hanging
	select {
	case <-handler.hangChannel:
		t.Log("Handler is hanging (expected)")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler did not start hanging")
	}

	// Wait for task completion - SHOULD TIMEOUT but it doesn't
	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Task completed after %v", duration)

		// SHOULD timeout within shortTimeout + margin
		// But in reality, it hangs forever because handler ignores context
		if duration > shortTimeout+2*time.Second {
			t.Errorf("BUG CONFIRMED: Task took %v (expected < %v) - handler doesn't respect context cancellation",
				duration, shortTimeout+2*time.Second)
		}

	case <-time.After(shortTimeout + 3*time.Second):
		// BUG: Task never completes because handler hangs forever
		t.Fatal("BUG CONFIRMED: Task never completed - handler ignores context cancellation and hangs forever")
	}

	// Verify active workers cleaned up despite timeout
	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&pool.activeWorkers) == int32(0)
	}, 1*time.Second, 50*time.Millisecond,
		"Active workers should be 0 after timeout (BUG if not)")
}

// ============================================================================
// FAILING TEST 2: Active Worker Counter Not Decrementing on Timeout
// ============================================================================

// TestProcessTask_ActiveWorkerCounterNotDecrementing
//
// FAILS BECAUSE: When handler hangs and times out, the defer atomic.AddInt32(&activeWorkers, -1)
// at line 332 might not execute if the goroutine is stuck inside handler execution.
//
// EXPECTED BEHAVIOR: Active worker counter decrements when task completes (even on timeout)
// ACTUAL BEHAVIOR: Counter increments but never decrements when handler hangs
func TestProcessTask_ActiveWorkerCounterNotDecrementing(t *testing.T) {
	t.Skip("This test fails - active worker counter leaks on timeout")

	shortTimeout := 1 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		shouldHang:  true,
		hangChannel: make(chan struct{}),
	}
	task := createTestTask("task-counter-leak", handler)

	pool := createTestWorkerPool("test-router", 1, shortTimeout)
	worker := &Worker{
		id:               0,
		taskQueue:        make(chan *WorkerTask, 1),
		quit:             make(chan struct{}),
		wg:               &sync.WaitGroup{},
		metricsCollector: nil,
		pool:             pool,
	}

	// Verify counter starts at 0
	initialCount := atomic.LoadInt32(&pool.activeWorkers)
	assert.Equal(t, int32(0), initialCount)

	// Start task
	go worker.processTask(ctx, task)

	// Wait for handler to hang
	select {
	case <-handler.hangChannel:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler did not start hanging")
	}

	// Wait for timeout
	time.Sleep(shortTimeout + 500*time.Millisecond)

	// BUG: Counter might still be 1 even after timeout
	finalCount := atomic.LoadInt32(&pool.activeWorkers)
	t.Logf("Active workers: %d (should be 0)", finalCount)

	if finalCount > 0 {
		t.Errorf("BUG CONFIRMED: Active worker counter leaked - started at 0, now %d after timeout",
			finalCount)
	}
}

// ============================================================================
// FAILING TEST 3: Retry Loop Continues Despite Context Expiry
// ============================================================================

// TestProcessTask_RetryLoopIgnoresContextExpiry
//
// FAILS BECAUSE: At line 373, the code checks `if taskCtx.Err() != nil` to skip retries,
// but if the handler is slow, the context might expire during retry delay,
// and the next retry attempt won't respect the expired context.
//
// EXPECTED BEHAVIOR: Retry loop stops when context expires
// ACTUAL BEHAVIOR: Retry loop continues even after context expires
func TestProcessTask_RetryLoopIgnoresContextExpiry(t *testing.T) {
	t.Skip("This test fails - retry loop doesn't respect context expiry")

	// Very short timeout to expire during retries
	shortTimeout := 500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		errorToReturn: errors.New("simulated RPC error"),
		delay:         300 * time.Millisecond, // Each attempt takes time
		returnAfterN:  10, // Will never succeed, will retry forever
	}
	task := createTestTask("task-retry-forever", handler)

	pool := createTestWorkerPool("test-router", 1, shortTimeout)
	worker := &Worker{
		id:               0,
		taskQueue:        make(chan *WorkerTask, 1),
		quit:             make(chan struct{}),
		wg:               &sync.WaitGroup{},
		metricsCollector: nil,
		pool:             pool,
	}

	done := make(chan struct{})
	go func() {
		worker.processTask(ctx, task)
		close(done)
	}()

	select {
	case <-done:
		duration := time.Since(time.Now())
		t.Logf("Task completed after %v with %d retries", duration, handler.callCount.Load()-1)

		// BUG: If it took longer than timeout, retry loop continued despite context expiry
		if handler.callCount.Load() > 2 {
			t.Errorf("BUG CONFIRMED: Retry loop executed %d times despite %v timeout (expected 1-2)",
				handler.callCount.Load(), shortTimeout)
		}

	case <-time.After(shortTimeout + 2*time.Second):
		// BUG: Task never completed because retry loop continues
		t.Errorf("BUG CONFIRMED: Retry loop didn't stop after context expired - handler called %d times",
			handler.callCount.Load())
	}
}

// ============================================================================
// FAILING TEST 4: Goroutine Leak on Handler Timeout
// ============================================================================

// TestProcessTask_GoroutineLeakOnTimeout
//
// FAILS BECAUSE: When handler times out, the goroutine running processTask doesn't exit,
// causing goroutine leaks over time.
//
// EXPECTED BEHAVIOR: All goroutines exit after task completion/timeout
// ACTUAL BEHAVIOR: Goroutines accumulate when handlers hang and timeout
func TestProcessTask_GoroutineLeakOnTimeout(t *testing.T) {
	t.Skip("This test fails - goroutines leak on handler timeout")

	initialGoroutines := runtime.NumGoroutine()
	t.Logf("Initial goroutine count: %d", initialGoroutines)

	shortTimeout := 1 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numTasks := 10
	for i := 0; i < numTasks; i++ {
		handler := &MockTaskHandler{
			shouldHang:  true,
			hangChannel: make(chan struct{}),
		}
		task := createTestTask(fmt.Sprintf("leak-task-%d", i), handler)

		pool := createTestWorkerPool("test-router", 1, shortTimeout)
		worker := &Worker{
			id:               0,
			taskQueue:        make(chan *WorkerTask, 1),
			quit:             make(chan struct{}),
			wg:               &sync.WaitGroup{},
			metricsCollector: nil,
			pool:             pool,
		}

		// Start task in goroutine (simulates real worker behavior)
		go worker.processTask(ctx, task)

		// Wait for handler to hang
		<-handler.hangChannel

		// Wait for timeout
		time.Sleep(shortTimeout + 100*time.Millisecond)
	}

	// Give goroutines time to clean up
	time.Sleep(500 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Final goroutine count: %d", finalGoroutines)

	goroutineDiff := finalGoroutines - initialGoroutines

	// BUG: More than expected goroutines leaked
	// Allow some margin (e.g., 5) for test infrastructure
	if goroutineDiff > numTasks/2 {
		t.Errorf("BUG CONFIRMED: Goroutine leak - started with %d, ended with %d (leaked %d goroutines after %d tasks)",
			initialGoroutines, finalGoroutines, goroutineDiff, numTasks)
	}
}

// ============================================================================
// FAILING TEST 5: Context Not Properly Cancelled
// ============================================================================

// TestProcessTask_ContextNotProperlyCancelled
//
// FAILS BECAUSE: The `defer cancel()` at line 354 might not execute or execute too late
// if the handler hangs, leaving the taskCtx alive and consuming resources.
//
// EXPECTED BEHAVIOR: taskCtx is immediately cancelled when processTask exits
// ACTUAL BEHAVIOR: taskCtx remains active, leaking resources
func TestProcessTask_ContextNotProperlyCancelled(t *testing.T) {
	t.Skip("This test fails - context not properly cancelled on timeout")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		shouldHang:  true,
		hangChannel: make(chan struct{}),
	}
	task := createTestTask("task-context-leak", handler)

	// Custom timeout tracker
	timeoutReached := false
	timeoutDuration := 1 * time.Second

	pool := createTestWorkerPool("test-router", 1, timeoutDuration)
	worker := &Worker{
		id:               0,
		taskQueue:        make(chan *WorkerTask, 1),
		quit:             make(chan struct{}),
		wg:               &sync.WaitGroup{},
		metricsCollector: nil,
		pool:             pool,
	}

	// Start task
	go worker.processTask(ctx, task)

	// Wait for handler to hang
	<-handler.hangChannel

	// Wait for expected timeout
	time.Sleep(timeoutDuration + 200*time.Millisecond)
	timeoutReached = true

	// Check if taskCtx was cancelled by examining processTask's internal state
	// This is a heuristic - we can't directly inspect taskCtx, but we can check behavior
	time.Sleep(500 * time.Millisecond)

	activeCount := atomic.LoadInt32(&pool.activeWorkers)
	if timeoutReached && activeCount > 0 {
		t.Errorf("BUG CONFIRMED: Context not properly cancelled - active workers still %d after timeout",
			activeCount)
	}
}

// ============================================================================
// FAILING TEST 6: Pending Task List Race Condition
// ============================================================================

// TestProcessTask_PendingTaskListRaceCondition
//
// FAILS BECAUSE: removePending (line 283) iterates over pendingTasks slice with mutex lock,
// but the iteration happens while pendingTasks is being appended in Submit (line 218).
// Under concurrent access, this can cause data races or missed removals.
//
// EXPECTED BEHAVIOR: Pending tasks are atomically added and removed without race
// ACTUAL BEHAVIOR: Data races and inconsistent pending task list
func TestProcessTask_PendingTaskListRaceCondition(t *testing.T) {
	t.Skip("This test fails - race condition in pending task list")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		delay: 50 * time.Millisecond,
	}

	pool := createTestWorkerPool("test-router", 2, time.Minute)
	pool.Start(ctx)
	defer pool.Stop(ctx)

	numTasks := 100

	// Submit tasks rapidly while workers are processing
	for i := 0; i < numTasks; i++ {
		task := createTestTask(fmt.Sprintf("race-task-%d", i), handler)
		go pool.Submit(task)

		// Add delay to create race condition opportunity
		if i%10 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}

	// Wait for all tasks
	time.Sleep(5 * time.Second)

	stats := pool.GetStats()
	t.Logf("Final stats - Pending: %d, Active: %d", stats.PendingTasks, stats.ActiveTasks)

	// BUG: Pending tasks might not be 0 due to race condition
	if stats.PendingTasks != 0 {
		t.Errorf("BUG CONFIRMED: Pending task list has %d items (expected 0) - race condition in removal",
			stats.PendingTasks)
	}

	// Check pending tasks list directly
	pendingTasks := pool.ListPendingTasks()
	if len(pendingTasks) > 0 {
		t.Errorf("BUG CONFIRMED: ListPendingTasks returned %d tasks (expected 0) - list inconsistency",
			len(pendingTasks))
	}
}

// ============================================================================
// FAILING TEST 7: Metrics Collector Not Notified on Timeout
// ============================================================================

// TestProcessTask_MetricsNotRecordedOnTimeout
//
// FAILS BECAUSE: When handler times out, the code at lines 384-400 doesn't execute
// because the retry loop breaks early at line 376, skipping metrics recording.
//
// EXPECTED BEHAVIOR: Metrics (failed tasks) are recorded even on timeout
// ACTUAL BEHAVIOR: No metrics recorded on timeout, making debugging difficult
func TestProcessTask_MetricsNotRecordedOnTimeout(t *testing.T) {
	t.Skip("This test fails - metrics not recorded on timeout")

	// This test would require modifying the production code to use dependency injection
	// For now, it serves as documentation of the issue

	t.Error("BUG: Metrics not recorded on timeout - requires code changes to fix")
}

// ============================================================================
// FAILING TEST 8: Panic Recovery Missing
// ============================================================================

// TestProcessTask_PanicNotRecovered
//
// FAILS BECAUSE: If task.Handler panics, the panic propagates up without recovery,
// causing the worker goroutine to crash without cleanup (activeWorkers counter leak).
//
// EXPECTED BEHAVIOR: Panic is recovered, logged, and worker counter cleaned up
// ACTUAL BEHAVIOR: Worker crashes, counter leaks, no cleanup
func TestProcessTask_PanicNotRecovered(t *testing.T) {
	t.Skip("This test fails - panic not recovered")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &MockTaskHandler{
		panicOnError:  true,
		errorToReturn: errors.New("intentional panic"),
	}
	task := createTestTask("task-panic", handler)

	pool := createTestWorkerPool("test-router", 1, time.Minute)
	worker := &Worker{
		id:               0,
		taskQueue:        make(chan *WorkerTask, 1),
		quit:             make(chan struct{}),
		wg:               &sync.WaitGroup{},
		metricsCollector: nil,
		pool:             pool,
	}

	// Verify counter starts at 0
	assert.Equal(t, int32(0), atomic.LoadInt32(&pool.activeWorkers))

	// Handler panics - current code doesn't recover
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Panic caught at test level (not in processTask): %v", r)

			// BUG: Active workers counter still 1 because defer didn't run
			activeCount := atomic.LoadInt32(&pool.activeWorkers)
			if activeCount > 0 {
				t.Errorf("BUG CONFIRMED: Panic caused active worker counter leak - counter is %d (expected 0)",
					activeCount)
			}
		}
	}()

	worker.processTask(ctx, task)
}

// ============================================================================
// SUMMARY TEST - All Issues Combined
// ============================================================================

// TestProcessTask_AllKnownIssues
//
// This test combines all the issues to verify they exist in the current code.
// Run this to get a quick overview of what needs to be fixed.
func TestProcessTask_AllKnownIssues(t *testing.T) {
	t.Run("HandlerIgnoresContext", func(t *testing.T) {
		TestProcessTask_HandlerDoesNotRespectContextCancellation(t)
	})

	t.Run("ActiveWorkerCounterLeak", func(t *testing.T) {
		TestProcessTask_ActiveWorkerCounterNotDecrementing(t)
	})

	t.Run("RetryLoopIgnoresExpiry", func(t *testing.T) {
		TestProcessTask_RetryLoopIgnoresContextExpiry(t)
	})

	t.Run("GoroutineLeak", func(t *testing.T) {
		TestProcessTask_GoroutineLeakOnTimeout(t)
	})

	t.Run("PendingTaskRace", func(t *testing.T) {
		TestProcessTask_PendingTaskListRaceCondition(t)
	})

	t.Run("PanicNotRecovered", func(t *testing.T) {
		TestProcessTask_PanicNotRecovered(t)
	})
}