//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
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
	glog.Infof("Starting RabbitMQ testcontainer...")

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

	glog.Infof("RabbitMQ testcontainer started at: %s", amqpURL)

	// Create publisher using hyperfleet-broker library with configMap
	// This allows us to pass configuration programmatically for testing
	configMap := map[string]string{
		"broker.type":        "rabbitmq",
		"broker.rabbitmq.url": amqpURL,
	}

	publisher, err := broker.NewPublisher(configMap)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create broker publisher: %w", err)
	}

	glog.Infof("RabbitMQ publisher initialized successfully")

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
			glog.Errorf("Error closing publisher: %v", err)
			errs = append(errs, err)
		}
	}

	// Terminate container
	if tc.container != nil {
		glog.Infof("Stopping RabbitMQ testcontainer...")
		if err := tc.container.Terminate(ctx); err != nil {
			glog.Errorf("Error terminating testcontainer: %v", err)
			errs = append(errs, err)
		}
		glog.Infof("RabbitMQ testcontainer stopped")
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errs)
	}

	return nil
}
