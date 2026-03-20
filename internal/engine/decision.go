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
	ReasonNeverProcessed    = "never processed"
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
	Reason        string // Human-readable explanation for the decision
	ShouldPublish bool   // Indicates whether an event should be published for the resource
}

// Evaluate determines if an event should be published for the resource.
//
// Decision Logic (in priority order):
//  1. Never-processed reconciliation: If status.LastUpdated is zero, publish immediately
//     (resource has never been processed by any adapter)
//  2. Generation-based reconciliation: If resource.Generation > status.ObservedGeneration,
//     publish immediately (spec has changed, adapter needs to reconcile)
//  3. Time-based reconciliation: If max age exceeded since last update, publish
//     - Uses status.LastUpdated as reference timestamp
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

	// Check if resource has never been processed by an adapter
	// LastUpdated is zero means no adapter has updated the status yet
	// This ensures first-time resources are published immediately
	if resource.Status.LastUpdated.IsZero() {
		return Decision{
			ShouldPublish: true,
			Reason:        ReasonNeverProcessed,
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
	// At this point, we know LastUpdated is not zero (checked above)
	// so we can use it directly for the max age calculation
	referenceTime := resource.Status.LastUpdated

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
