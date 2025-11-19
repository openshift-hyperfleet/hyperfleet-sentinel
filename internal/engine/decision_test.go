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
	return NewDecisionEngine(testBackoffNotReady, testBackoffReady)
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
	testBackoffNotReady = 10 * time.Second
	testBackoffReady    = 30 * time.Minute
	testResourceID      = "test-cluster-1"
	testResourceKind    = "Cluster"
)

func TestNewDecisionEngine(t *testing.T) {
	engine := newTestEngine()

	if engine == nil {
		t.Fatal("NewDecisionEngine returned nil")
	}

	if engine.backoffNotReady != testBackoffNotReady {
		t.Errorf("backoffNotReady = %v, want %v", engine.backoffNotReady, testBackoffNotReady)
	}

	if engine.backoffReady != testBackoffReady {
		t.Errorf("backoffReady = %v, want %v", engine.backoffReady, testBackoffReady)
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

		// Not-Ready resources (10s backoff)
		{
			name:               "not-Ready - backoff expired",
			resourcePhase:      "Pending",
			lastUpdated:        now.Add(-11 * time.Second), // 11s ago (> 10s backoff)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Not-Ready resources with expired backoff should publish",
		},
		{
			name:               "not-Ready - backoff not expired",
			resourcePhase:      "Provisioning",
			lastUpdated:        now.Add(-5 * time.Second), // 5s ago (< 10s backoff)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "backoff not expired",
			description:        "Not-Ready resources with active backoff should not publish",
		},
		{
			name:               "not-Ready - backoff exactly expired",
			resourcePhase:      "Failed",
			lastUpdated:        now.Add(-10 * time.Second), // Exactly 10s ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Not-Ready resources with exactly expired backoff should publish",
		},

		// Ready resources (30m backoff)
		{
			name:               "Ready - backoff expired",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-31 * time.Minute), // 31m ago (> 30m backoff)
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Ready resources with expired backoff should publish",
		},
		{
			name:               "Ready - backoff not expired",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-15 * time.Minute), // 15m ago (< 30m backoff)
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "backoff not expired",
			description:        "Ready resources with active backoff should not publish",
		},
		{
			name:               "Ready - backoff exactly expired",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-30 * time.Minute), // Exactly 30m ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Ready resources with exactly expired backoff should publish",
		},

		// Edge cases
		{
			name:               "LastUpdated in future - Ready",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(1 * time.Hour), // 1 hour in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "backoff not expired",
			description:        "Resources with LastUpdated in future should not publish (clock skew protection)",
		},
		{
			name:               "LastUpdated in future - not Ready",
			resourcePhase:      "Pending",
			lastUpdated:        now.Add(1 * time.Minute), // 1 minute in the future
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "backoff not expired",
			description:        "Not-Ready resources with LastUpdated in future should not publish",
		},
		{
			name:               "LastUpdated very old - Ready",
			resourcePhase:      "Ready",
			lastUpdated:        now.Add(-24 * time.Hour), // 24 hours ago
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Very old resources should publish (backoff long expired)",
		},
		{
			name:               "LastUpdated very recent - not Ready",
			resourcePhase:      "Provisioning",
			lastUpdated:        now.Add(-1 * time.Millisecond), // Just 1ms ago
			now:                now,
			wantShouldPublish:  false,
			wantReasonContains: "backoff not expired",
			description:        "Very recent updates should not publish immediately",
		},

		// Different phase values
		{
			name:               "Empty phase - treated as not Ready",
			resourcePhase:      "",
			lastUpdated:        now.Add(-11 * time.Second),
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Empty phase should use not-Ready backoff (10s)",
		},
		{
			name:               "Unknown phase - treated as not Ready",
			resourcePhase:      "SomeUnknownPhase",
			lastUpdated:        now.Add(-11 * time.Second),
			now:                now,
			wantShouldPublish:  true,
			wantReasonContains: "backoff expired",
			description:        "Unknown phase should use not-Ready backoff (10s)",
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

// TestDecisionEngine_Evaluate_ZeroBackoff tests edge case with zero backoff intervals
func TestDecisionEngine_Evaluate_ZeroBackoff(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name              string
		backoffNotReady   time.Duration
		backoffReady      time.Duration
		resourcePhase     string
		lastUpdated       time.Time
		wantShouldPublish bool
	}{
		{
			name:              "zero backoffNotReady - not Ready",
			backoffNotReady:   0,
			backoffReady:      30 * time.Minute,
			resourcePhase:     "Pending",
			lastUpdated:       now, // Even with now, should publish due to zero backoff
			wantShouldPublish: true,
		},
		{
			name:              "zero backoffReady - Ready",
			backoffNotReady:   10 * time.Second,
			backoffReady:      0,
			resourcePhase:     "Ready",
			lastUpdated:       now, // Even with now, should publish due to zero backoff
			wantShouldPublish: true,
		},
		{
			name:              "both zero backoffs - Ready",
			backoffNotReady:   0,
			backoffReady:      0,
			resourcePhase:     "Ready",
			lastUpdated:       now,
			wantShouldPublish: true,
		},
		{
			name:              "both zero backoffs - not Ready",
			backoffNotReady:   0,
			backoffReady:      0,
			resourcePhase:     "Pending",
			lastUpdated:       now,
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.backoffNotReady, tt.backoffReady)
			resource := newTestResource(testResourceID, testResourceKind, tt.resourcePhase, tt.lastUpdated)
			decision := engine.Evaluate(resource, now)

			assertDecision(t, decision, tt.wantShouldPublish, "")
		})
	}
}

// TestDecisionEngine_Evaluate_NegativeBackoff tests edge case with negative backoff intervals
func TestDecisionEngine_Evaluate_NegativeBackoff(t *testing.T) {
	now := time.Now()
	lastUpdated := now.Add(-5 * time.Second)

	tests := []struct {
		name              string
		backoffNotReady   time.Duration
		backoffReady      time.Duration
		resourcePhase     string
		wantShouldPublish bool
	}{
		{
			name:              "negative backoffNotReady",
			backoffNotReady:   -10 * time.Second,
			backoffReady:      30 * time.Minute,
			resourcePhase:     "Pending",
			wantShouldPublish: true, // Negative backoff means nextEventTime is in the past
		},
		{
			name:              "negative backoffReady",
			backoffNotReady:   10 * time.Second,
			backoffReady:      -10 * time.Minute,
			resourcePhase:     "Ready",
			wantShouldPublish: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewDecisionEngine(tt.backoffNotReady, tt.backoffReady)
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
		wantBackoff   time.Duration
		description   string
	}{
		{
			name:        "Ready - exact case",
			phase:       "Ready",
			wantBackoff: 30 * time.Minute,
			description: "Exact 'Ready' should use 30m backoff",
		},
		{
			name:        "ready - lowercase",
			phase:       "ready",
			wantBackoff: 30 * time.Minute,
			description: "Lowercase 'ready' should use 30m backoff (case-insensitive)",
		},
		{
			name:        "READY - uppercase",
			phase:       "READY",
			wantBackoff: 30 * time.Minute,
			description: "Uppercase 'READY' should use 30m backoff (case-insensitive)",
		},
		{
			name:        "rEaDy - mixed case",
			phase:       "rEaDy",
			wantBackoff: 30 * time.Minute,
			description: "Mixed case 'rEaDy' should use 30m backoff (case-insensitive)",
		},
		{
			name:        "Pending - not Ready",
			phase:       "Pending",
			wantBackoff: 10 * time.Second,
			description: "Non-Ready phases should use 10s backoff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set LastUpdated to now + 1ms to ensure backoff hasn't expired yet
			resource := newTestResource(testResourceID, testResourceKind, tt.phase, now.Add(1*time.Millisecond))

			decision := engine.Evaluate(resource, now)

			// Should not publish because backoff hasn't expired
			if decision.ShouldPublish {
				t.Errorf("ShouldPublish = true, want false (backoff should not be expired yet)")
			}

			// Verify backoff by checking the waiting time in the reason message
			// For Ready (30m): should see "30m" in the message
			// For not-Ready (10s): should see "10s" in the message
			if strings.Contains(decision.Reason, "backoff not expired") {
				if strings.Contains(decision.Reason, "30m") && tt.wantBackoff != 30*time.Minute {
					t.Errorf("Expected ~10s backoff but got 30m backoff. Description: %s", tt.description)
				}
				if strings.Contains(decision.Reason, "10s") && tt.wantBackoff != 10*time.Second {
					t.Errorf("Expected ~30m backoff but got 10s backoff. Description: %s", tt.description)
				}
			}

			// Test that it DOES publish after backoff expires
			futureNow := now.Add(tt.wantBackoff + 2*time.Millisecond)
			futureDecision := engine.Evaluate(resource, futureNow)

			if !futureDecision.ShouldPublish {
				t.Errorf("ShouldPublish = false after backoff expired, want true. Description: %s", tt.description)
			}
		})
	}
}
