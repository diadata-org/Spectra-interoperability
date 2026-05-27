package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// WorkerTask represents a task for the worker pool
type WorkerTask struct {
	ID      string
	Request *types.UpdateRequest
	Handler func(context.Context, *WorkerTask) error
}

// TaskInfo contains summary information about a pending or active task
type TaskInfo struct {
	ID         string
	Symbol     string
	ChainID    int64
	RouterID   string
	IntentHash string
	Timestamp  string // unix timestamp from OracleIntent
}

// WorkerPool manages a pool of workers for processing update requests
type WorkerPool struct {
	routerID         string // Router/oracle identifier for logging
	maxWorkers       int
	taskQueue        chan *WorkerTask
	workers          []*Worker
	shutdownChan     chan struct{}
	wg               sync.WaitGroup
	mu               sync.RWMutex
	running          bool
	metricsCollector *metrics.Collector
	activeWorkers    int32 // Track number of currently active workers
	taskTimeout      time.Duration
	pendingMu        sync.RWMutex
	pendingTasks     []*WorkerTask
}

// Worker represents a single worker in the pool
type Worker struct {
	id               int
	taskQueue        chan *WorkerTask
	quit             chan struct{}
	wg               *sync.WaitGroup
	metricsCollector *metrics.Collector
	pool             *WorkerPool // Reference to parent pool for metrics
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(routerID string, maxWorkers int, taskQueueSize int, taskTimeout time.Duration) *WorkerPool {
	// Use taskQueueSize if provided, otherwise fallback to maxWorkers*2
	queueSize := taskQueueSize
	if queueSize <= 0 {
		queueSize = maxWorkers * 2
	}

	if taskTimeout <= 0 {
		taskTimeout = 6 * time.Minute
	}

	logger.Infof("Creating worker pool for router %s: maxWorkers=%d, taskQueueSize=%d, taskTimeout=%v", routerID, maxWorkers, queueSize, taskTimeout)

	return &WorkerPool{
		routerID:     routerID,
		maxWorkers:   maxWorkers,
		taskQueue:    make(chan *WorkerTask, queueSize),
		shutdownChan: make(chan struct{}),
		taskTimeout:  taskTimeout,
	}
}

// SetMetricsCollector sets the metrics collector for the worker pool
func (wp *WorkerPool) SetMetricsCollector(collector *metrics.Collector) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	wp.metricsCollector = collector
	if collector != nil {
		collector.SetWorkerPoolSize(wp.maxWorkers)
	}
}

// Start starts the worker pool
func (wp *WorkerPool) Start(ctx context.Context) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return
	}

	wp.running = true
	wp.workers = make([]*Worker, wp.maxWorkers)

	for i := 0; i < wp.maxWorkers; i++ {
		worker := &Worker{
			id:               i,
			taskQueue:        wp.taskQueue,
			quit:             make(chan struct{}),
			wg:               &wp.wg,
			metricsCollector: wp.metricsCollector,
			pool:             wp,
		}
		wp.workers[i] = worker

		wp.wg.Add(1)
		go worker.start(ctx)
	}

	// Start health monitor goroutine
	go wp.healthMonitor(ctx)

	logger.Infof("[router=%s] Started worker pool with %d workers", wp.routerID, wp.maxWorkers)
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop(ctx context.Context) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return
	}

	logger.Infof("[router=%s] Stopping worker pool", wp.routerID)

	// Signal shutdown
	close(wp.shutdownChan)

	// Stop all workers
	for _, worker := range wp.workers {
		close(worker.quit)
	}

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Infof("[router=%s] All workers stopped", wp.routerID)
	case <-ctx.Done():
		logger.Warnf("[router=%s] Worker pool shutdown timed out", wp.routerID)
	}

	wp.running = false
}

// healthMonitor periodically monitors worker pool health and reports metrics
func (wp *WorkerPool) healthMonitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.shutdownChan:
			return
		case <-ticker.C:
			activeCount := atomic.LoadInt32(&wp.activeWorkers)
			queueSize := len(wp.taskQueue)
			queueCap := cap(wp.taskQueue)

			// Report metrics if collector is available
			if wp.metricsCollector != nil {
				wp.metricsCollector.SetActiveWorkers(activeCount)
				wp.metricsCollector.SetTaskQueueSize(int32(queueSize))
			}

			// Log warning if queue is getting full (>80% capacity)
			if queueCap > 0 && float64(queueSize)/float64(queueCap) > 0.8 {
				logger.Warnf("Worker pool queue nearing capacity for %s: %d/%d (%.1f%%), active workers: %d/%d",
					wp.routerID, queueSize, queueCap, float64(queueSize)/float64(queueCap)*100, activeCount, wp.maxWorkers)
			}

			// Log warning if all workers are busy and queue has items
			if int(activeCount) >= wp.maxWorkers && queueSize > 0 {
				logger.Warnf("All %d workers busy for %s with %d tasks queued  and %d active workers - consider increasing worker count",
					wp.maxWorkers, wp.routerID, queueSize, activeCount)
			}

			logger.Debugf("[router=%s] Worker pool health: active=%d/%d, queue=%d/%d",
				wp.routerID, activeCount, wp.maxWorkers, queueSize, queueCap)
		}
	}
}

// Submit submits a task to the worker pool
func (wp *WorkerPool) Submit(task *WorkerTask) {
	if task == nil {
		logger.Errorf("[router=%s] Cannot submit nil task to worker pool", wp.routerID)
		return
	}

	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.running {
		logger.Warnf("[router=%s] Worker pool not running, dropping task", wp.routerID)
		return
	}

	select {
	case wp.taskQueue <- task:
		wp.pendingMu.Lock()
		wp.pendingTasks = append(wp.pendingTasks, task)
		wp.pendingMu.Unlock()
		queueSize := len(wp.taskQueue)
		logger.Debugf("[router=%s] Task %s queued (queue: %d/%d)", wp.routerID, task.ID, queueSize, cap(wp.taskQueue))
		// Update queue size metric
		if wp.metricsCollector != nil {
			wp.metricsCollector.SetTaskQueueSize(int32(queueSize))
		}
	default:
		queueLen := len(wp.taskQueue)
		queueCap := cap(wp.taskQueue)
		symbol := "unknown"
		if task.Request != nil && task.Request.Intent != nil {
			symbol = task.Request.Intent.Symbol
		}
		logger.Errorf("[router=%s] CRITICAL: Task queue full (%d/%d), DROPPING task %s for symbol %s - consider increasing queue size or worker count",
			wp.routerID, queueLen, queueCap, task.ID, symbol)
		// Record dropped task metric
		if wp.metricsCollector != nil {
			wp.metricsCollector.IncWorkerTasksDropped()
		}
	}
}

// start starts a worker
func (w *Worker) start(ctx context.Context) {
	defer w.wg.Done()

	logger.Debugf("[router=%s][WORKER-%d] started", w.pool.routerID, w.id)

	for {
		select {
		case <-ctx.Done():
			logger.Debugf("[router=%s][WORKER-%d] stopped due to context cancellation", w.pool.routerID, w.id)
			return
		case <-w.quit:
			logger.Debugf("[router=%s][WORKER-%d] stopped due to quit signal", w.pool.routerID, w.id)
			return
		case task := <-w.taskQueue:
			logger.Debugf("[router=%s][WORKER-%d] picked up task %s", w.pool.routerID, w.id, task.ID)
			w.pool.removePending(task.ID)
			w.processTask(ctx, task)
		}
	}
}

// WorkerPoolStats contains worker pool statistics
type WorkerPoolStats struct {
	ActiveTasks   int32
	PendingTasks  int
	MaxWorkers    int
	TotalCapacity int
}

// GetStats returns current worker pool statistics
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	return WorkerPoolStats{
		ActiveTasks:   atomic.LoadInt32(&wp.activeWorkers),
		PendingTasks:  len(wp.taskQueue),
		MaxWorkers:    wp.maxWorkers,
		TotalCapacity: cap(wp.taskQueue),
	}
}

// removePending removes a task from the pending tracking list by ID
func (wp *WorkerPool) removePending(taskID string) {
	wp.pendingMu.Lock()
	defer wp.pendingMu.Unlock()

	for i, t := range wp.pendingTasks {
		if t.ID == taskID {
			wp.pendingTasks = append(wp.pendingTasks[:i], wp.pendingTasks[i+1:]...)
			logger.Debugf("[router=%s] Removed task %s from pending list (remaining: %d)", wp.routerID, taskID, len(wp.pendingTasks))
			return
		}
	}
	logger.Warnf("[router=%s] Task %s not found in pending list when trying to remove", wp.routerID, taskID)
}

func (wp *WorkerPool) ListPendingTasks() []TaskInfo {
	wp.pendingMu.RLock()
	defer wp.pendingMu.RUnlock()

	result := make([]TaskInfo, 0, len(wp.pendingTasks))
	for _, task := range wp.pendingTasks {
		info := TaskInfo{ID: task.ID}
		if task.Request != nil {
			info.RouterID = task.Request.RouterID
			info.IntentHash = task.Request.IntentHash.Hex()
			if task.Request.Intent != nil {
				info.Symbol = task.Request.Intent.Symbol
				if task.Request.Intent.Timestamp != nil {
					info.Timestamp = task.Request.Intent.Timestamp.String()
				}
			}
			if task.Request.DestinationChain != nil {
				info.ChainID = task.Request.DestinationChain.ChainID
			}
		}
		result = append(result, info)
	}

	queueLen := len(wp.taskQueue)
	if len(result) != queueLen {
		logger.Warnf("[router=%s] Pending tasks list out of sync: pendingTasks=%d, taskQueue=%d", wp.routerID, len(result), queueLen)
	}

	return result
}

// processTask processes a single task
func (w *Worker) processTask(ctx context.Context, task *WorkerTask) {
	// Track active workers
	atomic.AddInt32(&w.pool.activeWorkers, 1)
	defer atomic.AddInt32(&w.pool.activeWorkers, -1)

	startTime := time.Now()

	// Use Info level for task start to ensure visibility in production
	symbol := "unknown"
	chainID := int64(0)
	routerID := "unknown"
	if task.Request != nil {
		routerID = task.Request.RouterID
		if task.Request.Intent != nil && task.Request.Intent.Symbol != "" {
			symbol = task.Request.Intent.Symbol
		}
		if task.Request.DestinationChain != nil {
			chainID = task.Request.DestinationChain.ChainID
		}
	}
	logger.Infof("[router=%s][WORKER-%d] Starting task: %s, symbol=%s, chain=%d, task_router=%s, active_workers=%d",
		w.pool.routerID, w.id, task.ID, symbol, chainID, routerID, atomic.LoadInt32(&w.pool.activeWorkers))

	// Create timeout context to prevent workers from blocking forever
	taskCtx, cancel := context.WithTimeout(ctx, w.pool.taskTimeout)
	defer cancel()

	// Process the task with retry logic
	var err error
	maxRetries := 3
	retryCount := 0
	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			retryCount++
			logger.Debugf("[router=%s][WORKER-%d] retrying task %s (attempt %d/%d)", w.pool.routerID, w.id, task.ID, retry+1, maxRetries)
			time.Sleep(time.Second * time.Duration(retry))
		}

		err = task.Handler(taskCtx, task)
		if err == nil {
			break
		}

		// Don't retry on context timeout/cancellation
		if taskCtx.Err() != nil {
			logger.Warnf("[router=%s][WORKER-%d] Task %s context expired, not retrying: %v", w.pool.routerID, w.id, task.ID, taskCtx.Err())
			err = taskCtx.Err()
			break
		}

		logger.Errorf("[router=%s][WORKER-%d] Task %s failed (attempt %d/%d): %v", w.pool.routerID, w.id, task.ID, retry+1, maxRetries, err)
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Errorf("[router=%s][WORKER-%d] Task FAILED after %d retries: %s, symbol=%s, chain=%d, task_router=%s, duration=%v, error=%v",
			w.pool.routerID, w.id, maxRetries, task.ID, symbol, chainID, routerID, duration, err)
		if w.metricsCollector != nil {
			w.metricsCollector.IncWorkerTasksFailed()
			w.metricsCollector.ObserveTaskProcessingDuration(duration.Seconds())
		}
	} else {
		logger.Infof("[router=%s][WORKER-%d] Task COMPLETED: %s, symbol=%s, chain=%d, task_router=%s, duration=%v, retries=%d",
			w.pool.routerID, w.id, task.ID, symbol, chainID, routerID, duration, retryCount)
		if w.metricsCollector != nil {
			w.metricsCollector.IncWorkerTasksCompleted()
			w.metricsCollector.ObserveTaskProcessingDuration(duration.Seconds())
			if retryCount > 0 {
				w.metricsCollector.AddWorkerTaskRetries(retryCount)
			}
		}
	}
}
