package transaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

func TestQueue_StartStop(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)

	queue.Start()
	if !queue.running {
		t.Error("Queue should be running after Start()")
	}

	queue.Stop()
	if queue.running {
		t.Error("Queue should not be running after Stop()")
	}
}

func TestQueue_Submit_Success(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)
	queue.Start()
	defer queue.Stop()

	ctx := context.Background()

	executor := func(ctx context.Context) (*types.Transaction, error) {
		return nil, nil
	}

	tx, err := queue.Submit(ctx, executor)
	if err != nil {
		t.Errorf("Submit should succeed, got error: %v", err)
	}
	if tx != nil {
		t.Error("Submit should return nil transaction from executor")
	}
}

func TestQueue_Submit_Error(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)
	queue.Start()
	defer queue.Stop()

	ctx := context.Background()
	expectedErr := errors.New("test error")

	executor := func(ctx context.Context) (*types.Transaction, error) {
		return nil, expectedErr
	}

	tx, err := queue.Submit(ctx, executor)
	if err == nil {
		t.Error("Submit should return error")
	}
	if tx != nil {
		t.Error("Submit should return nil transaction on error")
	}
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestQueue_Submit_NotRunning(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)

	ctx := context.Background()
	executor := func(ctx context.Context) (*types.Transaction, error) {
		return &types.Transaction{}, nil
	}

	tx, err := queue.Submit(ctx, executor)
	if err == nil {
		t.Error("Submit should fail when queue is not running")
	}
	if tx != nil {
		t.Error("Submit should return nil transaction when queue is not running")
	}
}

func TestQueue_Submit_ContextCancelled(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)
	queue.Start()
	defer queue.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	executor := func(ctx context.Context) (*types.Transaction, error) {
		time.Sleep(100 * time.Millisecond)
		return &types.Transaction{}, nil
	}

	tx, err := queue.Submit(ctx, executor)
	if err == nil {
		t.Error("Submit should fail when context is cancelled")
	}
	if tx != nil {
		t.Error("Submit should return nil transaction when context is cancelled")
	}
}

func TestQueue_Sequential_Execution(t *testing.T) {
	queue := NewQueue("test-queue", 10, nil)
	queue.Start()
	defer queue.Stop()

	ctx := context.Background()
	executionChan := make(chan int, 3)

	executor1 := func(ctx context.Context) (*types.Transaction, error) {
		time.Sleep(50 * time.Millisecond)
		executionChan <- 1
		return nil, nil
	}

	executor2 := func(ctx context.Context) (*types.Transaction, error) {
		time.Sleep(50 * time.Millisecond)
		executionChan <- 2
		return nil, nil
	}

	executor3 := func(ctx context.Context) (*types.Transaction, error) {
		time.Sleep(50 * time.Millisecond)
		executionChan <- 3
		return nil, nil
	}

	// Submit tasks without blocking - they'll execute sequentially in the background
	go func() {
		queue.Submit(ctx, executor1)
	}()
	time.Sleep(10 * time.Millisecond) // Ensure first is queued first
	go func() {
		queue.Submit(ctx, executor2)
	}()
	time.Sleep(10 * time.Millisecond) // Ensure second is queued second
	go func() {
		queue.Submit(ctx, executor3)
	}()

	// Collect execution order
	executionOrder := []int{}
	for i := 0; i < 3; i++ {
		executionOrder = append(executionOrder, <-executionChan)
	}

	// Verify sequential execution
	if len(executionOrder) != 3 {
		t.Errorf("Expected 3 executions, got %d", len(executionOrder))
	}

	for i := 0; i < 3; i++ {
		if executionOrder[i] != i+1 {
			t.Errorf("Execution order should be sequential [1,2,3], got %v", executionOrder)
			break
		}
	}
}

func TestQueue_GetQueueLength(t *testing.T) {
	queue := NewQueue("test-queue", 100, nil)

	length := queue.GetQueueLength()
	if length != 0 {
		t.Errorf("Expected queue length 0, got %d", length)
	}
}
