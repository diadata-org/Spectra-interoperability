package processor

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

// EventWorkerPoolConfig configures the event processing worker pool
type EventWorkerPoolConfig struct {
	WorkerCount       int           `json:"worker_count"`       // Number of workers for event processing
	EventQueueSize    int           `json:"event_queue_size"`   // Size of event queue buffer
	ProcessingTimeout time.Duration `json:"processing_timeout"` // Timeout per event processing
	EnableStats       bool          `json:"enable_stats"`       // Enable statistics collection
}

// DefaultEventWorkerPoolConfig returns sensible defaults
func DefaultEventWorkerPoolConfig() *EventWorkerPoolConfig {
	return &EventWorkerPoolConfig{
		WorkerCount:       runtime.NumCPU(), // 1x CPU cores for I/O bound work
		EventQueueSize:    500,              // Buffer for 500 events
		ProcessingTimeout: 30 * time.Second, // 30s timeout per event
		EnableStats:       true,             // Enable stats by default
	}
}

// EventProcessor interface for processing individual events
type EventProcessor interface {
	ProcessEvent(ctx context.Context, event *types.EventData) error
}

// EventWorkerPool manages parallel event processing
type EventWorkerPool struct {
	config      *EventWorkerPoolConfig
	processor   EventProcessor
	eventQueue  chan *types.EventData
	workers     []*EventWorker
	
	// Control channels
	stopChan    chan struct{}
	stoppedChan chan struct{}
	wg          sync.WaitGroup
	
	// Statistics
	stats       *EventWorkerStats
}

// EventWorker processes events in parallel
type EventWorker struct {
	id          int
	pool        *EventWorkerPool
	eventChan   <-chan *types.EventData
	
	// Worker statistics
	eventsProcessed uint64
	eventsFailed    uint64
	totalTime       uint64 // nanoseconds
	lastEventTime   int64  // unix timestamp
}

// EventWorkerStats tracks event processing statistics
type EventWorkerStats struct {
	EventsReceived     uint64
	EventsProcessed    uint64
	EventsFailed       uint64
	EventsDropped      uint64
	ActiveWorkers      int32
	QueueLength        int32
	AverageProcessTime float64 // milliseconds
}

// NewEventWorkerPool creates a new event worker pool
func NewEventWorkerPool(config *EventWorkerPoolConfig, processor EventProcessor) *EventWorkerPool {
	if config == nil {
		config = DefaultEventWorkerPoolConfig()
	}
	
	pool := &EventWorkerPool{
		config:      config,
		processor:   processor,
		eventQueue:  make(chan *types.EventData, config.EventQueueSize),
		workers:     make([]*EventWorker, config.WorkerCount),
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
		stats:       &EventWorkerStats{},
	}
	
	// Create workers
	for i := 0; i < config.WorkerCount; i++ {
		pool.workers[i] = &EventWorker{
			id:        i,
			pool:      pool,
			eventChan: pool.eventQueue,
		}
	}
	
	return pool
}

// Start begins processing events with workers
func (ewp *EventWorkerPool) Start(ctx context.Context) error {
	logger.Infof("Starting event worker pool with %d workers", ewp.config.WorkerCount)
	
	// Start all workers
	for _, worker := range ewp.workers {
		ewp.wg.Add(1)
		go worker.start(ctx)
	}
	
	// Start statistics reporter if enabled
	if ewp.config.EnableStats {
		ewp.wg.Add(1)
		go ewp.statsReporter(ctx)
	}
	
	return nil
}

// Stop gracefully stops all workers
func (ewp *EventWorkerPool) Stop() error {
	logger.Info("Stopping event worker pool...")
	
	close(ewp.stopChan)
	ewp.wg.Wait()
	close(ewp.stoppedChan)
	
	logger.Info("Event worker pool stopped")
	return nil
}

// SubmitEvent submits an event for processing
func (ewp *EventWorkerPool) SubmitEvent(event *types.EventData) error {
	atomic.AddUint64(&ewp.stats.EventsReceived, 1)
	
	select {
	case ewp.eventQueue <- event:
		atomic.AddInt32(&ewp.stats.QueueLength, 1)
		return nil
	default:
		// Queue is full, drop the event
		atomic.AddUint64(&ewp.stats.EventsDropped, 1)
		logger.Warnf("Event queue full, dropping event: %s", event.TxHash.Hex())
		return nil // Don't return error to avoid blocking the caller
	}
}

// GetStats returns current event worker pool statistics
func (ewp *EventWorkerPool) GetStats() *EventWorkerStats {
	// Calculate average processing time
	var totalTime uint64
	var totalEvents uint64
	
	for _, worker := range ewp.workers {
		totalTime += atomic.LoadUint64(&worker.totalTime)
		totalEvents += atomic.LoadUint64(&worker.eventsProcessed)
	}
	
	avgTime := float64(0)
	if totalEvents > 0 {
		avgTime = float64(totalTime) / float64(totalEvents) / 1e6 // Convert to milliseconds
	}
	
	return &EventWorkerStats{
		EventsReceived:     atomic.LoadUint64(&ewp.stats.EventsReceived),
		EventsProcessed:    atomic.LoadUint64(&ewp.stats.EventsProcessed),
		EventsFailed:       atomic.LoadUint64(&ewp.stats.EventsFailed),
		EventsDropped:      atomic.LoadUint64(&ewp.stats.EventsDropped),
		ActiveWorkers:      atomic.LoadInt32(&ewp.stats.ActiveWorkers),
		QueueLength:        atomic.LoadInt32(&ewp.stats.QueueLength),
		AverageProcessTime: avgTime,
	}
}

// statsReporter periodically reports statistics
func (ewp *EventWorkerPool) statsReporter(ctx context.Context) {
	defer ewp.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ewp.stopChan:
			return
		case <-ticker.C:
			stats := ewp.GetStats()
			logger.Infof("Event worker pool stats: received=%d, processed=%d, failed=%d, dropped=%d, active=%d, queue=%d, avg_time=%.2fms",
				stats.EventsReceived,
				stats.EventsProcessed,
				stats.EventsFailed,
				stats.EventsDropped,
				stats.ActiveWorkers,
				stats.QueueLength,
				stats.AverageProcessTime,
			)
		}
	}
}

// EventWorker methods

// start begins processing events
func (ew *EventWorker) start(ctx context.Context) {
	defer ew.pool.wg.Done()
	
	logger.Debugf("Event worker %d started", ew.id)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ew.pool.stopChan:
			return
		case event := <-ew.eventChan:
			ew.processEvent(ctx, event)
		}
	}
}

// processEvent processes a single event
func (ew *EventWorker) processEvent(ctx context.Context, event *types.EventData) {
	startTime := time.Now()
	
	// Update active workers count
	atomic.AddInt32(&ew.pool.stats.ActiveWorkers, 1)
	defer atomic.AddInt32(&ew.pool.stats.ActiveWorkers, -1)
	
	// Update queue length
	atomic.AddInt32(&ew.pool.stats.QueueLength, -1)
	
	// Set processing timeout
	processCtx, cancel := context.WithTimeout(ctx, ew.pool.config.ProcessingTimeout)
	defer cancel()
	
	logger.Debugf("Event worker %d processing event: %s", ew.id, event.TxHash.Hex())
	
	// Process the event
	if err := ew.pool.processor.ProcessEvent(processCtx, event); err != nil {
		logger.Errorf("Event worker %d failed to process event %s: %v", 
			ew.id, event.TxHash.Hex(), err)
		
		atomic.AddUint64(&ew.eventsFailed, 1)
		atomic.AddUint64(&ew.pool.stats.EventsFailed, 1)
	} else {
		logger.Debugf("Event worker %d completed event: %s (took %v)", 
			ew.id, event.TxHash.Hex(), time.Since(startTime))
		
		atomic.AddUint64(&ew.eventsProcessed, 1)
		atomic.AddUint64(&ew.pool.stats.EventsProcessed, 1)
	}
	
	// Update timing statistics
	processingTime := time.Since(startTime)
	atomic.AddUint64(&ew.totalTime, uint64(processingTime))
	atomic.StoreInt64(&ew.lastEventTime, time.Now().Unix())
}