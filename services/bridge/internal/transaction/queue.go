package transaction

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

type Queue struct {
	queueKey string
	queue    chan *queuedRequest
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
	metrics  *metrics.Collector

	// Stats tracking
	totalSubmitted int64
	totalCompleted int64
	totalFailed    int64
	lastSubmitTime time.Time
	lastCompleteAt time.Time
	avgExecTime    time.Duration

	// Pending item tracking for API visibility
	pendingItems []SubmitMeta
}

type queuedRequest struct {
	ctx         context.Context
	executor    ExecutorFunc
	resultCh    chan *Result
	enqueueTime time.Time
	meta        SubmitMeta
}

// SubmitMeta carries caller-provided metadata about what the tx is for
type SubmitMeta struct {
	Symbol       string    `json:"symbol"`
	Contract     string    `json:"contract"`
	ChainID      int64     `json:"chain_id"`
	RouterID     string    `json:"router_id"`
	Enqueued     time.Time `json:"enqueued"`
	OnchainTS    int64     `json:"onchain_ts"`
}

func NewQueue(queueKey string, queueSize int, metrics *metrics.Collector) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		queueKey: queueKey,
		queue:    make(chan *queuedRequest, queueSize),
		ctx:      ctx,
		cancel:   cancel,
		metrics:  metrics,
	}
}

func (q *Queue) Start() {
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()

	logger.Infof("Starting transaction queue: %s", q.queueKey)

	q.wg.Add(1)
	go q.processQueue()
}

func (q *Queue) Stop() {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return
	}
	q.running = false
	q.mu.Unlock()

	logger.Infof("Stopping transaction queue: %s", q.queueKey)

	q.cancel()
	q.wg.Wait()

	logger.Infof("Transaction queue stopped: %s", q.queueKey)
}

func (q *Queue) Submit(ctx context.Context, executor ExecutorFunc, meta SubmitMeta) (*types.Transaction, error) {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return nil, fmt.Errorf("transaction queue is not running")
	}
	q.mu.Unlock()

	deadline, _ := ctx.Deadline()
	meta.Enqueued = time.Now()
	resultCh := make(chan *Result, 1)
	req := &queuedRequest{
		ctx:         ctx,
		executor:    executor,
		resultCh:    resultCh,
		enqueueTime: meta.Enqueued,
		meta:        meta,
	}

	t0 := time.Now()
	select {
	case q.queue <- req:
		q.mu.Lock()
		q.totalSubmitted++
		q.lastSubmitTime = time.Now()
		q.pendingItems = append(q.pendingItems, meta)
		q.mu.Unlock()
		if q.metrics != nil {
			q.metrics.SetQueueLength(q.queueKey, len(q.queue))
		}
		logger.Infof("[QUEUE] Submitted to serial queue: symbol=%s, queue_depth=%d, enqueue_took=%v, deadline_remaining=%v",
			meta.Symbol, len(q.queue), time.Since(t0), time.Until(deadline).Round(time.Millisecond))
	case <-ctx.Done():
		logger.Warnf("[QUEUE] Submit cancelled before enqueue: symbol=%s, took=%v, err=%v", meta.Symbol, time.Since(t0), ctx.Err())
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		logger.Warnf("[QUEUE] Submit timed out enqueueing: symbol=%s, queue_depth=%d", meta.Symbol, len(q.queue))
		return nil, fmt.Errorf("timeout submitting transaction to queue")
	}

	t1 := time.Now()
	select {
	case result := <-resultCh:
		logger.Infof("[QUEUE] Got result from serial queue: symbol=%s, wait_took=%v, deadline_remaining=%v",
			meta.Symbol, time.Since(t1), time.Until(deadline).Round(time.Millisecond))
		return result.Tx, result.Err
	case <-ctx.Done():
		logger.Warnf("[QUEUE] Submit cancelled while waiting for result: symbol=%s, waited=%v, deadline_remaining=%v, err=%v",
			meta.Symbol, time.Since(t1), time.Until(deadline).Round(time.Millisecond), ctx.Err())
		return nil, ctx.Err()
	}
}

func (q *Queue) processQueue() {
	defer q.wg.Done()

	logger.Infof("Transaction queue processor started: %s", q.queueKey)

	for {
		select {
		case <-q.ctx.Done():
			logger.Infof("Transaction queue processor stopping: %s", q.queueKey)
			return

		case req := <-q.queue:
			q.processRequest(req)
		}
	}
}

func (q *Queue) processRequest(req *queuedRequest) {
	// Dequeue from pending tracking (FIFO)
	q.mu.Lock()
	if len(q.pendingItems) > 0 {
		q.pendingItems = q.pendingItems[1:]
	}
	q.mu.Unlock()

	select {
	case <-req.ctx.Done():
		logger.Warnf("Transaction request cancelled before execution: %s symbol=%s", q.queueKey, req.meta.Symbol)
		req.resultCh <- &Result{
			Tx:  nil,
			Err: req.ctx.Err(),
		}
		return
	default:
	}

	// Record wait duration
	waitDuration := time.Since(req.enqueueTime).Seconds()
	if q.metrics != nil {
		q.metrics.ObserveQueueWaitDuration(q.queueKey, waitDuration)
		q.metrics.SetQueueLength(q.queueKey, len(q.queue))
	}

	startTime := time.Now()
	logger.Infof("[QUEUE-EXEC] Starting execution: symbol=%s, waited_in_queue=%v", req.meta.Symbol, time.Since(req.enqueueTime))
	tx, err := req.executor(req.ctx)
	duration := time.Since(startTime)
	logger.Infof("[QUEUE-EXEC] Execution done: symbol=%s, took=%v, err=%v", req.meta.Symbol, duration, err)

	if q.metrics != nil {
		q.metrics.ObserveQueueProcessingDuration(q.queueKey, duration.Seconds())
	}

	if err != nil {
		errorDetail := extractErrorDetail(err)
		logger.Errorf("Transaction execution failed for queue [%s] after %v: %s", q.queueKey, duration, errorDetail)
		q.mu.Lock()
		q.totalFailed++
		q.mu.Unlock()
	} else if tx != nil {
		logger.Infof("Transaction executed successfully for queue [%s] in %v: %s", q.queueKey, duration, tx.Hash().Hex())
	} else {
		logger.Infof("Transaction executed successfully for queue [%s] in %v (no tx returned)", q.queueKey, duration)
	}

	q.mu.Lock()
	if err == nil {
		q.totalCompleted++
		// Running average: avg = avg * 0.9 + current * 0.1
		if q.avgExecTime == 0 {
			q.avgExecTime = duration
		} else {
			q.avgExecTime = (q.avgExecTime*9 + duration*1) / 10
		}
	}
	q.lastCompleteAt = time.Now()
	q.mu.Unlock()

	select {
	case req.resultCh <- &Result{Tx: tx, Err: err}:
	case <-time.After(5 * time.Second):
		logger.Errorf("Timeout sending transaction result for %s", q.queueKey)
	}
}

func (q *Queue) GetQueueLength() int {
	return len(q.queue)
}

// QueueStats holds snapshot statistics for a queue
type QueueStats struct {
	Key             string        `json:"key"`
	Pending         int           `json:"pending"`
	Capacity        int           `json:"capacity"`
	Running         bool          `json:"running"`
	TotalSubmitted  int64         `json:"total_submitted"`
	TotalCompleted  int64         `json:"total_completed"`
	TotalFailed     int64         `json:"total_failed"`
	AvgExecTime     string        `json:"avg_exec_time"`
	LastSubmitTime  time.Time     `json:"last_submit_time"`
	LastCompleteAt  time.Time     `json:"last_complete_at"`
	ThroughputPerMin float64      `json:"throughput_per_min"`
	PendingItems    []SubmitMeta  `json:"pending_items"`
}

func (q *Queue) GetStats() QueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Copy pending items
	pending := make([]SubmitMeta, len(q.pendingItems))
	copy(pending, q.pendingItems)

	stats := QueueStats{
		Key:            q.queueKey,
		Pending:        len(q.queue),
		Capacity:       cap(q.queue),
		Running:        q.running,
		TotalSubmitted: q.totalSubmitted,
		TotalCompleted: q.totalCompleted,
		TotalFailed:    q.totalFailed,
		AvgExecTime:    q.avgExecTime.Round(time.Millisecond).String(),
		LastSubmitTime: q.lastSubmitTime,
		LastCompleteAt: q.lastCompleteAt,
		PendingItems:   pending,
	}

	// Calculate throughput from uptime
	if !q.lastCompleteAt.IsZero() && q.totalCompleted > 0 {
		uptime := time.Since(q.lastSubmitTime).Minutes()
		if uptime > 0 {
			stats.ThroughputPerMin = float64(q.totalCompleted) / uptime
		}
	}

	return stats
}

// extractErrorDetail extracts detailed error information including exact revert reasons
func extractErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	// Check for exact revert reason (new format from executor.go)
	// Format: "transaction simulation reverted: UnauthorizedSigner() - signer not in authorizedSigners mapping"
	if idx := strings.Index(errMsg, "transaction simulation reverted: "); idx != -1 {
		return errMsg[idx+len("transaction simulation reverted: "):]
	}

	// Check for diagnostic causes (when exact error couldn't be decoded)
	// Format: "transaction simulation failed: ... (Diagnostics: ...)"
	if strings.Contains(errMsg, "Diagnostics:") {
		if idx := strings.Index(errMsg, "transaction simulation failed: "); idx != -1 {
			return errMsg[idx+len("transaction simulation failed: "):]
		}
	}

	// For other errors, return as-is
	return errMsg
}
