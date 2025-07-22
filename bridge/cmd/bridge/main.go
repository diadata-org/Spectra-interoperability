package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/diadata.org/Spectra-interoperability/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/api"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/bridge"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/health"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/metrics"
	"github.com/sirupsen/logrus"
)

var (
	configPath = flag.String("config", "config.json", "Path to configuration file")
	logLevel   = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Configure logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logger.WithError(err).Fatal("Invalid log level")
	}
	logger.SetLevel(level)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	// Initialize database
	db, err := database.NewDB(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize database")
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		logger.WithError(err).Fatal("Failed to run database migrations")
	}

	// Initialize metrics collector
	var metricsCollector *metrics.Collector
	if cfg.Metrics.Enabled {
		metricsCollector = metrics.NewCollector()
		logger.Info("Metrics collection enabled")
	}

	// Create bridge instance
	bridgeService, err := bridge.NewBridge(cfg, db, metricsCollector)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create bridge")
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create Ethereum clients for health monitoring
	sourceClient, err := ethclient.Dial(cfg.Source.RPCURL)
	if err != nil {
		logger.WithError(err).Fatal("Failed to connect to source chain")
	}
	defer sourceClient.Close()

	destClients := make(map[int64]*ethclient.Client)
	for _, destConfig := range cfg.GetEnabledDestinations() {
		client, err := ethclient.Dial(destConfig.RPCURL)
		if err != nil {
			logger.WithError(err).Errorf("Failed to connect to destination chain %d", destConfig.ChainID)
			continue
		}
		destClients[destConfig.ChainID] = client
		defer client.Close()
	}

	// Create health monitor
	healthMonitor := health.NewHealthMonitor(&cfg.HealthCheck, db, sourceClient, destClients)

	// Start API server if enabled
	var apiServer *api.Server
	if cfg.API.Enabled {
		apiServer = api.NewServer(&cfg.API, db, healthMonitor, metricsCollector)
		if err := apiServer.Start(ctx); err != nil {
			logger.WithError(err).Fatal("Failed to start API server")
		}
		logger.WithField("address", cfg.API.ListenAddr).Info("API server started")
	}

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Start the bridge
	logger.Info("Starting Oracle Bridge")
	if err := bridgeService.Start(ctx); err != nil {
		logger.WithError(err).Fatal("Bridge failed")
	}

	// Wait for shutdown
	<-ctx.Done()
	logger.Info("Shutting down...")

	// Give components time to cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := bridgeService.Stop(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during bridge shutdown")
	}

	// Stop API server if running
	if apiServer != nil {
		if err := apiServer.Stop(); err != nil {
			logger.WithError(err).Error("Error during API server shutdown")
		}
	}

	logger.Info("Bridge stopped")
}