package arch

import (
	"encoding/hex"
	"errors"
	"fmt"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
)

// Signer holds a secp256k1 secret key and produces BIP-340 Schnorr signatures
// suitable for Arch's Taproot key-path tx signing.
type Signer struct {
	secret *secp256k1.PrivateKey
}

// NewSignerFromHex loads a 32-byte secret key from a 64-char hex string.
func NewSignerFromHex(secretHex string) (*Signer, error) {
	if len(secretHex) != 64 {
		return nil, fmt.Errorf("secret hex must be 64 chars, got %d", len(secretHex))
	}
	raw, err := hex.DecodeString(secretHex)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	if len(raw) != 32 {
		return nil, errors.New("secret must be 32 bytes")
	}
	priv := secp256k1.PrivKeyFromBytes(raw)
	return &Signer{secret: priv}, nil
}

// Pubkey returns the x-only public key (32 bytes). This is the form the
// Arch runtime stores in account-list Pubkey slots.
func (s *Signer) Pubkey() Pubkey {
	pub := s.secret.PubKey()
	// SerializeCompressed returns 33 bytes: prefix (0x02 or 0x03) + x.
	compressed := pub.SerializeCompressed()
	var out Pubkey
	copy(out[:], compressed[1:])
	return out
}

// SignDigest produces a 64-byte BIP-340 Schnorr signature (R || s) over digest.
func (s *Signer) SignDigest(digest [32]byte) ([64]byte, error) {
	sig, err := schnorr.Sign(s.secret, digest[:])
	if err != nil {
		return [64]byte{}, fmt.Errorf("schnorr sign: %w", err)
	}
	var out [64]byte
	copy(out[:], sig.Serialize())
	return out, nil
}
