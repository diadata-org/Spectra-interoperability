package bridge

import (
	"context"

	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// Destination dispatches a single UpdateRequest to a chain backend. Both
// EVM (existing *WriteClient) and Arch (new *ArchWriteClient) satisfy it.
type Destination interface {
	Send(ctx context.Context, req *bridgetypes.UpdateRequest) (TxResult, error)
	ReceiverAddress() string
	ChainID() int64
	Kind() string
}

// TxResult is the outcome of a Destination.Send call.
type TxResult struct {
	TxID       string
	Status     string // "Processed" | "Failed"
	Logs       []string
	Rejections []IntentRejection
}

// IntentRejection is one per-intent rejection parsed from the receiver's
// DIA_ORACLE.INTENT_REJECTED log lines. Always empty for EVM destinations.
type IntentRejection struct {
	IntentHash [32]byte
	Symbol     string
	Signer     [20]byte
	Reason     string // "UnauthorizedSigner" | "AlreadyProcessed" | "InvalidSignature"
}
