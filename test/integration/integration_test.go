//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/sentinel"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// MockPublisher implements broker.Publisher for integration testing
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

// createMockCluster creates a mock cluster response
func createMockCluster(id string, generation int, observedGeneration int, phase string, lastUpdated time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":         id,
		"href":       "/api/hyperfleet/v1/clusters/" + id,
		"kind":       "Cluster",
		"name":       id,
		"generation": generation,
		"created_at": "2025-01-01T09:00:00Z",
		"updated_at": "2025-01-01T10:00:00Z",
		"created_by": "test-user",
		"updated_by": "test-user",
		"spec":       map[string]interface{}{},
		"status": map[string]interface{}{
			"phase":                phase,
			"last_transition_time": "2025-01-01T10:00:00Z",
			"updated_at":           lastUpdated.Format(time.RFC3339),
			"observed_generation":  observedGeneration,
			"adapters":             []interface{}{},
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

// TestIntegration_EndToEnd tests the full Sentinel workflow end-to-end
func TestIntegration_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()

	// Create mock HyperFleet API server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// First call: Return clusters with one needing reconciliation
		if callCount == 1 {
			clusters := []map[string]interface{}{
				createMockCluster("cluster-1", 2, 2, "Ready", now.Add(-31*time.Minute)), // Max age exceeded
				createMockCluster("cluster-2", 1, 1, "Ready", now.Add(-5*time.Minute)),  // Within max age
			}
			response := createMockClusterList(clusters)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Subsequent calls: Empty list
		response := createMockClusterList([]map[string]interface{}{})
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
		PollInterval:   100 * time.Millisecond, // Short interval for testing
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
	}

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Run Sentinel in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for at least 2 polling cycles
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Wait for Sentinel to stop
	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Fatalf("Sentinel failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sentinel did not stop within timeout")
	}

	// Verify events were published
	if len(mockPublisher.publishedEvents) == 0 {
		t.Fatal("Expected at least 1 event to be published")
	}

	// Verify first event properties
	event := mockPublisher.publishedEvents[0]
	if event.Type() != "com.redhat.hyperfleet.Cluster.reconcile" {
		t.Errorf("Expected event type 'com.redhat.hyperfleet.Cluster.reconcile', got '%s'", event.Type())
	}
	if event.Source() != "hyperfleet-sentinel" {
		t.Errorf("Expected source 'hyperfleet-sentinel', got '%s'", event.Source())
	}

	// Verify metrics were collected
	metricCount := testutil.CollectAndCount(m.EventsPublished)
	if metricCount == 0 {
		t.Error("Expected EventsPublished metric to be collected")
	}

	metricCount = testutil.CollectAndCount(m.PollDuration)
	if metricCount == 0 {
		t.Error("Expected PollDuration metric to be collected")
	}
}

// TestIntegration_LabelSelectorFiltering tests resource filtering with label selectors
func TestIntegration_LabelSelectorFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := time.Now()

	// Create mock server that returns clusters with label
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Note: Label filtering is done client-side in the HyperFleet client
		// The server just returns all clusters that match the query
		clusters := []map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, "Ready", now.Add(-31*time.Minute)),
		}
		response := createMockClusterList(clusters)
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
		PollInterval:   100 * time.Millisecond,
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		ResourceSelector: []config.LabelSelector{
			{Label: "shard", Value: "1"},
		},
	}

	s := sentinel.NewSentinel(ctx, cfg, hyperfleetClient, decisionEngine, mockPublisher, log, m)

	// Run sentinel
	go func() {
		s.Start(ctx)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	time.Sleep(100 * time.Millisecond)

	// Verify event was published
	if len(mockPublisher.publishedEvents) == 0 {
		t.Error("Expected at least 1 event to be published")
	}

	// Verify metrics were collected with correct labels
	metricCount := testutil.CollectAndCount(m.EventsPublished)
	if metricCount == 0 {
		t.Error("Expected EventsPublished metric to be collected")
	}
}
