package publisher

import (
	"encoding/json"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// MockPublisher is a mock publisher for testing/development
// Implements broker.Publisher interface
type MockPublisher struct{}

// NewMockPublisher creates a new mock publisher
func NewMockPublisher() *MockPublisher {
	return &MockPublisher{}
}

// Publish logs the event instead of publishing
func (p *MockPublisher) Publish(topic string, event *cloudevents.Event) error {
	data, _ := json.MarshalIndent(event, "", "  ")
	fmt.Printf("[MOCK PUBLISH] topic=%s event=%s\n", topic, string(data))
	return nil
}

// Close is a no-op for the mock publisher
func (p *MockPublisher) Close() error {
	return nil
}
