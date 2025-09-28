package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
		configPath = flag.String("config", "config.json", "Path to configuration file")
		logLevel   = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	)
	flag.Parse()

	// Initialize logger
	logger.Init(*logLevel)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Log if using environment variable for private key
	if os.Getenv("BRIDGE_PRIVATE_KEY") != "" {
		log.Printf("Using private key from BRIDGE_PRIVATE_KEY environment variable")
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
		log.Printf("Starting metrics server on port %d", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Create database connection
	db, err := database.NewDB(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Create bridge service
	bridgeService, err := bridge.NewBridge(cfg, db, metricsCollector)
	if err != nil {
		log.Fatalf("Failed to create bridge service: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start bridge service
	startTime := time.Now()
	log.Printf("Starting DIA Oracle Bridge Service...")
	log.Printf("Source chain: %s (Chain ID: %d)", cfg.Source.Name, cfg.Source.ChainID)
	log.Printf("Monitoring %d destination chains", len(cfg.Destinations))

	// Start monitoring in a goroutine
	go func() {
		if err := bridgeService.Start(ctx); err != nil {
			log.Printf("Bridge service error: %v", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		uptime := utils.GetUptimeStringVerbose(startTime)
		log.Printf("Received signal %v, shutting down gracefully... (uptime: %s)", sig, uptime)
	case <-ctx.Done():
		uptime := utils.GetUptimeStringVerbose(startTime)
		log.Printf("Context cancelled, shutting down... (uptime: %s)", uptime)
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	log.Printf("Stopping bridge service...")
	if err := bridgeService.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	// Shutdown metrics server
	log.Printf("Stopping metrics server...")
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Failed to shutdown metrics server: %v", err)
	}

	totalUptime := utils.GetUptimeStringVerbose(startTime)
	log.Printf("Bridge service stopped successfully (total uptime: %s)", totalUptime)
}
