package engine

import (
	"fmt"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/resource"
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
	conditions *CompiledConditions
}

// NewDecisionEngine creates a new decision engine with pre-compiled CEL conditions.
// If conditions is nil, the engine will never publish (safe default).
func NewDecisionEngine(conditions *CompiledConditions) *DecisionEngine {
	return &DecisionEngine{
		conditions: conditions,
	}
}

// emptyCompiledConditions is a safe sentinel for nil conditions
var emptyCompiledConditions = &CompiledConditions{}

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
//     - Evaluates the reference_time CEL expression to get the reference timestamp
//     - If reference_time evaluation fails or returns zero, falls back to created_time
//     - Evaluates rules in order; first matching rule's max_age is used
//     - If no rule matches, uses the smallest max_age (most conservative)
//
// Adapter Contract:
//   - Adapters MUST update condition LastUpdatedTime on EVERY evaluation
//   - Adapters MUST update status.ObservedGeneration to resource.Generation when processing
//   - This prevents infinite event loops when adapters skip work due to unmet preconditions
//
// Returns a Decision indicating whether to publish and why. Returns ShouldPublish=false
// for invalid inputs (nil resource, zero now time).
func (e *DecisionEngine) Evaluate(r *client.Resource, now time.Time) Decision {
	// Validate inputs
	if r == nil {
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
	if r.Generation > r.Status.ObservedGeneration {
		return Decision{
			ShouldPublish: true,
			Reason:        ReasonGenerationChanged,
		}
	}

	// Guard against nil conditions (safe default: never publish)
	conditions := e.conditions
	if conditions == nil {
		conditions = emptyCompiledConditions
	}

	// Convert resource to map for CEL evaluation
	resourceMap := resource.ToMap(r)

	// Evaluate reference time from CEL expression
	referenceTime, ok := conditions.EvalReferenceTime(resourceMap)
	if !ok || referenceTime.IsZero() {
		// Fall back to created_time if reference_time evaluation fails or returns zero
		referenceTime = r.CreatedTime
	}

	// Determine max age by evaluating rules (first match wins)
	maxAge := conditions.EvalMaxAge(resourceMap)

	// Calculate the next event time based on reference timestamp
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
