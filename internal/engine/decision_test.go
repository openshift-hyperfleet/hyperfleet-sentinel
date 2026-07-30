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
	testAdapterName  = "validation"
)

var testRequiredAdapters = []string{testAdapterName}

func newDefaultDecisionConfig() *config.MessageDecisionConfig {
	return config.DefaultMessageDecision()
}

func newTestDecisionEngine(t *testing.T) *DecisionEngine {
	t.Helper()
	cfg := newDefaultDecisionConfig()
	engine, err := NewDecisionEngine(cfg, testRequiredAdapters)
	if err != nil {
		t.Fatalf("NewDecisionEngine failed: %v", err)
	}
	return engine
}

func newResourceReconciled(lastReportTime time.Time, generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		AdapterStatuses: []client.AdapterStatus{
			{
				Adapter:            testAdapterName,
				ObservedGeneration: generation,
				LastReportTime:     lastReportTime,
				Conditions: []client.AdapterCondition{
					{Type: "Available", Status: "True", LastTransitionTime: lastReportTime},
				},
			},
		},
	}
}

func newResourceNotReconciled(lastReportTime time.Time, generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		AdapterStatuses: []client.AdapterStatus{
			{
				Adapter:            testAdapterName,
				ObservedGeneration: generation,
				LastReportTime:     lastReportTime,
				Conditions: []client.AdapterCondition{
					{Type: "Available", Status: "False", LastTransitionTime: lastReportTime},
				},
			},
		},
	}
}

func newResourceGenMismatch(lastReportTime time.Time, generation, observedGeneration int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		AdapterStatuses: []client.AdapterStatus{
			{
				Adapter:            testAdapterName,
				ObservedGeneration: observedGeneration,
				LastReportTime:     lastReportTime,
				Conditions: []client.AdapterCondition{
					{Type: "Available", Status: "True", LastTransitionTime: lastReportTime},
				},
			},
		},
	}
}

func newResourceNoAdapterStatuses(generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
	}
}

func newResourceWithCondition(status string, lastUpdated time.Time, generation int32) *client.Resource {
	return &client.Resource{
		ID:          testResourceID,
		Kind:        testResourceKind,
		Generation:  generation,
		CreatedTime: time.Now().Add(-1 * time.Hour),
		Status: client.ResourceStatus{
			Conditions: []client.Condition{
				{
					Type:               "Reconciled",
					Status:             status,
					LastUpdatedTime:    lastUpdated,
					ObservedGeneration: generation,
				},
			},
		},
	}
}

func TestNewDecisionEngine(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		engine, err := NewDecisionEngine(newDefaultDecisionConfig(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if engine == nil {
			t.Fatal("engine is nil")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewDecisionEngine(nil, nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("invalid CEL expression", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{
				{Name: "bad", Expr: "this is not valid CEL !!!"},
			},
			Result: "bad",
		}
		_, err := NewDecisionEngine(cfg, nil)
		if err == nil {
			t.Fatal("expected error for invalid CEL expression")
		}
	})

	t.Run("invalid result expression", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{},
			Result: "not valid !!!",
		}
		_, err := NewDecisionEngine(cfg, nil)
		if err == nil {
			t.Fatal("expected error for invalid result expression")
		}
	})

	t.Run("simple boolean result", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{},
			Result: "true",
		}
		engine, err := NewDecisionEngine(cfg, nil)
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
			name:              "reconciled and stale - should publish",
			resource:          newResourceReconciled(now.Add(-31*time.Minute), 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "reconciled and recent - should not publish",
			resource:          newResourceReconciled(now.Add(-5*time.Minute), 2),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "not reconciled and debounced - should publish",
			resource:          newResourceNotReconciled(now.Add(-11*time.Second), 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "not reconciled and too recent - should not publish",
			resource:          newResourceNotReconciled(now.Add(-3*time.Second), 2),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "gen 1 with adapter status (adapter seen) - debounce applies, too recent",
			resource:          newResourceNotReconciled(now, 1),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "gen 1 no adapter statuses (truly new) - should publish immediately",
			resource:          newResourceNoAdapterStatuses(1),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "gen 1 with adapter status debounce exceeded - should publish via not_reconciled_and_debounced",
			resource:          newResourceNotReconciled(now.Add(-11*time.Second), 1),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "gen 1 reconciled recent - should not publish",
			resource:          newResourceReconciled(now.Add(-5*time.Minute), 1),
			now:               now,
			wantShouldPublish: false,
			wantReason:        "message decision result is false",
		},
		{
			name:              "gen 1 reconciled stale - should publish via reconciled_and_stale",
			resource:          newResourceReconciled(now.Add(-31*time.Minute), 1),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "generation mismatch (reconciled, recent) - should publish immediately",
			resource:          newResourceGenMismatch(now.Add(-1*time.Minute), 3, 2),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "generation mismatch (not reconciled, recent) - should publish immediately",
			resource:          newResourceGenMismatch(now.Add(-1*time.Second), 5, 4),
			now:               now,
			wantShouldPublish: true,
			wantReason:        "message decision matched",
		},
		{
			name:              "no generation mismatch (reconciled, recent) - should not publish",
			resource:          newResourceReconciled(now.Add(-1*time.Minute), 2),
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
			resource:          newResourceReconciled(now, 2),
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

func TestDecisionEngine_Evaluate_MissingAdapterStatus(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	t.Run("no adapter statuses generation 1 - new resource publishes", func(t *testing.T) {
		resource := newResourceNoAdapterStatuses(1)
		decision := engine.Evaluate(resource, now)

		if !decision.ShouldPublish {
			t.Errorf("expected ShouldPublish=true for new resource with no adapter statuses, got false")
		}
	})

	t.Run("no adapter statuses generation 2 - not reconciled debounce fails (no ref time)", func(t *testing.T) {
		resource := newResourceNoAdapterStatuses(2)
		decision := engine.Evaluate(resource, now)

		// No adapter statuses → all_adapters_available=false, latest_report_time=""
		// is_new_resource=false (gen!=1), generation_mismatch=true (adapters not current gen)
		if !decision.ShouldPublish {
			t.Errorf("expected ShouldPublish=true for resource with no adapter statuses and gen>1 (generation_mismatch), got false")
		}
	})
}

func TestDecisionEngine_Evaluate_CustomExpressions(t *testing.T) {
	now := time.Now()

	t.Run("always true", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{},
			Result: "true",
		}
		engine, err := NewDecisionEngine(cfg, nil)
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
			Params: []config.Param{},
			Result: "false",
		}
		engine, err := NewDecisionEngine(cfg, nil)
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
			Params: []config.Param{
				{Name: "gen", Expr: "resource.generation"},
				{Name: "is_first", Expr: "gen == 1"},
				{Name: "should_pub", Expr: "is_first"},
			},
			Result: "should_pub",
		}
		engine, err := NewDecisionEngine(cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := newResourceWithCondition("False", now, 1)
		decision := engine.Evaluate(resource, now)
		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true for generation 1")
		}

		resource2 := newResourceWithCondition("False", now, 2)
		decision2 := engine.Evaluate(resource2, now)
		if decision2.ShouldPublish {
			t.Error("expected ShouldPublish=false for generation 2")
		}
	})

	t.Run("condition function with custom condition name", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{
				{Name: "is_last_known_reconciled", Expr: `condition("LastKnownReconciled").status == "True"`},
			},
			Result: "is_last_known_reconciled",
		}
		engine, err := NewDecisionEngine(cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := &client.Resource{
			ID:         testResourceID,
			Kind:       testResourceKind,
			Generation: 1,
			Status: client.ResourceStatus{
				Conditions: []client.Condition{
					{Type: "LastKnownReconciled", Status: "True", LastUpdatedTime: now},
				},
			},
		}

		decision := engine.Evaluate(resource, now)
		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true for LastKnownReconciled=True condition")
		}

		resource2 := &client.Resource{
			ID:         testResourceID,
			Kind:       testResourceKind,
			Generation: 1,
			Status: client.ResourceStatus{
				Conditions: []client.Condition{
					{Type: "Reconciled", Status: "True", LastUpdatedTime: now},
				},
			},
		}

		decision2 := engine.Evaluate(resource2, now)
		if decision2.ShouldPublish {
			t.Error("expected ShouldPublish=false when LastKnownReconciled condition is missing")
		}
	})

	t.Run("adapterStatus function", func(t *testing.T) {
		cfg := &config.MessageDecisionConfig{
			Params: []config.Param{
				{Name: "val_gen", Expr: `adapterStatus("validation").observed_generation`},
			},
			Result: `val_gen == 3`,
		}
		engine, err := NewDecisionEngine(cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resource := &client.Resource{
			ID:         testResourceID,
			Kind:       testResourceKind,
			Generation: 3,
			AdapterStatuses: []client.AdapterStatus{
				{
					Adapter:            "validation",
					ObservedGeneration: 3,
					LastReportTime:     now,
					Conditions: []client.AdapterCondition{
						{Type: "Available", Status: "True", LastTransitionTime: now},
					},
				},
			},
		}

		decision := engine.Evaluate(resource, now)
		if !decision.ShouldPublish {
			t.Error("expected ShouldPublish=true when adapter observed_generation matches")
		}
	})
}

func TestDecisionEngine_Evaluate_ConsistentBehavior(t *testing.T) {
	engine := newTestDecisionEngine(t)
	now := time.Now()
	resource := newResourceReconciled(now.Add(-31*time.Minute), 2)

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

func TestDecisionEngine_Evaluate_ReconciledBoundary(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	tests := []struct {
		name              string
		lastReportOffset  time.Duration
		wantShouldPublish bool
	}{
		{"exactly 30m - should not publish (> not >=)", -30 * time.Minute, false},
		{"29m59s - should not publish", -29*time.Minute - 59*time.Second, false},
		{"31m - should publish", -31 * time.Minute, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newResourceReconciled(now.Add(tt.lastReportOffset), 2)
			decision := engine.Evaluate(resource, now)

			if decision.ShouldPublish != tt.wantShouldPublish {
				t.Errorf("ShouldPublish = %v, want %v (lastReport offset: %v)",
					decision.ShouldPublish, tt.wantShouldPublish, tt.lastReportOffset)
			}
		})
	}
}

func TestDecisionEngine_Evaluate_NotReconciledBoundary(t *testing.T) {
	now := time.Now()
	engine := newTestDecisionEngine(t)

	tests := []struct {
		name              string
		lastReportOffset  time.Duration
		wantShouldPublish bool
	}{
		{"exactly 10s - should not publish (> not >=)", -10 * time.Second, false},
		{"9s - should not publish", -9 * time.Second, false},
		{"11s - should publish", -11 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := newResourceNotReconciled(now.Add(tt.lastReportOffset), 2)
			decision := engine.Evaluate(resource, now)

			if decision.ShouldPublish != tt.wantShouldPublish {
				t.Errorf("ShouldPublish = %v, want %v (lastReport offset: %v)",
					decision.ShouldPublish, tt.wantShouldPublish, tt.lastReportOffset)
			}
		})
	}
}

func TestNewDecisionEngine_AcceptsStringHelperExpressions(t *testing.T) {
	cfg := &config.MessageDecisionConfig{
		Params: []config.Param{
			{Name: "channel_group", Expr: `"candidate-4.22".split("-")[0]`},
		},
		Result: `channel_group == "candidate"`,
	}
	engine, err := NewDecisionEngine(cfg, nil)
	if err != nil {
		t.Fatalf("ext.Strings() not registered — DecisionEngine rejects string helper expressions: %v", err)
	}
	decision := engine.Evaluate(newResourceWithCondition("True", time.Now(), 1), time.Now())
	if !decision.ShouldPublish {
		t.Fatalf("expected evaluation to succeed with string helper expression, got: %+v", decision)
	}
}

func TestBuildConditionsLookup(t *testing.T) {
	now := time.Now()
	conditions := []client.Condition{
		{
			Type:               "Reconciled",
			Status:             "True",
			LastUpdatedTime:    now,
			LastTransitionTime: now.Add(-1 * time.Hour),
			ObservedGeneration: 3,
		},
		{
			Type:               "LastKnownReconciled",
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

	reconciled, ok := lookup["Reconciled"]
	if !ok {
		t.Fatal("missing Reconciled condition")
	}
	if reconciled["status"] != "True" {
		t.Errorf("Reconciled status = %v, want True", reconciled["status"])
	}
	if reconciled["observed_generation"] != int64(3) {
		t.Errorf("Reconciled observed_generation = %v, want 3", reconciled["observed_generation"])
	}

	lkr, ok := lookup["LastKnownReconciled"]
	if !ok {
		t.Fatal("missing LastKnownReconciled condition")
	}
	if lkr["status"] != "False" {
		t.Errorf("LastKnownReconciled status = %v, want False", lkr["status"])
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

func TestBuildAdapterStatusLookup(t *testing.T) {
	now := time.Now()
	statuses := []client.AdapterStatus{
		{
			Adapter:            "validation",
			ObservedGeneration: 3,
			LastReportTime:     now,
			Conditions: []client.AdapterCondition{
				{Type: "Available", Status: "True", LastTransitionTime: now},
			},
		},
	}

	lookup := buildAdapterStatusLookup(statuses)

	if len(lookup) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lookup))
	}

	val, ok := lookup["validation"]
	if !ok {
		t.Fatal("missing validation adapter status")
	}
	if val["observed_generation"] != int64(3) {
		t.Errorf("observed_generation = %v, want 3", val["observed_generation"])
	}
}

func TestZeroAdapterStatus(t *testing.T) {
	zero := zeroAdapterStatus()

	if zero["adapter"] != "" {
		t.Errorf("adapter = %q, want empty string", zero["adapter"])
	}
	if zero["observed_generation"] != int64(0) {
		t.Errorf("observed_generation = %v, want 0", zero["observed_generation"])
	}
	if zero["last_report_time"] != "" {
		t.Errorf("last_report_time = %q, want empty string", zero["last_report_time"])
	}
}

func TestResourceToMap(t *testing.T) {
	now := time.Now()
	resource := &client.Resource{
		ID:          "res-1",
		Href:        "/api/v1/clusters/res-1",
		Kind:        "Cluster",
		Name:        "my-cluster",
		Generation:  3,
		CreatedTime: now,
		UpdatedTime: now,
		Labels:      map[string]string{"env": "prod"},
		Spec:        map[string]interface{}{"cloud_provider": "gcp"},
		OwnerReferences: &client.ObjectReference{
			ID:   "owner-1",
			Href: "/api/v1/owners/owner-1",
			Kind: "Owner",
		},
		References: map[string][]client.ObjectReference{
			"wif_config": {
				{ID: "wc-1", Kind: "WifConfig", Href: "/api/v1/resources/wc-1"},
			},
		},
	}

	m := resource.ToMap()

	if m["id"] != "res-1" {
		t.Errorf("id = %v, want res-1", m["id"])
	}
	if m["kind"] != "Cluster" {
		t.Errorf("kind = %v, want Cluster", m["kind"])
	}
	if m["name"] != "my-cluster" {
		t.Errorf("name = %v, want my-cluster", m["name"])
	}
	if m["generation"] != int64(3) {
		t.Errorf("generation = %v, want 3", m["generation"])
	}

	spec, ok := m["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("spec not found or wrong type")
	}
	if spec["cloud_provider"] != "gcp" {
		t.Errorf("spec.cloud_provider = %v, want gcp", spec["cloud_provider"])
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

	refs, ok := m["references"].(map[string]interface{})
	if !ok {
		t.Fatal("references not found or wrong type")
	}
	wifRefs, ok := refs["wif_config"].([]interface{})
	if !ok || len(wifRefs) != 1 {
		t.Fatalf("references.wif_config expected 1 item, got %v", refs["wif_config"])
	}
	wifRef, ok := wifRefs[0].(map[string]interface{})
	if !ok {
		t.Fatal("references.wif_config[0] wrong type")
	}
	if wifRef["id"] != "wc-1" {
		t.Errorf("references.wif_config[0].id = %v, want wc-1", wifRef["id"])
	}
}

func TestResourceToMap_NoOptionalFields(t *testing.T) {
	resource := &client.Resource{
		ID:         "res-2",
		Kind:       "NodePool",
		Generation: 1,
	}

	m := resource.ToMap()

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
