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

// createMockCluster creates a mock cluster response matching main branch spec
func createMockCluster(id string, generation int, observedGeneration int, phase string, lastUpdated time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":           id,
		"href":         "/api/hyperfleet/v1/clusters/" + id,
		"kind":         "Cluster",
		"name":         id,
		"generation":   generation,
		"created_time": "2025-01-01T09:00:00Z",
		"updated_time": "2025-01-01T10:00:00Z",
		"created_by":   "test-user@example.com",
		"updated_by":   "test-user@example.com",
		"spec":         map[string]interface{}{},
		"status": map[string]interface{}{
			"phase":                phase,
			"last_transition_time": "2025-01-01T10:00:00Z",
			"last_updated_time":    lastUpdated.Format(time.RFC3339),
			"observed_generation":  observedGeneration,
			"conditions":           []interface{}{},
		},
	}
}

// createMockClusterList creates a mock ClusterList response
func createMockClusterList(clusters []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"kind":  "ClusterList",
		"page":  1,
		"size":  len(clusters),
		"total": len(clusters),
		"items": clusters,
	}
}

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
		// Cluster exceeds max age (31 minutes ago)
		cluster := createMockCluster("cluster-1", 2, 2, "Ready", time.Now().Add(-31*time.Minute))
		response := createMockClusterList([]map[string]interface{}{cluster})
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

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

	if mockPublisher.publishedTopics[0] != "test-topic" {
		t.Errorf("Expected topic 'test-topic', got '%s'", mockPublisher.publishedTopics[0])
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
		// Cluster within max age and generation in sync - should NOT publish
		cluster := createMockCluster("cluster-1", 1, 1, "Ready", time.Now().Add(-5*time.Minute))
		response := createMockClusterList([]map[string]interface{}{cluster})
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

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
		if _, err := w.Write([]byte(`{"error": "internal server error"}`)); err != nil {
			t.Logf("Error writing error response: %v", err)
		}
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 1*time.Second) // Short timeout
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

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
		cluster := createMockCluster("cluster-1", 2, 2, "Ready", time.Now().Add(-31*time.Minute))
		response := createMockClusterList([]map[string]interface{}{cluster})
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{
		publishError: errors.New("broker connection failed"),
	}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

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
		clusters := []map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, "Ready", now.Add(-31*time.Minute)),   // Should publish (max age exceeded)
			createMockCluster("cluster-2", 1, 1, "Ready", now.Add(-5*time.Minute)),    // Should skip (within max age)
			createMockCluster("cluster-3", 3, 3, "NotReady", now.Add(-1*time.Minute)), // Should skip (NotReady within max age)
			createMockCluster("cluster-4", 5, 4, "Ready", now.Add(-5*time.Minute)),    // Should publish (generation changed)
		}
		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry)

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)

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
		if topic != "test-topic" {
			t.Errorf("Expected topic 'test-topic', got '%s'", topic)
		}
	}
}
