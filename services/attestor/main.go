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
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Init(configPath)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	if err := logger.Init(cfg.Logging.Level); err != nil {
		logger.Warnf("Invalid log level %s, using default: %v", cfg.Logging.Level, err)
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

	// Wait for shutdown signal or service error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logFields := map[string]interface{}{
		"symbols":            cfg.Attestor.Symbols,
		"oracle":             cfg.Oracle.Address,
		"oracle_client_type": cfg.Oracle.ClientType.String(),
		"registry":           cfg.Registry.Address,
		"polling_time":       cfg.Attestor.PollingTime.String(),
		"batch_mode":         cfg.Attestor.BatchMode,
		"mode":               cfg.Attestor.Mode.String(),
	}

	if cfg.Attestor.Mode == config.ModeReplica {
		logFields["replica_backup_delay"] = fmt.Sprintf("%ds", cfg.Attestor.ReplicaBackupDelay)
	}

	logger.WithFields(logFields).Info("Attestor service started")

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
	if oracleClient, ok := deps.oracle.(*client.GuardedOracleClient); ok {
		oracleClient.Close()
	}
	if deps.registry != nil {
		deps.registry.Close()
	}
	if deps.signer != nil {
		deps.signer.Close()
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
	var oracleClient interfaces.OracleReader
	var err error

	switch cfg.Oracle.ClientType {
	case config.OracleClientTypeGuarded:
		oracleClient, err = client.NewGuardedOracleClient(
			cfg.RPC.URLs,
			cfg.Oracle.Address,
			"",
			cfg.Attestor.PrivateKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create guarded oracle client: %w", err)
		}
		logger.WithField("client_type", "guarded").Info("Using GuardedOracleClient")

	case config.OracleClientTypeDIAV2:
		oracleClient, err = client.NewDIAOracleV2Client(
			cfg.RPC.URLs,
			cfg.Oracle.Address,
			"",
			cfg.Attestor.PrivateKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create DIAOracleV2 client: %w", err)
		}
		logger.WithField("client_type", "dia_v2").Info("Using DIAOracleV2Client")

	default:
		return nil, fmt.Errorf("unsupported oracle client type: %s", cfg.Oracle.ClientType)
	}

	// Create registry client
	registryClient, err := registry.NewClient(
		cfg.Attestor.PrivateKey,
		cfg.Registry.Address,
		cfg.RPC.RegistryURLs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create signer
	eip712Signer, err := signer.NewEIP712Signer(cfg.Attestor.PrivateKey, cfg.RPC.URLs)
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
