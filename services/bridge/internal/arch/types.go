package arch

import (
	"fmt"
	"math/big"
)

// Pubkey is a 32-byte Arch public key.
type Pubkey [32]byte

// EthAddress is the 20-byte Ethereum address that signs an OracleIntent.
type EthAddress [20]byte

// U256 is a 32-byte big-endian unsigned 256-bit integer.
type U256 [32]byte

// SystemProgramID is the canonical Arch system program ID: 32 zero bytes
// (base58 "11111111111111111111111111111111").
var SystemProgramID = Pubkey{}

// OracleIntent mirrors dia_arch_shared::intent::OracleIntent from sub-project 1.
// Field order is wire-binding: Borsh serializes struct fields in declaration order.
type OracleIntent struct {
	IntentType string
	Version    string
	ChainID    U256
	Nonce      U256
	Expiry     U256
	Symbol     string
	Price      U256
	Timestamp  U256
	Source     string
	Signature  []byte
	Signer     EthAddress
}

// AccountMeta describes one account passed to an instruction.
type AccountMeta struct {
	Pubkey     Pubkey
	IsSigner   bool
	IsWritable bool
}

// Instruction is a top-level Arch instruction.
type Instruction struct {
	ProgramID Pubkey
	Accounts  []AccountMeta
	Data      []byte
}

// U256FromBigInt encodes x as a 32-byte big-endian U256. Panics on negative input.
func U256FromBigInt(x *big.Int) U256 {
	if x.Sign() < 0 {
		panic(fmt.Sprintf("U256FromBigInt: negative input %s", x))
	}
	var out U256
	b := x.Bytes() // big-endian, leading zeros stripped
	if len(b) > 32 {
		panic(fmt.Sprintf("U256FromBigInt: value overflows U256 (%d bytes)", len(b)))
	}
	copy(out[32-len(b):], b)
	return out
}

// BigIntFromU256 decodes a U256 back to a positive *big.Int.
func BigIntFromU256(u U256) *big.Int {
	return new(big.Int).SetBytes(u[:])
}
