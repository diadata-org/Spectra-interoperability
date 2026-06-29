package bridge

import (
	"context"
	"strconv"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
)

// StartArchPoller polls the fee vault + payer balance gauges on a ticker.
// It runs until ctx is cancelled.
func StartArchPoller(ctx context.Context, routerID string, c *ArchWriteClient, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Run one tick immediately to seed gauges before the first interval.
		pollOnce(ctx, routerID, c)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pollOnce(ctx, routerID, c)
			}
		}
	}()
}

func pollOnce(ctx context.Context, routerID string, c *ArchWriteClient) {
	// archRPCInterface includes ReadAccountInfo, so the cast always succeeds
	// for any properly constructed ArchWriteClient.
	rpc := c.rpc
	chainStr := strconv.FormatInt(c.chainID, 10)

	vaultPDA, _ := arch.FeeVaultPDA(c.feeHookProgramID)
	if info, err := rpc.ReadAccountInfo(ctx, vaultPDA); err == nil && info != nil {
		metrics.ArchFeeVaultLamports.WithLabelValues(routerID, chainStr).Set(float64(info.Lamports))
	} else if err != nil {
		logger.Warnf("arch poller: read fee vault: %v", err)
	}

	payer := c.signer.Pubkey()
	if info, err := rpc.ReadAccountInfo(ctx, payer); err == nil && info != nil {
		// routerID is empty at gauge level; per-update counters carry RouterID label.
		metrics.ArchPayerBalanceLamports.WithLabelValues(routerID, chainStr).Set(float64(info.Lamports))
	} else if err != nil {
		logger.Warnf("arch poller: read payer balance: %v", err)
	}
}
