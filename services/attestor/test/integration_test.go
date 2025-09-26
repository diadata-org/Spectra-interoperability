// +build integration

package test

import (
	"context"
	"testing"
	"time"

	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/oracle"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/registry"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/service"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/signer"
)

func TestAttestorServiceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Load test configuration
	cfg := &config.Config{
		RPC: struct {
			URL         string `mapstructure:"url"`
			RegistryURL string `mapstructure:"registry_url"`
		}{
			URL:         "https://testnet-rpc.diadata.org",
			RegistryURL: "https://testnet-rpc.diadata.org",
		},
		Oracle: struct {
			Address string `mapstructure:"address"`
		}{
			Address: "0x0087342f5f4c7AB23a37c045c3EF710749527c88",
		},
		Registry: struct {
			Address string `mapstructure:"address"`
		}{
			Address: "0xd2313dcabB0E9447d800546b953E05dD47EB2eB9",
		},
		Attestor: struct {
			PrivateKey  string        `mapstructure:"private_key"`
			Symbols     []string      `mapstructure:"symbols"`
			PollingTime time.Duration `mapstructure:"polling_time"`
			BatchMode   bool          `mapstructure:"batch_mode"`
		}{
			PrivateKey:  getTestPrivateKey(),
			Symbols:     []string{"BTC/USD"},
			PollingTime: 5 * time.Second,
			BatchMode:   false,
		},
		Logging: struct {
			Level string `mapstructure:"level"`
		}{
			Level: "debug",
		},
		Metrics: struct {
			Port int `mapstructure:"port"`
		}{
			Port: 9090,
		},
		API: struct {
			Port int `mapstructure:"port"`
		}{
			Port: 9091,
		},
	}

	// Create dependencies
	oracleClient, err := client.NewOracleClient(
		cfg.RPC.URL,
		cfg.Oracle.Address,
		"",
		cfg.Attestor.PrivateKey,
	)
	if err != nil {
		t.Fatalf("Failed to create oracle client: %v", err)
	}

	oracleAdapter := oracle.NewClientAdapter(oracleClient)

	registryClient, err := registry.NewClient(
		cfg.Attestor.PrivateKey,
		cfg.RPC.RegistryURL,
		cfg.Registry.Address,
	)
	if err != nil {
		t.Fatalf("Failed to create registry client: %v", err)
	}

	eip712Signer, err := signer.NewEIP712Signer(cfg.Attestor.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	metricsCollector := metrics.NewPrometheusCollector()

	// Create service
	attestorService := service.NewAttestorService(
		cfg,
		oracleAdapter,
		registryClient,
		eip712Signer,
		metricsCollector,
	)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start service
	go func() {
		if err := attestorService.Start(ctx); err != nil {
			t.Errorf("Service error: %v", err)
		}
	}()

	// Wait for service to start
	time.Sleep(2 * time.Second)

	// Check if service is running
	if !attestorService.IsRunning() {
		t.Fatal("Service should be running")
	}

	// Wait for at least one attestation cycle
	time.Sleep(10 * time.Second)

	// Check health
	health := attestorService.Health()
	if !health["running"].(bool) {
		t.Error("Service health check failed")
	}

	// Stop service
	if err := attestorService.Stop(); err != nil {
		t.Errorf("Failed to stop service: %v", err)
	}
}

func TestBatchModeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Similar test but with batch mode enabled
	cfg := &config.Config{
		RPC: struct {
			URL         string `mapstructure:"url"`
			RegistryURL string `mapstructure:"registry_url"`
		}{
			URL:         "https://testnet-rpc.diadata.org",
			RegistryURL: "https://testnet-rpc.diadata.org",
		},
		Oracle: struct {
			Address string `mapstructure:"address"`
		}{
			Address: "0x0087342f5f4c7AB23a37c045c3EF710749527c88",
		},
		Registry: struct {
			Address string `mapstructure:"address"`
		}{
			Address: "0xd2313dcabB0E9447d800546b953E05dD47EB2eB9",
		},
		Attestor: struct {
			PrivateKey  string        `mapstructure:"private_key"`
			Symbols     []string      `mapstructure:"symbols"`
			PollingTime time.Duration `mapstructure:"polling_time"`
			BatchMode   bool          `mapstructure:"batch_mode"`
		}{
			PrivateKey:  getTestPrivateKey(),
			Symbols:     []string{"BTC/USD", "ETH/USD"},
			PollingTime: 5 * time.Second,
			BatchMode:   true,
		},
		Logging: struct {
			Level string `mapstructure:"level"`
		}{
			Level: "debug",
		},
		Metrics: struct {
			Port int `mapstructure:"port"`
		}{
			Port: 9092,
		},
		API: struct {
			Port int `mapstructure:"port"`
		}{
			Port: 9093,
		},
	}

	// Create dependencies
	oracleClient, err := client.NewOracleClient(
		cfg.RPC.URL,
		cfg.Oracle.Address,
		"",
		cfg.Attestor.PrivateKey,
	)
	if err != nil {
		t.Fatalf("Failed to create oracle client: %v", err)
	}

	oracleAdapter := oracle.NewClientAdapter(oracleClient)

	registryClient, err := registry.NewClient(
		cfg.Attestor.PrivateKey,
		cfg.RPC.RegistryURL,
		cfg.Registry.Address,
	)
	if err != nil {
		t.Fatalf("Failed to create registry client: %v", err)
	}

	eip712Signer, err := signer.NewEIP712Signer(cfg.Attestor.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	metricsCollector := metrics.NewPrometheusCollector()

	// Create service
	attestorService := service.NewAttestorService(
		cfg,
		oracleAdapter,
		registryClient,
		eip712Signer,
		metricsCollector,
	)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start service
	go func() {
		if err := attestorService.Start(ctx); err != nil {
			t.Errorf("Service error: %v", err)
		}
	}()

	// Wait for service to start
	time.Sleep(2 * time.Second)

	// Check if service is running
	if !attestorService.IsRunning() {
		t.Fatal("Service should be running")
	}

	// Wait for at least one batch attestation cycle
	time.Sleep(10 * time.Second)

	// Stop service
	if err := attestorService.Stop(); err != nil {
		t.Errorf("Failed to stop service: %v", err)
	}
}

// getTestPrivateKey returns a test private key
// WARNING: This is a test key only, never use in production
func getTestPrivateKey() string {
	return "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
}