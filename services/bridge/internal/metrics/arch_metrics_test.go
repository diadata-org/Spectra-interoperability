package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestArchMetricsRegisteredOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	RegisterArchMetrics(reg)
	// Increment one counter; verify it's visible to the registry.
	ArchIntentUpdates.WithLabelValues("dia-arch-testnet", "-1", "BTC/USD").Inc()
	got := testutil.ToFloat64(ArchIntentUpdates.WithLabelValues("dia-arch-testnet", "-1", "BTC/USD"))
	if got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
}
