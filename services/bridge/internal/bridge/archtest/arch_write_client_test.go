// Package archtest contains integration-style unit tests for bridge.ArchWriteClient.
// It lives in a separate directory so that pre-existing compile failures in
// internal/bridge/*_test.go (which reference Bridge methods not yet present)
// do not prevent our tests from running.
package archtest

import (
	"context"
	"math/big"
	"testing"
	"time"

	bridgeconfig "github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// mockArchRPC implements bridge.ArchRPCInterface for unit tests.
type mockArchRPC struct {
	sentBytes      []byte
	txID           string
	processedTx    *arch.ProcessedTx
	confirmCallCnt int
	blockhash      [32]byte
	sendErr        error
}

func (m *mockArchRPC) GetBestBlockHash(_ context.Context) ([32]byte, error) {
	return m.blockhash, nil
}

func (m *mockArchRPC) SendTransaction(_ context.Context, signed []byte) (string, error) {
	if m.sendErr != nil {
		return "", m.sendErr
	}
	m.sentBytes = signed
	return m.txID, nil
}

func (m *mockArchRPC) GetProcessedTransaction(_ context.Context, _ string) (*arch.ProcessedTx, error) {
	m.confirmCallCnt++
	return m.processedTx, nil
}

func (m *mockArchRPC) ReadAccountInfo(_ context.Context, _ arch.Pubkey) (*arch.AccountInfo, error) {
	return nil, nil
}

func sampleIntent(_ *testing.T) types.OracleIntent {
	return types.OracleIntent{
		IntentType: "PriceUpdate",
		Version:    "1",
		ChainID:    new(big.Int).SetUint64(1),
		Nonce:      new(big.Int).SetUint64(0x11),
		Expiry:     new(big.Int).SetUint64(0),
		Symbol:     "BTC/USD",
		Price:      new(big.Int).SetUint64(65000000000000),
		Timestamp:  new(big.Int).SetUint64(1700000017),
		Source:     "DIA",
		Signature:  []byte{0x01, 0x02},
		Signer:     common.HexToAddress("0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a"),
	}
}

func makeRequest(intent *types.OracleIntent) *types.UpdateRequest {
	return &types.UpdateRequest{
		ExtractedData: &bridgeconfig.ExtractedData{
			Enrichment: map[string]interface{}{
				"fullIntent": intent,
			},
		},
	}
}

func newTestClient(t *testing.T, mock *mockArchRPC, timeout ...time.Duration) *bridge.ArchWriteClient {
	t.Helper()
	signer, err := arch.NewSignerFromHex("1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}
	to := time.Second
	if len(timeout) > 0 {
		to = timeout[0]
	}
	return bridge.NewArchWriteClientWithRPC(
		-1,
		arch.Pubkey{0xab},
		arch.Pubkey{0xcd},
		mock,
		signer,
		to,
	)
}

func TestArchWriteClient_SuccessfulUpdate(t *testing.T) {
	mock := &mockArchRPC{
		txID: "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		processedTx: &arch.ProcessedTx{
			Status: "Processed",
			Logs: []string{
				"Program log: DIA_ORACLE.INTENT_UPDATE intent_hash=0xab00000000000000000000000000000000000000000000000000000000000000 symbol=\"BTC/USD\" price=65000000000000 timestamp=1700000017 signer=0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a",
			},
		},
	}

	c := newTestClient(t, mock)
	intent := sampleIntent(t)
	res, err := c.Send(context.Background(), makeRequest(&intent))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != "Processed" {
		t.Errorf("Status = %q, want Processed", res.Status)
	}
	if len(res.Rejections) != 0 {
		t.Errorf("got %d rejections, want 0", len(res.Rejections))
	}
	if len(mock.sentBytes) < 100 {
		t.Errorf("mock got %d signed bytes, want >100", len(mock.sentBytes))
	}
	if res.TxID != mock.txID {
		t.Errorf("TxID = %q, want %q", res.TxID, mock.txID)
	}
}

func TestArchWriteClient_RejectionParsing(t *testing.T) {
	mock := &mockArchRPC{
		txID: "1111111111111111111111111111111111111111111111111111111111111111",
		processedTx: &arch.ProcessedTx{
			Status: "Processed",
			Logs: []string{
				"Program log: DIA_ORACLE.INTENT_REJECTED intent_hash=0x1100000000000000000000000000000000000000000000000000000000000000 symbol=\"BTC/USD\" signer=0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa reason=UnauthorizedSigner",
			},
		},
	}

	c := newTestClient(t, mock)
	intent := sampleIntent(t)
	res, err := c.Send(context.Background(), makeRequest(&intent))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != "Processed" {
		t.Errorf("Status = %q, want Processed", res.Status)
	}
	if len(res.Rejections) != 1 {
		t.Fatalf("got %d rejections, want 1", len(res.Rejections))
	}
	if res.Rejections[0].Reason != "UnauthorizedSigner" {
		t.Errorf("reason = %q, want UnauthorizedSigner", res.Rejections[0].Reason)
	}
}

func TestArchWriteClient_SendError(t *testing.T) {
	signer, err := arch.NewSignerFromHex("2222222222222222222222222222222222222222222222222222222222222222")
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}
	mock := &mockArchRPC{sendErr: context.DeadlineExceeded}
	c := bridge.NewArchWriteClientWithRPC(-1, arch.Pubkey{0x01}, arch.Pubkey{0x02}, mock, signer, time.Second)
	intent := sampleIntent(t)
	_, err = c.Send(context.Background(), makeRequest(&intent))
	if err == nil {
		t.Fatal("expected error from SendTransaction, got nil")
	}
}

func TestArchWriteClient_ConfirmTimeout(t *testing.T) {
	signer, err := arch.NewSignerFromHex("3333333333333333333333333333333333333333333333333333333333333333")
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}
	// processedTx is nil → GetProcessedTransaction always returns (nil, nil) → timeout
	mock := &mockArchRPC{
		txID:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		processedTx: nil,
	}
	c := bridge.NewArchWriteClientWithRPC(-1, arch.Pubkey{0x01}, arch.Pubkey{0x02}, mock, signer, 300*time.Millisecond)
	intent := sampleIntent(t)
	_, err = c.Send(context.Background(), makeRequest(&intent))
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
