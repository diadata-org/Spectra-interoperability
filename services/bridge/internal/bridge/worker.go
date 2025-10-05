package bridge

import (
	"context"
	"sync"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
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
	maxWorkers   int
	taskQueue    chan *WorkerTask
	workers      []*Worker
	shutdownChan chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
	running      bool
}

// Worker represents a single worker in the pool
type Worker struct {
	id        int
	taskQueue chan *WorkerTask
	quit      chan struct{}
	wg        *sync.WaitGroup
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(maxWorkers int) *WorkerPool {
	return &WorkerPool{
		maxWorkers:   maxWorkers,
		taskQueue:    make(chan *WorkerTask, maxWorkers*2),
		shutdownChan: make(chan struct{}),
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
			id:        i,
			taskQueue: wp.taskQueue,
			quit:      make(chan struct{}),
			wg:        &wp.wg,
		}
		wp.workers[i] = worker

		wp.wg.Add(1)
		go worker.start(ctx)
	}

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

// Submit submits a task to the worker pool
func (wp *WorkerPool) Submit(task *WorkerTask) {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.running {
		logger.Warn("Worker pool not running, dropping task")
		return
	}

	select {
	case wp.taskQueue <- task:
		logger.Debugf("Task %s queued (queue: %d/%d)", task.ID, len(wp.taskQueue), cap(wp.taskQueue))
	default:
		queueLen := len(wp.taskQueue)
		queueCap := cap(wp.taskQueue)
		logger.Errorf("CRITICAL: Task queue full (%d/%d), DROPPING task %s for symbol %s - consider increasing queue size or worker count",
			queueLen, queueCap, task.ID, task.Request.Intent.Symbol)
		// TODO: Add metric for dropped tasks
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

// processTask processes a single task
func (w *Worker) processTask(ctx context.Context, task *WorkerTask) {
	startTime := time.Now()

	logger.Debugf("Worker %d processing task %s", w.id, task.ID)

	// Process the task with retry logic
	var err error
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			logger.Debugf("Worker %d retrying task %s (attempt %d/%d)", w.id, task.ID, retry+1, maxRetries)
			time.Sleep(time.Second * time.Duration(retry))
		}

		err = task.Handler(ctx, task)
		if err == nil {
			break
		}

		logger.Errorf("Worker %d task %s failed (attempt %d/%d): %v", w.id, task.ID, retry+1, maxRetries, err)
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Errorf("Worker %d task %s failed after %d retries: %v", w.id, task.ID, maxRetries, err)
	} else {
		logger.Debugf("Worker %d completed task %s in %v", w.id, task.ID, duration)
	}
}
