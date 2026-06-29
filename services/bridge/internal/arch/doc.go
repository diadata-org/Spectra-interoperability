// Package arch implements the wire-format primitives the Spectra bridge needs
// to talk to Arch Network: Borsh serialization for OracleIntent and the
// receiver/fee-hook instruction enums, PDA derivation, BIP-322 Taproot
// signing, a thin JSON-RPC client, and DIA_ORACLE.* log parsing.
//
// Parity with the on-chain receiver (sub-project 1) is asserted against the
// JSON fixtures under testdata/.
package arch
