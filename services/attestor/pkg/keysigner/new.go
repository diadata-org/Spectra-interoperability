package keysigner

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
)

// New constructs a KeySigner from configuration, branching on
// cfg.Attestor.SigningBackend.
func New(cfg *config.Config) (KeySigner, error) {
	switch cfg.Attestor.SigningBackend {
	case config.SigningBackendPKCS11:
		return newPKCS11KeySignerFromConfig(cfg)
	case config.SigningBackendPrivateKey, "":
		return NewHexKeySigner(cfg.Attestor.PrivateKey)
	default:
		return nil, fmt.Errorf("unsupported signing backend: %s", cfg.Attestor.SigningBackend)
	}
}

// newPKCS11KeySignerFromConfig constructs a PKCS11KeySigner (shared module
// implementation) from cfg.Attestor.PKCS11, reading the token PIN from
// config.PKCS11PINEnvVar (the attestor's own PIN variable, kept for
// deployment compatibility; the shared module's PKCS11_PIN convention is
// not used here so config validation stays the single source of truth).
func newPKCS11KeySignerFromConfig(cfg *config.Config) (*PKCS11KeySigner, error) {
	pin := os.Getenv(config.PKCS11PINEnvVar)
	if pin == "" {
		return nil, fmt.Errorf("pkcs11: %s environment variable is required", config.PKCS11PINEnvVar)
	}

	p11cfg := cfg.Attestor.PKCS11

	var keyID []byte
	if p11cfg.KeyID != "" {
		var err error
		keyID, err = hex.DecodeString(p11cfg.KeyID)
		if err != nil {
			return nil, fmt.Errorf("pkcs11: failed to decode key_id as hex: %w", err)
		}
	}

	return NewPKCS11KeySigner(PKCS11Params{
		ModulePath:     p11cfg.ModulePath,
		TokenLabel:     p11cfg.TokenLabel,
		SlotNumber:     p11cfg.SlotNumber,
		Pin:            pin,
		KeyLabel:       p11cfg.KeyLabel,
		PublicKeyLabel: p11cfg.PublicKeyLabel,
		KeyID:          keyID,
	})
}
