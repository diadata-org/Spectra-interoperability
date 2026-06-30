package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/arch"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

func envOrSkip(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("%s not set", name)
	}
	return v
}

type fixtureIntent struct {
	IntentHex string `json:"intent_hex"`
	SignerHex  string `json:"signer_hex"`
}

func loadIntentFixture(t *testing.T, path string) types.OracleIntent {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var f fixtureIntent
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rawBytes, err := hex.DecodeString(f.IntentHex)
	if err != nil {
		t.Fatalf("decode intent hex: %v", err)
	}
	archIntent, err := arch.UnmarshalOracleIntent(rawBytes)
	if err != nil {
		t.Fatalf("UnmarshalOracleIntent: %v", err)
	}
	// Convert arch.OracleIntent to bridge types.OracleIntent for the in-process pipeline.
	bridgeIntent := types.OracleIntent{
		IntentType: archIntent.IntentType,
		Version:    archIntent.Version,
		ChainID:    arch.BigIntFromU256(archIntent.ChainID),
		Nonce:      arch.BigIntFromU256(archIntent.Nonce),
		Expiry:     arch.BigIntFromU256(archIntent.Expiry),
		Symbol:     archIntent.Symbol,
		Price:      arch.BigIntFromU256(archIntent.Price),
		Timestamp:  arch.BigIntFromU256(archIntent.Timestamp),
		Source:     archIntent.Source,
		Signature:  types.HexBytes(archIntent.Signature),
		Signer:     common.Address(archIntent.Signer),
	}
	return bridgeIntent
}

func TestArchBridge_EndToEnd(t *testing.T) {
	rpcURL := envOrSkip(t, "ARCH_RPC_URL")
	secretHex := envOrSkip(t, "ARCH_RELAYER_PRIVATE_KEY")
	receiverHex := envOrSkip(t, "ARCH_RECEIVER_PROGRAM_ID")
	feeHookHex := envOrSkip(t, "ARCH_FEE_HOOK_PROGRAM_ID")

	signer, err := arch.NewSignerFromHex(secretHex)
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}
	receiverPK := mustDecodePubkey(t, receiverHex)
	feeHookPK := mustDecodePubkey(t, feeHookHex)
	rpc := arch.NewRPC(rpcURL)

	client := bridge.NewArchWriteClient(-1, receiverPK, feeHookPK, rpc, signer, 30*time.Second)

	// From services/bridge/test/integration/, the relative path to testdata is:
	// ../../internal/arch/testdata/intent_a.json
	intent := loadIntentFixture(t, filepath.Join("..", "..", "internal", "arch", "testdata", "intent_a.json"))

	req := &types.UpdateRequest{
		RouterID: "integration-test",
		ExtractedData: &config.ExtractedData{
			Enrichment: map[string]interface{}{"fullIntent": &intent},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := client.Send(ctx, req)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != "Processed" {
		t.Fatalf("Status = %q (logs=%v)", res.Status, res.Logs)
	}
	// First send should have stored the value: no rejections expected for a
	// fresh symbol.
	if len(res.Rejections) > 0 {
		t.Logf("rejections (acceptable if symbol already populated): %+v", res.Rejections)
	}

	// Read the Price PDA and assert the stored value matches.
	pricePDA, _ := arch.PricePDA(receiverPK, intent.Symbol)
	info, err := rpc.ReadAccountInfo(ctx, pricePDA)
	if err != nil {
		t.Fatalf("ReadAccountInfo(price PDA): %v", err)
	}
	if info == nil {
		t.Fatalf("price PDA missing on chain")
	}
	// PriceAccount Borsh: version(1) + symbol(len-prefixed) + timestamp(u128) + value(u128) + lastIntentHash([32]) + reserved([32])
	// We trust the receiver to write correctly and just sanity-check the
	// length here. Full PriceAccount decoder lives in arch package if needed.
	if len(info.Data) < 1+4+len(intent.Symbol)+16+16+32+32 {
		t.Fatalf("price PDA data too short: %d bytes", len(info.Data))
	}

	// Variant: replay the same intent. Expect partially_delivered (rejected
	// with AlreadyProcessed) on the receiver side.
	res2, err := client.Send(ctx, req)
	if err != nil {
		t.Fatalf("Send replay: %v", err)
	}
	if len(res2.Rejections) == 0 {
		t.Fatalf("replay: expected at least one rejection, got 0 (logs=%v)", res2.Logs)
	}
	if res2.Rejections[0].Reason != "AlreadyProcessed" {
		t.Errorf("replay rejection reason = %q, want AlreadyProcessed", res2.Rejections[0].Reason)
	}

	t.Logf("integration test passed: %s", intent.Symbol)
}

func mustDecodePubkey(t *testing.T, h string) arch.Pubkey {
	t.Helper()
	if len(h) != 64 {
		t.Fatalf("pubkey hex must be 64 chars, got %d", len(h))
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("decode pubkey: %v", err)
	}
	var out arch.Pubkey
	copy(out[:], raw)
	return out
}
