package transaction

import (
	"fmt"
	"sync"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

type QueueManager struct {
	queues    map[string]*Queue
	mu        sync.RWMutex
	queueSize int
	running   bool
}

func NewQueueManager(queueSize int) *QueueManager {
	if queueSize <= 0 {
		queueSize = 100
	}
	return &QueueManager{
		queues:    make(map[string]*Queue),
		queueSize: queueSize,
	}
}

func (qm *QueueManager) Start() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if qm.running {
		return
	}

	qm.running = true
	logger.Info("Transaction queue manager started")
}

func (qm *QueueManager) Stop() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if !qm.running {
		return
	}

	logger.Info("Stopping transaction queue manager")

	for key, queue := range qm.queues {
		queue.Stop()
		logger.Infof("Stopped queue: %s", key)
	}

	qm.queues = make(map[string]*Queue)
	qm.running = false

	logger.Info("Transaction queue manager stopped")
}

func (qm *QueueManager) GetOrCreateQueue(walletAddress string, chainID int64) (*Queue, error) {
	queueKey := fmt.Sprintf("%s-%d", walletAddress, chainID)

	qm.mu.RLock()
	if queue, exists := qm.queues[queueKey]; exists {
		qm.mu.RUnlock()
		return queue, nil
	}
	qm.mu.RUnlock()

	qm.mu.Lock()
	defer qm.mu.Unlock()

	if queue, exists := qm.queues[queueKey]; exists {
		return queue, nil
	}

	if !qm.running {
		return nil, fmt.Errorf("queue manager is not running")
	}

	queue := NewQueue(queueKey, qm.queueSize)
	queue.Start()
	qm.queues[queueKey] = queue

	logger.Infof("Created new transaction queue: %s", queueKey)

	return queue, nil
}

func (qm *QueueManager) GetQueueStats() map[string]int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	stats := make(map[string]int)
	for key, queue := range qm.queues {
		stats[key] = queue.GetQueueLength()
	}
	return stats
}
