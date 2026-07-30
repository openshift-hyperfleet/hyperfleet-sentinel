package publisher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/queue"
)

// MockPublisher is a mock publisher for testing/development
type MockPublisher struct{}

// NewMockPublisher creates a new mock publisher
func NewMockPublisher() *MockPublisher {
	return &MockPublisher{}
}

// Publish logs the message instead of publishing
func (p *MockPublisher) Publish(ctx context.Context, msg *queue.QueueMessage) error {
	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	fmt.Printf("[MOCK PUBLISH] message=%s\n", string(data))
	return nil
}

// Health is a no-op for the mock publisher
func (p *MockPublisher) Health(ctx context.Context) error { return nil }
