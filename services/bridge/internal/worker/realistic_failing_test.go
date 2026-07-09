package worker

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Worker pool tests for async-confirm architecture
// Handlers return quickly (~1s simulate+send). Receipt confirmation is async.
// ============================================================================

// CountingHandler tracks calls and can inject errors or delays
type CountingHandler struct {
	callCount   atomic.Int64
	errorCount  atomic.Int64
	shouldError bool
	delay       time.Duration
}

func (h *CountingHandler) Execute(ctx context.Context, task *WorkerTask) error {
	h.callCount.Add(1)
	if h.delay > 0 {
		select {
		case <-time.After(h.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if h.shouldError {
		h.errorCount.Add(1)
		return fmt.Errorf("simulated RPC error")
	}
	return nil
}

func makeTestTask(id string, handler func(context.Context, *WorkerTask) error) *WorkerTask {
	return makeTestTaskWithSymbol(id, "BTC/USD", handler)
}

func makeTestTaskWithSymbol(id, symbol string, handler func(context.Context, *WorkerTask) error) *WorkerTask {
	return &WorkerTask{
		ID: id,
		Request: &bridgetypes.UpdateRequest{
			RouterID: "test-router",
			Intent: &bridgetypes.OracleIntent{
				Symbol:    symbol,
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
		Handler: handler,
	}
}

// TestProcessTask_Success tests that a fast handler completes immediately
func TestProcessTask_Success(t *testing.T) {
	handler := &CountingHandler{}
	task := makeTestTask("fast-success", handler.Execute)

	pool := createTestWorkerPool("test-router", 1, 30*time.Second)
	worker := &Worker{
		id:        0,
		taskQueue: make(chan *WorkerTask, 1),
		quit:      make(chan struct{}),
		pool:      pool,
	}

	done := make(chan struct{})
	go func() {
		worker.processTask(context.Background(), task)
		close(done)
	}()

	select {
	case <-done:
		assert.Equal(t, int64(1), handler.callCount.Load(), "handler should be called once")
	case <-time.After(5 * time.Second):
		t.Fatal("task didn't complete within 5s")
	}

	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&pool.activeWorkers) == 0
	}, 1*time.Second, 50*time.Millisecond, "active workers should be 0")
}

// TestProcessTask_RetriesOnError tests retry logic when handler fails
func TestProcessTask_RetriesOnError(t *testing.T) {
	handler := &CountingHandler{shouldError: true}
	task := makeTestTask("retry-test", handler.Execute)

	pool := createTestWorkerPool("test-router", 1, 30*time.Second)
	worker := &Worker{
		id:        0,
		taskQueue: make(chan *WorkerTask, 1),
		quit:      make(chan struct{}),
		pool:      pool,
	}

	done := make(chan struct{})
	go func() {
		worker.processTask(context.Background(), task)
		close(done)
	}()

	select {
	case <-done:
		// 3 attempts (loop runs for retry=0,1,2)
		assert.Equal(t, int64(3), handler.callCount.Load(), "handler should be called 3 times (maxRetries)")
	case <-time.After(30 * time.Second):
		t.Fatal("task didn't complete within 30s")
	}
}

// TestProcessTask_ContextCancelStopsRetries tests that context cancellation prevents retries
func TestProcessTask_ContextCancelStopsRetries(t *testing.T) {
	handler := &CountingHandler{shouldError: true}
	task := makeTestTask("cancel-test", handler.Execute)

	// Short timeout to trigger context expiry
	pool := createTestWorkerPool("test-router", 1, 1*time.Second)
	worker := &Worker{
		id:        0,
		taskQueue: make(chan *WorkerTask, 1),
		quit:      make(chan struct{}),
		pool:      pool,
	}

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		worker.processTask(context.Background(), task)
		close(done)
	}()

	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Task completed in %v after %d handler calls", duration, handler.callCount.Load())
		// Should have stopped after context expired, not all 3 retries
		assert.Less(t, handler.callCount.Load(), int64(4), "should not have retried 3 times after context expiry")
	case <-time.After(15 * time.Second):
		t.Fatal("task didn't complete within 15s")
	}
}

// TestProcessTask_ContextRespectedDuringDelay tests handler respects context during slow ops
func TestProcessTask_ContextRespectedDuringDelay(t *testing.T) {
	handler := &CountingHandler{delay: 10 * time.Second}
	task := makeTestTask("delay-cancel", handler.Execute)

	// 2s timeout, handler wants to sleep 10s
	pool := createTestWorkerPool("test-router", 1, 2*time.Second)
	worker := &Worker{
		id:        0,
		taskQueue: make(chan *WorkerTask, 1),
		quit:      make(chan struct{}),
		pool:      pool,
	}

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		worker.processTask(context.Background(), task)
		close(done)
	}()

	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Task completed in %v", duration)
		// Handler respects context (select on ctx.Done), so should complete in ~2s
		assert.Less(t, duration, 5*time.Second, "should complete near timeout, not wait full 10s delay")
	case <-time.After(10 * time.Second):
		t.Fatal("task didn't complete - handler ignored context")
	}
}

// TestProcessTask_MultipleWorkers tests pool with multiple workers processing tasks
func TestProcessTask_MultipleWorkers(t *testing.T) {
	numWorkers := 3
	numTasks := 6

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := createTestWorkerPool("test-router", numWorkers, 30*time.Second)
	pool.Start(ctx)
	defer pool.Stop(ctx)

	// Use a handler that completes quickly
	handler := &CountingHandler{delay: 100 * time.Millisecond}

	symbols := []string{"BTC/USD", "ETH/USD", "SOL/USD", "DOGE/USD", "ARB/USD", "MNT/USD"}
	for i := 0; i < numTasks; i++ {
		task := makeTestTaskWithSymbol(fmt.Sprintf("multi-task-%d", i), symbols[i], handler.Execute)
		pool.Submit(task)
	}

	// All tasks should complete quickly: 6 tasks, 3 workers, 100ms each = ~200ms
	assert.Eventually(t, func() bool {
		stats := pool.GetStats()
		return stats.PendingTasks == 0 && stats.ActiveTasks == 0
	}, 5*time.Second, 100*time.Millisecond, "all tasks should complete")

	assert.Equal(t, int64(numTasks), handler.callCount.Load(), "all tasks should be processed")
}
