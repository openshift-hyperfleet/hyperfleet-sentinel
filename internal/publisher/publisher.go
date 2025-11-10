package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// Publisher publishes CloudEvents to a message broker
type Publisher interface {
	Publish(ctx context.Context, resource *client.Resource, reason string) error
	Close() error
}

// CloudEvent represents a CloudEvent 1.0 message
type CloudEvent struct {
	SpecVersion     string                 `json:"specversion"`
	Type            string                 `json:"type"`
	Source          string                 `json:"source"`
	ID              string                 `json:"id"`
	Time            string                 `json:"time"`
	DataContentType string                 `json:"datacontenttype"`
	Data            map[string]interface{} `json:"data"`
}

// NewCloudEvent creates a CloudEvent for a resource
func NewCloudEvent(resource *client.Resource, reason string) *CloudEvent {
	return &CloudEvent{
		SpecVersion:     "1.0",
		Type:            fmt.Sprintf("com.redhat.hyperfleet.%s.reconcile", resource.Kind),
		Source:          "hyperfleet-sentinel",
		ID:              uuid.New().String(),
		Time:            time.Now().UTC().Format(time.RFC3339),
		DataContentType: "application/json",
		Data: map[string]interface{}{
			"resourceType": resource.Kind,
			"resourceId":   resource.ID,
			"reason":       reason,
		},
	}
}

// MarshalJSON converts the CloudEvent to JSON
func (e *CloudEvent) MarshalJSON() ([]byte, error) {
	type Alias CloudEvent
	return json.Marshal(&struct{ *Alias }{Alias: (*Alias)(e)})
}

// MockPublisher is a mock publisher for testing/development
type MockPublisher struct{}

// NewMockPublisher creates a new mock publisher
func NewMockPublisher() *MockPublisher {
	return &MockPublisher{}
}

// Publish logs the event instead of publishing
func (p *MockPublisher) Publish(ctx context.Context, resource *client.Resource, reason string) error {
	event := NewCloudEvent(resource, reason)
	data, _ := json.MarshalIndent(event, "", "  ")
	fmt.Printf("[MOCK PUBLISH] %s\n", string(data))
	return nil
}

// Close is a no-op for the mock publisher
func (p *MockPublisher) Close() error {
	return nil
}
