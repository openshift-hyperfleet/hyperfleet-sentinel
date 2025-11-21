package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// Decision reasons
const (
	ReasonMaxAgeExceeded = "max age exceeded"
	ReasonNilResource    = "resource is nil"
	ReasonZeroNow        = "now time is zero"
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
//   - Uses status.LastUpdated as the reference timestamp for max age calculation
//   - If LastUpdated is zero (resource never processed by adapter), falls back to created_time
//   - Publishes if max age has been exceeded since the reference timestamp
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

	// Determine the reference timestamp for max age calculation
	// Use LastUpdated if available (adapter has processed the resource)
	// Otherwise fall back to created_time (resource is newly created)
	referenceTime := resource.Status.LastUpdated
	if referenceTime.IsZero() {
		referenceTime = resource.CreatedTime
	}

	// Determine the appropriate max age based on resource status
	// Use case-insensitive comparison for robustness
	var maxAge time.Duration
	if strings.EqualFold(resource.Status.Phase, PhaseReady) {
		maxAge = e.maxAgeReady
	} else {
		maxAge = e.maxAgeNotReady
	}

	// Calculate the next event time based on reference timestamp
	// Adapters update LastUpdated on every check, enabling proper max age
	// calculation even when resources stay in the same phase
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
