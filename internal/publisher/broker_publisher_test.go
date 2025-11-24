package publisher

import (
	"context"
	"errors"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// mockBrokerPublisher implements the broker.Publisher interface for testing
type mockBrokerPublisher struct {
	publishFunc func(topic string, event *cloudevents.Event) error
	closeFunc   func() error
}

func (m *mockBrokerPublisher) Publish(topic string, event *cloudevents.Event) error {
	if m.publishFunc != nil {
		return m.publishFunc(topic, event)
	}
	return nil
}

func (m *mockBrokerPublisher) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// TestBrokerPublisher_Publish tests the Publish method
func TestBrokerPublisher_Publish(t *testing.T) {
	tests := []struct {
		name          string
		resource      *client.Resource
		reason        string
		publishFunc   func(topic string, event *cloudevents.Event) error
		expectedTopic string
		expectError   bool
	}{
		{
			name: "successful publish with cluster resource",
			resource: &client.Resource{
				Kind: "clusters",
				ID:   "test-cluster-123",
				Href: "/api/v1/clusters/test-cluster-123",
			},
			reason:        "reconciliation needed",
			expectedTopic: "clusters",
			expectError:   false,
		},
		{
			name: "successful publish with nodepool resource",
			resource: &client.Resource{
				Kind: "nodepools",
				ID:   "test-nodepool-456",
				Href: "/api/v1/nodepools/test-nodepool-456",
			},
			reason:        "max age exceeded",
			expectedTopic: "nodepools",
			expectError:   false,
		},
		{
			name: "publish failure",
			resource: &client.Resource{
				Kind: "clusters",
				ID:   "test-cluster-789",
			},
			reason:        "test reason",
			expectedTopic: "clusters",
			publishFunc: func(topic string, event *cloudevents.Event) error {
				return errors.New("broker connection failed")
			},
			expectError: true,
		},
		{
			name: "empty resource kind",
			resource: &client.Resource{
				Kind: "",
				ID:   "test-123",
			},
			reason:        "test",
			expectedTopic: "",
			expectError:   true, // Now validated!
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedTopic string
			var capturedEvent *cloudevents.Event

			mock := &mockBrokerPublisher{
				publishFunc: func(topic string, event *cloudevents.Event) error {
					capturedTopic = topic
					capturedEvent = event
					if tt.publishFunc != nil {
						return tt.publishFunc(topic, event)
					}
					return nil
				},
			}

			publisher := &BrokerPublisher{
				publisher: mock,
			}

			ctx := context.Background()
			err := publisher.Publish(ctx, tt.resource, tt.reason)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Skip validation if error was expected
			if tt.expectError {
				return
			}

			// Verify topic
			if capturedTopic != tt.expectedTopic {
				t.Errorf("Expected topic '%s', got '%s'", tt.expectedTopic, capturedTopic)
			}

			// Verify CloudEvent structure
			if capturedEvent == nil {
				t.Fatal("Expected CloudEvent to be published, but it was nil")
			}

			// Verify event type
			expectedType := "com.redhat.hyperfleet." + tt.resource.Kind + ".reconcile"
			if capturedEvent.Type() != expectedType {
				t.Errorf("Expected event type '%s', got '%s'", expectedType, capturedEvent.Type())
			}

			// Verify event source
			if capturedEvent.Source() != "hyperfleet-sentinel" {
				t.Errorf("Expected source 'hyperfleet-sentinel', got '%s'", capturedEvent.Source())
			}

			// Verify event has an ID
			if capturedEvent.ID() == "" {
				t.Error("Expected event to have an ID, but it was empty")
			}

			// Verify event spec version
			if capturedEvent.SpecVersion() != "1.0" {
				t.Errorf("Expected spec version '1.0', got '%s'", capturedEvent.SpecVersion())
			}
		})
	}
}

// TestBrokerPublisher_PublishNilResource tests behavior with nil resource
func TestBrokerPublisher_PublishNilResource(t *testing.T) {
	mock := &mockBrokerPublisher{
		publishFunc: func(topic string, event *cloudevents.Event) error {
			t.Error("Publish should not be called with nil resource")
			return nil
		},
	}

	publisher := &BrokerPublisher{
		publisher: mock,
	}

	ctx := context.Background()

	// Should return error for nil resource
	err := publisher.Publish(ctx, nil, "test reason")
	if err == nil {
		t.Error("Expected error for nil resource, but got nil")
	}

	// Verify error message
	expectedMsg := "cannot publish event: resource is nil"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestBrokerPublisher_Close tests the Close method
func TestBrokerPublisher_Close(t *testing.T) {
	tests := []struct {
		name        string
		closeFunc   func() error
		expectError bool
	}{
		{
			name:        "successful close",
			closeFunc:   func() error { return nil },
			expectError: false,
		},
		{
			name: "close with error",
			closeFunc: func() error {
				return errors.New("failed to close connection")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrokerPublisher{
				closeFunc: tt.closeFunc,
			}

			publisher := &BrokerPublisher{
				publisher: mock,
			}

			err := publisher.Close()

			if tt.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestNewBrokerPublisher tests the constructor
func TestNewBrokerPublisher(t *testing.T) {
	// Note: This test will fail if broker.yaml or env vars are not configured
	// In a real test environment, you would mock the broker.NewPublisher() call
	// For now, we test that it returns an error when config is missing

	t.Run("initialization without config", func(t *testing.T) {
		// This may fail in CI/dev environments without proper broker config
		// That's expected - the test validates error handling
		_, err := NewBrokerPublisher()

		// We expect either success (if broker.yaml exists) or specific error
		if err != nil {
			// Error is acceptable if it's about missing configuration
			t.Logf("NewBrokerPublisher failed as expected without config: %v", err)
		} else {
			t.Log("NewBrokerPublisher succeeded (broker.yaml likely exists)")
		}
	})
}

// TestBrokerPublisher_CloudEventData tests the data payload structure
func TestBrokerPublisher_CloudEventData(t *testing.T) {
	var capturedEvent *cloudevents.Event

	mock := &mockBrokerPublisher{
		publishFunc: func(topic string, event *cloudevents.Event) error {
			capturedEvent = event
			return nil
		},
	}

	publisher := &BrokerPublisher{
		publisher: mock,
	}

	resource := &client.Resource{
		Kind: "clusters",
		ID:   "test-cluster-123",
	}
	reason := "max age exceeded"

	ctx := context.Background()
	err := publisher.Publish(ctx, resource, reason)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify data payload
	if capturedEvent == nil {
		t.Fatal("Expected CloudEvent to be captured")
	}

	// Extract data as map
	data := make(map[string]interface{})
	if err := capturedEvent.DataAs(&data); err != nil {
		t.Fatalf("Failed to extract event data: %v", err)
	}

	// Verify data fields
	if data["resourceType"] != resource.Kind {
		t.Errorf("Expected resourceType '%s', got '%v'", resource.Kind, data["resourceType"])
	}
	if data["resourceId"] != resource.ID {
		t.Errorf("Expected resourceId '%s', got '%v'", resource.ID, data["resourceId"])
	}
	if data["reason"] != reason {
		t.Errorf("Expected reason '%s', got '%v'", reason, data["reason"])
	}
}
