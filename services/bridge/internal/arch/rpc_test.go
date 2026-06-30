package arch

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeArchRPC echoes a single canned response per method. Tests inspect the
// inbound request to assert the JSON-RPC envelope is correctly formed.
func fakeArchRPC(t *testing.T, responses map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad json", 400)
			return
		}
		body, ok := responses[req.Method]
		if !ok {
			t.Errorf("unexpected method %q", req.Method)
			http.Error(w, "method not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body)) //nolint:errcheck
	}))
}

func TestRPCGetBlockCount(t *testing.T) {
	srv := fakeArchRPC(t, map[string]string{
		"get_block_count": `{"jsonrpc":"2.0","id":1,"result":12345}`,
	})
	defer srv.Close()
	c := NewRPC(srv.URL)
	got, err := c.GetBlockCount(context.Background())
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if got != 12345 {
		t.Fatalf("got %d, want 12345", got)
	}
}

func TestRPCReadAccountInfo_NotFound(t *testing.T) {
	srv := fakeArchRPC(t, map[string]string{
		"read_account_info": `{"jsonrpc":"2.0","id":1,"error":{"code":404,"message":"not found"}}`,
	})
	defer srv.Close()
	c := NewRPC(srv.URL)
	info, err := c.ReadAccountInfo(context.Background(), Pubkey{})
	if err != nil {
		t.Fatalf("ReadAccountInfo: %v", err)
	}
	if info != nil {
		t.Fatalf("got %+v, want nil for not-found", info)
	}
}

func TestRPCSendTransaction(t *testing.T) {
	srv := fakeArchRPC(t, map[string]string{
		"send_transaction": `{"jsonrpc":"2.0","id":1,"result":"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}`,
	})
	defer srv.Close()
	c := NewRPC(srv.URL)
	txID, err := c.SendTransaction(context.Background(), []byte{0xde, 0xad, 0xbe, 0xef})
	if err != nil {
		t.Fatalf("SendTransaction: %v", err)
	}
	if !strings.HasPrefix(txID, "abcdef") {
		t.Fatalf("got txID %q", txID)
	}
}

func TestBuildAndSignTransaction_Roundtrip(t *testing.T) {
	signer, err := NewSignerFromHex("1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("NewSignerFromHex: %v", err)
	}
	ix := Instruction{
		ProgramID: Pubkey{0x01, 0x02, 0x03},
		Accounts: []AccountMeta{
			{Pubkey: signer.Pubkey(), IsSigner: true, IsWritable: true},
			{Pubkey: SystemProgramID, IsSigner: false, IsWritable: false},
		},
		Data: []byte{0x01, 0x02, 0x03, 0x04},
	}
	var blockhash [32]byte
	copy(blockhash[:], []byte("blockhashpaddedtoexactly32bytes!"))
	signed, err := BuildAndSignTransaction(ix, signer, blockhash)
	if err != nil {
		t.Fatalf("BuildAndSignTransaction: %v", err)
	}
	if len(signed) < 100 {
		t.Fatalf("signed tx suspiciously short: %d bytes (%s)", len(signed), hex.EncodeToString(signed))
	}
	// Specific byte assertions land once the implementer documents the wire
	// format in rpc.go's leading Godoc. The integration test in Task 17 is
	// the end-to-end validator-accepted gate.
}
