package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	testTopic        = "test-topic"
	testResourceKind = "Cluster"
)

// createMockCluster creates a mock cluster response matching the API spec.
func createMockCluster(
	id string,
	generation int,
	observedGeneration int,
	ready bool,
	lastUpdated time.Time,
) map[string]interface{} {
	readyStatus := "False"
	if ready {
		readyStatus = "True"
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
				{
					"type":                 "Ready",
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

// mockServerForResources creates a mock HTTP server that returns the given resources.
func mockServerForResources(t *testing.T, clusters []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := createMockClusterList(clusters)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Error encoding response: %v", err)
		}
	}))
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
	m.mockLogger.Info(ctx, fmt.Sprintf("broker publishing event to topic %s", topic))
	return nil
}

func (m *MockPublisherWithLogger) Close() error { return nil }

func (m *MockPublisherWithLogger) Health(ctx context.Context) error { return nil }

// newTestDecisionEngine creates a CEL-based decision engine with default config.
func newTestDecisionEngine(t *testing.T) *engine.DecisionEngine {
	t.Helper()
	cfg := config.DefaultMessageDecision()
	de, err := engine.NewDecisionEngine(cfg)
	if err != nil {
		t.Fatalf("NewDecisionEngine failed: %v", err)
	}
	return de
}

// newTestSentinelConfig creates a config for testing (no max_age fields).
func newTestSentinelConfig() *config.SentinelConfig {
	return &config.SentinelConfig{
		ResourceType:    "clusters",
		Topic:           testTopic,
		MessageDecision: config.DefaultMessageDecision(),
		MessageData: map[string]interface{}{
			"id":   "resource.id",
			"kind": "resource.kind",
		},
	}
}

// TestTrigger_Success tests successful event publishing
func TestTrigger_Success(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// Ready cluster with stale last_updated (> 30m) should trigger publish
	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(mockPublisher.publishedEvents) != 1 {
		t.Errorf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

	if len(mockPublisher.publishedTopics) != 1 || mockPublisher.publishedTopics[0] != testTopic {
		t.Errorf("Expected topic '%s', got %v", testTopic, mockPublisher.publishedTopics)
	}

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

	// Not-ready for only 1 second — within debounce (10s), should be skipped.
	// Generation > 1 so is_new_resource won't fire.
	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-1", 2, 2, false, time.Now().Add(-1*time.Second)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"error": "internal server error"}`)); err != nil {
			t.Logf("Error writing error response: %v", err)
		}
	}))
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 1*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)

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

	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{
		publishError: errors.New("broker connection failed"),
	}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
	if err != nil {
		t.Errorf("Expected no error (graceful degradation), got %v", err)
	}
}

// TestTrigger_MixedResources tests handling of multiple resources with different outcomes
func TestTrigger_MixedResources(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	server := mockServerForResources(t, []map[string]interface{}{
		// Should publish (not ready, debounce exceeded: 1min > 10s, generation > 1)
		createMockCluster("cluster-3", 3, 3, false, now.Add(-1*time.Minute)),
		// Should be skipped (empty ID)
		createMockCluster("", 1, 1, false, now.Add(-15*time.Second)),
		// Should publish (ready, stale: 31min > 30min)
		createMockCluster("cluster-1", 2, 2, true, now.Add(-31*time.Minute)),
		// Should not publish (ready, recent: 5min < 30min, generation > 1)
		createMockCluster("cluster-4", 5, 4, true, now.Add(-5*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// cluster-3: not ready + debounced → publish
	// cluster-1: ready + stale → publish
	// cluster-4: ready + recent, not new → skip
	if len(mockPublisher.publishedEvents) != 2 {
		t.Errorf("Expected 2 published events, got %d", len(mockPublisher.publishedEvents))
	}

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

	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-xyz", 2, 2, true, now.Add(-31*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()
	cfg.MessageData = map[string]interface{}{
		"id":     "resource.id",
		"kind":   "resource.kind",
		"origin": `"sentinel"`,
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
	if _, ok := data["reason"]; ok {
		t.Errorf("Expected 'reason' to be absent (hardcoded field), but found it")
	}
}

// TestTrigger_WithNestedMessageData verifies that nested objects are correctly built.
func TestTrigger_WithNestedMessageData(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-nest", 2, 2, true, now.Add(-31*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, _ := client.NewHyperFleetClient(server.URL, 10*time.Second)
	decisionEngine := newTestDecisionEngine(t)
	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()
	cfg.MessageData = map[string]interface{}{
		"id": "resource.id",
		"resource": map[string]interface{}{
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
	cfg := newTestSentinelConfig()
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
	decision := engine.Decision{ShouldPublish: true, Reason: "message decision matched"}
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

	mockPublisherWithLogger := &MockPublisherWithLogger{
		mockLogger: mockLogger,
	}

	ctx := context.Background()
	ctx = logger.WithDecisionReason(ctx, "message decision matched")
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

	if reason, ok := brokerCtx.Value(logger.DecisionReasonCtxKey).(string); !ok || reason != "message decision matched" {
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

func TestTrigger_CreatesRequiredSpans(t *testing.T) {
	ctx := context.Background()

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

	server := mockServerForResources(t, []map[string]interface{}{
		createMockCluster("cluster-1", 2, 2, true, time.Now().Add(-31*time.Minute)),
	})
	defer server.Close()

	hyperfleetClient, err := client.NewHyperFleetClient(server.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to create HyperFleet client: %v", err)
	}
	decisionEngine := newTestDecisionEngine(t)

	mockPublisher := &MockPublisher{}
	log := logger.NewHyperFleetLogger()

	registry := prometheus.NewRegistry()
	metrics.NewSentinelMetrics(registry, "test")

	cfg := newTestSentinelConfig()
	cfg.Topic = "hyperfleet-clusters"
	cfg.MessagingSystem = "rabbitmq"

	s, err := NewSentinel(cfg, hyperfleetClient, decisionEngine, mockPublisher, log)
	if err != nil {
		t.Fatalf("NewSentinel failed: %v", err)
	}

	err = s.trigger(ctx)
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}

	err = tp.ForceFlush(ctx)
	if err != nil {
		t.Fatalf("force flush error: %v", err)
	}

	spans := exporter.GetSpans()

	spanNames := make(map[string]bool)
	for _, span := range spans {
		spanNames[span.Name] = true
	}

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

	if len(spans) < 3 {
		t.Errorf("Expected at least 3 spans, got %d. Spans: %v", len(spans), getSpanNames(spans))
	}

	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.system", cfg.MessagingSystem)
	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.operation.type", "publish")
	validateSpanAttribute(t, spans, "hyperfleet-clusters publish", "messaging.destination.name", cfg.Topic)

	if len(mockPublisher.publishedEvents) != 1 {
		t.Errorf("Expected 1 published event, got %d", len(mockPublisher.publishedEvents))
	}

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

func getSpanNames(spans []tracetest.SpanStub) []string {
	names := make([]string, len(spans))
	for i, span := range spans {
		names[i] = span.Name
	}
	return names
}
