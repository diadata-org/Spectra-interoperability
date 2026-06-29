package bridge

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

// archRPCInterface is the subset of arch.RPC methods ArchWriteClient calls.
// Defined here so tests can substitute a mock.
type archRPCInterface interface {
	GetBestBlockHash(ctx context.Context) ([32]byte, error)
	SendTransaction(ctx context.Context, signed []byte) (string, error)
	GetProcessedTransaction(ctx context.Context, txID string) (*arch.ProcessedTx, error)
}

// ArchWriteClient is the Arch-Network implementation of the Destination
// interface. It serializes a single OracleIntent into an Arch transaction
// calling the receiver program's HandleIntentUpdate instruction, signs with
// BIP-340 Schnorr, sends, waits for confirmation, and parses logs into
// per-intent rejections.
type ArchWriteClient struct {
	chainID           int64
	receiverProgramID arch.Pubkey
	feeHookProgramID  arch.Pubkey
	rpc               archRPCInterface
	signer            *arch.Signer
	confirmTimeout    time.Duration
}

// NewArchWriteClient constructs an ArchWriteClient.
func NewArchWriteClient(
	chainID int64,
	receiverProgramID arch.Pubkey,
	feeHookProgramID arch.Pubkey,
	rpc *arch.RPC,
	signer *arch.Signer,
	confirmTimeout time.Duration,
) *ArchWriteClient {
	if confirmTimeout <= 0 {
		confirmTimeout = 30 * time.Second
	}
	return &ArchWriteClient{
		chainID:           chainID,
		receiverProgramID: receiverProgramID,
		feeHookProgramID:  feeHookProgramID,
		rpc:               rpc,
		signer:            signer,
		confirmTimeout:    confirmTimeout,
	}
}

// newArchWriteClientForTest is unexported and lets tests in package bridge inject a mock RPC.
func newArchWriteClientForTest(rpc archRPCInterface, signer *arch.Signer, receiverProgramID, feeHookProgramID arch.Pubkey) *ArchWriteClient {
	return &ArchWriteClient{
		chainID:           -1,
		receiverProgramID: receiverProgramID,
		feeHookProgramID:  feeHookProgramID,
		rpc:               rpc,
		signer:            signer,
		confirmTimeout:    1 * time.Second,
	}
}

// ArchRPCInterface is the RPC subset used by ArchWriteClient.
// It is exported so that external test packages can implement a mock.
type ArchRPCInterface = archRPCInterface

// NewArchWriteClientWithRPC constructs an ArchWriteClient with a custom RPC
// implementation. Intended for test use only (e.g. package bridge_test).
func NewArchWriteClientWithRPC(
	chainID int64,
	receiverProgramID arch.Pubkey,
	feeHookProgramID arch.Pubkey,
	rpc ArchRPCInterface,
	signer *arch.Signer,
	confirmTimeout time.Duration,
) *ArchWriteClient {
	if confirmTimeout <= 0 {
		confirmTimeout = 30 * time.Second
	}
	return &ArchWriteClient{
		chainID:           chainID,
		receiverProgramID: receiverProgramID,
		feeHookProgramID:  feeHookProgramID,
		rpc:               rpc,
		signer:            signer,
		confirmTimeout:    confirmTimeout,
	}
}

func (c *ArchWriteClient) Kind() string { return "arch" }
func (c *ArchWriteClient) ChainID() int64 { return c.chainID }

// ReceiverAddress returns the hex-encoded receiver program ID.
func (c *ArchWriteClient) ReceiverAddress() string {
	return hex.EncodeToString(c.receiverProgramID[:])
}

// Send builds, signs, and sends a HandleIntentUpdate transaction for the
// given intent. It waits for confirmation and parses the receiver's logs.
func (c *ArchWriteClient) Send(ctx context.Context, req *types.UpdateRequest) (TxResult, error) {
	bridgeIntent, ok := req.ExtractedData.Enrichment["fullIntent"].(*types.OracleIntent)
	if !ok || bridgeIntent == nil {
		return TxResult{}, fmt.Errorf("arch send: missing fullIntent in enrichment")
	}
	archIntent := bridgeOracleIntentToArch(bridgeIntent)

	// Derive PDAs (pure).
	cfgPDA, _ := arch.ConfigPDA(c.receiverProgramID)
	dedupPDA, _ := arch.DedupPDA(c.receiverProgramID)
	pricePDA, _ := arch.PricePDA(c.receiverProgramID, archIntent.Symbol)
	feeCfgPDA, _ := arch.FeeConfigPDA(c.feeHookProgramID)
	feeVaultPDA, _ := arch.FeeVaultPDA(c.feeHookProgramID)

	// Borsh-encode HandleIntentUpdate { intent }.
	data, err := arch.BuildHandleIntentUpdateData(archIntent)
	if err != nil {
		return TxResult{}, fmt.Errorf("arch send: encode instruction: %w", err)
	}

	payer := c.signer.Pubkey()
	ix := arch.Instruction{
		ProgramID: c.receiverProgramID,
		Accounts: []arch.AccountMeta{
			// §3.3 required order:
			// 1. dedup PDA — writable
			{Pubkey: dedupPDA, IsSigner: false, IsWritable: true},
			// 2. config PDA — readonly
			{Pubkey: cfgPDA, IsSigner: false, IsWritable: false},
			// 3. payer (signer.Pubkey()) — signer + writable
			{Pubkey: payer, IsSigner: true, IsWritable: true},
			// 4. system program — readonly
			{Pubkey: arch.SystemProgramID, IsSigner: false, IsWritable: false},
			// 5. fee_config PDA — writable
			{Pubkey: feeCfgPDA, IsSigner: false, IsWritable: true},
			// 6. fee_vault PDA — writable
			{Pubkey: feeVaultPDA, IsSigner: false, IsWritable: true},
			// 7. price PDA for intent.Symbol — writable
			{Pubkey: pricePDA, IsSigner: false, IsWritable: true},
		},
		Data: data,
	}

	blockhash, err := c.rpc.GetBestBlockHash(ctx)
	if err != nil {
		return TxResult{}, fmt.Errorf("arch send: get blockhash: %w", err)
	}
	signed, err := arch.BuildAndSignTransaction(ix, c.signer, blockhash)
	if err != nil {
		return TxResult{}, fmt.Errorf("arch send: build/sign: %w", err)
	}
	txID, err := c.rpc.SendTransaction(ctx, signed)
	if err != nil {
		return TxResult{}, fmt.Errorf("arch send: rpc send: %w", err)
	}

	deadline := time.Now().Add(c.confirmTimeout)
	for {
		processed, err := c.rpc.GetProcessedTransaction(ctx, txID)
		if err != nil {
			return TxResult{}, fmt.Errorf("arch send: confirm %s: %w", txID, err)
		}
		if processed != nil {
			events := arch.ParseIntentEvents(processed.Logs)
			var rejections []IntentRejection
			for _, e := range events {
				if e.Kind == "rejected" {
					rejections = append(rejections, IntentRejection{
						IntentHash: e.IntentHash,
						Symbol:     e.Symbol,
						Signer:     [20]byte(e.Signer),
						Reason:     e.Reason,
					})
				}
			}
			// Emit metrics from parsed events.
			chainIDStr := strconv.FormatInt(c.chainID, 10)
			routerID := req.RouterID
			for _, e := range events {
				switch e.Kind {
				case "update":
					metrics.ArchIntentUpdates.WithLabelValues(routerID, chainIDStr, e.Symbol).Inc()
				case "stale":
					metrics.ArchIntentStale.WithLabelValues(routerID, chainIDStr, e.Symbol).Inc()
				case "rejected":
					metrics.ArchIntentRejected.WithLabelValues(routerID, chainIDStr, e.Reason).Inc()
				}
			}
			return TxResult{
				TxID:       txID,
				Status:     processed.Status,
				Logs:       processed.Logs,
				Rejections: rejections,
			}, nil
		}
		if time.Now().After(deadline) {
			return TxResult{}, fmt.Errorf("arch send: confirm %s: timeout after %s", txID, c.confirmTimeout)
		}
		select {
		case <-ctx.Done():
			return TxResult{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// bridgeOracleIntentToArch converts the bridge-side types.OracleIntent (which
// uses *big.Int for U256 fields and common.Address for Signer) into
// arch.OracleIntent ([32]byte fields, EthAddress for Signer).
func bridgeOracleIntentToArch(b *types.OracleIntent) arch.OracleIntent {
	var signer arch.EthAddress
	copy(signer[:], b.Signer.Bytes())
	return arch.OracleIntent{
		IntentType: b.IntentType,
		Version:    b.Version,
		ChainID:    arch.U256FromBigInt(b.ChainID),
		Nonce:      arch.U256FromBigInt(b.Nonce),
		Expiry:     arch.U256FromBigInt(b.Expiry),
		Symbol:     b.Symbol,
		Price:      arch.U256FromBigInt(b.Price),
		Timestamp:  arch.U256FromBigInt(b.Timestamp),
		Source:     b.Source,
		Signature:  b.Signature,
		Signer:     signer,
	}
}
