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
func newTestResource(id, kind, phase string, lastUpdated time.Time) *client.Resource {
	return &client.Resource{
		ID:   id,
		Kind: kind,
		Status: client.ResourceStatus{
			Phase:       phase,
			LastUpdated: lastUpdated,
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
		name                string
		resourcePhase       string
		lastUpdated         time.Time
		now                 time.Time
		wantShouldPublish   bool
		wantReasonContains  string
		description         string
	}{
		// First reconciliation (zero LastUpdated) tests
		{
			name:               "first reconciliation - zero LastUpdated Ready",
			resourcePhase:      "Ready",
			lastUpdated:        time.Time{}, // Zero time
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "first reconciliation",
			description:        "Resources with zero LastUpdated should always publish (first time)",
		},
		{
			name:               "first reconciliation - zero LastUpdated not Ready",
			resourcePhase:      "Pending",
			lastUpdated:        time.Time{}, // Zero time
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "first reconciliation",
			description:        "Resources with zero LastUpdated should always publish regardless of phase",
		},

		// Not-Ready resources (10s max age)
		{
			name:               "not-Ready - max age exceeded",
			resourcePhase:      "Pending",
			lastUpdated:        now.Add(-11 * time.Second), // 11s ago (> 10s max age)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Not-Ready resources with exceeded max age should publish",
		},
		{
			name:               "not-Ready - max age not exceeded",
			resourcePhase:      "Provisioning",
			lastUpdated:        now.Add(-5 * time.Second), // 5s ago (< 10s max age)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Not-Ready resources within max age should not publish",
		},
		{
			name:               "not-Ready - max age exactly exceeded",
			resourcePhase:      "Failed",
			lastUpdated:        now.Add(-10 * time.Second), // Exactly 10s ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Not-Ready resources with exactly exceeded max age should publish",
		},

		// Ready resources (30m max age)
		{
			name:               "Ready - max age exceeded",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-31 * time.Minute), // 31m ago (> 30m max age)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Ready resources with exceeded max age should publish",
		},
		{
			name:               "Ready - max age not exceeded",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-15 * time.Minute), // 15m ago (< 30m max age)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Ready resources within max age should not publish",
		},
		{
			name:               "Ready - max age exactly exceeded",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-30 * time.Minute), // Exactly 30m ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Ready resources with exactly exceeded max age should publish",
		},

		// Edge cases
		{
			name:               "LastUpdated in future - Ready",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(1 * time.Hour), // 1 hour in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Resources with LastUpdated in future should not publish (clock skew protection)",
		},
		{
			name:               "LastUpdated in future - not Ready",
			resourcePhase:      "Pending",
			lastUpdated:        now.Add(1 * time.Minute), // 1 minute in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Not-Ready resources with LastUpdated in future should not publish",
		},
		{
			name:               "LastUpdated very old - Ready",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-24 * time.Hour), // 24 hours ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Very old resources should publish (max age long exceeded)",
		},
		{
			name:               "LastUpdated very recent - not Ready",
			resourcePhase:      "Provisioning",
			lastUpdated:        now.Add(-1 * time.Millisecond), // Just 1ms ago
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "max age not exceeded",
			description:        "Very recent updates should not publish immediately",
		},

		// Different phase values
		{
			name:               "Empty phase - treated as not Ready",
			resourcePhase:      "",
			lastUpdated:        now.Add(-11 * time.Second),
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Empty phase should use not-Ready max age (10s)",
		},
		{
			name:               "Unknown phase - treated as not Ready",
			resourcePhase:      "SomeUnknownPhase",
			lastUpdated:        now.Add(-11 * time.Second),
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "max age exceeded",
			description:        "Unknown phase should use not-Ready max age (10s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newTestResource(testResourceID, testResourceKind, tt.resourcePhase, tt.lastUpdated)
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
		resourcePhase     string
		lastUpdated       time.Time
		wantShouldPublish bool
	}{
		{
			name:              "zero maxAgeNotReady - not Ready",
			maxAgeNotReady:    0,
			maxAgeReady:       30 * time.Minute,
			resourcePhase:     "Pending",
			lastUpdated:       now, // Even with now, should publish due to zero max age
			wantShouldPublish: true,
		},
		{
			name:              "zero maxAgeReady - Ready",
			maxAgeNotReady:    10 * time.Second,
			maxAgeReady:       0,
			resourcePhase:     "Ready",
			lastUpdated:       now, // Even with now, should publish due to zero max age
			wantShouldPublish: true,
		},
		{
			name:              "both zero max ages - Ready",
			maxAgeNotReady:    0,
			maxAgeReady:       0,
			resourcePhase:     "Ready",
			lastUpdated:       now,
			wantShouldPublish: true,
		},
		{
			name:              "both zero max ages - not Ready",
			maxAgeNotReady:    0,
			maxAgeReady:       0,
			resourcePhase:     "Pending",
			lastUpdated:       now,
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.maxAgeNotReady, tt.maxAgeReady)
			resource := newTestResource(testResourceID, testResourceKind, tt.resourcePhase, tt.lastUpdated)
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
		resourcePhase     string
		wantShouldPublish bool
	}{
		{
			name:              "negative maxAgeNotReady",
			maxAgeNotReady:    -10 * time.Second,
			maxAgeReady:       30 * time.Minute,
			resourcePhase:     "Pending",
			wantShouldPublish: true, // Negative max age means nextEventTime is in the past
		},
		{
			name:              "negative maxAgeReady",
			maxAgeNotReady:    10 * time.Second,
			maxAgeReady:       -10 * time.Minute,
			resourcePhase:     "Ready",
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.maxAgeNotReady, tt.maxAgeReady)
			resource := newTestResource(testResourceID, testResourceKind, tt.resourcePhase, lastUpdated)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, "")
		})
	}
}

// TestDecisionEngine_Evaluate_ConsistentBehavior tests that multiple calls with same inputs produce same results
func TestDecisionEngine_Evaluate_ConsistentBehavior(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()
	resource := newTestResource(testResourceID, testResourceKind, "Ready", now.Add(-31*time.Minute))

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
			resource:          newTestResource(testResourceID, testResourceKind, "Ready", now),
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

// TestDecisionEngine_Evaluate_CaseInsensitivePhase tests case-insensitive phase comparison
func TestDecisionEngine_Evaluate_CaseInsensitivePhase(t *testing.T) {
	engine := newTestEngine()
	now := time.Now()

	tests := []struct {
		name          string
		phase         string
		wantMaxAge    time.Duration
		description   string
	}{
		{
			name:        "Ready - exact case",
			phase:       "Ready",
			wantMaxAge:  30 * time.Minute,
			description: "Exact 'Ready' should use 30m max age",
		},
		{
			name:        "ready - lowercase",
			phase:       "ready",
			wantMaxAge:  30 * time.Minute,
			description: "Lowercase 'ready' should use 30m max age (case-insensitive)",
		},
		{
			name:        "READY - uppercase",
			phase:       "READY",
			wantMaxAge:  30 * time.Minute,
			description: "Uppercase 'READY' should use 30m max age (case-insensitive)",
		},
		{
			name:        "rEaDy - mixed case",
			phase:       "rEaDy",
			wantMaxAge:  30 * time.Minute,
			description: "Mixed case 'rEaDy' should use 30m max age (case-insensitive)",
		},
		{
			name:        "Pending - not Ready",
			phase:       "Pending",
			wantMaxAge:  10 * time.Second,
			description: "Non-Ready phases should use 10s max age",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set LastUpdated to now + 1ms to ensure max age hasn't been exceeded yet
			resource := newTestResource(testResourceID, testResourceKind, tt.phase, now.Add(1*time.Millisecond))

			decision := engine.Evaluate(resource, now)

			// Should not publish because max age hasn't been exceeded
			if decision.ShouldPublish {
				t.Errorf("ShouldPublish = true, want false (max age should not be exceeded yet)")
			}

			// Verify max age by checking the waiting time in the reason message
			// For Ready (30m): should see "30m" in the message
			// For not-Ready (10s): should see "10s" in the message
			if strings.Contains(decision.Reason, "max age not exceeded") {
				if strings.Contains(decision.Reason, "30m") && tt.wantMaxAge != 30*time.Minute {
					t.Errorf("Expected ~10s max age but got 30m max age. Description: %s", tt.description)
				}
				if strings.Contains(decision.Reason, "10s") && tt.wantMaxAge != 10*time.Second {
					t.Errorf("Expected ~30m max age but got 10s max age. Description: %s", tt.description)
				}
			}

			// Test that it DOES publish after max age is exceeded
			futureNow := now.Add(tt.wantMaxAge + 2*time.Millisecond)
			futureDecision := engine.Evaluate(resource, futureNow)

			if !futureDecision.ShouldPublish {
				t.Errorf("ShouldPublish = false after max age exceeded, want true. Description: %s", tt.description)
			}
		})
	}
}
