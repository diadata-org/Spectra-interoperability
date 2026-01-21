package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/metrics"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/utils"
)

func main() {
	// Command line flags
	var (
		configPath = flag.String("config", "config", "Path to configuration file or directory (supports YAML format)")
		logLevel   = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	)
	flag.Parse()

	// Initialize logger
	logger.Init(*logLevel)

	// Load modular configuration
	modularCfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Create configuration service for easy access
	cfgService := config.NewConfigService(modularCfg)

	// Log if using environment variable for private key
	if os.Getenv("BRIDGE_PRIVATE_KEY") != "" {
		logger.Infof("Using private key from BRIDGE_PRIVATE_KEY environment variable")
	}

	// Initialize metrics collector
	metricsCollector := metrics.NewCollector()

	// Start metrics server
	metricsPort := 8081
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", metricsPort),
		Handler: promhttp.Handler(),
	}

	go func() {
		logger.Infof("Starting metrics server on port %d", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("Metrics server error: %v", err)
		}
	}()

	// Create database connection
	db, err := database.NewDB(modularCfg.Infrastructure.Database.Driver, modularCfg.Infrastructure.Database.DSN)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Create bridge service
	bridgeService, err := bridge.NewBridge(modularCfg, cfgService, db, metricsCollector)
	if err != nil {
		logger.Fatalf("Failed to create bridge service: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start bridge service
	startTime := time.Now()
	logger.Infof("Starting DIA Oracle Bridge Service...")
	sourceConfig := cfgService.GetInfrastructure().Source
	logger.Infof("Source chain: %s (Chain ID: %d)", sourceConfig.Name, sourceConfig.ChainID)
	logger.Infof("Monitoring %d destination chains", len(cfgService.GetEnabledChains()))

	// Start bridge service
	if err := bridgeService.Start(ctx); err != nil {
		logger.Fatalf("Failed to start bridge service: %v", err)
	}

	// Start a goroutine to wait for bridge completion
	bridgeDone := make(chan struct{})
	go func() {
		bridgeService.Wait()
		close(bridgeDone)
	}()

	// Wait for shutdown signal or bridge completion
	select {
	case sig := <-sigChan:
		uptime := utils.GetUptimeStringVerbose(startTime)
		logger.Infof("Received signal %v, shutting down gracefully... (uptime: %s)", sig, uptime)
		cancel()
	case <-bridgeDone:
		uptime := utils.GetUptimeStringVerbose(startTime)
		logger.Infof("Bridge service completed, shutting down... (uptime: %s)", uptime)
		cancel()
	}

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	shutdownComplete := make(chan struct{})

	// Set up force shutdown on second signal or timeout
	go func() {
		select {
		case sig := <-sigChan:
			logger.Warnf("Received second signal %v, forcing immediate shutdown!", sig)
			os.Exit(1)
		case <-shutdownCtx.Done():
			logger.Warnf("Graceful shutdown timeout (30s), forcing exit")
			os.Exit(1)
		case <-shutdownComplete:
			return
		}
	}()

	logger.Infof("Stopping bridge service...")
	if err := bridgeService.Stop(shutdownCtx); err != nil {
		logger.Errorf("Error during shutdown: %v", err)
	}

	// Shutdown metrics server
	logger.Infof("Stopping metrics server...")
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("Failed to shutdown metrics server: %v", err)
	}

	totalUptime := utils.GetUptimeStringVerbose(startTime)
	logger.Infof("Bridge service stopped successfully (total uptime: %s)", totalUptime)

	close(shutdownComplete)
}
