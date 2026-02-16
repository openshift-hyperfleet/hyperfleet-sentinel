package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/health"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/sentinel"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "sentinel",
		Short: "HyperFleet Sentinel - Resource polling and event publishing service",
		Long: `HyperFleet Sentinel watches HyperFleet API resources and publishes
reconciliation events to a message broker based on configurable max age intervals.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	rootCmd.AddCommand(newServeCommand())

	if err := rootCmd.Execute(); err != nil {
		// Print error to stderr since SilenceErrors is true and logging may not be initialized
		cmd := strings.Join(os.Args[1:], " ")
		if cmd == "" {
			cmd = "(no command)"
		}
		fmt.Fprintf(os.Stderr, "Error executing command 'sentinel %s': %v\n", cmd, err)
		os.Exit(1)
	}
}

func newServeCommand() *cobra.Command {
	var (
		configFile         string
		logLevel           string
		logFormat          string
		logOutput          string
		healthBindAddress  string
		metricsBindAddress string
	)

	cmd := &cobra.Command{
		Use:           "serve",
		Short:         "Start the Sentinel service",
		Long:          `Start the HyperFleet Sentinel service with the specified configuration.`,
		SilenceUsage:  true, // Don't print usage on error
		SilenceErrors: true, // Don't print errors - we handle logging ourselves
		RunE: func(cmd *cobra.Command, args []string) error {
			// Initialize logging configuration
			// Precedence: flags → environment variables → defaults
			logCfg, err := initLogging(logLevel, logFormat, logOutput)
			if err != nil {
				return fmt.Errorf("failed to initialize logging: %w", err)
			}

			// Load and validate configuration from YAML and env vars
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return err
			}
			return runServe(cfg, logCfg, healthBindAddress, metricsBindAddress)
		},
	}

	// Add --config flag for YAML file path
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to configuration file (YAML)")

	// Add logging flags per HyperFleet logging specification
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Log level: debug, info, warn, error (default: info)")
	cmd.Flags().StringVar(&logFormat, "log-format", "", "Log format: text, json (default: text)")
	cmd.Flags().StringVar(&logOutput, "log-output", "", "Log output: stdout, stderr (default: stdout)")

	// Server bind address flags (consistent with hyperfleet-api)
	cmd.Flags().StringVar(&healthBindAddress, "health-server-bindaddress", ":8080", "Health server bind address")
	cmd.Flags().StringVar(&metricsBindAddress, "metrics-server-bindaddress", ":9090", "Metrics server bind address")

	return cmd
}

// getConfigValue returns the flag value if set, otherwise falls back to the environment variable.
// This implements the precedence: flags → environment variables → defaults
func getConfigValue(flag, envVar string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv(envVar)
}

// initLogging initializes the logging configuration following the precedence:
// flags → environment variables → defaults
func initLogging(flagLevel, flagFormat, flagOutput string) (*logger.LogConfig, error) {
	cfg := logger.DefaultConfig()
	cfg.Version = version
	cfg.Component = "sentinel"

	// Apply log level
	if levelStr := getConfigValue(flagLevel, "LOG_LEVEL"); levelStr != "" {
		level, err := logger.ParseLogLevel(levelStr)
		if err != nil {
			return nil, err
		}
		cfg.Level = level
	}

	// Apply log format
	if formatStr := getConfigValue(flagFormat, "LOG_FORMAT"); formatStr != "" {
		format, err := logger.ParseLogFormat(formatStr)
		if err != nil {
			return nil, err
		}
		cfg.Format = format
	}

	// Apply log output
	if outputStr := getConfigValue(flagOutput, "LOG_OUTPUT"); outputStr != "" {
		output, err := logger.ParseLogOutput(outputStr)
		if err != nil {
			return nil, err
		}
		cfg.Output = output
	}

	// Set global config so all loggers use the same configuration
	logger.SetGlobalConfig(cfg)

	return cfg, nil
}

func runServe(cfg *config.SentinelConfig, logCfg *logger.LogConfig, healthBindAddress, metricsBindAddress string) error {
	// Initialize context and logger
	ctx := context.Background()
	log := logger.NewHyperFleetLoggerWithConfig(logCfg)

	log.Extra("commit", commit).
		Extra("log_level", logCfg.Level.String()).
		Extra("log_format", logCfg.Format.String()).
		Info(ctx, "Starting HyperFleet Sentinel")

	// Initialize Prometheus metrics registry
	registry := prometheus.NewRegistry()
	// Register metrics once (uses sync.Once internally)
	metrics.NewSentinelMetrics(registry)

	// Initialize components
	hyperfleetClient, err := client.NewHyperFleetClient(cfg.HyperFleetAPI.Endpoint, cfg.HyperFleetAPI.Timeout)
	if err != nil {
		log.Errorf(ctx, "Failed to initialize OpenAPI client: %v", err)
		return fmt.Errorf("failed to initialize OpenAPI client: %w", err)

	}
	decisionEngine := engine.NewDecisionEngine(cfg.MaxAgeNotReady, cfg.MaxAgeReady)

	// Initialize publisher using hyperfleet-broker library
	// Configuration is loaded from broker.yaml or BROKER_CONFIG_FILE env var
	pub, err := broker.NewPublisher(log)
	if err != nil {
		log.Errorf(ctx, "Failed to initialize broker publisher: %v", err)
		return fmt.Errorf("failed to initialize broker publisher: %w", err)
	}
	if pub != nil {
		defer func() {
			if err := pub.Close(); err != nil {
				log.Errorf(ctx, "Error closing publisher: %v", err)
			}
		}()
	}
	log.Info(ctx, "Initialized broker publisher")

	// Initialize readiness checker with dependency checks.
	// Checks are evaluated on each /readyz request.
	readiness := health.NewReadinessChecker(log)
	readiness.AddCheck("broker", func() error {
		if pub == nil {
			return fmt.Errorf("broker publisher not initialized")
		}
		return pub.Health(ctx)
	})
	readiness.SetReady(true)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize sentinel
	s := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, pub, log)

	// Health server on port 8080 (/healthz, /readyz)
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", readiness.HealthzHandler())
	healthMux.HandleFunc("/readyz", readiness.ReadyzHandler())

	healthServer := &http.Server{
		Addr:         healthBindAddress,
		Handler:      healthMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Metrics server on port 9090 (/metrics)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	metricsServer := &http.Server{
		Addr:         metricsBindAddress,
		Handler:      metricsMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start HTTP servers in background
	go func() {
		log.Infof(ctx, "Starting health server on %s", healthBindAddress)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf(ctx, "Health server error: %v", err)
		}
	}()

	go func() {
		log.Infof(ctx, "Starting metrics server on %s", metricsBindAddress)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf(ctx, "Metrics server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info(ctx, "Received shutdown signal")
		// Set readiness to false so /readyz returns 503 during shutdown
		readiness.SetReady(false)
		cancel()

		// Shutdown HTTP servers (20s timeout per graceful-shutdown standard)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer shutdownCancel()
		if err := healthServer.Shutdown(shutdownCtx); err != nil {
			log.Errorf(shutdownCtx, "Health server shutdown error: %v", err)
		}
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Errorf(shutdownCtx, "Metrics server shutdown error: %v", err)
		}
	}()

	// Start sentinel
	log.Info(ctx, "Starting sentinel loop")
	if err := s.Start(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("sentinel failed: %w", err)
	}

	log.Info(ctx, "Sentinel stopped gracefully")
	return nil
}
