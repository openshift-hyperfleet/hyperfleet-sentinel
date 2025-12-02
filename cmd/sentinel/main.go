package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newServeCommand creates the Cobra "serve" command which starts the Sentinel service.
// The command accepts a --config / -c flag to specify a YAML configuration file, loads
// configuration using config.LoadConfig, and invokes runServe with the resulting config.
func newServeCommand() *cobra.Command {
	var configFile string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Sentinel service",
		Long:  `Start the HyperFleet Sentinel service with the specified configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load and validate configuration from YAML and env vars
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return err
			}
			return runServe(cfg)
		},
	}

	// Add --config flag for YAML file path
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to configuration file (YAML)")

	return cmd
}

// runServe bootstraps and runs the Sentinel service, including metrics/health HTTP server, broker publisher, HyperFleet client, decision engine, and graceful shutdown handling.
// cfg is the loaded sentinel configuration used to initialize service components.
// It returns an error when initialization fails or if the sentinel loop exits with an unexpected error; it returns nil when the service stops gracefully.
func runServe(cfg *config.SentinelConfig) error {
	// Initialize context and logger
	ctx := context.Background()
	log := logger.NewHyperFleetLogger()

	log.Infof(ctx, "Starting HyperFleet Sentinel version=%s commit=%s", version, commit)

	// Initialize Prometheus metrics registry
	registry := prometheus.NewRegistry()
	// Register metrics once (uses sync.Once internally)
	metrics.NewSentinelMetrics(registry)

	// Initialize components
	hyperfleetClient := client.NewHyperFleetClient(cfg.HyperFleetAPI.Endpoint, cfg.HyperFleetAPI.Timeout)
	decisionEngine := engine.NewDecisionEngine(cfg.MaxAgeNotReady, cfg.MaxAgeReady)

	// Initialize publisher using hyperfleet-broker library
	// Configuration is loaded from broker.yaml or BROKER_CONFIG_FILE env var
	pub, err := broker.NewPublisher()
	if err != nil {
		return fmt.Errorf("failed to initialize broker publisher: %w", err)
	}
	if pub != nil {
		defer func() {
			if err := pub.Close(); err != nil {
				log.Infof(ctx, "Error closing publisher: %v", err)
			}
		}()
	}
	log.Info(ctx, "Initialized broker publisher")

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize sentinel
	s := sentinel.NewSentinel(cfg, hyperfleetClient, decisionEngine, pub, log)

	// Start metrics and health HTTP server
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			log.Infof(r.Context(), "Error writing health response: %v", err)
		}
	})

	// Metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	metricsServer := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start HTTP server in background
	go func() {
		log.Info(ctx, "Starting metrics server on :8080")
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Infof(ctx, "Metrics server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info(ctx, "Received shutdown signal")
		cancel()

		// Shutdown metrics server
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Infof(shutdownCtx, "Metrics server shutdown error: %v", err)
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