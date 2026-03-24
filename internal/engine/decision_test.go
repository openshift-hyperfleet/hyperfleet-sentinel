package engine

import (
	"testing"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
)

// Test helpers

const (
	testResourceID   = "test-cluster-1"
	testResourceKind = "Cluster"
)

// newDefaultDecisionConfig returns the default message decision config
// matching the sentinel architecture: ref_time, is_ready, is_new_resource,
// ready_and_stale, not_ready_and_debounced.
func newDefaultDecisionConfig() *config.MessageDecisionConfig {
	return config.DefaultMessageDecision()
}

// newTestDecisionEngine creates a decision engine with the default config.
func newTestDecisionEngine(t *testing.T) *DecisionEngine {
	t.Helper()
	cfg := newDefaultDecisionConfig()
	engine, err := NewDecisionEngine(cfg)
	if err != nil {
		t.Fatalf("NewDecisionEngine failed: %v", err)
	}
	return engine
}

// newResourceWithCondition creates a test resource with a single "Ready" condition.
func newResourceWithCondition(status string, lastUpdated time.Time, generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		Status: client.ResourceStatus{
			Conditions: []client.Condition{
				{
					Type:               "Ready",
					Status:             status,
					LastUpdatedTime:    lastUpdated,
					ObservedGeneration: generation,
				},
			},
		},
	}
}

// newResourceWithGenerationMismatch creates a test resource where generation > observed_generation.
func newResourceWithGenerationMismatch(
	status string, lastUpdated time.Time, generation, observedGeneration int32,
) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		Status: client.ResourceStatus{
			Conditions: []client.Condition{
				{
					Type:               "Ready",
					Status:             status,
					LastUpdatedTime:    lastUpdated,
					ObservedGeneration: observedGeneration,
				},
			},
		},
	}
}

// newResourceNoConditions creates a test resource with no conditions.
func newResourceNoConditions(generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		Status:      client.ResourceStatus{},
	}
}

func TestNewDecisionEngine(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		engine, err := NewDecisionEngine(newDefaultDecisionConfig())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if engine == nil {
			t.Fatal("engine is nil")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewDecisionEngine(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("invalid CEL expression", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{
				"bad": "this is not valid CEL !!!",
			},
			Result: "bad",
		}
		_, err := NewDecisionEngine(cfg)
		if err == nil {
			t.Fatal("expected error for invalid CEL expression")
		}
	})

	t.Run("invalid result expression", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{},
			Result: "not valid !!!",
		}
		_, err := NewDecisionEngine(cfg)
		if err == nil {
			t.Fatal("expected error for invalid result expression")
		}
	})

	t.Run("simple boolean result", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{},
			Result: "true",
		}
		engine, err := NewDecisionEngine(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if engine == nil {
			t.Fatal("engine is nil")
		}
	})
}

func TestDecisionEngine_Evaluate(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	tests := []struct {
		resource          *client.Resource
		now               time.Time
		name              string
		wantReason        string
		wantShouldPublish bool
	}{
		{
			name:              "ready and stale - should publish",
			resource:          newResourceWithCondition("True", now.Add(-31*time.Minute), 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "ready and recent - should not publish",
			resource:          newResourceWithCondition("True", now.Add(-5*time.Minute), 2),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "not ready and debounced - should publish",
			resource:          newResourceWithCondition("False", now.Add(-11*time.Second), 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "not ready and too recent - should not publish",
			resource:          newResourceWithCondition("False", now.Add(-3*time.Second), 2),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "new resource (generation 1, not ready) - should publish",
			resource:          newResourceWithCondition("False", now, 1),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "generation mismatch (ready, recent) - should publish immediately",
			resource:          newResourceWithGenerationMismatch("True", now.Add(-1*time.Minute), 3, 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "generation mismatch (not ready, recent) - should publish immediately",
			resource:          newResourceWithGenerationMismatch("False", now.Add(-1*time.Second), 5, 4),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "no generation mismatch (ready, recent) - should not publish",
			resource:          newResourceWithGenerationMismatch("True", now.Add(-1*time.Minute), 2, 2),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "nil resource - should not publish",
			resource:          nil,
			now:               now,
			wantShouldPublish: false,
			wantReason:        "resource is nil",
		},
		{
			name:              "zero now time - should not publish",
			resource:          newResourceWithCondition("True", now, 2),
			now:               time.Time{},
			wantShouldPublish: false,
			wantReason:        "now time is zero",
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

func TestDecisionEngine_Evaluate_MissingCondition(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	// Resource with no conditions at all - condition("Ready") returns zero-value.
	// Zero-value: status="" (not "True"), last_updated_time is zero time (very old).
	// is_ready = false, is_new_resource depends on generation.
	t.Run("no conditions generation 1 - new resource publishes", func(t *testing.T) {
		resource := newResourceNoConditions(1)
		decision := engine.Evaluate(resource, now)

		if !decision.ShouldPublish {
			t.Errorf("expected ShouldPublish=true for new resource with no conditions, got false")
		}
	})

	t.Run("no conditions generation 2 - generation mismatch publishes", func(t *testing.T) {
		resource := newResourceNoConditions(2)
		decision := engine.Evaluate(resource, now)

		// No conditions → observed_generation=0, generation=2 → generation_mismatch triggers
		if !decision.ShouldPublish {
			t.Errorf("expected ShouldPublish=true for resource with generation mismatch (gen=2, observed=0), got false")
		}
	})
}

func TestDecisionEngine_Evaluate_CustomExpressions(t *testing.T) {
	now := time.Now()

	t.Run("always true", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{},
			Result: "true",
		}
		engine, err := NewDecisionEngine(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := newResourceWithCondition("True", now, 2)
		decision := engine.Evaluate(resource, now)

		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true for always-true result")
		}
	})

	t.Run("always false", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{},
			Result: "false",
		}
		engine, err := NewDecisionEngine(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := newResourceWithCondition("True", now, 2)
		decision := engine.Evaluate(resource, now)

		if decision.ShouldPublish {
			t.Error("expected ShouldPublish=false for always-false result")
		}
	})

	t.Run("param chain with dependencies", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{
				"gen":        "resource.generation",
				"is_first":   "gen == 1",
				"should_pub": "is_first",
			},
			Result: "should_pub",
		}
		engine, err := NewDecisionEngine(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// generation 1 → should publish
		resource := newResourceWithCondition("False", now, 1)
		decision := engine.Evaluate(resource, now)
		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true for generation 1")
		}

		// generation 2 → should not publish
		resource2 := newResourceWithCondition("False", now, 2)
		decision2 := engine.Evaluate(resource2, now)
		if decision2.ShouldPublish {
			t.Error("expected ShouldPublish=false for generation 2")
		}
	})

	t.Run("condition function with custom condition name", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: map[string]string{
				"is_available": `condition("Available").status == "True"`,
			},
			Result: "is_available",
		}
		engine, err := NewDecisionEngine(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := &client.Resource{
			ID:         testResourceID,
			Kind:       testResourceKind,
			Generation: 1,
			Status: client.ResourceStatus{
				Conditions: []client.Condition{
					{Type: "Available", Status: "True", LastUpdatedTime: now},
				},
			},
		}

		decision := engine.Evaluate(resource, now)
		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true for Available=True condition")
		}

		// Missing Available condition → zero-value → status="" → false
		resource2 := &client.Resource{
			ID:         testResourceID,
			Kind:       testResourceKind,
			Generation: 1,
			Status: client.ResourceStatus{
				Conditions: []client.Condition{
					{Type: "Ready", Status: "True", LastUpdatedTime: now},
				},
			},
		}

		decision2 := engine.Evaluate(resource2, now)
		if decision2.ShouldPublish {
			t.Error("expected ShouldPublish=false when Available condition is missing")
		}
	})
}

func TestDecisionEngine_Evaluate_ConsistentBehavior(t *testing.T) {
	engine := newTestDecisionEngine(t)
	now := time.Now()
	resource := newResourceWithCondition("True", now.Add(-31*time.Minute), 2)

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

func TestDecisionEngine_Evaluate_ReadyBoundary(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	// Default ready_and_stale threshold is 30m (strictly greater than)
	tests := []struct {
		name              string
		lastUpdated       time.Duration
		wantShouldPublish bool
	}{
		{"exactly 30m - should not publish (> not >=)", -30 * time.Minute, false},
		{"29m59s - should not publish", -29*time.Minute - 59*time.Second, false},
		{"31m - should publish", -31 * time.Minute, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newResourceWithCondition("True", now.Add(tt.lastUpdated), 2)
			decision := engine.Evaluate(resource, now)

			if decision.ShouldPublish != tt.wantShouldPublish {
				t.Errorf("ShouldPublish = %v, want %v (lastUpdated offset: %v)",
					decision.ShouldPublish, tt.wantShouldPublish, tt.lastUpdated)
			}
		})
	}
}

func TestDecisionEngine_Evaluate_NotReadyBoundary(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	// Default not_ready_and_debounced threshold is 10s (strictly greater than)
	tests := []struct {
		name              string
		lastUpdated       time.Duration
		wantShouldPublish bool
	}{
		{"exactly 10s - should not publish (> not >=)", -10 * time.Second, false},
		{"9s - should not publish", -9 * time.Second, false},
		{"11s - should publish", -11 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newResourceWithCondition("False", now.Add(tt.lastUpdated), 2)
			decision := engine.Evaluate(resource, now)

			if decision.ShouldPublish != tt.wantShouldPublish {
				t.Errorf("ShouldPublish = %v, want %v (lastUpdated offset: %v)",
					decision.ShouldPublish, tt.wantShouldPublish, tt.lastUpdated)
			}
		})
	}
}

func TestBuildConditionsLookup(t *testing.T) {
	now := time.Now()
	conditions := []client.Condition{
		{
			Type:               "Ready",
			Status:             "True",
			LastUpdatedTime:    now,
			LastTransitionTime: now.Add(-1 * time.Hour),
			ObservedGeneration: 3,
		},
		{
			Type:               "Available",
			Status:             "False",
			LastUpdatedTime:    now.Add(-5 * time.Minute),
			LastTransitionTime: now.Add(-10 * time.Minute),
			ObservedGeneration: 2,
		},
	}

	lookup := buildConditionsLookup(conditions)

	if len(lookup) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lookup))
	}

	ready, ok := lookup["Ready"]
	if !ok {
		t.Fatal("missing Ready condition")
	}
	if ready["status"] != "True" {
		t.Errorf("Ready status = %v, want True", ready["status"])
	}
	if ready["observed_generation"] != int64(3) {
		t.Errorf("Ready observed_generation = %v, want 3", ready["observed_generation"])
	}

	avail, ok := lookup["Available"]
	if !ok {
		t.Fatal("missing Available condition")
	}
	if avail["status"] != "False" {
		t.Errorf("Available status = %v, want False", avail["status"])
	}
}

func TestBuildConditionsLookup_Empty(t *testing.T) {
	lookup := buildConditionsLookup(nil)
	if len(lookup) != 0 {
		t.Errorf("expected empty map, got %d entries", len(lookup))
	}
}

func TestZeroCondition(t *testing.T) {
	zero := zeroCondition()

	if zero["status"] != "" {
		t.Errorf("status = %q, want empty string", zero["status"])
	}
	if zero["observed_generation"] != int64(0) {
		t.Errorf("observed_generation = %v, want 0", zero["observed_generation"])
	}
	if zero["last_updated_time"] != "" {
		t.Errorf("last_updated_time = %q, want empty string", zero["last_updated_time"])
	}
	if zero["last_transition_time"] != "" {
		t.Errorf("last_transition_time = %q, want empty string", zero["last_transition_time"])
	}
}

func TestResourceToMap(t *testing.T) {
	now := time.Now()
	resource := &client.Resource{
		ID:          "res-1",
		Href:        "/api/v1/clusters/res-1",
		Kind:        "Cluster",
		Generation:  3,
		CreatedTime: now,
		UpdatedTime: now,
		Labels:      map[string]string{"env": "prod"},
		OwnerReferences: &client.OwnerReference{
			ID:   "owner-1",
			Href: "/api/v1/owners/owner-1",
			Kind: "Owner",
		},
	}

	m := resourceToMap(resource)

	if m["id"] != "res-1" {
		t.Errorf("id = %v, want res-1", m["id"])
	}
	if m["kind"] != "Cluster" {
		t.Errorf("kind = %v, want Cluster", m["kind"])
	}
	if m["generation"] != int64(3) {
		t.Errorf("generation = %v, want 3", m["generation"])
	}

	labels, ok := m["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("labels not found or wrong type")
	}
	if labels["env"] != "prod" {
		t.Errorf("labels.env = %v, want prod", labels["env"])
	}

	owner, ok := m["owner_references"].(map[string]interface{})
	if !ok {
		t.Fatal("owner_references not found or wrong type")
	}
	if owner["id"] != "owner-1" {
		t.Errorf("owner_references.id = %v, want owner-1", owner["id"])
	}
}

func TestResourceToMap_NoOptionalFields(t *testing.T) {
	resource := &client.Resource{
		ID:         "res-2",
		Kind:       "NodePool",
		Generation: 1,
	}

	m := resourceToMap(resource)

	if _, ok := m["labels"]; ok {
		t.Error("labels should not be present when empty")
	}
	if _, ok := m["owner_references"]; ok {
		t.Error("owner_references should not be present when nil")
	}
	if _, ok := m["metadata"]; ok {
		t.Error("metadata should not be present when nil")
	}
}
