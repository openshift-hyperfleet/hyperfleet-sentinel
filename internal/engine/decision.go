package engine

import (
	"fmt"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// Decision reasons
const (
	ReasonMaxAgeExceeded    = "max age exceeded"
	ReasonGenerationChanged = "generation changed"
	ReasonNilResource       = "resource is nil"
	ReasonZeroNow           = "now time is zero"
)

// DecisionEngine evaluates whether a resource needs an event published
type DecisionEngine struct {
	maxAgeNotReady time.Duration
	maxAgeReady    time.Duration
}

// NewDecisionEngine creates a new decision engine
func NewDecisionEngine(maxAgeNotReady, maxAgeReady time.Duration) *DecisionEngine {
	return &DecisionEngine{
		maxAgeNotReady: maxAgeNotReady,
		maxAgeReady:    maxAgeReady,
	}
}

// Decision represents the result of evaluating a resource
type Decision struct {
	// ShouldPublish indicates whether an event should be published for the resource
	ShouldPublish bool
	// Reason provides a human-readable explanation for the decision
	Reason string
}

// Evaluate determines if an event should be published for the resource.
//
// Decision Logic (in priority order):
//  1. Generation-based reconciliation: If resource.Generation > status.ObservedGeneration,
//     publish immediately (spec has changed, adapter needs to reconcile)
//  2. Time-based reconciliation: If max age exceeded since last update, publish
//     - Uses status.LastUpdated as reference timestamp
//     - If LastUpdated is zero (never processed), falls back to created_time
//
// Max Age Intervals:
//   - Resources with Ready=true: maxAgeReady (default 30m)
//   - Resources with Ready=false: maxAgeNotReady (default 10s)
//
// Adapter Contract:
//   - Adapters MUST update status.LastUpdated on EVERY evaluation
//   - Adapters MUST update status.ObservedGeneration to resource.Generation when processing
//   - This prevents infinite event loops when adapters skip work due to unmet preconditions
//
// Returns a Decision indicating whether to publish and why. Returns ShouldPublish=false
// for invalid inputs (nil resource, zero now time).
func (e *DecisionEngine) Evaluate(resource *client.Resource, now time.Time) Decision {
	// Validate inputs
	if resource == nil {
		return Decision{
			ShouldPublish: false,
			Reason:        ReasonNilResource,
		}
	}

	if now.IsZero() {
		return Decision{
			ShouldPublish: false,
			Reason:        ReasonZeroNow,
		}
	}

	// Check for generation mismatch
	// This triggers immediate reconciliation regardless of max age
	if resource.Generation > resource.Status.ObservedGeneration {
		return Decision{
			ShouldPublish: true,
			Reason:        ReasonGenerationChanged,
		}
	}

	// Determine the reference timestamp for max age calculation
	// Use LastUpdated if available (adapter has processed the resource)
	// Otherwise fall back to created_time (resource is newly created)
	referenceTime := resource.Status.LastUpdated
	if referenceTime.IsZero() {
		referenceTime = resource.CreatedTime
	}

	// Determine the appropriate max age based on resource ready status
	var maxAge time.Duration
	if resource.Status.Ready {
		maxAge = e.maxAgeReady
	} else {
		maxAge = e.maxAgeNotReady
	}

	// Calculate the next event time based on reference timestamp
	// Adapters update LastUpdated on every check, enabling proper max age
	// calculation even when resources stay in the same status
	nextEventTime := referenceTime.Add(maxAge)

	// Check if enough time has passed
	if now.Before(nextEventTime) {
		timeUntilNext := nextEventTime.Sub(now)
		return Decision{
			ShouldPublish: false,
			Reason:        fmt.Sprintf("max age not exceeded (waiting %s)", timeUntilNext),
		}
	}

	return Decision{
		ShouldPublish: true,
		Reason:        ReasonMaxAgeExceeded,
	}
}
