package archtest

import (
	"context"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// readMock implements bridge.ArchRPCInterface and always returns 123456 lamports.
type readMock struct{}

func (readMock) GetBestBlockHash(_ context.Context) ([32]byte, error) { return [32]byte{}, nil }
func (readMock) SendTransaction(_ context.Context, _ []byte) (string, error) {
	return "", nil
}
func (readMock) GetProcessedTransaction(_ context.Context, _ string) (*arch.ProcessedTx, error) {
	return nil, nil
}
func (readMock) ReadAccountInfo(_ context.Context, _ arch.Pubkey) (*arch.AccountInfo, error) {
	return &arch.AccountInfo{Lamports: 123456}, nil
}

func TestArchPoller_UpdatesGauges(t *testing.T) {
	metrics.RegisterArchMetrics(prometheus.NewRegistry())

	signer, err := arch.NewSignerFromHex("1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}

	c := bridge.NewArchWriteClientWithRPC(
		-1,
		arch.Pubkey{0xab},
		arch.Pubkey{0xcd},
		readMock{},
		signer,
		time.Second,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge.StartArchPoller(ctx, "test-router", c, 50*time.Millisecond)
	time.Sleep(75 * time.Millisecond)

	got := testutil.ToFloat64(metrics.ArchFeeVaultLamports.WithLabelValues("test-router", "-1"))
	if got != 123456 {
		t.Fatalf("ArchFeeVaultLamports got %v, want 123456", got)
	}
}
