package transaction

import (
	"testing"
)

func TestQueueManager_StartStop(t *testing.T) {
	qm := NewQueueManager(10, nil)

	qm.Start()
	if !qm.running {
		t.Error("QueueManager should be running after Start()")
	}

	qm.Stop()
	if qm.running {
		t.Error("QueueManager should not be running after Stop()")
	}
}

func TestQueueManager_GetOrCreateQueue(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	walletAddr := "0x1234567890123456789012345678901234567890"
	chainID := int64(1)

	queue1, err := qm.GetOrCreateQueue(walletAddr, chainID)
	if err != nil {
		t.Errorf("GetOrCreateQueue should succeed, got error: %v", err)
	}
	if queue1 == nil {
		t.Error("GetOrCreateQueue should return non-nil queue")
	}

	queue2, err := qm.GetOrCreateQueue(walletAddr, chainID)
	if err != nil {
		t.Errorf("GetOrCreateQueue should succeed on second call, got error: %v", err)
	}
	if queue2 != queue1 {
		t.Error("GetOrCreateQueue should return the same queue for same wallet+chain")
	}
}

func TestQueueManager_DifferentQueues_ForDifferentWallets(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	wallet1 := "0x1111111111111111111111111111111111111111"
	wallet2 := "0x2222222222222222222222222222222222222222"
	chainID := int64(1)

	queue1, err := qm.GetOrCreateQueue(wallet1, chainID)
	if err != nil {
		t.Errorf("GetOrCreateQueue failed: %v", err)
	}

	queue2, err := qm.GetOrCreateQueue(wallet2, chainID)
	if err != nil {
		t.Errorf("GetOrCreateQueue failed: %v", err)
	}

	if queue1 == queue2 {
		t.Error("Different wallets should have different queues")
	}
}

func TestQueueManager_DifferentQueues_ForDifferentChains(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	walletAddr := "0x1234567890123456789012345678901234567890"
	chain1 := int64(1)
	chain2 := int64(2)

	queue1, err := qm.GetOrCreateQueue(walletAddr, chain1)
	if err != nil {
		t.Errorf("GetOrCreateQueue failed: %v", err)
	}

	queue2, err := qm.GetOrCreateQueue(walletAddr, chain2)
	if err != nil {
		t.Errorf("GetOrCreateQueue failed: %v", err)
	}

	if queue1 == queue2 {
		t.Error("Different chains should have different queues")
	}
}

func TestQueueManager_GetOrCreateQueue_NotRunning(t *testing.T) {
	qm := NewQueueManager(10, nil)

	walletAddr := "0x1234567890123456789012345678901234567890"
	chainID := int64(1)

	queue, err := qm.GetOrCreateQueue(walletAddr, chainID)
	if err == nil {
		t.Error("GetOrCreateQueue should fail when QueueManager is not running")
	}
	if queue != nil {
		t.Error("GetOrCreateQueue should return nil queue when not running")
	}
}

func TestQueueManager_GetQueueStats(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	wallet1 := "0x1111111111111111111111111111111111111111"
	wallet2 := "0x2222222222222222222222222222222222222222"
	chain1 := int64(1)
	chain2 := int64(2)

	qm.GetOrCreateQueue(wallet1, chain1)
	qm.GetOrCreateQueue(wallet2, chain1)
	qm.GetOrCreateQueue(wallet1, chain2)

	stats := qm.GetQueueStats()

	expectedKeys := 3
	if len(stats) != expectedKeys {
		t.Errorf("Expected %d queue stats entries, got %d", expectedKeys, len(stats))
	}

	key1 := "0x1111111111111111111111111111111111111111-1"
	key2 := "0x2222222222222222222222222222222222222222-1"
	key3 := "0x1111111111111111111111111111111111111111-2"

	if _, exists := stats[key1]; !exists {
		t.Errorf("Expected stats for key %s", key1)
	}
	if _, exists := stats[key2]; !exists {
		t.Errorf("Expected stats for key %s", key2)
	}
	if _, exists := stats[key3]; !exists {
		t.Errorf("Expected stats for key %s", key3)
	}
}

func TestQueueManager_Stop_CleansUpQueues(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()

	walletAddr := "0x1234567890123456789012345678901234567890"
	chainID := int64(1)

	qm.GetOrCreateQueue(walletAddr, chainID)

	if len(qm.queues) != 1 {
		t.Errorf("Expected 1 queue before stop, got %d", len(qm.queues))
	}

	qm.Stop()

	if len(qm.queues) != 0 {
		t.Errorf("Expected 0 queues after stop, got %d", len(qm.queues))
	}
}

func TestQueueManager_ConcurrentAccess(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	walletAddr := "0x1234567890123456789012345678901234567890"
	chainID := int64(1)

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, err := qm.GetOrCreateQueue(walletAddr, chainID)
			if err != nil {
				t.Errorf("GetOrCreateQueue failed: %v", err)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if len(qm.queues) != 1 {
		t.Errorf("Expected 1 queue despite concurrent access, got %d", len(qm.queues))
	}
}
