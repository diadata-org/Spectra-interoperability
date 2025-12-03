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
}

type queuedRequest struct {
	ctx         context.Context
	executor    ExecutorFunc
	resultCh    chan *Result
	enqueueTime time.Time
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

func (q *Queue) Submit(ctx context.Context, executor ExecutorFunc) (*types.Transaction, error) {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return nil, fmt.Errorf("transaction queue is not running")
	}
	q.mu.Unlock()

	resultCh := make(chan *Result, 1)
	req := &queuedRequest{
		ctx:         ctx,
		executor:    executor,
		resultCh:    resultCh,
		enqueueTime: time.Now(),
	}

	select {
	case q.queue <- req:
		if q.metrics != nil {
			q.metrics.SetQueueLength(q.queueKey, len(q.queue))
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout submitting transaction to queue")
	}

	select {
	case result := <-resultCh:
		return result.Tx, result.Err
	case <-ctx.Done():
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
	select {
	case <-req.ctx.Done():
		logger.Warnf("Transaction request cancelled before execution: %s", q.queueKey)
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
	tx, err := req.executor(req.ctx)
	duration := time.Since(startTime)

	if q.metrics != nil {
		q.metrics.ObserveQueueProcessingDuration(q.queueKey, duration.Seconds())
	}

	if err != nil {
		errorDetail := extractErrorDetail(err)
		logger.Errorf("Transaction execution failed for queue [%s] after %v: %s", q.queueKey, duration, errorDetail)
	} else if tx != nil {
		logger.Infof("Transaction executed successfully for queue [%s] in %v: %s", q.queueKey, duration, tx.Hash().Hex())
	} else {
		logger.Infof("Transaction executed successfully for queue [%s] in %v (no tx returned)", q.queueKey, duration)
	}

	select {
	case req.resultCh <- &Result{Tx: tx, Err: err}:
	case <-time.After(5 * time.Second):
		logger.Errorf("Timeout sending transaction result for %s", q.queueKey)
	}
}

func (q *Queue) GetQueueLength() int {
	return len(q.queue)
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
