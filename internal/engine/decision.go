package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// Decision reasons
const (
	ReasonFirstReconciliation = "first reconciliation (LastUpdated is zero)"
	ReasonMaxAgeExceeded      = "max age exceeded"
	ReasonNilResource         = "resource is nil"
	ReasonZeroNow             = "now time is zero"
)

// Phase values
const (
	PhaseReady = "Ready"
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
// Decision Logic:
//   - First reconciliation (zero LastUpdated): Always publish to trigger initial adapter evaluation
//   - Subsequent reconciliations: Publish if max age has been exceeded since LastUpdated
//
// Max Age Intervals:
//   - Resources with Phase="Ready": maxAgeReady (default 30m)
//   - Resources with Phase≠"Ready": maxAgeNotReady (default 10s)
//
// Adapter Contract:
//   - Adapters MUST update status.LastUpdated on EVERY evaluation
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

	// Handle first reconciliation - resources with zero LastUpdated have never been processed
	// Always publish to trigger initial adapter evaluation
	if resource.Status.LastUpdated.IsZero() {
		return Decision{
			ShouldPublish: true,
			Reason:        ReasonFirstReconciliation,
		}
	}

	// Determine the appropriate max age based on resource status
	// Use case-insensitive comparison for robustness
	var maxAge time.Duration
	if strings.EqualFold(resource.Status.Phase, PhaseReady) {
		maxAge = e.maxAgeReady
	} else {
		maxAge = e.maxAgeNotReady
	}

	// Calculate the next event time based on last update from adapter
	// Adapters update LastUpdated on every check, enabling proper max age
	// calculation even when resources stay in the same phase
	nextEventTime := resource.Status.LastUpdated.Add(maxAge)

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
