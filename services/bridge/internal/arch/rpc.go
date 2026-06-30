// Package arch contains the Arch Network JSON-RPC client and transaction builder.
//
// # RuntimeTransaction wire format (arch_sdk 0.6.5, arch_program 0.6.5)
//
// Verified against:
//   - ~/.cargo/registry/src/.../arch_sdk-0.6.5/src/types/runtime_transaction.rs
//     (RuntimeTransaction::serialize)
//   - ~/.cargo/registry/src/.../arch_program-0.6.5/src/sanitized.rs
//     (ArchMessage::serialize, SanitizedInstruction::serialize)
//
// Layout of the serialized RuntimeTransaction:
//
//	[version        : u32 LE  ]  — currently 0  (4 bytes)
//	[num_signatures : u8      ]  — count of signer pubkeys  (1 byte)
//	[signatures     : 64B × n ]  — BIP-340 Schnorr (R||s), one per signer
//	[ArchMessage               ]  — serialized message (see below)
//	  [num_required_signatures    : u8]
//	  [num_readonly_signed        : u8]
//	  [num_readonly_unsigned      : u8]
//	  [num_account_keys           : u32 LE]
//	  [account_keys               : 32B × n]
//	  [recent_blockhash           : 32B]
//	  [num_instructions           : u32 LE]
//	  [for each instruction:
//	     program_id_index         : u8
//	     num_account_indices      : u32 LE
//	     account_indices          : []u8
//	     data_len                 : u32 LE
//	     data                     : []u8  ]
//
// All length fields use plain little-endian u32 (NOT Solana compact-u16).
//
// # Signing digest
//
// From ArchMessage::hash() in sanitized.rs (uses sha256 crate which outputs hex):
//
//	first  = sha256(serialized_message)          → 32 raw bytes
//	hexStr = hex_encode(first)                   → 64 ASCII bytes
//	second = sha256(hexStr_as_bytes)             → 32 raw bytes
//
// The 32 raw bytes of `second` are passed to Signer.SignDigest([32]byte).
// (In the JS/Rust SDK the signing input is the ASCII-hex of `second`, but
// for BIP-322 the library does its own wrapping; Go uses SignDigest directly
// over the 32-byte raw digest.)
//
// # send_transaction wire format
//
// The serialized RuntimeTransaction bytes are base64-encoded and sent as the
// sole JSON-RPC param: params: ["<base64>"].
package arch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// RPC is a thin JSON-RPC 2.0 client for the Arch validator.
type RPC struct {
	url    string
	client *http.Client
	id     atomic.Uint64
}

// NewRPC constructs an RPC client pointed at url.
func NewRPC(url string) *RPC {
	return &RPC{url: url, client: http.DefaultClient}
}

// AccountInfo is the decoded form of read_account_info's result.
type AccountInfo struct {
	Data     []byte
	Lamports uint64
	Owner    Pubkey
}

// ProcessedTx is the decoded form of get_processed_transaction's result.
type ProcessedTx struct {
	Status      string // "Processed" | "Failed"
	Logs        []string
	CustomError *uint32
}

// ---- JSON-RPC plumbing ----

type jsonrpcReq struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      uint64        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type jsonrpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// errNotFound is the sentinel returned when the validator responds with code 404.
var errNotFound = fmt.Errorf("arch: not found")

func (c *RPC) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	id := c.id.Add(1)
	req := jsonrpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("%s: read body: %w", method, err)
	}
	var resp jsonrpcResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("%s: decode: %w (body: %s)", method, err, string(respBody))
	}
	if resp.Error != nil {
		if resp.Error.Code == 404 {
			return errNotFound
		}
		return fmt.Errorf("%s: rpc error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("%s: decode result: %w", method, err)
		}
	}
	return nil
}

// ---- RPC methods ----

// GetBlockCount calls get_block_count and returns the current block height.
func (c *RPC) GetBlockCount(ctx context.Context) (uint64, error) {
	var out uint64
	if err := c.call(ctx, "get_block_count", []interface{}{}, &out); err != nil {
		return 0, err
	}
	return out, nil
}

// GetBestBlockHash calls get_best_block_hash. Returns the 32-byte hash.
// The validator returns it as a hex string without 0x prefix.
func (c *RPC) GetBestBlockHash(ctx context.Context) ([32]byte, error) {
	var hexStr string
	if err := c.call(ctx, "get_best_block_hash", []interface{}{}, &hexStr); err != nil {
		return [32]byte{}, err
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil || len(raw) != 32 {
		return [32]byte{}, fmt.Errorf("get_best_block_hash: bad hex %q", hexStr)
	}
	var out [32]byte
	copy(out[:], raw)
	return out, nil
}

// ReadAccountInfo calls read_account_info. Returns (nil, nil) when the account
// does not exist (validator returns error code 404).
func (c *RPC) ReadAccountInfo(ctx context.Context, pubkey Pubkey) (*AccountInfo, error) {
	pubkeyHex := hex.EncodeToString(pubkey[:])
	var raw struct {
		DataB64  string `json:"data"`
		Lamports uint64 `json:"lamports"`
		OwnerHex string `json:"owner"`
	}
	if err := c.call(ctx, "read_account_info", []interface{}{pubkeyHex}, &raw); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(raw.DataB64)
	if err != nil {
		return nil, fmt.Errorf("read_account_info: decode data: %w", err)
	}
	ownerBytes, err := hex.DecodeString(raw.OwnerHex)
	if err != nil || len(ownerBytes) != 32 {
		return nil, fmt.Errorf("read_account_info: decode owner: %w", err)
	}
	var ownerKey Pubkey
	copy(ownerKey[:], ownerBytes)
	return &AccountInfo{Data: data, Lamports: raw.Lamports, Owner: ownerKey}, nil
}

// SendTransaction calls send_transaction with the base64-encoded serialized
// RuntimeTransaction bytes. Returns the transaction ID hex string.
func (c *RPC) SendTransaction(ctx context.Context, signedTxBytes []byte) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(signedTxBytes)
	var txID string
	if err := c.call(ctx, "send_transaction", []interface{}{b64}, &txID); err != nil {
		return "", err
	}
	return txID, nil
}

// GetProcessedTransaction calls get_processed_transaction. Returns (nil, nil)
// while the transaction is still pending; the caller should poll.
func (c *RPC) GetProcessedTransaction(ctx context.Context, txID string) (*ProcessedTx, error) {
	var raw struct {
		Status json.RawMessage `json:"status"`
		Logs   []string        `json:"logs"`
	}
	if err := c.call(ctx, "get_processed_transaction", []interface{}{txID}, &raw); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	// status is a tagged enum:
	//   {"type":"processed"} or {"type":"failed","reason":{"custom":N}}
	var tag struct {
		Type   string `json:"type"`
		Reason struct {
			Custom *uint32 `json:"custom"`
		} `json:"reason"`
	}
	if err := json.Unmarshal(raw.Status, &tag); err != nil {
		return nil, fmt.Errorf("get_processed_transaction: decode status: %w", err)
	}
	out := &ProcessedTx{Logs: raw.Logs}
	switch tag.Type {
	case "processed":
		out.Status = "Processed"
	case "failed":
		out.Status = "Failed"
		out.CustomError = tag.Reason.Custom
	default:
		out.Status = tag.Type
	}
	return out, nil
}

// ---- Transaction builder ----

// BuildAndSignTransaction builds a RuntimeTransaction from a single instruction
// and signs it with signer. The returned bytes are the serialized
// RuntimeTransaction ready to pass to SendTransaction.
//
// Wire layout produced (matches arch_sdk 0.6.5 RuntimeTransaction::serialize):
//
//	version(u32 LE) || num_sigs(u8) || sigs(64B×n) || message
//
// where message = ArchMessage::serialize() — see package-level Godoc.
func BuildAndSignTransaction(ix Instruction, signer *Signer, recentBlockhash [32]byte) ([]byte, error) {
	// 1. Assemble the unique ordered account list.
	accounts := assembleAccounts(ix, signer.Pubkey())

	// 2. Ensure the program ID is in the account list (appended as readonly
	//    non-signer if not already present).
	if _, err := slotIndexOf(accounts, ix.ProgramID); err != nil {
		accounts = append(accounts, accountSlot{Pubkey: ix.ProgramID, IsSigner: false, IsWritable: false})
	}

	// 3. Derive header counts.
	numReqSigs, numReadSigned, numReadUnsigned := classifyAccounts(accounts)

	// 4. Build the ArchMessage serialization.
	msg, err := serializeMessage(accounts, numReqSigs, numReadSigned, numReadUnsigned, ix, recentBlockhash)
	if err != nil {
		return nil, err
	}

	// 5. Compute the signing digest: sha256(hex_encode(sha256(msg_bytes))).
	//    This mirrors ArchMessage::hash() in arch_program 0.6.5 sanitized.rs
	//    which uses the sha256 crate's digest() (hex output) twice.
	digest := archMessageDigest(msg)

	// 6. Sign.
	sig, err := signer.SignDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("BuildAndSignTransaction: %w", err)
	}

	// 7. Serialize the full RuntimeTransaction.
	var w bytes.Buffer
	// version: u32 LE = 0
	var vbuf [4]byte
	binary.LittleEndian.PutUint32(vbuf[:], 0)
	w.Write(vbuf[:])
	// num_signatures: u8
	w.WriteByte(1)
	// signature: 64 bytes
	w.Write(sig[:])
	// message
	w.Write(msg)

	return w.Bytes(), nil
}

// serializeMessage produces the ArchMessage wire bytes from the assembled
// account list, header counts, instruction, and recent blockhash.
func serializeMessage(
	accounts []accountSlot,
	numReqSigs, numReadSigned, numReadUnsigned uint8,
	ix Instruction,
	recentBlockhash [32]byte,
) ([]byte, error) {
	programIdx, err := slotIndexOf(accounts, ix.ProgramID)
	if err != nil {
		return nil, fmt.Errorf("serializeMessage: program id not found: %w", err)
	}
	accountIndices, err := accountIndicesOf(accounts, ix.Accounts)
	if err != nil {
		return nil, fmt.Errorf("serializeMessage: %w", err)
	}

	var w bytes.Buffer

	// Header: 3 bytes
	w.WriteByte(numReqSigs)
	w.WriteByte(numReadSigned)
	w.WriteByte(numReadUnsigned)

	// Account keys: u32 LE count + 32B each
	writeU32LE(&w, uint32(len(accounts)))
	for _, a := range accounts {
		w.Write(a.Pubkey[:])
	}

	// Recent blockhash: 32 bytes
	w.Write(recentBlockhash[:])

	// Instructions: u32 LE count = 1
	writeU32LE(&w, 1)
	// program_id_index: u8
	w.WriteByte(uint8(programIdx))
	// num_account_indices: u32 LE
	writeU32LE(&w, uint32(len(accountIndices)))
	// account indices: []u8
	w.Write(accountIndices)
	// data_len: u32 LE
	writeU32LE(&w, uint32(len(ix.Data)))
	// data
	w.Write(ix.Data)

	return w.Bytes(), nil
}

// archMessageDigest computes the signing digest used by ArchMessage::hash().
// From sanitized.rs (sha256 crate outputs hex strings):
//
//	first  = sha256(msg_bytes)        → 32 raw bytes
//	hexStr = hex_encode(first)        → 64 ASCII bytes
//	digest = sha256(hexStr_as_bytes)  → 32 raw bytes
func archMessageDigest(msg []byte) [32]byte {
	first := sha256.Sum256(msg)
	hexStr := hex.EncodeToString(first[:]) // 64 ASCII chars
	return sha256.Sum256([]byte(hexStr))
}

// writeU32LE writes a uint32 in little-endian to w.
func writeU32LE(w *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	w.Write(buf[:])
}

// ---- Account assembly helpers ----

type accountSlot struct {
	Pubkey     Pubkey
	IsSigner   bool
	IsWritable bool
}

// assembleAccounts builds the unique ordered account list for a single-instruction
// transaction. Ordering follows the Arch/Solana convention:
//
//  1. Writable signers (payer first)
//  2. Read-only signers
//  3. Writable non-signers
//  4. Read-only non-signers
//
// The signer's pubkey is always placed at index 0 (fee-payer).
func assembleAccounts(ix Instruction, payer Pubkey) []accountSlot {
	seen := map[Pubkey]bool{payer: true}
	var wSig, rSig, wNS, rNS []accountSlot

	// Payer is always a writable signer at index 0.
	wSig = append(wSig, accountSlot{Pubkey: payer, IsSigner: true, IsWritable: true})

	for _, a := range ix.Accounts {
		if seen[a.Pubkey] {
			continue
		}
		seen[a.Pubkey] = true
		switch {
		case a.IsSigner && a.IsWritable:
			wSig = append(wSig, accountSlot{Pubkey: a.Pubkey, IsSigner: true, IsWritable: true})
		case a.IsSigner && !a.IsWritable:
			rSig = append(rSig, accountSlot{Pubkey: a.Pubkey, IsSigner: true, IsWritable: false})
		case !a.IsSigner && a.IsWritable:
			wNS = append(wNS, accountSlot{Pubkey: a.Pubkey, IsSigner: false, IsWritable: true})
		default:
			rNS = append(rNS, accountSlot{Pubkey: a.Pubkey, IsSigner: false, IsWritable: false})
		}
	}

	out := make([]accountSlot, 0, len(wSig)+len(rSig)+len(wNS)+len(rNS))
	out = append(out, wSig...)
	out = append(out, rSig...)
	out = append(out, wNS...)
	out = append(out, rNS...)
	return out
}

// classifyAccounts computes the three MessageHeader counts from the assembled
// account list.
func classifyAccounts(accounts []accountSlot) (numReqSigs, numReadSigned, numReadUnsigned uint8) {
	for _, a := range accounts {
		if a.IsSigner {
			numReqSigs++
			if !a.IsWritable {
				numReadSigned++
			}
		} else if !a.IsWritable {
			numReadUnsigned++
		}
	}
	return
}

// slotIndexOf returns the index of pk in accounts.
func slotIndexOf(accounts []accountSlot, pk Pubkey) (int, error) {
	for i, a := range accounts {
		if a.Pubkey == pk {
			return i, nil
		}
	}
	return -1, fmt.Errorf("pubkey %x not in account list", pk[:4])
}

// accountIndicesOf maps each AccountMeta's pubkey to its index in accounts.
func accountIndicesOf(accounts []accountSlot, metas []AccountMeta) ([]byte, error) {
	out := make([]byte, len(metas))
	for i, m := range metas {
		idx, err := slotIndexOf(accounts, m.Pubkey)
		if err != nil {
			return nil, err
		}
		out[i] = byte(idx)
	}
	return out, nil
}
