package publisher

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// BrokerPublisher implements the Publisher interface using the hyperfleet-broker library.
//
// This wrapper serves several purposes:
//  1. Adapts hyperfleet-broker's generic publisher to Sentinel's Publisher interface
//  2. Converts Sentinel's CloudEvent format to the cloudevents.Event format expected by the broker
//  3. Provides input validation (nil resources, empty resource kinds)
//  4. Adds error context (topic, event ID, resource ID) for better debugging
//  5. Maps resource.Kind to broker topic name (e.g., "clusters" -> clusters topic)
//
// The wrapper allows Sentinel to use the shared hyperfleet-broker library while maintaining
// its own Publisher abstraction, making it easier to test and potentially swap implementations.
type BrokerPublisher struct {
	publisher broker.Publisher
}

// NewBrokerPublisher creates a new publisher using the hyperfleet-broker library
// Configuration is loaded from broker.yaml or BROKER_CONFIG_FILE environment variable
func NewBrokerPublisher() (*BrokerPublisher, error) {
	pub, err := broker.NewPublisher()
	if err != nil {
		return nil, fmt.Errorf("failed to create broker publisher: %w", err)
	}

	return &BrokerPublisher{
		publisher: pub,
	}, nil
}

// Publish publishes a CloudEvent for a resource to the message broker
func (p *BrokerPublisher) Publish(ctx context.Context, resource *client.Resource, reason string) error {
	// Validate input
	if resource == nil {
		return fmt.Errorf("cannot publish event: resource is nil")
	}
	if resource.Kind == "" {
		return fmt.Errorf("cannot publish event: resource.Kind is empty")
	}

	// Create CloudEvent using the existing helper
	sentinelEvent := NewCloudEvent(resource, reason)

	// Convert to cloudevents.Event for the broker library
	event := cloudevents.NewEvent()
	event.SetSpecVersion(sentinelEvent.SpecVersion)
	event.SetType(sentinelEvent.Type)
	event.SetSource(sentinelEvent.Source)
	event.SetID(sentinelEvent.ID)
	if err := event.SetData(cloudevents.ApplicationJSON, sentinelEvent.Data); err != nil {
		return fmt.Errorf("failed to set event data: %w", err)
	}

	// Determine topic based on resource type
	topic := resource.Kind

	// Publish to broker
	// Note: broker library Publish doesn't take context, so we can't propagate
	// cancellation/timeout from ctx. This is a known limitation.
	if err := p.publisher.Publish(topic, &event); err != nil {
		return fmt.Errorf("failed to publish event to broker (topic=%s, eventID=%s, resourceID=%s): %w",
			topic, event.ID(), resource.ID, err)
	}

	return nil
}

// Close closes the broker publisher and releases resources
func (p *BrokerPublisher) Close() error {
	return p.publisher.Close()
}
