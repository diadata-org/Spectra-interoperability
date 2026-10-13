package config

import (
	"os"
	"testing"
)

func TestParseSigningBackend(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SigningBackend
		wantErr bool
	}{
		{name: "privatekey", input: "privatekey", want: SigningBackendPrivateKey},
		{name: "pkcs11", input: "pkcs11", want: SigningBackendPKCS11},
		{name: "invalid", input: "kms", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSigningBackend(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSigningBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSigningBackend() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newValidTestConfig() *Config {
	cfg := &Config{}
	cfg.Attestor.SigningBackend = SigningBackendPrivateKey
	cfg.Attestor.PrivateKey = "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	cfg.Attestor.Symbols = []string{"BTC/USD"}
	cfg.Attestor.PollingTime = 300_000_000 // 300ms in nanoseconds
	cfg.Attestor.DeviationThreshold = 50
	cfg.Attestor.ForceUpdateInterval = 0
	cfg.Attestor.Guardian.Default = GuardianParams{
		MaxDeviationBips:   500,
		MaxTimestampAge:    3600,
		MinGuardianMatches: 1,
	}
	cfg.Oracle.Address = "0x1234567890abcdef1234567890abcdef12345678"
	cfg.Oracle.ClientType = OracleClientTypeGuarded
	cfg.Registry.Address = "0x1234567890abcdef1234567890abcdef12345678"
	return cfg
}

func TestValidateSigningBackend_PrivateKey(t *testing.T) {
	cfg := newValidTestConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected valid privatekey config to pass, got: %v", err)
	}
}

func TestValidateSigningBackend_PrivateKeyMissing(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when private_key is empty and backend is privatekey")
	}
}

func TestValidateSigningBackend_PKCS11MutuallyExclusiveWithPrivateKey(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.ModulePath = "/usr/lib/softhsm/libsofthsm2.so"
	cfg.Attestor.PKCS11.TokenLabel = "test"
	cfg.Attestor.PKCS11.KeyLabel = "test-key"
	// cfg.Attestor.PrivateKey is still set from newValidTestConfig(), which
	// should be rejected as mutually exclusive with the pkcs11 backend.

	t.Setenv(PKCS11PINEnvVar, "1234")

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when both private_key and signing_backend=pkcs11 are set")
	}
}

func TestValidateSigningBackend_PKCS11Valid(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.ModulePath = "/usr/lib/softhsm/libsofthsm2.so"
	cfg.Attestor.PKCS11.TokenLabel = "test"
	cfg.Attestor.PKCS11.KeyLabel = "test-key"

	t.Setenv(PKCS11PINEnvVar, "1234")

	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected valid pkcs11 config to pass, got: %v", err)
	}
}

func TestValidateSigningBackend_PKCS11MissingPIN(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.ModulePath = "/usr/lib/softhsm/libsofthsm2.so"
	cfg.Attestor.PKCS11.TokenLabel = "test"
	cfg.Attestor.PKCS11.KeyLabel = "test-key"

	os.Unsetenv(PKCS11PINEnvVar)

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when PKCS11 PIN env var is not set")
	}
}

func TestValidateSigningBackend_PKCS11MissingModulePath(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.TokenLabel = "test"
	cfg.Attestor.PKCS11.KeyLabel = "test-key"

	t.Setenv(PKCS11PINEnvVar, "1234")

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when module_path is missing")
	}
}

func TestValidateSigningBackend_PKCS11TokenLabelAndSlotMutuallyExclusive(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.ModulePath = "/usr/lib/softhsm/libsofthsm2.so"
	cfg.Attestor.PKCS11.TokenLabel = "test"
	slot := 0
	cfg.Attestor.PKCS11.SlotNumber = &slot
	cfg.Attestor.PKCS11.KeyLabel = "test-key"

	t.Setenv(PKCS11PINEnvVar, "1234")

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when both token_label and slot_number are set")
	}
}

func TestValidateSigningBackend_PKCS11MissingKeyIdentifier(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.PrivateKey = ""
	cfg.Attestor.SigningBackend = SigningBackendPKCS11
	cfg.Attestor.PKCS11.ModulePath = "/usr/lib/softhsm/libsofthsm2.so"
	cfg.Attestor.PKCS11.TokenLabel = "test"

	t.Setenv(PKCS11PINEnvVar, "1234")

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error when neither key_label nor key_id is set")
	}
}

func TestValidateSigningBackend_InvalidBackend(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Attestor.SigningBackend = SigningBackend("kms")

	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for unsupported signing backend")
	}
}
