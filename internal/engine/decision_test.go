package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// Test helpers and factories

// newTestResource creates a test resource with the given parameters
// This follows TRex pattern of using test factories for consistent test data
func newTestResource(ready bool, lastUpdated time.Time) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  1,                              // Default generation
		CreatedTime: time.Now().Add(-1 * time.Hour), // Default: created 1 hour ago
		Status: client.ResourceStatus{
			Ready:              ready,
			LastUpdated:        lastUpdated,
			ObservedGeneration: 1, // Default: in sync with generation
		},
	}
}

// newTestResourceWithCreatedTime creates a test resource with explicit created_time
func newTestResourceWithCreatedTime(id, kind string, ready bool, createdTime, lastUpdated time.Time) *client.Resource {
	return &client.Resource{
		ID:          id,
		Kind:        kind,
		Generation:  1, // Default generation
		CreatedTime: createdTime,
		Status: client.ResourceStatus{
			Ready:              ready,
			LastUpdated:        lastUpdated,
			ObservedGeneration: 1, // Default: in sync with generation
		},
	}
}

// newTestResourceWithGeneration creates a test resource with explicit generation values
func newTestResourceWithGeneration(id, kind string, ready bool, lastUpdated time.Time, generation, observedGeneration int32) *client.Resource {
	return &client.Resource{
		ID:          id,
		Kind:        kind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour), // Default: created 1 hour ago
		Status: client.ResourceStatus{
			Ready:              ready,
			LastUpdated:        lastUpdated,
			ObservedGeneration: observedGeneration,
		},
	}
}

// newTestEngine creates a decision engine with standard test values
func newTestEngine() *DecisionEngine {
	return NewDecisionEngine(testMaxAgeNotReady, testMaxAgeReady)
}

// assertDecision verifies a decision matches expected values
func assertDecision(t *testing.T, got Decision, wantPublish bool, wantReasonContains string) {
	t.Helper()

	if got.ShouldPublish != wantPublish {
		t.Errorf("ShouldPublish = %v, want %v", got.ShouldPublish, wantPublish)
	}

	if wantReasonContains != "" && !strings.Contains(got.Reason, wantReasonContains) {
		t.Errorf("Reason = %q, want it to contain %q", got.Reason, wantReasonContains)
	}

	if got.Reason == "" {
		t.Error("Reason should never be empty")
	}
}

// Test constants
const (
	testMaxAgeNotReady = 10 * time.Second
	testMaxAgeReady    = 30 * time.Minute
	testResourceID     = "test-cluster-1"
	testResourceKind   = "Cluster"
)

func TestNewDecisionEngine(t *testing.T) {
	engine := newTestEngine()

	if engine == nil {
		t.Fatal("NewDecisionEngine returned nil")
	}

	if engine.maxAgeNotReady != testMaxAgeNotReady {
		t.Errorf("maxAgeNotReady = %v, want %v", engine.maxAgeNotReady, testMaxAgeNotReady)
	}

	if engine.maxAgeReady != testMaxAgeReady {
		t.Errorf("maxAgeReady = %v, want %v", engine.maxAgeReady, testMaxAgeReady)
	}
}

func TestDecisionEngine_Evaluate(t *testing.T) {
	now := time.Now()
	engine := newTestEngine()

	tests := []struct {
		name               string
		ready              bool
		lastUpdated        time.Time
		now                time.Time
		wantShouldPublish  bool
		wantReasonContains string
		description        string
	}{
		// Zero LastUpdated tests - should fall back to created_time
		// These tests use the test factory default (created 1 hour ago)
		{
			name:               "zero LastUpdated - ready",
			ready:              true,
			lastUpdated:        time.Time{}, // Zero time - will use created_time
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Resources with zero LastUpdated should use created_time and publish (created > 30m ago)",
		},
		{
			name:               "zero LastUpdated - not ready",
			ready:              false,
			lastUpdated:        time.Time{}, // Zero time - will use created_time
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Resources with zero LastUpdated should use created_time and publish (created > 10s ago)",
		},

		// Not-ready resources (10s max age)
		{
			name:               "not ready - max age exceeded",
			ready:              false,
			lastUpdated:        now.Add(-11 * time.Second), // 11s ago (> 10s max age)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Not-ready resources with exceeded max age should publish",
		},
		{
			name:               "not ready - max age not exceeded",
			ready:              false,
			lastUpdated:        now.Add(-5 * time.Second), // 5s ago (< 10s max age)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Not-ready resources within max age should not publish",
		},
		{
			name:               "not ready - max age exactly exceeded",
			ready:              false,
			lastUpdated:        now.Add(-10 * time.Second), // Exactly 10s ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Not-ready resources with exactly exceeded max age should publish",
		},

		// Ready resources (30m max age)
		{
			name:               "ready - max age exceeded",
			ready:              true,
			lastUpdated:        now.Add(-31 * time.Minute), // 31m ago (> 30m max age)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Ready resources with exceeded max age should publish",
		},
		{
			name:               "ready - max age not exceeded",
			ready:              true,
			lastUpdated:        now.Add(-15 * time.Minute), // 15m ago (< 30m max age)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Ready resources within max age should not publish",
		},
		{
			name:               "ready - max age exactly exceeded",
			ready:              true,
			lastUpdated:        now.Add(-30 * time.Minute), // Exactly 30m ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Ready resources with exactly exceeded max age should publish",
		},

		// Edge cases
		{
			name:               "LastUpdated in future - ready",
			ready:              true,
			lastUpdated:        now.Add(1 * time.Hour), // 1 hour in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Resources with LastUpdated in future should not publish (clock skew protection)",
		},
		{
			name:               "LastUpdated in future - not ready",
			ready:              false,
			lastUpdated:        now.Add(1 * time.Minute), // 1 minute in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Not-ready resources with LastUpdated in future should not publish",
		},
		{
			name:               "LastUpdated very old - ready",
			ready:              true,
			lastUpdated:        now.Add(-24 * time.Hour), // 24 hours ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Very old resources should publish (max age long exceeded)",
		},
		{
			name:               "LastUpdated very recent - not ready",
			ready:              false,
			lastUpdated:        now.Add(-1 * time.Millisecond), // Just 1ms ago
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Very recent updates should not publish immediately",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newTestResource(tt.ready, tt.lastUpdated)
			decision := engine.Evaluate(resource, tt.now)

			assertDecision(t, decision, tt.wantShouldPublish, tt.wantReasonContains)

			// Additional context on failure
			if t.Failed() {
				t.Logf("Test description: %s", tt.description)
			}
		})
	}
}

// TestDecisionEngine_Evaluate_ZeroMaxAge tests edge case with zero max age intervals
func TestDecisionEngine_Evaluate_ZeroMaxAge(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name              string
		maxAgeNotReady    time.Duration
		maxAgeReady       time.Duration
		ready             bool
		lastUpdated       time.Time
		wantShouldPublish bool
	}{
		{
			name:              "zero maxAgeNotReady - not ready",
			maxAgeNotReady:    0,
			maxAgeReady:       30 * time.Minute,
			ready:             false,
			lastUpdated:       now, // Even with now, should publish due to zero max age
			wantShouldPublish: true,
		},
		{
			name:              "zero maxAgeReady - ready",
			maxAgeNotReady:    10 * time.Second,
			maxAgeReady:       0,
			ready:             true,
			lastUpdated:       now, // Even with now, should publish due to zero max age
			wantShouldPublish: true,
		},
		{
			name:              "both zero max ages - ready",
			maxAgeNotReady:    0,
			maxAgeReady:       0,
			ready:             true,
			lastUpdated:       now,
			wantShouldPublish: true,
		},
		{
			name:              "both zero max ages - not ready",
			maxAgeNotReady:    0,
			maxAgeReady:       0,
			ready:             false,
			lastUpdated:       now,
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.maxAgeNotReady, tt.maxAgeReady)
			resource := newTestResource(tt.ready, tt.lastUpdated)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, "")
		})
	}
}

// TestDecisionEngine_Evaluate_NegativeMaxAge tests edge case with negative max age intervals
func TestDecisionEngine_Evaluate_NegativeMaxAge(t *testing.T) {
	now := time.Now()
	lastUpdated := now.Add(-5 * time.Second)

	tests := []struct {
		name              string
		maxAgeNotReady    time.Duration
		maxAgeReady       time.Duration
		ready             bool
		wantShouldPublish bool
	}{
		{
			name:              "negative maxAgeNotReady",
			maxAgeNotReady:    -10 * time.Second,
			maxAgeReady:       30 * time.Minute,
			ready:             false,
			wantShouldPublish: true, // Negative max age means nextEventTime is in the past
		},
		{
			name:              "negative maxAgeReady",
			maxAgeNotReady:    10 * time.Second,
			maxAgeReady:       -10 * time.Minute,
			ready:             true,
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.maxAgeNotReady, tt.maxAgeReady)
			resource := newTestResource(tt.ready, lastUpdated)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, "")
		})
	}
}

// TestDecisionEngine_Evaluate_ConsistentBehavior tests that multiple calls with same inputs produce same results
func TestDecisionEngine_Evaluate_ConsistentBehavior(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()
	resource := newTestResource(true, now.Add(-31*time.Minute))

	// Call multiple times - should get same result
	decision1 := engine.Evaluate(resource, now)
	decision2 := engine.Evaluate(resource, now)
	decision3 := engine.Evaluate(resource, now)

	if decision1.ShouldPublish != decision2.ShouldPublish || decision1.ShouldPublish != decision3.ShouldPublish {
		t.Error("Evaluate should return consistent results for same inputs")
	}

	if decision1.Reason != decision2.Reason || decision1.Reason != decision3.Reason {
		t.Error("Evaluate should return consistent reason for same inputs")
	}
}

// TestDecisionEngine_Evaluate_InvalidInputs tests handling of invalid inputs
func TestDecisionEngine_Evaluate_InvalidInputs(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()

	tests := []struct {
		name              string
		resource          *client.Resource
		now               time.Time
		wantShouldPublish bool
		wantReason        string
	}{
		{
			name:              "nil resource",
			resource:          nil,
			now:               now,
			wantShouldPublish: false,
			wantReason:        ReasonNilResource,
		},
		{
			name:              "zero now time",
			resource:          newTestResource(true, now),
			now:               time.Time{}, // Zero time
			wantShouldPublish: false,
			wantReason:        ReasonZeroNow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := engine.Evaluate(tt.resource, tt.now)

			if decision.ShouldPublish != tt.wantShouldPublish {
				t.Errorf("ShouldPublish = %v, want %v", decision.ShouldPublish, tt.wantShouldPublish)
			}

			if decision.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}

// TestDecisionEngine_Evaluate_CreatedTimeFallback tests that created_time is used when lastUpdated is zero
func TestDecisionEngine_Evaluate_CreatedTimeFallback(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()

	tests := []struct {
		name               string
		createdTime        time.Time
		lastUpdated        time.Time
		ready              bool
		wantShouldPublish  bool
		wantReasonContains string
		description        string
	}{
		{
			name:               "zero lastUpdated - created 11s ago - not ready",
			createdTime:        now.Add(-11 * time.Second),
			lastUpdated:        time.Time{}, // Zero - should use created_time
			ready:              false,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Should use created_time and publish (11s > 10s max age)",
		},
		{
			name:               "zero lastUpdated - created 5s ago - not ready",
			createdTime:        now.Add(-5 * time.Second),
			lastUpdated:        time.Time{}, // Zero - should use created_time
			ready:              false,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Should use created_time and not publish (5s < 10s max age)",
		},
		{
			name:               "zero lastUpdated - created 31m ago - ready",
			createdTime:        now.Add(-31 * time.Minute),
			lastUpdated:        time.Time{}, // Zero - should use created_time
			ready:              true,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Should use created_time and publish (31m > 30m max age)",
		},
		{
			name:               "zero lastUpdated - created 15m ago - ready",
			createdTime:        now.Add(-15 * time.Minute),
			lastUpdated:        time.Time{}, // Zero - should use created_time
			ready:              true,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Should use created_time and not publish (15m < 30m max age)",
		},
		{
			name:               "non-zero lastUpdated - should ignore created_time",
			createdTime:        now.Add(-1 * time.Hour),   // Created long ago
			lastUpdated:        now.Add(-5 * time.Second), // Updated recently
			ready:              false,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Should use lastUpdated, not created_time (5s < 10s max age)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newTestResourceWithCreatedTime(testResourceID, testResourceKind, tt.ready, tt.createdTime, tt.lastUpdated)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, tt.wantReasonContains)

			// Additional context on failure
			if t.Failed() {
				t.Logf("Test description: %s", tt.description)
				t.Logf("Created time: %v", tt.createdTime)
				t.Logf("LastUpdated: %v", tt.lastUpdated)
			}
		})
	}
}

// TestDecisionEngine_Evaluate_GenerationBasedReconciliation tests generation-based reconciliation
func TestDecisionEngine_Evaluate_GenerationBasedReconciliation(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()

	tests := []struct {
		name               string
		generation         int32
		observedGeneration int32
		ready              bool
		lastUpdated        time.Time
		wantShouldPublish  bool
		wantReasonContains string
		description        string
	}{
		// Generation mismatch tests - should publish immediately
		{
			name:               "generation ahead by 1 - ready",
			generation:         2,
			observedGeneration: 1,
			ready:              true,
			lastUpdated:        now, // Even with recent update, should publish due to generation
			wantShouldPublish:  true,
			wantReasonContains: ReasonGenerationChanged,
			description:        "Spec change should trigger immediate reconciliation (ready)",
		},
		{
			name:               "generation ahead by 1 - not ready",
			generation:         3,
			observedGeneration: 2,
			ready:              false,
			lastUpdated:        now, // Even with recent update, should publish due to generation
			wantShouldPublish:  true,
			wantReasonContains: ReasonGenerationChanged,
			description:        "Spec change should trigger immediate reconciliation (not ready)",
		},
		{
			name:               "generation ahead by many - ready",
			generation:         10,
			observedGeneration: 5,
			ready:              true,
			lastUpdated:        now,
			wantShouldPublish:  true,
			wantReasonContains: ReasonGenerationChanged,
			description:        "Multiple spec changes should trigger immediate reconciliation",
		},
		{
			name:               "generation ahead - zero observedGeneration",
			generation:         1,
			observedGeneration: 0,
			ready:              true,
			lastUpdated:        now,
			wantShouldPublish:  true,
			wantReasonContains: ReasonGenerationChanged,
			description:        "First reconciliation should be triggered by generation (never processed)",
		},

		// Generation in sync tests - should follow normal max age logic
		{
			name:               "generation in sync - recent update - ready",
			generation:         5,
			observedGeneration: 5,
			ready:              true,
			lastUpdated:        now.Add(-15 * time.Minute), // 15m ago (< 30m max age)
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "When generations match, follow normal max age logic (ready)",
		},
		{
			name:               "generation in sync - recent update - not ready",
			generation:         2,
			observedGeneration: 2,
			ready:              false,
			lastUpdated:        now.Add(-5 * time.Second), // 5s ago (< 10s max age)
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "When generations match, follow normal max age logic (not ready)",
		},
		{
			name:               "generation in sync - max age exceeded - ready",
			generation:         3,
			observedGeneration: 3,
			ready:              true,
			lastUpdated:        now.Add(-31 * time.Minute), // 31m ago (> 30m max age)
			wantShouldPublish:  true,
			wantReasonContains: ReasonMaxAgeExceeded,
			description:        "When generations match and max age exceeded, publish (ready)",
		},
		{
			name:               "generation in sync - max age exceeded - not ready",
			generation:         1,
			observedGeneration: 1,
			ready:              false,
			lastUpdated:        now.Add(-11 * time.Second), // 11s ago (> 10s max age)
			wantShouldPublish:  true,
			wantReasonContains: ReasonMaxAgeExceeded,
			description:        "When generations match and max age exceeded, publish (not ready)",
		},

		// Edge cases
		{
			name:               "both generation and observedGeneration are zero",
			generation:         0,
			observedGeneration: 0,
			ready:              true,
			lastUpdated:        now.Add(-5 * time.Minute),
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Zero generations should be treated as in sync (defensive)",
		},
		{
			name:               "observedGeneration ahead of generation (defensive)",
			generation:         5,
			observedGeneration: 10,
			ready:              true,
			lastUpdated:        now.Add(-5 * time.Minute),
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "ObservedGeneration > Generation shouldn't happen, but handle defensively",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newTestResourceWithGeneration(
				testResourceID,
				testResourceKind,
				tt.ready,
				tt.lastUpdated,
				tt.generation,
				tt.observedGeneration,
			)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, tt.wantReasonContains)

			// Additional context on failure
			if t.Failed() {
				t.Logf("Test description: %s", tt.description)
				t.Logf("Generation: %d, ObservedGeneration: %d", tt.generation, tt.observedGeneration)
				t.Logf("Ready: %t, LastUpdated: %v", tt.ready, tt.lastUpdated)
			}
		})
	}
}
