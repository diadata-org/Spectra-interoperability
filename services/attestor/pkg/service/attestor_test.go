package service

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
)

// Mock implementations

type mockOracleReader struct {
	values map[string]*interfaces.OracleValue
	err    error
}

func (m *mockOracleReader) GetGuardedValue(ctx context.Context, symbol string, params config.GuardianParams) (*big.Int, *big.Int, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if val, ok := m.values[symbol]; ok {
		return val.Price, val.Timestamp, nil
	}
	return nil, nil, errors.New("value not found")
}

type mockRegistryClient struct {
	publishErr        error
	txHash            string
	latestIntent      *interfaces.LatestIntent
	latestIntentError error
}

func (m *mockRegistryClient) PublishIntent(ctx context.Context, signedIntent []byte) (string, error) {
	if m.publishErr != nil {
		return "", m.publishErr
	}
	return m.txHash, nil
}

func (m *mockRegistryClient) PublishBatchIntents(ctx context.Context, signedIntents []byte) (string, error) {
	if m.publishErr != nil {
		return "", m.publishErr
	}
	return m.txHash, nil
}

func (m *mockRegistryClient) GetLatestIntentByType(ctx context.Context, intentType, symbol string) (*interfaces.LatestIntent, error) {
	if m.latestIntentError != nil {
		return nil, m.latestIntentError
	}
	if m.latestIntent != nil {
		return m.latestIntent, nil
	}
	return nil, errors.New("intent not found")
}

type mockIntentSigner struct {
	signErr   error
	signature []byte
}

func (m *mockIntentSigner) SignIntent(ctx context.Context, price, volume *big.Int, symbol string) ([]byte, error) {
	if m.signErr != nil {
		return nil, m.signErr
	}
	return m.signature, nil
}

func (m *mockIntentSigner) SignBatchIntent(ctx context.Context, values []interfaces.SymbolData) ([]byte, error) {
	if m.signErr != nil {
		return nil, m.signErr
	}
	return m.signature, nil
}

type mockMetricsCollector struct {
	intentsCreated   map[string]int
	intentsPublished map[string]int
}

func newMockMetricsCollector() *mockMetricsCollector {
	return &mockMetricsCollector{
		intentsCreated:   make(map[string]int),
		intentsPublished: make(map[string]int),
	}
}

func (m *mockMetricsCollector) RecordIntentCreated(symbol string, success bool) {
	key := symbol
	if !success {
		key += "_error"
	}
	m.intentsCreated[key]++
}

func (m *mockMetricsCollector) RecordIntentPublished(symbol string, success bool) {
	key := symbol
	if !success {
		key += "_error"
	}
	m.intentsPublished[key]++
}

func (m *mockMetricsCollector) RecordProcessingDuration(symbol string, mode string, duration time.Duration) {
}
func (m *mockMetricsCollector) RecordOracleFetchDuration(symbol string, duration time.Duration) {}

// Tests

func TestNewAttestorService(t *testing.T) {
	cfg := &config.Config{}
	oracle := &mockOracleReader{}
	registry := &mockRegistryClient{}
	signer := &mockIntentSigner{}
	metrics := newMockMetricsCollector()

	service := NewAttestorService(cfg, oracle, registry, signer, metrics)

	if service == nil {
		t.Fatal("Expected service to be created")
	}
	if service.config != cfg {
		t.Error("Expected config to be set")
	}
	if service.oracle != oracle {
		t.Error("Expected oracle to be set")
	}
	if service.registry != registry {
		t.Error("Expected registry to be set")
	}
	if service.signer != signer {
		t.Error("Expected signer to be set")
	}
	if service.metrics != metrics {
		t.Error("Expected metrics to be set")
	}
	if service.running {
		t.Error("Expected service to not be running initially")
	}
}

func TestAttestorService_StartStop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Attestor.Symbols = []string{"BTC/USD"}
	cfg.Attestor.PollingTime = 100 * time.Millisecond
	cfg.Attestor.BatchMode = false
	cfg.Attestor.Guardian.Default = config.GuardianParams{
		MaxDeviationBips:   500,
		MaxTimestampAge:    3600,
		MinGuardianMatches: 1,
	}

	oracle := &mockOracleReader{
		values: map[string]*interfaces.OracleValue{
			"BTC/USD": {
				Price:     big.NewInt(50000),
				Timestamp: big.NewInt(time.Now().Unix()),
			},
		},
	}
	registry := &mockRegistryClient{txHash: "0xabc123"}
	signer := &mockIntentSigner{signature: []byte("signature")}
	metrics := newMockMetricsCollector()

	service := NewAttestorService(cfg, oracle, registry, signer, metrics)

	// Start service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := service.Start(ctx); err != nil {
			t.Errorf("Failed to start service: %v", err)
		}
	}()

	// Wait for service to start
	time.Sleep(50 * time.Millisecond)

	if !service.IsRunning() {
		t.Error("Expected service to be running")
	}

	// Try starting again (should fail)
	err := service.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running service")
	}

	// Stop service
	if err := service.Stop(); err != nil {
		t.Errorf("Failed to stop service: %v", err)
	}

	if service.IsRunning() {
		t.Error("Expected service to not be running after stop")
	}

	// Try stopping again (should fail)
	if err := service.Stop(); err == nil {
		t.Error("Expected error when stopping already stopped service")
	}
}

func TestAttestorService_ProcessSingleAttestation(t *testing.T) {
	tests := []struct {
		name          string
		oracleErr     error
		signerErr     error
		registryErr   error
		expectSuccess bool
	}{
		{
			name:          "successful attestation",
			expectSuccess: true,
		},
		{
			name:          "oracle error",
			oracleErr:     errors.New("oracle error"),
			expectSuccess: false,
		},
		{
			name:          "signer error",
			signerErr:     errors.New("signer error"),
			expectSuccess: false,
		},
		{
			name:          "registry error",
			registryErr:   errors.New("registry error"),
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Attestor.Symbols = []string{"BTC/USD"}
			cfg.Attestor.Guardian.Default = config.GuardianParams{
				MaxDeviationBips:   500,
				MaxTimestampAge:    3600,
				MinGuardianMatches: 1,
			}

			oracle := &mockOracleReader{
				values: map[string]*interfaces.OracleValue{
					"BTC/USD": {
						Price:     big.NewInt(50000),
						Timestamp: big.NewInt(time.Now().Unix()),
					},
				},
				err: tt.oracleErr,
			}
			registry := &mockRegistryClient{
				txHash:     "0xabc123",
				publishErr: tt.registryErr,
			}
			signer := &mockIntentSigner{
				signature: []byte("signature"),
				signErr:   tt.signerErr,
			}
			metrics := newMockMetricsCollector()

			service := NewAttestorService(cfg, oracle, registry, signer, metrics)

			ctx := context.Background()
			err := service.processSingleAttestation(ctx, "BTC/USD")

			if tt.expectSuccess {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
				if metrics.intentsCreated["BTC/USD"] != 1 {
					t.Error("Expected intent created metric to be recorded")
				}
				if metrics.intentsPublished["BTC/USD"] != 1 {
					t.Error("Expected intent published metric to be recorded")
				}
			} else {
				if err == nil {
					t.Error("Expected error but got success")
				}
			}
		})
	}
}

func TestAttestorService_ProcessBatchAttestation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Attestor.Symbols = []string{"BTC/USD", "ETH/USD"}
	cfg.Attestor.BatchMode = true
	cfg.Attestor.Guardian.Default = config.GuardianParams{
		MaxDeviationBips:   500,
		MaxTimestampAge:    3600,
		MinGuardianMatches: 1,
	}

	oracle := &mockOracleReader{
		values: map[string]*interfaces.OracleValue{
			"BTC/USD": {
				Price:     big.NewInt(50000),
				Timestamp: big.NewInt(time.Now().Unix()),
			},
			"ETH/USD": {
				Price:     big.NewInt(3000),
				Timestamp: big.NewInt(time.Now().Unix()),
			},
		},
	}
	registry := &mockRegistryClient{txHash: "0xabc123"}
	signer := &mockIntentSigner{signature: []byte("batch_signature")}
	metrics := newMockMetricsCollector()

	service := NewAttestorService(cfg, oracle, registry, signer, metrics)

	ctx := context.Background()
	err := service.processBatchAttestation(ctx)

	if err != nil {
		t.Errorf("Expected success but got error: %v", err)
	}

	// Check metrics
	if metrics.intentsCreated["BTC/USD"] != 1 {
		t.Error("Expected BTC/USD intent created metric to be recorded")
	}
	if metrics.intentsCreated["ETH/USD"] != 1 {
		t.Error("Expected ETH/USD intent created metric to be recorded")
	}
	if metrics.intentsPublished["BTC/USD"] != 1 {
		t.Error("Expected BTC/USD intent published metric to be recorded")
	}
	if metrics.intentsPublished["ETH/USD"] != 1 {
		t.Error("Expected ETH/USD intent published metric to be recorded")
	}
}

func TestAttestorService_ProcessBatchAttestation_NoValidData(t *testing.T) {
	cfg := &config.Config{}
	cfg.Attestor.Symbols = []string{"BTC/USD", "ETH/USD"}
	cfg.Attestor.BatchMode = true
	cfg.Attestor.Guardian.Default = config.GuardianParams{
		MaxDeviationBips:   500,
		MaxTimestampAge:    3600,
		MinGuardianMatches: 1,
	}

	// Oracle returns errors for all symbols
	oracle := &mockOracleReader{
		err: errors.New("oracle unavailable"),
	}
	registry := &mockRegistryClient{txHash: "0xabc123"}
	signer := &mockIntentSigner{signature: []byte("batch_signature")}
	metrics := newMockMetricsCollector()

	service := NewAttestorService(cfg, oracle, registry, signer, metrics)

	ctx := context.Background()
	err := service.processBatchAttestation(ctx)

	if err == nil {
		t.Error("Expected error when no valid data collected")
	}

	// Check that error metrics were recorded
	if metrics.intentsCreated["BTC/USD_error"] != 1 {
		t.Error("Expected BTC/USD error metric to be recorded")
	}
	if metrics.intentsCreated["ETH/USD_error"] != 1 {
		t.Error("Expected ETH/USD error metric to be recorded")
	}
}

func TestAttestorService_Health(t *testing.T) {
	cfg := &config.Config{}
	cfg.Attestor.Symbols = []string{"BTC/USD", "ETH/USD"}
	cfg.Attestor.BatchMode = true
	cfg.Attestor.PollingTime = 5 * time.Minute
	cfg.Attestor.Guardian.Default = config.GuardianParams{
		MaxDeviationBips:   500,
		MaxTimestampAge:    3600,
		MinGuardianMatches: 1,
	}

	service := NewAttestorService(cfg, nil, nil, nil, nil)

	health := service.Health()

	running, ok := health["running"].(bool)
	if !ok || running {
		t.Error("Expected running to be false")
	}

	configHealth, ok := health["config"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected config in health check")
	}

	symbols, ok := configHealth["symbols"].([]string)
	if !ok || len(symbols) != 2 {
		t.Error("Expected symbols in health check")
	}

	batchMode, ok := configHealth["batch_mode"].(bool)
	if !ok || !batchMode {
		t.Error("Expected batch_mode to be true")
	}

	pollingTime, ok := configHealth["polling_time"].(string)
	if !ok || pollingTime != "5m0s" {
		t.Error("Expected correct polling_time")
	}
}
