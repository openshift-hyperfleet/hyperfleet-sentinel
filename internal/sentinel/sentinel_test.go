package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// createMockCluster creates a mock cluster response matching the new API spec.
// Ready status is determined by the ready bool parameter.
func createMockCluster(id string, generation int, observedGeneration int, ready bool, lastUpdated time.Time) map[string]interface{} {
	// Map ready bool to condition status
	readyStatus := "False"
	if ready {
		readyStatus = "True"
	}

	readyCondition := map[string]interface{}{
		"type":                 "Ready",
		"status":               readyStatus,
		"created_time":         "2025-01-01T09:00:00Z",
		"last_transition_time": "2025-01-01T10:00:00Z",
		"last_updated_time":    lastUpdated.Format(time.RFC3339),
		"observed_generation":  observedGeneration,
	}

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
			"conditions": []map[string]interface{}{
				readyCondition,
				{
					"type":                 "Available",
					"status":               readyStatus,
					"created_time":         "2025-01-01T09:00:00Z",
					"last_transition_time": "2025-01-01T10:00:00Z",
					"last_updated_time":    lastUpdated.Format(time.RFC3339),
					"observed_generation":  observedGeneration,
				},
			},
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

func (m *MockPublisher) Publish(ctx context.Context, topic string, event *cloudevents.Event) error {
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

func (m *MockPublisher) Health(ctx context.Context) error { return nil }

type MockPublisherWithLogger struct {
	mockLogger *logger.MockLoggerWithContext
}

func (m *MockPublisherWithLogger) Publish(ctx context.Context, topic string, event *cloudevents.Event) error {
	// Simulate what broker does - log with the provided context
	m.mockLogger.Info(ctx, fmt.Sprintf("broker publishing event to topic %s", topic))
	return nil
}

func (m *MockPublisherWithLogger) Close() error { return nil }

func (m *MockPublisherWithLogger) Health(ctx context.Context) error { return nil }

// mockServerForConditionQueries creates a mock server that handles the two-query
// strategy. It routes responses based on the search parameter:
//   - Queries containing "Ready='False'" return notReadyClusters
//   - Queries containing "Ready='True'" return staleClusters
func mockServerForConditionQueries(t *testing.T, notReadyClusters, staleClusters []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		var clusters []map[string]interface{}

		switch {
		case strings.Contains(search, "Ready='False'"):
			clusters = notReadyClusters
		case strings.Contains(search, "Ready='True'"):
			clusters = staleClusters
		default:
			t.Errorf("unexpected search query: %q", search)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
}

// TestTrigger_Success tests successful event publishing
func TestTrigger_Success(t *testing.T) {
	ctx := context.Background()

	// Create mock server that returns a stale cluster (exceeds max age)
	server := mockServerForConditionQueries(t,
		nil, // no not-ready clusters
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, time.Now().Add(-31*time.Minute)),
		},
	)
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute
	err = s.trigger(ctx)
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

	// Create mock server - both queries return empty (nothing needs reconciliation)
	server := mockServerForConditionQueries(t, nil, nil)
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute
	err = s.trigger(ctx)
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

	// Create mock server that returns 500 errors for all queries
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
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute
	err = s.trigger(ctx)

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

	// Create mock server with a stale cluster
	server := mockServerForConditionQueries(t,
		nil,
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, time.Now().Add(-31*time.Minute)),
		},
	)
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
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute
	err = s.trigger(ctx)
	// Verify - trigger should succeed even if publish fails (graceful degradation)
	if err != nil {
		t.Errorf("Expected no error (graceful degradation), got %v", err)
	}
}

// TestTrigger_MixedResources tests handling of multiple resources with different outcomes
func TestTrigger_MixedResources(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// With condition-based filtering:
	// - Not-ready query returns: cluster-3 (not ready, max age exceeded)
	// - Stale query returns: cluster-1 (ready, stale), cluster-4 (ready, stale with generation change)
	// - cluster-2 (ready, within max age) is NOT returned by either query (optimization!)
	server := mockServerForConditionQueries(t,
		[]map[string]interface{}{
			createMockCluster("cluster-3", 3, 3, false, now.Add(-1*time.Minute)), // Not ready, max age exceeded (1min > 10s)
		},
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)), // Ready, stale (max age exceeded)
			createMockCluster("cluster-4", 5, 4, true, now.Add(-31*time.Minute)), // Ready, stale + generation changed
		},
	)
	defer server.Close()

	// Setup components
	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry (registers metrics once via sync.Once)
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute
	err = s.trigger(ctx)
	// Verify
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Should publish for:
	// - cluster-1 (ready max age exceeded: 31min > 30min)
	// - cluster-3 (not ready max age exceeded: 1min > 10s)
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

// TestTrigger_WithMessageDataConfig verifies that configured field definitions are
// used in place of the hardcoded payload when MessageData is set.
func TestTrigger_WithMessageDataConfig(t *testing.T) {
	ctx := context.Background()

	server := mockServerForConditionQueries(t,
		nil,
		[]map[string]interface{}{
			createMockCluster("cluster-xyz", 2, 2, true, time.Now().Add(-31*time.Minute)),
		},
	)
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":     "resource.id",
			"kind":   "resource.kind",
			"origin": `"sentinel"`,
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	if err := s.trigger(ctx); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(mockPublisher.publishedEvents) != 1 {
		t.Fatalf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

	var data map[string]interface{}
	if err := json.Unmarshal(mockPublisher.publishedEvents[0].Data(), &data); err != nil {
		t.Fatalf("Failed to unmarshal event data: %v", err)
	}

	if data["id"] != "cluster-xyz" {
		t.Errorf("Expected id 'cluster-xyz', got %v", data["id"])
	}
	if data["kind"] != "Cluster" {
		t.Errorf("Expected kind 'Cluster', got %v", data["kind"])
	}
	if data["origin"] != "sentinel" {
		t.Errorf("Expected origin 'sentinel', got %v", data["origin"])
	}
	// Hardcoded fields should NOT be present when builder is configured
	if _, ok := data["reason"]; ok {
		t.Errorf("Expected 'reason' to be absent (hardcoded field), but found it")
	}
}

// TestTrigger_WithNestedMessageData verifies that nested objects are correctly built.
func TestTrigger_WithNestedMessageData(t *testing.T) {
	ctx := context.Background()

	server := mockServerForConditionQueries(t,
		nil,
		[]map[string]interface{}{
			createMockCluster("cluster-nest", 1, 1, true, time.Now().Add(-31*time.Minute)),
		},
	)
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id": "resource.id",
			"resource": map[string]interface{}{
				"id":   "resource.id",
				"kind": "resource.kind",
			},
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	if err := s.trigger(ctx); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(mockPublisher.publishedEvents) != 1 {
		t.Fatalf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

	var data map[string]interface{}
	if err := json.Unmarshal(mockPublisher.publishedEvents[0].Data(), &data); err != nil {
		t.Fatalf("Failed to unmarshal event data: %v", err)
	}

	nested, ok := data["resource"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected 'resource' to be a nested map, got %T", data["resource"])
	}
	if nested["id"] != "cluster-nest" {
		t.Errorf("Expected nested id 'cluster-nest', got %v", nested["id"])
	}
	if nested["kind"] != "Cluster" {
		t.Errorf("Expected nested kind 'Cluster', got %v", nested["kind"])
	}
}

// TestBuildEventData_WithBuilder tests buildEventData directly with a configured builder.
func TestBuildEventData_WithBuilder(t *testing.T) {
	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}
	log := logger.NewHyperFleetLogger()
	s, err := NewSentinel(cfg, nil, nil, nil, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	resource := &client.Resource{
		ID:         "cls-direct",
		Kind:       "Cluster",
		Href:       "/api/v1/clusters/cls-direct",
		Generation: 1,
	}
	decision := engine.Decision{ShouldPublish: true, Reason: "max_age_exceeded"}
	ctx := logger.WithDecisionReason(context.Background(), decision.Reason)

	data := s.buildEventData(ctx, resource, decision)
	if data["id"] != "cls-direct" {
		t.Errorf("Expected id 'cls-direct', got %v", data["id"])
	}
	if data["kind"] != "Cluster" {
		t.Errorf("Expected kind 'Cluster', got %v", data["kind"])
	}
}

func TestTrigger_ContextPropagationToBroker(t *testing.T) {
	var capturedLogs []string
	var capturedContexts []context.Context

	mockLogger := &logger.MockLoggerWithContext{
		CapturedLogs:     &capturedLogs,
		CapturedContexts: &capturedContexts,
	}

	// Create mock publisher that uses our mock logger
	mockPublisherWithLogger := &MockPublisherWithLogger{
		mockLogger: mockLogger,
	}

	ctx := context.Background()
	ctx = logger.WithDecisionReason(ctx, "max_age_exceeded")
	ctx = logger.WithTopic(ctx, "test-topic")
	ctx = logger.WithSubset(ctx, "clusters")
	ctx = logger.WithTraceID(ctx, "trace-123")
	ctx = logger.WithSpanID(ctx, "span-456")

	event := cloudevents.NewEvent()
	event.SetSpecVersion(cloudevents.VersionV1)
	event.SetType("com.redhat.hyperfleet.cluster.reconcile")
	event.SetSource("hyperfleet-sentinel")
	event.SetID("test-id")

	err := mockPublisherWithLogger.Publish(ctx, "test-topic", &event)
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if len(capturedContexts) == 0 {
		t.Fatal("no context captured by broker logger")
	}

	brokerCtx := capturedContexts[0]

	// Test context values propagated to broker
	if reason, ok := brokerCtx.Value(logger.DecisionReasonCtxKey).(string); !ok || reason != "max_age_exceeded" {
		t.Errorf("decision_reason not propagated: got %v", reason)
	}

	if topic, ok := brokerCtx.Value(logger.TopicCtxKey).(string); !ok || topic != "test-topic" {
		t.Errorf("topic not propagated: got %v", topic)
	}

	if traceID, ok := brokerCtx.Value(logger.TraceIDCtxKey).(string); !ok || traceID != "trace-123" {
		t.Errorf("trace_id not propagated: got %v", traceID)
	}

	if spanID, ok := brokerCtx.Value(logger.SpanIDCtxKey).(string); !ok || spanID != "span-456" {
		t.Errorf("span_id not propagated: got %v", spanID)
	}
}

// TestMergeResources tests deduplication of resources from multiple queries
func TestMergeResources(t *testing.T) {
	tests := []struct {
		name    string
		a       []client.Resource
		b       []client.Resource
		wantIDs []string
	}{
		{
			name:    "no overlap",
			a:       []client.Resource{{ID: "a1"}, {ID: "a2"}},
			b:       []client.Resource{{ID: "b1"}, {ID: "b2"}},
			wantIDs: []string{"a1", "a2", "b1", "b2"},
		},
		{
			name:    "with overlap - first slice wins",
			a:       []client.Resource{{ID: "shared", Kind: "from-a"}},
			b:       []client.Resource{{ID: "shared", Kind: "from-b"}, {ID: "unique"}},
			wantIDs: []string{"shared", "unique"},
		},
		{
			name:    "empty first",
			a:       nil,
			b:       []client.Resource{{ID: "b1"}},
			wantIDs: []string{"b1"},
		},
		{
			name:    "empty second",
			a:       []client.Resource{{ID: "a1"}},
			b:       nil,
			wantIDs: []string{"a1"},
		},
		{
			name:    "both empty",
			a:       nil,
			b:       nil,
			wantIDs: []string{},
		},
		{
			name:    "empty IDs are treated as distinct",
			a:       []client.Resource{{ID: "", Kind: "from-a"}},
			b:       []client.Resource{{ID: "", Kind: "from-b"}},
			wantIDs: []string{"", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeResources(tt.a, tt.b)
			if len(result) != len(tt.wantIDs) {
				t.Fatalf("Expected %d resources, got %d", len(tt.wantIDs), len(result))
			}
			for i, wantID := range tt.wantIDs {
				if result[i].ID != wantID {
					t.Errorf("result[%d].ID = %q, want %q", i, result[i].ID, wantID)
				}
			}
		})
	}

	// Verify first slice takes precedence for duplicates
	a := []client.Resource{{ID: "dup", Kind: "from-a"}}
	b := []client.Resource{{ID: "dup", Kind: "from-b"}}
	result := mergeResources(a, b)
	if result[0].Kind != "from-a" {
		t.Errorf("Expected Kind 'from-a' (first slice precedence), got %q", result[0].Kind)
	}
}

// TestTrigger_ConditionBasedQueries verifies the correct TSL search parameters are sent
func TestTrigger_ConditionBasedQueries(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	var receivedSearchParams []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		receivedSearchParams = append(receivedSearchParams, search)

		// Return appropriate responses based on query
		var clusters []map[string]interface{}
		if strings.Contains(search, "Ready='False'") {
			clusters = []map[string]interface{}{
				createMockCluster("not-ready-1", 1, 1, false, now.Add(-15*time.Second)),
			}
		} else if strings.Contains(search, "Ready='True'") {
			clusters = []map[string]interface{}{
				createMockCluster("stale-1", 2, 2, true, now.Add(-31*time.Minute)),
			}
		}

		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify two queries were made
	if len(receivedSearchParams) != 2 {
		t.Fatalf("Expected 2 API queries, got %d", len(receivedSearchParams))
	}

	// Query 1: Not-ready resources
	if !strings.Contains(receivedSearchParams[0], "status.conditions.Ready='False'") {
		t.Errorf("First query should filter not-ready resources, got: %q", receivedSearchParams[0])
	}

	// Query 2: Stale ready resources
	if !strings.Contains(receivedSearchParams[1], "status.conditions.Ready='True'") {
		t.Errorf("Second query should filter ready resources, got: %q", receivedSearchParams[1])
	}
	if !strings.Contains(receivedSearchParams[1], "last_updated_time") {
		t.Errorf("Second query should include time-based filter, got: %q", receivedSearchParams[1])
	}

	// Both resources should trigger events
	if len(mockPublisher.publishedEvents) != 2 {
		t.Errorf("Expected 2 published events, got %d", len(mockPublisher.publishedEvents))
	}
}

// TestTrigger_StaleQueryFailure tests graceful degradation when the stale query fails
func TestTrigger_StaleQueryFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	queryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount++
		search := r.URL.Query().Get("search")

		if strings.Contains(search, "Ready='False'") {
			// Not-ready query succeeds
			clusters := []map[string]interface{}{
				createMockCluster("not-ready-1", 1, 1, false, now.Add(-15*time.Second)),
			}
			response := createMockClusterList(clusters)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Logf("Error encoding response: %v", err)
			}
		} else {
			// Stale query fails
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"error": "internal server error"}`)); err != nil {
				t.Logf("Error writing error response: %v", err)
			}
		}
	}))
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 1*time.Second)
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:   "clusters",
		Topic:          "test-topic",
		MaxAgeNotReady: 10 * time.Second,
		MaxAgeReady:    30 * time.Minute,
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Should succeed with graceful degradation (not-ready results only)
	err = s.trigger(ctx)
	if err != nil {
		t.Errorf("Expected no error (graceful degradation), got %v", err)
	}

	// Should still publish event for the not-ready resource
	if len(mockPublisher.publishedEvents) != 1 {
		t.Errorf("Expected 1 published event (from not-ready query), got %d", len(mockPublisher.publishedEvents))
	}
}
