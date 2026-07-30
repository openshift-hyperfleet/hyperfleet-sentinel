package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/engine"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/metrics"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/payload"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/queue"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Sentinel polls the HyperFleet API and triggers reconciliation events
type Sentinel struct {
	lastSuccessfulPoll time.Time
	publisher          *queue.Publisher
	logger             logger.HyperFleetLogger
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
	pub *queue.Publisher,
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
	s.logger.Infof(ctx, "Starting sentinel resource_type=%s poll_interval=%s",
		s.config.ResourceType, s.config.PollInterval)

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

	// span: sentinel.poll
	ctx, pollSpan := telemetry.StartSpan(ctx, "sentinel.poll",
		attribute.String("hyperfleet.resource_type", s.config.ResourceType))
	defer pollSpan.End()

	// Get metric labels
	resourceType := s.config.ResourceType
	resourceSelector := metrics.GetResourceSelectorLabel(s.config.ResourceSelector)

	// Add subset to context for structured logging
	ctx = logger.WithSubset(ctx, resourceType)

	s.logger.Debug(ctx, "Starting trigger cycle")

	// Convert label selectors to map for filtering
	labelSelector := s.config.ResourceSelector.ToMap()

	// Fetch resources with adapter statuses
	resources, err := s.client.FetchResourceStatuses(ctx, s.config.ResourceType, labelSelector)
	if err != nil {
		pollSpan.RecordError(err)
		pollSpan.SetStatus(codes.Error, "fetch resources failed")
		errorType := "fetch_error"
		if client.IsTokenError(err) {
			errorType = "auth_error"
		}
		metrics.UpdateAPIErrorsMetric(resourceType, resourceSelector, errorType)
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
		// span: sentinel.evaluate
		evalCtx, evalSpan := telemetry.StartSpan(ctx, "sentinel.evaluate",
			attribute.String("hyperfleet.resource_type", s.config.ResourceType),
			attribute.String("hyperfleet.resource_id", resource.ID),
		)

		if resource.ID == "" {
			s.logger.Warnf(ctx, "Skipping resource with empty ID kind=%s", resource.Kind)
			evalSpan.End()
			continue
		}

		decision := s.decisionEngine.Evaluate(resource, now)
		evalSpan.SetAttributes(attribute.String("hyperfleet.decision_reason", decision.Reason))

		if decision.ShouldPublish {
			pending++

			// Add decision reason to context for structured logging
			eventCtx := logger.WithDecisionReason(evalCtx, decision.Reason)

			ownerRefs := "null"
			if resource.OwnerReferences != nil {
				ownerJSON, _ := json.Marshal(resource.OwnerReferences)
				ownerRefs = string(ownerJSON)
			}

			// Publish one message per required adapter
			for _, adapter := range s.config.RequiredAdapters {
				msg := &queue.QueueMessage{
					ResourceID:      resource.ID,
					Kind:            resource.Kind,
					TargetAdapter:   adapter,
					Href:            resource.Href,
					Generation:      resource.Generation,
					OwnerReferences: ownerRefs,
					EventType:       fmt.Sprintf("com.redhat.hyperfleet.%s.reconcile", strings.ToLower(resource.Kind)),
				}

				if err := s.publisher.Publish(eventCtx, msg); err != nil {
					evalSpan.RecordError(err)
					evalSpan.SetStatus(codes.Error, "publish failed")
					s.logger.Errorf(eventCtx, "Failed to publish queue message resource_id=%s adapter=%s error=%v", resource.ID, adapter, err)
					continue
				}

				published++
			}

			// Record successful event publication
			metrics.UpdateEventsPublishedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.Infof(eventCtx, "Published queue messages resource_id=%s adapters=%d", resource.ID, len(s.config.RequiredAdapters))
		} else {
			// Add decision reason to context for structured logging
			skipCtx := logger.WithDecisionReason(evalCtx, decision.Reason)

			// Record skipped resource
			metrics.UpdateResourcesSkippedMetric(resourceType, resourceSelector, decision.Reason)

			s.logger.Debugf(skipCtx, "Skipped resource resource_id=%s", resource.ID)
			skipped++
		}

		evalSpan.End()
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
	metrics.UpdateLastSuccessfulPollTimestampMetric()

	return nil
}
