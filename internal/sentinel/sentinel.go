package sentinel

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/openshift-hyperfleet/hyperfleet-broker/broker"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

// Sentinel polls the HyperFleet API and triggers reconciliation events
type Sentinel struct {
	config         *config.SentinelConfig
	client         *client.HyperFleetClient
	decisionEngine *engine.DecisionEngine
	publisher      broker.Publisher
	logger         logger.HyperFleetLogger
}

// NewSentinel creates a new sentinel
func NewSentinel(
	cfg *config.SentinelConfig,
	client *client.HyperFleetClient,
	decisionEngine *engine.DecisionEngine,
	pub broker.Publisher,
	log logger.HyperFleetLogger,
) *Sentinel {
	return &Sentinel{
		config:         cfg,
		client:         client,
		decisionEngine: decisionEngine,
		publisher:      pub,
		logger:         log,
	}
}

// Start starts the polling loop
func (s *Sentinel) Start(ctx context.Context) error {
	s.logger.Infof(ctx, "Starting sentinel resource_type=%s poll_interval=%s max_age_not_ready=%s max_age_ready=%s",
		s.config.ResourceType, s.config.PollInterval, s.config.MaxAgeNotReady, s.config.MaxAgeReady)

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.trigger(ctx); err != nil {
		s.logger.Infof(ctx, "Initial trigger failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info(ctx, "Stopping sentinel due to context cancellation")
			return ctx.Err()
		case <-ticker.C:
			if err := s.trigger(ctx); err != nil {
				s.logger.Infof(ctx, "Trigger failed: %v", err)
			}
		}
	}
}

// trigger checks resources and publishes events to trigger reconciliation
func (s *Sentinel) trigger(ctx context.Context) error {
	startTime := time.Now()
	s.logger.V(2).Info(ctx, "Starting trigger cycle")

	// Get metric labels
	resourceType := s.config.ResourceType
	resourceSelector := metrics.GetResourceSelectorLabel(s.config.ResourceSelector)

	// Convert label selectors to map for filtering
	labelSelector := s.config.ResourceSelector.ToMap()

	// Fetch resources from HyperFleet API
	resources, err := s.client.FetchResources(ctx, client.ResourceType(s.config.ResourceType), labelSelector)
	if err != nil {
		// Record API error
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

		decision := s.decisionEngine.Evaluate(resource, now)

		if decision.ShouldPublish {
			pending++

			// Create CloudEvent
			event := cloudevents.NewEvent()
			event.SetSpecVersion(cloudevents.VersionV1)
			event.SetType(fmt.Sprintf("com.redhat.hyperfleet.%s.reconcile", resource.Kind))
			event.SetSource("hyperfleet-sentinel")
			event.SetID(uuid.New().String())
			if err := event.SetData(cloudevents.ApplicationJSON, map[string]interface{}{
				"kind":       resource.Kind,
				"id":         resource.ID,
				"generation": resource.Generation,
				"href":       resource.Href,
				"reason":     decision.Reason,
			}); err != nil {
				s.logger.Infof(ctx, "Failed to set event data resource_id=%s error=%v", resource.ID, err)
				continue
			}

			// Publish to broker using configured topic
			topic := s.config.Topic
			if err := s.publisher.Publish(topic, &event); err != nil {
				// Record broker error
				metrics.UpdateBrokerErrorsMetric(resourceType, resourceSelector, "publish_error")
				s.logger.Infof(ctx, "Failed to publish event resource_id=%s error=%v", resource.ID, err)
				continue
			}

			// Record successful event publication
			metrics.UpdateEventsPublishedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.Infof(ctx, "Published event resource_id=%s phase=%s reason=%s topic=%s",
				resource.ID, resource.Status.Phase, decision.Reason, topic)
			published++
		} else {
			// Record skipped resource
			metrics.UpdateResourcesSkippedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.V(2).Infof(ctx, "Skipped resource resource_id=%s phase=%s reason=%s",
				resource.ID, resource.Status.Phase, decision.Reason)
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

	return nil
}
