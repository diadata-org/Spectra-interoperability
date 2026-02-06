package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// initializeChainStats initializes chain statistics
func (b *Bridge) initializeChainStats() {
	// Check if configService is available
	if b.configService == nil {
		logger.Warnf("configService is nil, skipping chain stats initialization")
		return
	}

	infra := b.configService.GetInfrastructure()
	if infra == nil {
		logger.Warnf("Infrastructure config is nil, skipping chain stats initialization")
		return
	}

	// Source chain stats
	sourceConfig := infra.Source
	b.stats.ChainStats[sourceConfig.ChainID] = &bridgetypes.ChainStatus{
		ChainID:   sourceConfig.ChainID,
		Name:      sourceConfig.Name,
		Connected: true,
	}

	// Destination chain stats
	for _, destClient := range b.writeClients {
		b.stats.ChainStats[destClient.chainConfig.ChainID] = &bridgetypes.ChainStatus{
			ChainID:   destClient.chainConfig.ChainID,
			Name:      destClient.chainConfig.Name,
			Connected: true,
		}
	}
}

// healthCheck performs periodic health checks
func (b *Bridge) healthCheck(ctx context.Context) {
	// Use HealthCheck interval from config
	ticker := time.NewTicker(b.configService.GetInfrastructure().HealthCheck.CheckInterval.Duration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownChan:
			return
		case <-ticker.C:
			b.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck performs health checks on all chains
func (b *Bridge) performHealthCheck(ctx context.Context) {
	// Log worker pool status for debugging
	if b.workerPool != nil {
		workerStats := b.workerPool.GetStats()
		queueSize := 0
		if b.eventSource != nil {
			queueSize = b.eventSource.GetQueueSize()
		}
		logger.Infof("[HEALTH] Worker pool: active=%d/%d, pending=%d/%d, update_queue=%d",
			workerStats.ActiveTasks, workerStats.MaxWorkers,
			workerStats.PendingTasks, workerStats.TotalCapacity,
			queueSize)
	}

	// Check source chain
	sourceConfig := b.configService.GetInfrastructure().Source
	if err := b.checkChainHealth(ctx, b.readClient, sourceConfig.ChainID); err != nil {
		logger.Errorf("Source chain health check failed: %v", err)
	}

	// Check destination chains
	for _, destClient := range b.writeClients {
		if err := b.checkChainHealth(ctx, destClient.client, destClient.chainConfig.ChainID); err != nil {
			logger.Errorf("Destination chain %d health check failed: %v", destClient.chainConfig.ChainID, err)
		}
	}
}

// checkChainHealth checks the health of a single chain
func (b *Bridge) checkChainHealth(ctx context.Context, client rpc.EthClient, chainID int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	chainStats := b.stats.ChainStats[chainID]
	if chainStats == nil {
		return fmt.Errorf("chain stats not found for chain %d", chainID)
	}

	// Get latest block
	latestBlock, err := client.BlockNumber(ctx)
	if err != nil {
		chainStats.Connected = false
		chainStats.LastError = err.Error()
		return err
	}

	chainStats.Connected = true
	chainStats.LatestBlock = latestBlock
	chainStats.LastHealthCheck = time.Now()
	chainStats.LastError = ""

	return nil
}
