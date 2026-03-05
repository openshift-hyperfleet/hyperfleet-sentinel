package sentinel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/payload"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

const (
	// notReadyFilter selects resources whose Ready condition is False.
	notReadyFilter = "status.conditions.Ready='False'"

	// staleReadyFilterFmt selects resources whose Ready condition is True but
	// last_updated_time is older than the given cutoff (RFC 3339 timestamp).
	staleReadyFilterFmt = "status.conditions.Ready='True' and status.conditions.Ready.last_updated_time<='%s'"
)

// Sentinel polls the HyperFleet API and triggers reconciliation events
type Sentinel struct {
	config         *config.SentinelConfig
	client         *client.HyperFleetClient
	decisionEngine *engine.DecisionEngine
	publisher      broker.Publisher
	logger         logger.HyperFleetLogger

	mu                 sync.RWMutex
	lastSuccessfulPoll time.Time
	payloadBuilder *payload.Builder
}

// NewSentinel creates a new sentinel
func NewSentinel(
	cfg *config.SentinelConfig,
	client *client.HyperFleetClient,
	decisionEngine *engine.DecisionEngine,
	pub broker.Publisher,
	log logger.HyperFleetLogger,
) (*Sentinel, error) {
	s := &Sentinel{
		config:         cfg,
		client:         client,
		decisionEngine: decisionEngine,
		publisher:      pub,
		logger:         log,
	}

	if cfg.MessageData != nil {
		builder, err := payload.NewBuilder(cfg.MessageData, log)
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
	s.logger.Infof(ctx, "Starting sentinel resource_type=%s poll_interval=%s max_age_not_ready=%s max_age_ready=%s",
		s.config.ResourceType, s.config.PollInterval, s.config.MaxAgeNotReady, s.config.MaxAgeReady)

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.trigger(ctx); err != nil {
		s.logger.Errorf(ctx, "Initial trigger failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info(ctx, "Stopping sentinel due to context cancellation")
			return ctx.Err()
		case <-ticker.C:
			if err := s.trigger(ctx); err != nil {
				s.logger.Errorf(ctx, "Trigger failed: %v", err)
			}
		}
	}
}

// trigger checks resources and publishes events to trigger reconciliation
func (s *Sentinel) trigger(ctx context.Context) error {
	startTime := time.Now()

	// Get metric labels
	resourceType := s.config.ResourceType
	resourceSelector := metrics.GetResourceSelectorLabel(s.config.ResourceSelector)
	topic := s.config.Topic

	// Add subset to context for structured logging
	ctx = logger.WithSubset(ctx, resourceType)
	ctx = logger.WithTopic(ctx, topic)

	s.logger.Debug(ctx, "Starting trigger cycle")

	// Convert label selectors to map for filtering
	labelSelector := s.config.ResourceSelector.ToMap()

	// Fetch resources using condition-based server-side filtering:
	// Query 1: Not-ready resources (need frequent reconciliation)
	// Query 2: Stale ready resources (exceeded max age)
	resources, err := s.fetchFilteredResources(ctx, labelSelector)
	if err != nil {
		metrics.UpdateAPIErrorsMetric(resourceType, resourceSelector, "fetch_error")
		return fmt.Errorf("failed to fetch resources: %w", err)
	}

	s.logger.Infof(ctx, "Fetched resources count=%d label_selectors=%d", len(resources), len(s.config.ResourceSelector))

	now := time.Now()
	published := 0
	skipped := 0
	pending := 0

	// Evaluate each resource
	for i := range resources {
		resource := &resources[i]

		if resource.ID == "" {
			s.logger.Warnf(ctx, "Skipping resource with empty ID kind=%s", resource.Kind)
			continue
		}

		decision := s.decisionEngine.Evaluate(resource, now)

		if decision.ShouldPublish {
			pending++

			// Add decision reason to context for structured logging
			eventCtx := logger.WithDecisionReason(ctx, decision.Reason)

			eventData := s.buildEventData(eventCtx, resource, decision)

			// Create CloudEvent
			event := cloudevents.NewEvent()
			event.SetSpecVersion(cloudevents.VersionV1)
			event.SetType(fmt.Sprintf("com.redhat.hyperfleet.%s.reconcile", resource.Kind))
			event.SetSource("hyperfleet-sentinel")
			event.SetID(uuid.New().String())
			if err := event.SetData(cloudevents.ApplicationJSON, eventData); err != nil {
				s.logger.Errorf(eventCtx, "Failed to set event data resource_id=%s error=%v", resource.ID, err)
				continue
			}

			// Publish to broker using configured topic
			if err := s.publisher.Publish(eventCtx, topic, &event); err != nil {
				// Record broker error
				metrics.UpdateBrokerErrorsMetric(resourceType, resourceSelector, "publish_error")
				s.logger.Errorf(eventCtx, "Failed to publish event resource_id=%s error=%v", resource.ID, err)
				continue
			}

			// Record successful event publication
			metrics.UpdateEventsPublishedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.Infof(eventCtx, "Published event resource_id=%s ready=%t",
				resource.ID, resource.Status.Ready)
			published++
		} else {
			// Add decision reason to context for structured logging
			skipCtx := logger.WithDecisionReason(ctx, decision.Reason)

			// Record skipped resource
			metrics.UpdateResourcesSkippedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.Debugf(skipCtx, "Skipped resource resource_id=%s ready=%t",
				resource.ID, resource.Status.Ready)
			skipped++
		}
	}

	// Record pending resources count
	metrics.UpdatePendingResourcesMetric(resourceType, resourceSelector, pending)

	// Record poll duration
	duration := time.Since(startTime).Seconds()
	metrics.UpdatePollDurationMetric(resourceType, resourceSelector, duration)

	s.logger.Infof(ctx, "Trigger cycle completed total=%d published=%d skipped=%d duration=%.3fs",
		len(resources), published, skipped, duration)

	s.mu.Lock()
	s.lastSuccessfulPoll = time.Now()
	s.mu.Unlock()

	return nil
}

// fetchFilteredResources makes two targeted API queries to fetch only resources
// that likely need reconciliation, reducing network traffic compared to fetching
// all resources:
//
//  1. Not-ready resources: status.conditions.Ready='False'
//  2. Stale ready resources: Ready='True' with last_updated_time older than max_age_ready
//
// Results are merged and deduplicated by resource ID. The DecisionEngine still
// evaluates the filtered set in memory (e.g., for generation-based checks).
func (s *Sentinel) fetchFilteredResources(ctx context.Context, labelSelector map[string]string) ([]client.Resource, error) {
	rt := client.ResourceType(s.config.ResourceType)

	// Query 1: Not-ready resources
	notReadyResources, err := s.client.FetchResources(ctx, rt, labelSelector, notReadyFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch not-ready resources: %w", err)
	}
	s.logger.Debugf(ctx, "Fetched not-ready resources count=%d", len(notReadyResources))

	// Query 2: Stale ready resources (last_updated_time exceeded max_age_ready)
	cutoff := time.Now().Add(-s.config.MaxAgeReady)
	staleFilter := fmt.Sprintf(staleReadyFilterFmt, cutoff.Format(time.RFC3339))
	staleResources, err := s.client.FetchResources(ctx, rt, labelSelector, staleFilter)
	if err != nil {
		// Propagate context cancellation/timeout — the caller must see these
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Graceful degradation: for transient/API errors, continue with not-ready results
		s.logger.Errorf(ctx, "Failed to fetch stale resources, continuing with not-ready only: %v", err)
		return notReadyResources, nil
	}
	s.logger.Debugf(ctx, "Fetched stale ready resources count=%d", len(staleResources))

	return mergeResources(notReadyResources, staleResources), nil
}

// mergeResources combines two resource slices, deduplicating by resource ID.
// Resources from the first slice take precedence when duplicates are found.
func mergeResources(a, b []client.Resource) []client.Resource {
	seen := make(map[string]struct{}, len(a))
	result := make([]client.Resource, 0, len(a)+len(b))

	for i := range a {
		seen[a[i].ID] = struct{}{}
		result = append(result, a[i])
	}
	for i := range b {
		if _, exists := seen[b[i].ID]; !exists {
			result = append(result, b[i])
		}
	}
	return result
}

// buildEventData builds the CloudEvent data payload for a resource using the
// configured payload builder.
func (s *Sentinel) buildEventData(ctx context.Context, resource *client.Resource, decision engine.Decision) map[string]interface{} {
	if s.payloadBuilder == nil {
		s.logger.Errorf(ctx, "payload builder not initialized for resource_id=%s", resource.ID)
		return map[string]interface{}{}
	}
	return s.payloadBuilder.BuildPayload(ctx, resource, decision.Reason)
}
