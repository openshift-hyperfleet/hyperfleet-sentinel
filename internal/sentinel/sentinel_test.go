package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	testTopic        = "test-topic"
	testResourceKind = "Cluster"
)

// createMockCluster creates a mock cluster response matching the new API spec.
// Ready status is determined by the ready bool parameter.
func createMockCluster(
	id string,
	generation int,
	observedGeneration int,
	ready bool,
	lastUpdated time.Time,
) map[string]interface{} {
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
		"kind":         testResourceKind,
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
	publishError    error
	publishedEvents []*cloudevents.Event
	publishedTopics []string
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

// mockServerForConditionQueries creates a mock HTTP server that routes responses
// based on condition-based search parameters. Each poll cycle makes two queries:
// one for not-ready resources (Ready='False') and one for stale-ready resources (Ready='True').
func mockServerForConditionQueries(
	t *testing.T,
	notReadyClusters, staleClusters []map[string]interface{},
) *httptest.Server {
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
	now := time.Now()

	// Stale ready cluster exceeds max age (31 minutes ago)
	server := mockServerForConditionQueries(t,
		nil, // no not-ready clusters
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)),
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
		Topic:          testTopic,
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

	if mockPublisher.publishedTopics[0] != testTopic {
		t.Errorf("Expected topic '%s', got '%s'", testTopic, mockPublisher.publishedTopics[0])
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

	server := mockServerForConditionQueries(t,
		[]map[string]interface{}{
			// Not-ready for only 1 second — within max_age_not_ready (10s), should be skipped
			createMockCluster("cluster-1", 1, 1, false, time.Now().Add(-1*time.Second)),
		},
		nil, // no stale clusters
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
		Topic:          testTopic,
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
		Topic:          testTopic,
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
	now := time.Now()

	server := mockServerForConditionQueries(t,
		nil, // no not-ready clusters
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)),
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
		Topic:          testTopic,
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

	// Split resources between not-ready and stale queries
	server := mockServerForConditionQueries(t,
		// Not-ready query
		[]map[string]interface{}{
			// Should publish (not ready max age exceeded: 1min > 10s)
			createMockCluster("cluster-3", 3, 3, false, now.Add(-1*time.Minute)),
			// Should be skipped (empty ID)
			createMockCluster("", 1, 1, false, now.Add(-15*time.Second)),
		},
		// Stale query
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)), // Should publish (max age exceeded)
			createMockCluster("cluster-4", 5, 4, true, now.Add(-5*time.Minute)),  // Should publish (generation changed)
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
		Topic:          testTopic,
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
	// - cluster-3 (not ready max age exceeded: 1min > 10s)
	// - cluster-1 (ready max age exceeded: 31min > 30min)
	// - cluster-4 (generation changed: 5 > 4)
	if len(mockPublisher.publishedEvents) != 3 {
		t.Errorf("Expected 3 published events, got %d", len(mockPublisher.publishedEvents))
	}

	// Verify topics
	for _, topic := range mockPublisher.publishedTopics {
		if topic != testTopic {
			t.Errorf("Expected topic '%s', got '%s'", testTopic, topic)
		}
	}
}

// TestTrigger_WithMessageDataConfig verifies that configured field definitions are
// used in place of the hardcoded payload when MessageData is set.
func TestTrigger_WithMessageDataConfig(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	server := mockServerForConditionQueries(t,
		nil,
		[]map[string]interface{}{
			createMockCluster("cluster-xyz", 2, 2, true, now.Add(-31*time.Minute)),
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
		Topic:          testTopic,
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
	if data["kind"] != testResourceKind {
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
	now := time.Now()

	server := mockServerForConditionQueries(t,
		nil,
		[]map[string]interface{}{
			createMockCluster("cluster-nest", 1, 1, true, now.Add(-31*time.Minute)),
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
		Topic:          testTopic,
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
	if nested["kind"] != testResourceKind {
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
		Kind:       testResourceKind,
		Href:       "/api/v1/clusters/cls-direct",
		Generation: 1,
	}
	decision := engine.Decision{ShouldPublish: true, Reason: "max_age_exceeded"}
	ctx := logger.WithDecisionReason(context.Background(), decision.Reason)

	data := s.buildEventData(ctx, resource, decision)
	if data["id"] != "cls-direct" {
		t.Errorf("Expected id 'cls-direct', got %v", data["id"])
	}
	if data["kind"] != testResourceKind {
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
	ctx = logger.WithTopic(ctx, testTopic)
	ctx = logger.WithSubset(ctx, "clusters")
	ctx = logger.WithTraceID(ctx, "trace-123")
	ctx = logger.WithSpanID(ctx, "span-456")

	event := cloudevents.NewEvent()
	event.SetSpecVersion(cloudevents.VersionV1)
	event.SetType("com.redhat.hyperfleet.cluster.reconcile")
	event.SetSource("hyperfleet-sentinel")
	event.SetID("test-id")

	err := mockPublisherWithLogger.Publish(ctx, testTopic, &event)
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

	if topic, ok := brokerCtx.Value(logger.TopicCtxKey).(string); !ok || topic != testTopic {
		t.Errorf("topic not propagated: got %v", topic)
	}

	if traceID, ok := brokerCtx.Value(logger.TraceIDCtxKey).(string); !ok || traceID != "trace-123" {
		t.Errorf("trace_id not propagated: got %v", traceID)
	}

	if spanID, ok := brokerCtx.Value(logger.SpanIDCtxKey).(string); !ok || spanID != "span-456" {
		t.Errorf("span_id not propagated: got %v", spanID)
	}
}

// TestMergeResources tests the mergeResources deduplication function
func TestMergeResources(t *testing.T) {
	tests := []struct {
		name    string
		a       []client.Resource
		b       []client.Resource
		wantIDs []string
		wantLen int
	}{
		{
			name:    "no overlap",
			a:       []client.Resource{{ID: "a1"}, {ID: "a2"}},
			b:       []client.Resource{{ID: "b1"}},
			wantIDs: []string{"a1", "a2", "b1"},
			wantLen: 3,
		},
		{
			name:    "duplicate in b removed",
			a:       []client.Resource{{ID: "x"}},
			b:       []client.Resource{{ID: "x"}, {ID: "y"}},
			wantIDs: []string{"x", "y"},
			wantLen: 2,
		},
		{
			name:    "both empty",
			a:       nil,
			b:       nil,
			wantIDs: nil,
			wantLen: 0,
		},
		{
			name:    "a empty",
			a:       nil,
			b:       []client.Resource{{ID: "b1"}},
			wantIDs: []string{"b1"},
			wantLen: 1,
		},
		{
			name:    "b empty",
			a:       []client.Resource{{ID: "a1"}},
			b:       nil,
			wantIDs: []string{"a1"},
			wantLen: 1,
		},
		{
			name:    "all duplicates",
			a:       []client.Resource{{ID: "x"}, {ID: "y"}},
			b:       []client.Resource{{ID: "x"}, {ID: "y"}},
			wantIDs: []string{"x", "y"},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeResources(tt.a, tt.b)
			if len(result) != tt.wantLen {
				t.Errorf("mergeResources() returned %d resources, want %d", len(result), tt.wantLen)
			}
			for i, id := range tt.wantIDs {
				if i < len(result) && result[i].ID != id {
					t.Errorf("mergeResources()[%d].ID = %q, want %q", i, result[i].ID, id)
				}
			}
		})
	}
}

// TestMergeResources_FirstSlicePrecedence verifies that resources from the first
// slice take precedence when duplicates are found.
func TestMergeResources_FirstSlicePrecedence(t *testing.T) {
	a := []client.Resource{{ID: "dup", Kind: "from-a"}}
	b := []client.Resource{{ID: "dup", Kind: "from-b"}}

	result := mergeResources(a, b)
	if len(result) != 1 {
		t.Fatalf("Expected 1 resource, got %d", len(result))
	}
	if result[0].Kind != "from-a" {
		t.Errorf("Expected resource from first slice (Kind='from-a'), got Kind=%q", result[0].Kind)
	}
}

// TestTrigger_ConditionBasedQueries verifies that trigger sends two condition-based
// queries (not-ready + stale) and correctly processes results from both.
func TestTrigger_ConditionBasedQueries(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	var capturedSearchParams []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		mu.Lock()
		capturedSearchParams = append(capturedSearchParams, search)
		mu.Unlock()

		var clusters []map[string]interface{}
		switch {
		case strings.Contains(search, "Ready='False'"):
			clusters = []map[string]interface{}{
				createMockCluster("not-ready-1", 1, 1, false, now.Add(-15*time.Second)),
			}
		case strings.Contains(search, "Ready='True'"):
			clusters = []map[string]interface{}{
				createMockCluster("stale-1", 2, 2, true, now.Add(-31*time.Minute)),
			}
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

	if err := s.trigger(ctx); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify two queries were sent
	mu.Lock()
	params := make([]string, len(capturedSearchParams))
	copy(params, capturedSearchParams)
	mu.Unlock()

	if len(params) != 2 {
		t.Fatalf("Expected 2 search queries (not-ready + stale), got %d", len(params))
	}

	hasNotReady := false
	hasStale := false
	for _, search := range params {
		if strings.Contains(search, "Ready='False'") {
			hasNotReady = true
		}
		if strings.Contains(search, "Ready='True'") {
			hasStale = true
		}
	}
	if !hasNotReady {
		t.Error("No not-ready query (Ready='False') was sent")
	}
	if !hasStale {
		t.Error("No stale query (Ready='True') was sent")
	}

	// Both resources should trigger events
	if len(mockPublisher.publishedEvents) != 2 {
		t.Errorf("Expected 2 published events (1 not-ready + 1 stale), got %d", len(mockPublisher.publishedEvents))
	}
}

// TestTrigger_StaleQueryFailure tests graceful degradation when the stale query
// fails but the not-ready query succeeds.
func TestTrigger_StaleQueryFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	var queryCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		search := r.URL.Query().Get("search")

		switch {
		case strings.Contains(search, "Ready='False'"):
			// Not-ready query succeeds
			clusters := []map[string]interface{}{
				createMockCluster("not-ready-1", 1, 1, false, now.Add(-15*time.Second)),
			}
			response := createMockClusterList(clusters)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Logf("Error encoding response: %v", err)
			}
		case strings.Contains(search, "Ready='True'"):
			// Stale query fails
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"error": "internal server error"}`)); err != nil {
				t.Logf("Error writing error response: %v", err)
			}
		default:
			t.Errorf("unexpected search query: %q", search)
			w.WriteHeader(http.StatusBadRequest)
			return
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

	// Both queries should have been attempted (stale query retries on 500, so queryCount >= 2)
	if queryCount.Load() < 2 {
		t.Errorf("Expected at least 2 queries (not-ready + stale), got %d", queryCount.Load())
	}
}

func TestTrigger_CreatesRequiredSpans(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory trace exporter for span verification
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(exporter),
	)
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Errorf("shutdown of tracer provider: %v", err)
		}
		otel.SetTracerProvider(previousProvider)
	}()

	server := mockServerForConditionQueries(t,
		nil, // no not-ready clusters
		[]map[string]interface{}{
			createMockCluster("cluster-1", 2, 2, true, time.Now().Add(-31*time.Minute)),
		},
	)
	defer server.Close()

	hyperfleetClient, err := client.NewHyperFleetClient(server.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to create HyperFleet client: %v", err)
	}
	decisionEngine := engine.NewDecisionEngine(10*time.Second, 30*time.Minute)

	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	// Create metrics with a test registry
	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := &config.SentinelConfig{
		ResourceType:    "clusters",
		Topic:           "hyperfleet-clusters",
		MaxAgeNotReady:  10 * time.Second,
		MaxAgeReady:     30 * time.Minute,
		MessagingSystem: "rabbitmq",
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	// Execute trigger
	err = s.trigger(ctx)
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}

	// Force flush spans to exporter
	err = tp.ForceFlush(ctx)
	if err != nil {
		t.Fatalf("force flush error: %v", err)
	}

	// Verify spans were created
	spans := exporter.GetSpans()

	// Build map of span names for easy checking
	spanNames := make(map[string]bool)
	for _, span := range spans {
		spanNames[span.Name] = true
	}

	// Verify required spans exist
	requiredSpans := []string{
		"sentinel.poll",
		"sentinel.evaluate",
		"hyperfleet-clusters publish",
	}

	for _, requiredSpan := range requiredSpans {
		if !spanNames[requiredSpan] {
			t.Errorf("Required span '%s' not found. Found spans: %v", requiredSpan, getSpanNames(spans))
		}
	}

	// Verify we got the expected number of spans
	// Should have: 1 poll + 1 evaluate + 1 publish = 3 spans minimum
	if len(spans) < 3 {
		t.Errorf("Expected at least 3 spans, got %d. Spans: %v", len(spans), getSpanNames(spans))
	}

	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.system", cfg.MessagingSystem)
	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.operation.type", "publish")
	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.destination.name", cfg.Topic)

	// Verify the CloudEvent was published (basic functional verification)
	if len(mockPublisher.publishedEvents) != 1 {
		t.Errorf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

	// Verify CloudEvent has traceparent extension
	if len(mockPublisher.publishedEvents) > 0 {
		event := mockPublisher.publishedEvents[0]
		extensions := event.Extensions()
		if traceparent, exists := extensions["traceparent"]; !exists {
			t.Error("Expected CloudEvent to contain traceparent extension for trace propagation")
		} else if traceparentStr, ok := traceparent.(string); !ok || len(traceparentStr) != 55 {
			t.Errorf("Expected valid W3C traceparent format, got: %v", traceparent)
		}
	}
}

func validateSpanAttribute(t *testing.T, spans []tracetest.SpanStub, spanName, attrKey, expectedValue string) {
	for _, span := range spans {
		if span.Name == spanName {
			for _, attr := range span.Attributes {
				if string(attr.Key) == attrKey {
					if attr.Value.AsString() != expectedValue {
						t.Errorf("Span '%s': expected %s=%s, got %s", spanName, attrKey, expectedValue, attr.Value.AsString())
					}
					return
				}
			}
			t.Errorf("Span '%s': attribute '%s' not found", spanName, attrKey)
			return
		}
	}
	t.Errorf("Span '%s' not found", spanName)
}

// Helper function for span name extraction
func getSpanNames(spans []tracetest.SpanStub) []string {
	names := make([]string, len(spans))
	for i, span := range spans {
		names[i] = span.Name
	}
	return names
}
