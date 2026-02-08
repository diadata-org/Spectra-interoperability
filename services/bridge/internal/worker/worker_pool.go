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

// WorkerPool manages a pool of workers for processing update requests
type WorkerPool struct {
	routerID         string            // Router/oracle identifier for logging
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

	logger.Infof("Started worker pool with %d workers", wp.maxWorkers)
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop(ctx context.Context) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return
	}

	logger.Info("Stopping worker pool")

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
		logger.Info("All workers stopped")
	case <-ctx.Done():
		logger.Warn("Worker pool shutdown timed out")
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
				logger.Warnf("All %d workers busy for %s with %d tasks queued - consider increasing worker count",
					wp.maxWorkers, wp.routerID, queueSize)
			}

			logger.Debugf("Worker pool health: active=%d/%d, queue=%d/%d",
				activeCount, wp.maxWorkers, queueSize, queueCap)
		}
	}
}

// Submit submits a task to the worker pool
func (wp *WorkerPool) Submit(task *WorkerTask) {
	if task == nil {
		logger.Error("Cannot submit nil task to worker pool")
		return
	}

	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.running {
		logger.Warn("Worker pool not running, dropping task")
		return
	}

	select {
	case wp.taskQueue <- task:
		queueSize := len(wp.taskQueue)
		logger.Debugf("Task %s queued (queue: %d/%d)", task.ID, queueSize, cap(wp.taskQueue))
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
		logger.Errorf("CRITICAL: Task queue full (%d/%d), DROPPING task %s for symbol %s - consider increasing queue size or worker count",
			queueLen, queueCap, task.ID, symbol)
		// Record dropped task metric
		if wp.metricsCollector != nil {
			wp.metricsCollector.IncWorkerTasksDropped()
		}
	}
}

// start starts a worker
func (w *Worker) start(ctx context.Context) {
	defer w.wg.Done()

	logger.Debugf("Worker %d started", w.id)

	for {
		select {
		case <-ctx.Done():
			logger.Debugf("Worker %d stopped due to context cancellation", w.id)
			return
		case <-w.quit:
			logger.Debugf("Worker %d stopped due to quit signal", w.id)
			return
		case task := <-w.taskQueue:
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
	logger.Infof("[WORKER-%d] Starting task: %s, router=%s, symbol=%s, chain=%d, active_workers=%d",
		w.id, task.ID, routerID, symbol, chainID, atomic.LoadInt32(&w.pool.activeWorkers))

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
			logger.Debugf("Worker %d retrying task %s (attempt %d/%d)", w.id, task.ID, retry+1, maxRetries)
			time.Sleep(time.Second * time.Duration(retry))
		}

		err = task.Handler(taskCtx, task)
		if err == nil {
			break
		}

		// Don't retry on context timeout/cancellation
		if taskCtx.Err() != nil {
			logger.Warnf("[WORKER-%d] Task %s context expired, not retrying: %v", w.id, task.ID, taskCtx.Err())
			err = taskCtx.Err()
			break
		}

		logger.Errorf("Worker %d task %s failed (attempt %d/%d): %v", w.id, task.ID, retry+1, maxRetries, err)
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Errorf("[WORKER-%d] Task FAILED after %d retries: %s, router=%s, symbol=%s, chain=%d, duration=%v, error=%v",
			w.id, maxRetries, task.ID, routerID, symbol, chainID, duration, err)
		if w.metricsCollector != nil {
			w.metricsCollector.IncWorkerTasksFailed()
			w.metricsCollector.ObserveTaskProcessingDuration(duration.Seconds())
		}
	} else {
		logger.Infof("[WORKER-%d] Task COMPLETED: %s, router=%s, symbol=%s, chain=%d, duration=%v, retries=%d",
			w.id, task.ID, routerID, symbol, chainID, duration, retryCount)
		if w.metricsCollector != nil {
			w.metricsCollector.IncWorkerTasksCompleted()
			w.metricsCollector.ObserveTaskProcessingDuration(duration.Seconds())
			if retryCount > 0 {
				w.metricsCollector.AddWorkerTaskRetries(retryCount)
			}
		}
	}
}
