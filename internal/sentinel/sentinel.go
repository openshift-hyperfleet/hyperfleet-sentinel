package sentinel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	hfl "github.com/openshift-hyperfleet/hyperfleet-logger"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/logctx"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/payload"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// otelMessagingSystem maps broker type identifiers to OTel semantic convention values
var otelMessagingSystem = map[string]string{
	"googlepubsub": "gcp_pubsub",
}

func brokerTypeToOTel(brokerType string) string {
	if v, ok := otelMessagingSystem[brokerType]; ok {
		return v
	}
	return brokerType
}

// Sentinel polls the HyperFleet API and triggers reconciliation events
type Sentinel struct {
	lastSuccessfulPoll time.Time
	publisher          broker.Publisher
	config             *config.SentinelConfig
	client             *client.HyperFleetClient
	decisionEngine     *engine.DecisionEngine
	payloadBuilder     *payload.Builder
	mu                 sync.RWMutex
}

// NewSentinel creates a new sentinel
func NewSentinel(
	cfg *config.SentinelConfig,
	client *client.HyperFleetClient,
	decisionEngine *engine.DecisionEngine,
	pub broker.Publisher,
) (*Sentinel, error) {
	s := &Sentinel{
		config:         cfg,
		client:         client,
		decisionEngine: decisionEngine,
		publisher:      pub,
	}

	if cfg.MessageData != nil {
		builder, err := payload.NewBuilder(cfg.MessageData)
		if err != nil {
			return nil, fmt.Errorf("failed to create payload builder: %w", err)
		}
		s.payloadBuilder = builder
	}

	return s, nil
}

func (s *Sentinel) LastSuccessfulPoll() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSuccessfulPoll
}

// Start starts the polling loop
func (s *Sentinel) Start(ctx context.Context) error {
	slog.InfoContext(ctx, "Starting sentinel",
		"resource_type", s.config.ResourceType, "poll_interval", s.config.PollInterval)

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.trigger(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial trigger failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Stopping sentinel due to context cancellation")
			return ctx.Err()
		case <-ticker.C:
			if err := s.trigger(ctx); err != nil {
				slog.ErrorContext(ctx, "Trigger failed", "error", err)
			}
		}
	}
}

// trigger checks resources and publishes events to trigger reconciliation
func (s *Sentinel) trigger(ctx context.Context) error {
	startTime := time.Now()

	// span: sentinel.poll
	ctx, pollSpan := telemetry.StartSpan(ctx, "sentinel.poll",
		attribute.String("hyperfleet.resource_type", s.config.ResourceType))
	defer pollSpan.End()

	// Get metric labels
	resourceType := s.config.ResourceType
	resourceSelector := metrics.GetResourceSelectorLabel(s.config.ResourceSelector)
	topic := ""
	if s.config.Clients.Broker != nil {
		topic = s.config.Clients.Broker.Topic
	}

	ctx = hfl.Set(ctx, hfl.ResourceTypeKey, resourceType)
	ctx = hfl.Set(ctx, logctx.TopicKey, topic)

	slog.DebugContext(ctx, "Starting trigger cycle")

	// Convert label selectors to map for filtering
	labelSelector := s.config.ResourceSelector.ToMap()

	// Fetch all resources matching label selectors.
	// TODO(HYPERFLEET-805): Add optional server_filters config for server-side pre-filtering
	// to reduce the result set before CEL evaluation. Currently fetches the full result set
	// and evaluates each resource in-memory. At large scale, use resource_selector labels
	// to shard across multiple Sentinel instances.
	resources, err := s.client.FetchResources(ctx, s.config.ResourceType, labelSelector)
	if err != nil {
		// Record API error
		pollSpan.RecordError(err)
		pollSpan.SetStatus(codes.Error, "fetch resources failed")
		errorType := "fetch_error"
		if client.IsTokenError(err) {
			errorType = "auth_error"
		}
		metrics.UpdateAPIErrorsMetric(resourceType, resourceSelector, errorType)
		return fmt.Errorf("failed to fetch resources: %w", err)
	}

	slog.InfoContext(ctx, "Fetched resources",
		"count", len(resources), "label_selectors", len(s.config.ResourceSelector))

	now := time.Now()
	published := 0
	skipped := 0
	pending := 0

	publishSpanName := topic + " publish"

	// Evaluate each resource
	for i := range resources {
		resource := &resources[i]
		// span: sentinel.evaluate
		evalCtx, evalSpan := telemetry.StartSpan(ctx, "sentinel.evaluate",
			attribute.String("hyperfleet.resource_type", s.config.ResourceType),
			attribute.String("hyperfleet.resource_id", resource.ID),
		)
		evalCtx = hfl.Set(evalCtx, hfl.ResourceIDKey, resource.ID)

		if resource.ID == "" {
			slog.WarnContext(evalCtx, "Skipping resource with empty ID", "kind", resource.Kind)
			evalSpan.End()
			continue
		}

		decision := s.decisionEngine.Evaluate(resource, now)
		evalSpan.SetAttributes(attribute.String("hyperfleet.decision_reason", decision.Reason))
		evalCtx = hfl.Set(evalCtx, logctx.DecisionReasonKey, decision.Reason)

		if decision.ShouldPublish {
			pending++

			eventData := s.buildEventData(evalCtx, resource, decision)

			// Create CloudEvent
			event := cloudevents.NewEvent()
			event.SetSpecVersion(cloudevents.VersionV1)
			event.SetType(fmt.Sprintf("com.redhat.hyperfleet.%s.reconcile", strings.ToLower(resource.Kind)))
			event.SetSource("hyperfleet-sentinel")

			// Generate UUID v7 for event ID
			eventID, err := uuid.NewV7()
			if err != nil {
				slog.ErrorContext(evalCtx, "Failed to generate UUID v7 for event ID", "error", err)
				evalSpan.RecordError(err)
				evalSpan.SetStatus(codes.Error, "generate event ID failed")
				evalSpan.End()
				continue
			}
			event.SetID(eventID.String())

			if err := event.SetData(cloudevents.ApplicationJSON, eventData); err != nil {
				slog.ErrorContext(evalCtx, "Failed to set event data", "error", err)
				evalSpan.RecordError(err)
				evalSpan.SetStatus(codes.Error, "set event data failed")
				evalSpan.End()
				continue
			}

			// span: publish (child of sentinel.evaluate)
			publishCtx, publishSpan := telemetry.StartSpan(evalCtx, publishSpanName,
				attribute.String("messaging.system", brokerTypeToOTel(s.publisher.BrokerType())),
				attribute.String("messaging.operation.type", "publish"),
				attribute.String("messaging.destination.name", topic),
				attribute.String("messaging.message.id", event.ID()),
			)

			if publishSpan.SpanContext().IsValid() {
				telemetry.SetTraceContext(&event, publishSpan)
			}

			// Publish to broker using configured topic
			if err := s.publisher.Publish(publishCtx, topic, &event); err != nil {
				publishSpan.RecordError(err)
				publishSpan.SetStatus(codes.Error, "publish failed")
				// Record broker error
				metrics.UpdateBrokerErrorsMetric(resourceType, resourceSelector, "publish_error")
				slog.ErrorContext(publishCtx, "Failed to publish event", "error", err)
				publishSpan.End()
				evalSpan.End()
				continue
			}

			publishSpan.End()

			// Record successful event publication
			metrics.UpdateEventsPublishedMetric(resourceType, resourceSelector, decision.Reason)

			slog.InfoContext(evalCtx, "Published event")
			published++
		} else {
			// Record skipped resource
			metrics.UpdateResourcesSkippedMetric(resourceType, resourceSelector, decision.Reason)

			slog.DebugContext(evalCtx, "Skipped resource")
			skipped++
		}

		evalSpan.End()
	}

	// Record pending resources count
	metrics.UpdatePendingResourcesMetric(resourceType, resourceSelector, pending)

	// Record poll duration
	duration := time.Since(startTime).Seconds()
	metrics.UpdatePollDurationMetric(resourceType, resourceSelector, duration)

	slog.InfoContext(ctx, "Trigger cycle completed",
		"total", len(resources), "published", published,
		"skipped", skipped, "duration", duration)

	s.mu.Lock()
	s.lastSuccessfulPoll = time.Now()
	s.mu.Unlock()
	metrics.UpdateLastSuccessfulPollTimestampMetric()

	return nil
}

// buildEventData builds the CloudEvent data payload for a resource using the
// configured payload builder.
func (s *Sentinel) buildEventData(
	ctx context.Context,
	resource *client.Resource,
	decision engine.Decision,
) map[string]any {
	if s.payloadBuilder == nil {
		slog.ErrorContext(ctx, "payload builder not initialized", "resource_id", resource.ID)
		return map[string]any{}
	}
	return s.payloadBuilder.BuildPayload(ctx, resource, decision.Reason)
}
