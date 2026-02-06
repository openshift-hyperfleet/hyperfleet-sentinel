//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

// RabbitMQTestContainer manages a RabbitMQ testcontainer for integration testing
type RabbitMQTestContainer struct {
	container *rabbitmq.RabbitMQContainer
	publisher broker.Publisher
}

// NewRabbitMQTestContainer creates and starts a RabbitMQ testcontainer
func NewRabbitMQTestContainer(ctx context.Context) (*RabbitMQTestContainer, error) {
	log := logger.NewHyperFleetLogger()
	log.Info(ctx, "Starting RabbitMQ testcontainer...")

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

	log.Extra("amqp_url", amqpURL).Info(ctx, "RabbitMQ testcontainer started")

	// Create publisher using hyperfleet-broker library with configMap
	// This allows us to pass configuration programmatically for testing
	configMap := map[string]string{
		"broker.type":         "rabbitmq",
		"broker.rabbitmq.url": amqpURL,
	}

	publisher, err := broker.NewPublisher(log, configMap)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create broker publisher: %w", err)
	}

	log.Info(ctx, "RabbitMQ publisher initialized successfully")

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
	log := logger.NewHyperFleetLogger()
	var errs []error

	// Close publisher
	if tc.publisher != nil {
		if err := tc.publisher.Close(); err != nil {
			log.Errorf(ctx, "Error closing publisher: %v", err)
			errs = append(errs, err)
		}
	}

	// Terminate container with background context (test context may be canceled)
	if tc.container != nil {
		log.Info(ctx, "Stopping RabbitMQ testcontainer...")
		// Use background context with timeout for cleanup, as test context may be canceled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := tc.container.Terminate(cleanupCtx); err != nil {
			log.Errorf(cleanupCtx, "Error terminating testcontainer: %v", err)
			errs = append(errs, err)
		}
		log.Info(cleanupCtx, "RabbitMQ testcontainer stopped")
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
		log := logger.NewHyperFleetLogger()
		ctx := context.Background()
		log.Info(ctx, "Initializing integration test helper...")

		// Start shared RabbitMQ testcontainer
		rabbitMQ, err := NewRabbitMQTestContainer(ctx)
		if err != nil {
			log.Errorf(ctx, "Failed to start shared RabbitMQ testcontainer: %v", err)
			os.Exit(1)
		}

		testHelper = &IntegrationHelper{
			RabbitMQ: rabbitMQ,
		}

		log.Info(ctx, "Integration test helper initialized successfully")
	})

	return testHelper
}

// Teardown cleans up shared resources
func (h *IntegrationHelper) Teardown() {
	log := logger.NewHyperFleetLogger()
	ctx := context.Background()
	if h.RabbitMQ != nil {
		log.Info(ctx, "Cleaning up shared RabbitMQ testcontainer...")
		if err := h.RabbitMQ.Close(ctx); err != nil {
			log.Errorf(ctx, "Error cleaning up RabbitMQ: %v", err)
		}
	}
}
