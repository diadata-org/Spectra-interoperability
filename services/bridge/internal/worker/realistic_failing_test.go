package worker

import (
	"context"
	"fmt"
	"math/big"
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
// REALISTIC FAILING TESTS - Simulating Production Issues
// ============================================================================

// HangingRPCHandler simulates a real RPC handler that doesn't respect context
// This is what happens when go-ethereum RPC client blocks on network I/O
type HangingRPCHandler struct {
	callCount     atomic.Int64
	shouldHang    bool
	hangDuration  time.Duration
	hangStarted   chan struct{}
	hangCompleted chan struct{}
}

func (h *HangingRPCHandler) Execute(ctx context.Context, task *WorkerTask) error {
	h.callCount.Add(1)

	if h.shouldHang {
		if h.hangStarted != nil {
			h.hangStarted <- struct{}{}
		}

		// Simulate RPC call that blocks without checking context
		// This is what happens when RPC client doesn't respect context
		time.Sleep(h.hangDuration)

		if h.hangCompleted != nil {
			h.hangCompleted <- struct{}{}
		}
	}

	return nil
}

// TestProcessTask_RealRPCTimeoutScenario
//
// REAL PRODUCTION ISSUE: When RPC client (go-ethereum) makes a call that hangs due to network issues,
// the handler blocks forever because:
// 1. RPC client doesn't check context during I/O operations
// 2. Task timeout expires but handler is still blocked on network I/O
// 3. processTask doesn't forcefully interrupt the handler
//
// This test creates a realistic scenario where handler simulates a slow RPC call.
func TestProcessTask_RealRPCTimeoutScenario(t *testing.T) {
	// Create handler that simulates slow RPC (10 seconds)
	handler := &HangingRPCHandler{
		shouldHang:    true,
		hangDuration:  10 * time.Second, // Long-running RPC
		hangStarted:   make(chan struct{}),
		hangCompleted: make(chan struct{}),
	}

	// Task timeout is only 2 seconds
	shortTimeout := 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	task := &WorkerTask{
		ID: "real-rpc-timeout",
		Request: &bridgetypes.UpdateRequest{
			RouterID: "test-router",
			Intent: &bridgetypes.OracleIntent{
				Symbol:    "BTC/USD",
				Price:     big.NewInt(50000000000000000),
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
	case <-handler.hangStarted:
		t.Logf("Handler started hanging at %v", time.Since(startTime))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler did not start hanging")
	}

	// Wait for task completion (should timeout at 2 seconds)
	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Task completed after %v", duration)

		// BUG: If duration is close to handler hang duration (10s) instead of timeout (2s),
		// then taskCtx timeout didn't work
		if duration > 5*time.Second {
			t.Errorf("BUG CONFIRMED: Task took %v (expected ~%v for timeout) - Handler continued despite task timeout",
				duration, shortTimeout)
		}

	case <-time.After(shortTimeout + 12*time.Second):
		// Check if handler actually completed
		select {
		case <-handler.hangCompleted:
			t.Errorf("BUG CONFIRMED: Handler completed its full %s hang despite %s task timeout",
				handler.hangDuration, shortTimeout)
		case <-time.After(100 * time.Millisecond):
			t.Errorf("BUG CONFIRMED: Task never completed - handler is still hanging after timeout")
		}
	}

	// Verify active workers cleaned up
	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&pool.activeWorkers) == int32(0)
	}, 1*time.Second, 50*time.Millisecond,
		"Active workers should be 0 after task completion")
}

// TestProcessTask_NetworkTimeoutScenario simulates actual network timeout
func TestProcessTask_NetworkTimeoutScenario(t *testing.T) {
	handler := &HangingRPCHandler{
		shouldHang:    true,
		hangDuration:  30 * time.Second,
		hangStarted:   make(chan struct{}),
		hangCompleted: make(chan struct{}),
	}

	// Very short timeout to simulate network issue
	networkTimeout := 500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	task := &WorkerTask{
		ID: "network-timeout",
		Request: &bridgetypes.UpdateRequest{
			RouterID: "test-router",
			Intent: &bridgetypes.OracleIntent{
				Symbol:    "ETH/USD",
				Price:     big.NewInt(3000000000000000),
				Timestamp: big.NewInt(time.Now().Unix()),
				Expiry:    big.NewInt(time.Now().Add(time.Hour).Unix()),
				Nonce:     big.NewInt(54321),
				Signer:    common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12"),
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

	pool := createTestWorkerPool("test-router", 1, networkTimeout)
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

	// Wait for hang
	<-handler.hangStarted

	// Task should timeout quickly
	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Task completed in %v", duration)

		if duration > 2*time.Second {
			t.Errorf("BUG: Task took too long (%v vs expected %v timeout)", duration, networkTimeout)
		}

	case <-time.After(networkTimeout + 3*time.Second):
		t.Errorf("BUG: Task didn't timeout within expected time")
	}
}

// TestProcessTask_MultipleWorkersTimeout tests multiple workers with hanging handlers
func TestProcessTask_MultipleWorkersTimeout(t *testing.T) {
	numWorkers := 3
	taskTimeout := 1 * time.Second
	handlerHangDuration := 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := createTestWorkerPool("test-router", numWorkers, taskTimeout)
	pool.Start(ctx)
	defer pool.Stop(ctx)

	handlers := make([]*HangingRPCHandler, numWorkers)

	// Submit tasks that will hang
	for i := 0; i < numWorkers; i++ {
		handler := &HangingRPCHandler{
			shouldHang:    true,
			hangDuration:  handlerHangDuration,
			hangStarted:   make(chan struct{}),
			hangCompleted: make(chan struct{}),
		}
		handlers[i] = handler

		task := &WorkerTask{
			ID: fmt.Sprintf("multi-worker-task-%d", i),
			Request: &bridgetypes.UpdateRequest{
				RouterID: "test-router",
				Intent: &bridgetypes.OracleIntent{
					Symbol:    "BTC/USD",
					Price:     big.NewInt(50000000000000000),
					Timestamp: big.NewInt(time.Now().Unix()),
					Expiry:    big.NewInt(time.Now().Add(time.Hour).Unix()),
					Nonce:     big.NewInt(int64(i)),
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

		pool.Submit(task)
	}

	// Wait for all handlers to start hanging
	for _, handler := range handlers {
		select {
		case <-handler.hangStarted:
		case <-time.After(1 * time.Second):
			t.Errorf("Handler didn't start hanging")
		}
	}

	t.Logf("All %d handlers have started hanging", numWorkers)

	// All tasks should timeout within taskTimeout + margin
	timeoutThreshold := taskTimeout + 2*time.Second
	startTime := time.Now()

	// Monitor pool until all tasks complete
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)

		stats := pool.GetStats()
		t.Logf("Progress: Queue=%d, Active=%d, Expected=%d", stats.PendingTasks, stats.ActiveTasks, numWorkers)

		if stats.PendingTasks == 0 && stats.ActiveTasks == 0 {
			duration := time.Since(startTime)
			t.Logf("All tasks completed in %v", duration)

			if duration > timeoutThreshold {
				t.Errorf("BUG: Tasks took %v to complete (expected < %v) - workers didn't respect timeout",
					duration, timeoutThreshold)
			}

			// Check if handlers actually completed their hang
			completedHangs := 0
			for _, handler := range handlers {
				select {
				case <-handler.hangCompleted:
					completedHangs++
				default:
				}
			}

			if completedHangs > 0 {
				t.Errorf("BUG: %d/%d handlers completed their %s hang despite %s task timeout",
					completedHangs, numWorkers, handlerHangDuration, taskTimeout)
			}

			return
		}
	}

	t.Errorf("BUG: Tasks didn't complete within timeout threshold of %v", timeoutThreshold)
}

// TestProcessTask_ConcurrentSubmissionWithTimeout tests realistic load scenario
func TestProcessTask_ConcurrentSubmissionWithTimeout(t *testing.T) {
	numWorkers := 2
	numTasks := 5
	taskTimeout := 1 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := createTestWorkerPool("test-router", numWorkers, taskTimeout)
	pool.Start(ctx)
	defer pool.Stop(ctx)

	handlers := make([]*HangingRPCHandler, numTasks)

	// Submit tasks concurrently
	for i := 0; i < numTasks; i++ {
		handler := &HangingRPCHandler{
			shouldHang:   true,
			hangDuration: 15 * time.Second,
			hangStarted:  make(chan struct{}),
		}
		handlers[i] = handler

		task := &WorkerTask{
			ID: fmt.Sprintf("concurrent-task-%d", i),
			Request: &bridgetypes.UpdateRequest{
				RouterID: "test-router",
				Intent: &bridgetypes.OracleIntent{
					Symbol:    "BTC/USD",
					Price:     big.NewInt(50000000000000000),
					Timestamp: big.NewInt(time.Now().Unix()),
					Expiry:    big.NewInt(time.Now().Add(time.Hour).Unix()),
					Nonce:     big.NewInt(int64(i)),
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

		pool.Submit(task)
		time.Sleep(10 * time.Millisecond) // Small delay between submissions
	}

	t.Logf("Submitted %d tasks to %d workers", numTasks, numWorkers)

	// All tasks should complete within reasonable time
	maxWaitTime := time.Duration(numTasks)*taskTimeout + 5*time.Second
	startTime := time.Now()

	// Monitor completion
	completed := false
	for !completed {
		time.Sleep(200 * time.Millisecond)

		stats := pool.GetStats()
		if stats.PendingTasks == 0 && stats.ActiveTasks == 0 {
			completed = true
			duration := time.Since(startTime)
			t.Logf("All %d tasks completed in %v", numTasks, duration)

			// Calculate expected completion time:
			// - numWorkers tasks start immediately
			// - Remaining tasks wait in queue
			expectedMaxTime := time.Duration((numTasks+numWorkers-1)/numWorkers)*taskTimeout + 2*time.Second

			if duration > expectedMaxTime*2 { // Allow 2x margin
				t.Errorf("BUG: Tasks took %v (expected < %v) - queue or timeout issue",
					duration, expectedMaxTime)
			}
		}

		if time.Since(startTime) > maxWaitTime {
			t.Errorf("BUG: Tasks didn't complete within %v timeout", maxWaitTime)
			break
		}
	}
}