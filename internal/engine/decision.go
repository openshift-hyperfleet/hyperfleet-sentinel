package engine

import (
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// DecisionEngine evaluates whether a resource needs an event published
type DecisionEngine struct {
	backoffNotReady time.Duration
	backoffReady    time.Duration
}

// NewDecisionEngine creates a new decision engine
func NewDecisionEngine(backoffNotReady, backoffReady time.Duration) *DecisionEngine {
	return &DecisionEngine{
		backoffNotReady: backoffNotReady,
		backoffReady:    backoffReady,
	}
}

// Decision represents the result of evaluating a resource
type Decision struct {
	ShouldPublish bool
	Reason        string
}

// Evaluate determines if an event should be published for the resource
func (e *DecisionEngine) Evaluate(resource *client.Resource, now time.Time) Decision {
	// Determine the appropriate backoff based on resource status
	var backoff time.Duration
	if resource.Status.Phase == "Ready" {
		backoff = e.backoffReady
	} else {
		backoff = e.backoffNotReady
	}

	// Calculate the next event time based on last update from adapter
	// Using LastUpdated (updated on every adapter check) instead of LastTransitionTime
	// (which only updates when phase changes) prevents infinite loops and ensures
	// proper backoff behavior even when resources stay in the same phase
	nextEventTime := resource.Status.LastUpdated.Add(backoff)

	// Check if enough time has passed
	if now.Before(nextEventTime) {
		timeUntilNext := nextEventTime.Sub(now)
		return Decision{
			ShouldPublish: false,
			Reason:        "backoff not expired (waiting " + timeUntilNext.String() + ")",
		}
	}

	return Decision{
		ShouldPublish: true,
		Reason:        "backoff expired",
	}
}
