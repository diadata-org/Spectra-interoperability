package arch

import (
	"crypto/sha256"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// findProgramAddress implements the Arch-compatible PDA algorithm:
// for bump from 255 down to 0, compute sha256(seeds || bump || programID)
// and accept the first result that is NOT a valid secp256k1 x-only public key
// (off-curve).
func findProgramAddress(seeds [][]byte, programID Pubkey) (Pubkey, uint8) {
	for bump := 255; bump >= 0; bump-- {
		h := sha256.New()
		for _, s := range seeds {
			h.Write(s)
		}
		h.Write([]byte{byte(bump)})
		h.Write(programID[:])
		var candidate Pubkey
		sum := h.Sum(nil)
		copy(candidate[:], sum)
		if isOffCurve(candidate) {
			return candidate, uint8(bump)
		}
	}
	panic("findProgramAddress: no off-curve candidate found in 256 attempts")
}

// isOffCurve returns true when `candidate` does NOT represent a valid secp256k1
// public key. This mirrors arch_program::pubkey::Pubkey::is_on_curve which calls
// bitcoin::secp256k1::PublicKey::from_slice with the raw 32-byte hash.
// That function requires 33 (compressed) or 65 (uncompressed) bytes, so a
// 32-byte input always fails, meaning every candidate is "off-curve" and bump=255
// is always accepted on the first try.
func isOffCurve(candidate Pubkey) bool {
	// Pass raw 32-byte candidate: ParsePubKey requires 33 or 65 bytes, so this
	// always returns an error, matching Rust's bitcoin::secp256k1::PublicKey::from_slice
	// behaviour with a 32-byte input.
	_, err := secp256k1.ParsePubKey(candidate[:])
	return err != nil
}

// ConfigPDA derives the receiver's Config PDA: seeds = [utf8("config")].
func ConfigPDA(programID Pubkey) (Pubkey, uint8) {
	return findProgramAddress([][]byte{[]byte("config")}, programID)
}

// DedupPDA derives the receiver's Dedup PDA: seeds = [utf8("dedup")].
func DedupPDA(programID Pubkey) (Pubkey, uint8) {
	return findProgramAddress([][]byte{[]byte("dedup")}, programID)
}

// PricePDA derives the per-symbol Price PDA: seeds = [utf8("price"), sha256(utf8(symbol))].
func PricePDA(programID Pubkey, symbol string) (Pubkey, uint8) {
	symHash := sha256.Sum256([]byte(symbol))
	return findProgramAddress([][]byte{[]byte("price"), symHash[:]}, programID)
}

// FeeConfigPDA derives the fee-hook FeeConfig PDA: seeds = [utf8("fee_config")].
func FeeConfigPDA(programID Pubkey) (Pubkey, uint8) {
	return findProgramAddress([][]byte{[]byte("fee_config")}, programID)
}

// FeeVaultPDA derives the fee-hook FeeVault PDA: seeds = [utf8("fee_vault")].
func FeeVaultPDA(programID Pubkey) (Pubkey, uint8) {
	return findProgramAddress([][]byte{[]byte("fee_vault")}, programID)
}
