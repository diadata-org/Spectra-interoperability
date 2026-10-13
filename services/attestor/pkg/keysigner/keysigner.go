// Package keysigner is a thin facade over the shared
// github.com/diadata-org/keysigner module.
//
// The implementation (HexKeySigner, PKCS11KeySigner, recovery helpers and
// their tests) lives in the shared module so lumina-guardians and any other
// DIA service sign through the exact same code. This package only keeps:
//
//   - type/function re-exports actually used by attestor call sites;
//   - the config-driven constructor New(cfg) in new.go, which maps the
//     attestor's viper configuration onto keysigner.PKCS11Params.
//
// Do not add signing logic here — it belongs in the shared module.
package keysigner

import (
	"github.com/diadata-org/keysigner"
	"github.com/diadata-org/keysigner/evm"
)

// KeySigner performs ECDSA signing on behalf of a single Ethereum address,
// without exposing the underlying private key material.
type KeySigner = keysigner.KeySigner

// HexKeySigner is a KeySigner backed by an in-memory ECDSA private key.
type HexKeySigner = keysigner.HexKeySigner

// NewHexKeySigner parses a hex-encoded ECDSA private key (with or without
// a 0x prefix) into a KeySigner.
func NewHexKeySigner(privateKeyHex string) (*HexKeySigner, error) {
	return keysigner.NewHexKeySigner(privateKeyHex)
}

// PKCS11KeySigner is a KeySigner backed by a PKCS#11 token (e.g. SoftHSM,
// AWS CloudHSM).
type PKCS11KeySigner = keysigner.PKCS11KeySigner

// PKCS11Params configures a PKCS11KeySigner.
type PKCS11Params = keysigner.PKCS11Params

// NewPKCS11KeySigner opens a PKCS#11 session and returns a KeySigner backed
// by the token key identified by params.
func NewPKCS11KeySigner(params PKCS11Params) (*PKCS11KeySigner, error) {
	return keysigner.NewPKCS11KeySigner(params)
}

// EVMSigner wraps a KeySigner with EVM transaction signing.
type EVMSigner = evm.Signer

// NewEVMSigner wraps ks for EVM transaction signing.
func NewEVMSigner(ks KeySigner) EVMSigner {
	return evm.New(ks)
}
