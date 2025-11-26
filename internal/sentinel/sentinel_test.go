package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// MockPublisher implements broker.Publisher for testing
type MockPublisher struct {
	publishedEvents []*cloudevents.Event
	publishedTopics []string
	publishError    error
}

func (m *MockPublisher) Publish(topic string, event *cloudevents.Event) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedEvents = append(m.publishedEvents, event)
	m.publishedTopics = append(m.publishedTopics, topic)
	return nil
}

func (m *MockPublisher) Close() error {
	return nil
}

// TestTrigger_Success tests successful event publishing
func TestTrigger_Success(t *testing.T) {
	ctx := context.Background()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":   "cluster-1",
					"href": "/api/hyperfleet/v1/clusters/cluster-1",
					"kind": "Cluster",
					"generation": 2,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        time.Now().Add(-31 * time.Minute).Format(time.RFC3339), // Exceeds max age
						"observedGeneration": 2,
					},
				},
			},
			"total": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	m := metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Execute
	err := s.trigger(ctx)

	// Verify
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(mockPublisher.publishedEvents) != 1 {
		t.Errorf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

	if len(mockPublisher.publishedTopics) != 1 {
		t.Errorf("Expected 1 topic, got %d", len(mockPublisher.publishedTopics))
	}

	if mockPublisher.publishedTopics[0] != "Cluster" {
		t.Errorf("Expected topic 'Cluster', got '%s'", mockPublisher.publishedTopics[0])
	}

	// Verify CloudEvent properties
	event := mockPublisher.publishedEvents[0]
	if event.Type() != "com.redhat.hyperfleet.Cluster.reconcile" {
		t.Errorf("Expected event type 'com.redhat.hyperfleet.Cluster.reconcile', got '%s'", event.Type())
	}
	if event.Source() != "hyperfleet-sentinel" {
		t.Errorf("Expected source 'hyperfleet-sentinel', got '%s'", event.Source())
	}
	if event.SpecVersion() != cloudevents.VersionV1 {
		t.Errorf("Expected CloudEvents v1, got '%s'", event.SpecVersion())
	}
}

// TestTrigger_NoEventsPublished tests when no events should be published
func TestTrigger_NoEventsPublished(t *testing.T) {
	ctx := context.Background()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":   "cluster-1",
					"kind": "Cluster",
					"generation": 1,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        time.Now().Add(-5 * time.Minute).Format(time.RFC3339), // Within max age
						"observedGeneration": 1,                                                      // Generation in sync
					},
				},
			},
			"total": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	m := metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Execute
	err := s.trigger(ctx)

	// Verify
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(mockPublisher.publishedEvents) != 0 {
		t.Errorf("Expected 0 published events, got %d", len(mockPublisher.publishedEvents))
	}
}

// TestTrigger_FetchError tests handling of fetch errors
func TestTrigger_FetchError(t *testing.T) {
	ctx := context.Background()

	// Create mock server that returns 500 errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 1*time.Second) // Short timeout
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	m := metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Execute
	err := s.trigger(ctx)

	// Verify
	if err == nil {
		t.Error("Expected error, got nil")
	}

	if len(mockPublisher.publishedEvents) != 0 {
		t.Errorf("Expected 0 published events on error, got %d", len(mockPublisher.publishedEvents))
	}
}

// TestTrigger_PublishError tests handling of publish errors (graceful degradation)
func TestTrigger_PublishError(t *testing.T) {
	ctx := context.Background()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":   "cluster-1",
					"kind": "Cluster",
					"generation": 2,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        time.Now().Add(-31 * time.Minute).Format(time.RFC3339),
						"observedGeneration": 2,
					},
				},
			},
			"total": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{
		publishError: errors.New("broker connection failed"),
	}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	m := metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Execute
	err := s.trigger(ctx)

	// Verify - trigger should succeed even if publish fails (graceful degradation)
	if err != nil {
		t.Errorf("Expected no error (graceful degradation), got %v", err)
	}
}

// TestTrigger_MixedResources tests handling of multiple resources with different outcomes
func TestTrigger_MixedResources(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":   "cluster-1",
					"kind": "Cluster",
					"generation": 2,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        now.Add(-31 * time.Minute).Format(time.RFC3339), // Should publish
						"observedGeneration": 2,
					},
				},
				{
					"id":   "cluster-2",
					"kind": "Cluster",
					"generation": 1,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        now.Add(-5 * time.Minute).Format(time.RFC3339), // Should skip
						"observedGeneration": 1,
					},
				},
				{
					"id":   "cluster-3",
					"kind": "Cluster",
					"generation": 3,
					"status": map[string]interface{}{
						"phase":              "NotReady",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        now.Add(-1 * time.Minute).Format(time.RFC3339), // Should skip
						"observedGeneration": 3,
					},
				},
				{
					"id":   "cluster-4",
					"kind": "Cluster",
					"generation": 5,
					"status": map[string]interface{}{
						"phase":              "Ready",
						"lastTransitionTime": "2025-01-01T10:00:00Z",
						"lastUpdated":        now.Add(-5 * time.Minute).Format(time.RFC3339),
						"observedGeneration": 4, // Generation changed - should publish
					},
				},
			},
			"total": 4,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger(ctx)

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	m := metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Execute
	err := s.trigger(ctx)

	// Verify
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Should publish for:
	// - cluster-1 (Ready max age exceeded: 31min > 30min)
	// - cluster-3 (NotReady max age exceeded: 1min > 10s)
	// - cluster-4 (generation changed: 5 > 4)
	if len(mockPublisher.publishedEvents) != 3 {
		t.Errorf("Expected 3 published events, got %d", len(mockPublisher.publishedEvents))
	}

	// Verify topics
	for _, topic := range mockPublisher.publishedTopics {
		if topic != "Cluster" {
			t.Errorf("Expected topic 'Cluster', got '%s'", topic)
		}
	}
}
