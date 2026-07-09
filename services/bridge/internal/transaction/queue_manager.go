package transaction

import (
	"fmt"
	"sync"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

type QueueManager struct {
	queues    map[string]*Queue
	mu        sync.RWMutex
	queueSize int
	running   bool
	metrics   *metrics.Collector
}

func NewQueueManager(queueSize int, metrics *metrics.Collector) *QueueManager {
	return &QueueManager{
		queues:    make(map[string]*Queue),
		queueSize: queueSize,
		metrics:   metrics,
	}
}

func (qm *QueueManager) Start() {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	if qm.running {
		return
	}
	qm.running = true
	logger.Infof("Transaction queue manager started with queue size %d", qm.queueSize)
}

func (qm *QueueManager) Stop() {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	if !qm.running {
		return
	}
	qm.running = false

	for key, queue := range qm.queues {
		queue.Stop()
		logger.Infof("Stopped queue: %s", key)
	}

	logger.Infof("Transaction queue manager stopped")
}

func (qm *QueueManager) GetOrCreateQueue(walletAddr string, chainID int64) (*Queue, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	queueKey := fmt.Sprintf("%s-%d", walletAddr, chainID)

	if queue, exists := qm.queues[queueKey]; exists {
		return queue, nil
	}

	queue := NewQueue(queueKey, qm.queueSize, qm.metrics)
	queue.Start()
	qm.queues[queueKey] = queue

	logger.Infof("Created new transaction queue: %s", queueKey)
	return queue, nil
}

func (qm *QueueManager) GetQueueCount() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return len(qm.queues)
}

func (qm *QueueManager) GetQueueLengths() map[string]int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	lengths := make(map[string]int, len(qm.queues))
	for key, queue := range qm.queues {
		lengths[key] = queue.GetQueueLength()
	}
	return lengths
}

// GetQueueStats returns queue statistics (alias for GetQueueLengths)
func (qm *QueueManager) GetQueueStats() map[string]int {
	return qm.GetQueueLengths()
}

// GetAllQueueStats returns detailed stats for all queues as maps for API consumption
func (qm *QueueManager) GetAllQueueStats() []map[string]interface{} {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	stats := make([]map[string]interface{}, 0, len(qm.queues))
	for _, queue := range qm.queues {
		s := queue.GetStats()
		stats = append(stats, map[string]interface{}{
			"key":                s.Key,
			"pending":            s.Pending,
			"capacity":           s.Capacity,
			"running":            s.Running,
			"total_submitted":    s.TotalSubmitted,
			"total_completed":    s.TotalCompleted,
			"total_failed":       s.TotalFailed,
			"avg_exec_time":      s.AvgExecTime,
			"last_submit_time":   s.LastSubmitTime,
			"last_complete_at":   s.LastCompleteAt,
			"throughput_per_min": s.ThroughputPerMin,
			"pending_items":      s.PendingItems,
		})
	}
	return stats
}
