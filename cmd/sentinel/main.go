package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
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

func runServe(cfg *config.SentinelConfig) error {
	// Initialize logger
	ctx := context.Background()
	log := logger.NewHyperFleetLogger(ctx)

	log.Infof("Starting HyperFleet Sentinel version=%s commit=%s", version, commit)

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
		defer pub.Close()
	}
	log.Info("Initialized broker publisher")

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize sentinel with context for proper cancellation
	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, pub, log)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("Received shutdown signal")
		cancel()
	}()

	// Start sentinel
	log.Info("Starting sentinel loop")
	if err := s.Start(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("sentinel failed: %w", err)
	}

	log.Info("Sentinel stopped gracefully")
	return nil
}
