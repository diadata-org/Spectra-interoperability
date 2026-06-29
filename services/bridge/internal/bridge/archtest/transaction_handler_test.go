package archtest

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	bridgeconfig "github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// stubArchDest is a minimal Destination that returns a pre-configured TxResult.
type stubArchDest struct {
	chainID int64
	result  bridge.TxResult
}

func (s *stubArchDest) Send(_ context.Context, _ *bridgetypes.UpdateRequest) (bridge.TxResult, error) {
	return s.result, nil
}
func (s *stubArchDest) ReceiverAddress() string { return "" }
func (s *stubArchDest) ChainID() int64          { return s.chainID }
func (s *stubArchDest) Kind() string             { return "arch" }

func archUpdateReq(chainID int64) *bridgetypes.UpdateRequest {
	return &bridgetypes.UpdateRequest{
		DestinationChain: &bridgeconfig.DestinationConfig{ChainID: chainID},
	}
}

// TestProcess_ArchPartialDelivery_PersistsRejections verifies that when
// Destination.Send returns 2 rejections, TransactionHandler.Process executes
// 2 INSERT statements into dia_arch_rejections.
func TestProcess_ArchPartialDelivery_PersistsRejections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	hash1 := [32]byte{0x11}
	hash2 := [32]byte{0x22}
	signer1 := [20]byte{0xaa}
	signer2 := [20]byte{0xbb}
	txID := "abcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd"

	// Expect 2 INSERT calls — one per rejection.
	mock.ExpectExec(`INSERT INTO dia_arch_rejections`).
		WithArgs(nil, hash1[:], "BTC/USD", signer1[:], "UnauthorizedSigner", txID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO dia_arch_rejections`).
		WithArgs(nil, hash2[:], "ETH/USD", signer2[:], "AlreadyProcessed", txID).
		WillReturnResult(sqlmock.NewResult(2, 1))

	dest := &stubArchDest{
		chainID: -1,
		result: bridge.TxResult{
			TxID:   txID,
			Status: "Processed",
			Rejections: []bridge.IntentRejection{
				{IntentHash: hash1, Symbol: "BTC/USD", Signer: signer1, Reason: "UnauthorizedSigner"},
				{IntentHash: hash2, Symbol: "ETH/USD", Signer: signer2, Reason: "AlreadyProcessed"},
			},
		},
	}

	handler := bridge.NewTransactionHandler(
		map[int64]bridge.Destination{-1: dest},
		nil,
		nil,
		db,
	)

	if err := handler.Process(context.Background(), archUpdateReq(-1)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}

// TestProcess_ArchSuccess_NoRejections verifies that a fully delivered Arch
// transaction triggers no INSERT calls.
func TestProcess_ArchSuccess_NoRejections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// No expectations set — any unexpected SQL call will fail the test.

	dest := &stubArchDest{
		chainID: -1,
		result: bridge.TxResult{
			TxID:       "0000000000000000000000000000000000000000000000000000000000000000",
			Status:     "Processed",
			Rejections: nil,
		},
	}

	handler := bridge.NewTransactionHandler(
		map[int64]bridge.Destination{-1: dest},
		nil,
		nil,
		db,
	)

	if err := handler.Process(context.Background(), archUpdateReq(-1)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations not met: %v", err)
	}
}
