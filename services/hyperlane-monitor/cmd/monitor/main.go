package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/config"
	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/internal/database"
	"github.com/diadata.org/Spectra-interoperability/services/hyperlane-monitor/internal/monitor"
	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
)

func main() {
	var (
		configPath = flag.String("config", "config/config.json", "Path to configuration file")
		migrate    = flag.Bool("migrate", false, "Run database migrations")
		debug      = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	// Enable debug logging if requested
	if *debug || os.Getenv("DEBUG") == "true" {
		logger.SetLevel("debug")
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	db, err := database.NewRepository(cfg.Database.DSN)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if *migrate {
		logger.Info("Running database migrations...")
		if err := db.RunMigrations(); err != nil {
			logger.Fatalf("Failed to run migrations: %v", err)
		}
		logger.Info("Migrations completed successfully")
		os.Exit(0)
	}

	service, err := monitor.NewService(cfg, db)
	if err != nil {
		logger.Fatalf("Failed to create monitoring service: %v", err)
	}

	metricsPort := cfg.MetricsPort
	if metricsPort == 0 {
		metricsPort = 9091
	}
	
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infof("Received signal: %v", sig)
		cancel()
	}()

	if err := service.Start(ctx); err != nil {
		logger.Fatalf("Failed to start service: %v", err)
	}

	<-ctx.Done()
	logger.Info("Shutting down...")

	if err := metricsServer.Shutdown(context.Background()); err != nil {
		logger.Errorf("Failed to shutdown metrics server: %v", err)
	}

	service.Stop()
	logger.Info("Shutdown complete")
}