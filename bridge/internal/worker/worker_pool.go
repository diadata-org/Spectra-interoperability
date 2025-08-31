package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/contracts"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// WorkerPool manages concurrent transaction processing
type WorkerPool struct {
	config           *config.WorkerPoolConfig
	db               *database.DB
	contractClients  map[int64]map[string]contracts.ContractClient // chainID -> contractAddr -> client
	workers          []*Worker
	taskQueue        chan *Task
	priorityQueue    *PriorityQueue
	metrics          *metrics.Collector
	
	mu               sync.RWMutex
	stats            *WorkerStats
	rateLimiters     map[string]*RateLimiter // per chain+contract rate limiting
	
	stopChan         chan struct{}
	stoppedChan      chan struct{}
}

// Worker represents a single worker in the pool
type Worker struct {
	id               int
	pool             *WorkerPool
	taskChan         <-chan *Task
	
	mu               sync.Mutex
	currentTask      *Task
	lastTaskTime     time.Time
}

// Task represents a work item
type Task struct {
	UpdateRequest    *types.UpdateRequest
	RetryCount       int
	MaxRetries       int
	Priority         int
	CreatedAt        time.Time
}

// WorkerStats tracks worker pool statistics
type WorkerStats struct {
	TasksReceived    uint64
	TasksProcessed   uint64
	TasksSucceeded   uint64
	TasksFailed      uint64
	TasksRetried     uint64
	ActiveWorkers    int32
	QueueSize        int32
	TotalGasUsed     uint64
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(
	cfg *config.WorkerPoolConfig,
	db *database.DB,
	contractClients map[int64]map[string]contracts.ContractClient,
	metricsCollector *metrics.Collector,
) *WorkerPool {
	pool := &WorkerPool{
		config:          cfg,
		db:              db,
		contractClients: contractClients,
		taskQueue:       make(chan *Task, cfg.TaskQueueSize),
		priorityQueue:   NewPriorityQueue(cfg.TaskQueueSize),
		metrics:         metricsCollector,
		stats:           &WorkerStats{},
		rateLimiters:    make(map[string]*RateLimiter),
		stopChan:        make(chan struct{}),
		stoppedChan:     make(chan struct{}),
	}

	// Create workers
	pool.workers = make([]*Worker, cfg.MaxWorkers)
	for i := 0; i < cfg.MaxWorkers; i++ {
		worker := &Worker{
			id:       i,
			pool:     pool,
			taskChan: pool.taskQueue,
		}
		pool.workers[i] = worker
	}

	// Create rate limiters for each chain+contract combination
	pool.initializeRateLimiters()

	return pool
}

// Start begins processing tasks
func (wp *WorkerPool) Start(ctx context.Context) error {
	logger.Info("Starting worker pool with %d workers", wp.config.MaxWorkers)

	// Start priority queue processor
	go wp.priorityQueueProcessor(ctx)

	// Start all workers
	for _, worker := range wp.workers {
		go worker.start(ctx)
	}

	// Start stats reporter
	go wp.statsReporter(ctx)

	return nil
}

// Stop gracefully stops the worker pool
func (wp *WorkerPool) Stop() error {
	logger.Info("Stopping worker pool")
	
	close(wp.stopChan)
	
	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		// Wait for all workers to stop
		for _, worker := range wp.workers {
			worker.waitForCompletion()
		}
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All workers stopped")
	case <-time.After(30 * time.Second):
		logger.Warn("Worker pool stop timeout")
	}

	close(wp.stoppedChan)
	return nil
}

// Submit submits a new update request to the pool
func (wp *WorkerPool) Submit(request *types.UpdateRequest) error {
	atomic.AddUint64(&wp.stats.TasksReceived, 1)

	task := &Task{
		UpdateRequest: request,
		RetryCount:    0,
		MaxRetries:    wp.config.MaxRetries,
		Priority:      request.Priority,
		CreatedAt:     time.Now(),
	}

	// Add to priority queue
	if err := wp.priorityQueue.Push(task); err != nil {
		return fmt.Errorf("queue full: %w", err)
	}

	atomic.AddInt32(&wp.stats.QueueSize, 1)
	return nil
}

// priorityQueueProcessor moves tasks from priority queue to work queue
func (wp *WorkerPool) priorityQueueProcessor(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.stopChan:
			return
		case <-ticker.C:
			// Move tasks from priority queue to work queue
			for len(wp.taskQueue) < cap(wp.taskQueue) {
				task := wp.priorityQueue.Pop()
				if task == nil {
					break
				}

				select {
				case wp.taskQueue <- task:
					atomic.AddInt32(&wp.stats.QueueSize, -1)
				default:
					// Work queue full, put back in priority queue
					wp.priorityQueue.Push(task)
					break
				}
			}
		}
	}
}

// Worker methods

func (w *Worker) start(ctx context.Context) {
	logger.Debugf("Worker %d started", w.id)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.pool.stopChan:
			return
		case task := <-w.taskChan:
			w.processTask(ctx, task)
		}
	}
}

func (w *Worker) processTask(ctx context.Context, task *Task) {
	atomic.AddInt32(&w.pool.stats.ActiveWorkers, 1)
	defer atomic.AddInt32(&w.pool.stats.ActiveWorkers, -1)

	w.mu.Lock()
	w.currentTask = task
	w.lastTaskTime = time.Now()
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.currentTask = nil
		w.mu.Unlock()
	}()

	// Apply rate limiting
	rateLimitKey := fmt.Sprintf("%d-%s", 
		task.UpdateRequest.DestinationChain.ChainID,
		task.UpdateRequest.Contract.Address)
	
	if rateLimiter, exists := w.pool.rateLimiters[rateLimitKey]; exists {
		if err := rateLimiter.Wait(ctx); err != nil {
			logger.Warnf("Rate limiter error: %v", err)
			return
		}
	}

	// Create task context with timeout
	taskCtx, cancel := context.WithTimeout(ctx, w.pool.config.TaskTimeout)
	defer cancel()

	// Log transaction attempt
	txLog := &database.TransactionLog{
		IntentHash:           task.UpdateRequest.IntentHash.Hex(),
		DestinationChainID:   task.UpdateRequest.DestinationChain.ChainID,
		DestinationChainName: task.UpdateRequest.DestinationChain.Name,
		ContractAddress:      task.UpdateRequest.Contract.Address,
		ContractName:         task.UpdateRequest.Contract.Name,
		ContractType:         task.UpdateRequest.Contract.Type,
		Status:               "pending",
		FromAddress:          "", // Set by contract client
		ToAddress:            task.UpdateRequest.Contract.Address,
		Symbol:               task.UpdateRequest.Intent.Symbol,
		Price:                task.UpdateRequest.Intent.Price.String(),
		RetryCount:           task.RetryCount,
		MaxRetries:           task.MaxRetries,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if err := w.pool.db.LogTransaction(txLog); err != nil {
		logger.Errorf("Failed to log transaction: %v", err)
	}

	// Execute update
	result := w.executeUpdate(taskCtx, task.UpdateRequest)

	// Update transaction log
	if result.Error != nil {
		logger.Errorf("Task failed for %s on %s: %v", 
			task.UpdateRequest.Intent.Symbol,
			task.UpdateRequest.Contract.Name,
			result.Error)

		txLog.Status = "failed"
		errMsg := result.Error.Error()
		txLog.ErrorMessage = &errMsg
		
		// Check if error is due to insufficient balance
		if isInsufficientBalanceError(result.Error) {
			if w.pool.metrics != nil {
				w.pool.metrics.IncInsufficientBalance()
			}
			logger.Errorf("Transaction failed due to insufficient balance: %v", result.Error)
		}
		
		// Retry if applicable
		if task.RetryCount < task.MaxRetries && isRetryableError(result.Error) {
			task.RetryCount++
			atomic.AddUint64(&w.pool.stats.TasksRetried, 1)
			
			// Re-queue with exponential backoff
			time.Sleep(time.Duration(task.RetryCount) * w.pool.config.RetryDelay)
			w.pool.Submit(task.UpdateRequest)
			
			logger.Infof("Retrying task (attempt %d/%d)", task.RetryCount+1, task.MaxRetries)
		} else {
			atomic.AddUint64(&w.pool.stats.TasksFailed, 1)
			
			// Update metrics for failed transactions
			if w.pool.metrics != nil {
				w.pool.metrics.IncTransactionsFailed()
			}
		}
	} else {
		logger.Infof("Task completed successfully for %s on %s, tx: %s",
			task.UpdateRequest.Intent.Symbol,
			task.UpdateRequest.Contract.Name,
			result.TxHash)

		txLog.Status = "confirmed"
		txLog.TransactionHash = &result.TxHash
		txLog.GasUsed = &result.GasUsed
		gasPrice := result.GasPrice.String()
		txLog.GasPrice = &gasPrice
		
		atomic.AddUint64(&w.pool.stats.TasksSucceeded, 1)
		atomic.AddUint64(&w.pool.stats.TotalGasUsed, result.GasUsed)
		
		// Update metrics
		if w.pool.metrics != nil {
			w.pool.metrics.IncTransactionsConfirmed()
			w.pool.metrics.AddTransactionGasUsed(result.GasUsed)
			w.pool.metrics.ObserveTransactionDuration(result.Duration.Seconds())
		}

		// Update contract symbol tracking
		w.pool.db.UpdateContractSymbol(
			task.UpdateRequest.DestinationChain.ChainID,
			task.UpdateRequest.Contract.Address,
			task.UpdateRequest.Intent.Symbol,
			task.UpdateRequest.IntentHash.Hex(),
			task.UpdateRequest.Intent.Price.String(),
		)
	}

	// Update transaction status
	if txLog.ID > 0 {
		w.pool.db.UpdateTransactionStatus(
			txLog.ID,
			txLog.Status,
			txLog.TransactionHash,
			txLog.GasUsed,
			txLog.GasPrice,
		)
	}

	atomic.AddUint64(&w.pool.stats.TasksProcessed, 1)
}

func (w *Worker) executeUpdate(ctx context.Context, request *types.UpdateRequest) *types.UpdateResult {
	// Get contract client
	chainClients, exists := w.pool.contractClients[request.DestinationChain.ChainID]
	if !exists {
		return &types.UpdateResult{
			Error: fmt.Errorf("no clients for chain %d", request.DestinationChain.ChainID),
		}
	}

	client, exists := chainClients[request.Contract.Address]
	if !exists {
		return &types.UpdateResult{
			Error: fmt.Errorf("no client for contract %s", request.Contract.Address),
		}
	}

	// Execute the update
	return client.UpdateOracle(ctx, request)
}

func (w *Worker) waitForCompletion() {
	// Wait for current task to complete
	for {
		w.mu.Lock()
		currentTask := w.currentTask
		w.mu.Unlock()

		if currentTask == nil {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// Helper methods

func (wp *WorkerPool) initializeRateLimiters() {
	// This would be populated from configuration
	// For now, create default rate limiters
}

func (wp *WorkerPool) statsReporter(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.stopChan:
			return
		case <-ticker.C:
			wp.logStats()
		}
	}
}

func (wp *WorkerPool) logStats() {
	received := atomic.LoadUint64(&wp.stats.TasksReceived)
	processed := atomic.LoadUint64(&wp.stats.TasksProcessed)
	succeeded := atomic.LoadUint64(&wp.stats.TasksSucceeded)
	failed := atomic.LoadUint64(&wp.stats.TasksFailed)
	retried := atomic.LoadUint64(&wp.stats.TasksRetried)
	active := atomic.LoadInt32(&wp.stats.ActiveWorkers)
	queueSize := atomic.LoadInt32(&wp.stats.QueueSize)
	gasUsed := atomic.LoadUint64(&wp.stats.TotalGasUsed)

	logger.Infof("Worker pool stats - Received: %d, Processed: %d, Succeeded: %d, Failed: %d, Retried: %d, Active: %d, Queue: %d, Gas: %d",
		received, processed, succeeded, failed, retried, active, queueSize, gasUsed)
}

// GetStats returns current worker pool statistics
func (wp *WorkerPool) GetStats() types.WorkerStats {
	return types.WorkerStats{
		TasksReceived:  atomic.LoadUint64(&wp.stats.TasksReceived),
		TasksProcessed: atomic.LoadUint64(&wp.stats.TasksProcessed),
		TasksSucceeded: atomic.LoadUint64(&wp.stats.TasksSucceeded),
		TasksFailed:    atomic.LoadUint64(&wp.stats.TasksFailed),
		TasksRetried:   atomic.LoadUint64(&wp.stats.TasksRetried),
		ActiveWorkers:  atomic.LoadInt32(&wp.stats.ActiveWorkers),
		QueueSize:      atomic.LoadInt32(&wp.stats.QueueSize),
		TotalGasUsed:   atomic.LoadUint64(&wp.stats.TotalGasUsed),
	}
}

func isRetryableError(err error) bool {
	// Check if error is retryable
	// - Network errors
	// - Timeout errors
	// - Nonce errors
	// - Gas price errors
	
	errStr := err.Error()
	
	// Don't retry on insufficient balance errors
	if isInsufficientBalanceError(err) {
		return false
	}
	
	retryableErrors := []string{
		"connection refused",
		"timeout",
		"nonce too low",
		"replacement transaction underpriced",
	}

	for _, retryable := range retryableErrors {
		if contains(errStr, retryable) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func isInsufficientBalanceError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "insufficient balance") || 
		strings.Contains(errStr, "insufficient funds")
}