//go:build integration

package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
)

// RabbitMQTestContainer manages a RabbitMQ testcontainer for integration testing
type RabbitMQTestContainer struct {
	container *rabbitmq.RabbitMQContainer
	publisher broker.Publisher
}

// NewRabbitMQTestContainer creates and starts a RabbitMQ testcontainer
func NewRabbitMQTestContainer(ctx context.Context) (*RabbitMQTestContainer, error) {
	slog.InfoContext(ctx, "Starting RabbitMQ testcontainer...")

	// Start RabbitMQ container
	container, err := rabbitmq.Run(ctx,
		"rabbitmq:3.13-management-alpine",
		rabbitmq.WithAdminUsername("guest"),
		rabbitmq.WithAdminPassword("guest"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server startup complete").
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start RabbitMQ testcontainer: %w", err)
	}

	// Get AMQP connection URL
	amqpURL, err := container.AmqpURL(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get AMQP URL: %w", err)
	}

	slog.InfoContext(ctx, "RabbitMQ testcontainer started", "amqp_url", amqpURL)

	// Create publisher using hyperfleet-broker library with configMap
	// This allows us to pass configuration programmatically for testing
	configMap := map[string]string{
		"broker.type":         "rabbitmq",
		"broker.rabbitmq.url": amqpURL,
	}

	metricsRecorder := broker.NewMetricsRecorder("sentinel-test", "test", prometheus.NewRegistry())
	publisher, err := broker.NewPublisher(slog.Default(), metricsRecorder, configMap)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create broker publisher: %w", err)
	}

	slog.InfoContext(ctx, "RabbitMQ publisher initialized successfully")

	return &RabbitMQTestContainer{
		container: container,
		publisher: publisher,
	}, nil
}

// Publisher returns the broker publisher connected to the testcontainer
func (tc *RabbitMQTestContainer) Publisher() broker.Publisher {
	return tc.publisher
}

// Close stops the RabbitMQ testcontainer and closes the publisher
func (tc *RabbitMQTestContainer) Close(ctx context.Context) error {
	var errs []error

	// Close publisher
	if tc.publisher != nil {
		if err := tc.publisher.Close(); err != nil {
			slog.ErrorContext(ctx, "Error closing publisher", "error", err)
			errs = append(errs, err)
		}
	}

	// Terminate container with background context (test context may be canceled)
	if tc.container != nil {
		slog.InfoContext(ctx, "Stopping RabbitMQ testcontainer...")
		// Use background context with timeout for cleanup, as test context may be canceled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := tc.container.Terminate(cleanupCtx); err != nil {
			slog.ErrorContext(cleanupCtx, "Error terminating testcontainer", "error", err)
			errs = append(errs, err)
		}
		slog.InfoContext(cleanupCtx, "RabbitMQ testcontainer stopped")
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errs)
	}

	return nil
}

var (
	testHelper *IntegrationHelper
	once       sync.Once
)

// IntegrationHelper provides shared resources for integration tests
type IntegrationHelper struct {
	RabbitMQ *RabbitMQTestContainer
}

// NewHelper creates or returns the singleton integration test helper
func NewHelper() *IntegrationHelper {
	once.Do(func() {
		ctx := context.Background()
		slog.InfoContext(ctx, "Initializing integration test helper...")

		// Start shared RabbitMQ testcontainer
		rabbitMQ, err := NewRabbitMQTestContainer(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to start shared RabbitMQ testcontainer", "error", err)
			os.Exit(1)
		}

		testHelper = &IntegrationHelper{
			RabbitMQ: rabbitMQ,
		}

		slog.InfoContext(ctx, "Integration test helper initialized successfully")
	})

	return testHelper
}

// Teardown cleans up shared resources
func (h *IntegrationHelper) Teardown() {
	ctx := context.Background()
	if h.RabbitMQ != nil {
		slog.InfoContext(ctx, "Cleaning up shared RabbitMQ testcontainer...")
		if err := h.RabbitMQ.Close(ctx); err != nil {
			slog.ErrorContext(ctx, "Error cleaning up RabbitMQ", "error", err)
		}
	}
}
