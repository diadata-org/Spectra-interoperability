package worker

import (
	"container/heap"
	"fmt"
	"sync"
)

// PriorityQueue implements a thread-safe priority queue for tasks
type PriorityQueue struct {
	mu      sync.Mutex
	items   priorityQueueItems
	maxSize int
}

// priorityQueueItems implements heap.Interface
type priorityQueueItems []*Task

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue(maxSize int) *PriorityQueue {
	pq := &PriorityQueue{
		items:   make(priorityQueueItems, 0),
		maxSize: maxSize,
	}
	heap.Init(&pq.items)
	return pq
}

// Push adds a task to the queue
func (pq *PriorityQueue) Push(task *Task) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) >= pq.maxSize {
		return fmt.Errorf("queue is full")
	}

	heap.Push(&pq.items, task)
	return nil
}

// Pop removes and returns the highest priority task
func (pq *PriorityQueue) Pop() *Task {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) == 0 {
		return nil
	}

	task := heap.Pop(&pq.items).(*Task)
	return task
}

// Size returns the number of items in the queue
func (pq *PriorityQueue) Size() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.items)
}

// Len returns the number of items in the queue
func (pqi priorityQueueItems) Len() int {
	return len(pqi)
}

// Less reports whether the element with index i should sort before j
func (pqi priorityQueueItems) Less(i, j int) bool {
	// Higher priority comes first
	if pqi[i].Priority != pqi[j].Priority {
		return pqi[i].Priority > pqi[j].Priority
	}
	// If priority is same, older tasks come first
	return pqi[i].CreatedAt.Before(pqi[j].CreatedAt)
}

// Swap swaps the elements with indexes i and j
func (pqi priorityQueueItems) Swap(i, j int) {
	pqi[i], pqi[j] = pqi[j], pqi[i]
}

// Push adds an element
func (pqi *priorityQueueItems) Push(x interface{}) {
	task := x.(*Task)
	*pqi = append(*pqi, task)
}

// Pop removes and returns the last element
func (pqi *priorityQueueItems) Pop() interface{} {
	old := *pqi
	n := len(old)
	task := old[n-1]
	*pqi = old[0 : n-1]
	return task
}
