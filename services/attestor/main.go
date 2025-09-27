package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/api"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/client"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/config"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/interfaces"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/registry"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/service"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/signer"
	"github.com/diadata.org/Spectra-interoperability/services/attestor/pkg/utils"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Init(configPath)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	if err := logger.Init(cfg.Logging.Level); err != nil {
		logger.Warnf("Invalid log level %s, using default: %v", cfg.Logging.Level, err)
	}

	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		logger.Fatalf("Invalid configuration: %v", err)
	}

	// Create dependencies
	deps, err := createDependencies(cfg)
	if err != nil {
		logger.Fatalf("Failed to create dependencies: %v", err)
	}

	// Create attestor service
	attestorService := service.NewAttestorService(
		cfg,
		deps.oracle,
		deps.registry,
		deps.signer,
		deps.metrics,
	)

	// Create API server
	apiServer := api.NewServer(cfg, attestorService)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metricsServer := metrics.NewMetricsServer(cfg.Metrics.Port)

	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// Start metrics server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := metricsServer.Start(); err != nil {
			errCh <- fmt.Errorf("metrics server error: %w", err)
		}
	}()

	// Start API server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := apiServer.Start(); err != nil {
			errCh <- fmt.Errorf("API server error: %w", err)
		}
	}()

	// Start attestor service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := attestorService.Start(ctx); err != nil {
			errCh <- fmt.Errorf("attestor service error: %w", err)
		}
	}()

	// Create .env.example if needed
	if err := utils.CreateEnvTemplate(); err != nil {
		logger.Warnf("Failed to create .env.example file: %v", err)
	}

	// Wait for shutdown signal or service error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.WithFields(map[string]interface{}{
		"symbols":      cfg.Attestor.Symbols,
		"oracle":       cfg.Oracle.Address,
		"registry":     cfg.Registry.Address,
		"polling_time": cfg.Attestor.PollingTime.String(),
		"batch_mode":   cfg.Attestor.BatchMode,
	}).Info("Attestor service started")

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		logger.WithField("signal", sig).Info("Received shutdown signal, exiting gracefully...")
	case err := <-errCh:
		logger.WithError(err).Error("Service error occurred, shutting down...")
	}

	// Cancel context to stop services
	cancel()

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop services gracefully with timeout
	logger.Info("Stopping services...")

	if err := attestorService.Stop(); err != nil {
		logger.Errorf("Error stopping attestor service: %v", err)
	}

	if err := apiServer.Stop(); err != nil {
		logger.Errorf("Error stopping API server: %v", err)
	}

	if err := metricsServer.Stop(shutdownCtx); err != nil {
		logger.Errorf("Error stopping metrics server: %v", err)
	}

	// Close client connections
	if oracleClient, ok := deps.oracle.(*client.OracleClient); ok {
		oracleClient.Close()
	}
	if deps.registry != nil {
		deps.registry.Close()
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All services stopped gracefully")
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded, forcing exit")
	}

	logger.Info("Shutdown complete")
}

// dependencies holds all the service dependencies
type dependencies struct {
	oracle   interfaces.OracleReader
	registry *registry.Client
	signer   *signer.EIP712Signer
	metrics  interfaces.MetricsCollector
}

// createDependencies creates all the service dependencies
func createDependencies(cfg *config.Config) (*dependencies, error) {
	// Create oracle client
	oracleClient, err := client.NewOracleClient(
		cfg.RPC.URLs,
		cfg.Oracle.Address,
		"", // signed address not used in new architecture
		cfg.Attestor.PrivateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create oracle client: %w", err)
	}

	// Create registry client
	registryClient, err := registry.NewClient(
		cfg.Attestor.PrivateKey,
		cfg.Registry.Address,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create signer
	eip712Signer, err := signer.NewEIP712Signer(cfg.Attestor.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	// Create metrics collector
	metricsCollector := metrics.NewPrometheusCollector()

	return &dependencies{
		oracle:   oracleClient,
		registry: registryClient,
		signer:   eip712Signer,
		metrics:  metricsCollector,
	}, nil
}

// validateConfig validates the configuration
func validateConfig(cfg *config.Config) error {
	if cfg.Attestor.PrivateKey == "" {
		return fmt.Errorf("private key not configured")
	}

	if cfg.Oracle.Address == "" {
		return fmt.Errorf("oracle address not configured")
	}

	if cfg.Registry.Address == "" {
		return fmt.Errorf("registry address not configured")
	}

	if len(cfg.Attestor.Symbols) == 0 {
		return fmt.Errorf("no symbols configured")
	}

	if cfg.Attestor.PollingTime <= 0 {
		return fmt.Errorf("invalid polling time: %v", cfg.Attestor.PollingTime)
	}

	return nil
}
