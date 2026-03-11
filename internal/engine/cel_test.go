package engine

import (
	"testing"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
)

// ============================================================================
// CEL Custom Function Tests
// ============================================================================

func TestCELFunctions_Status(t *testing.T) {
	resourceMap := newTestResourceMap("True", time.Now())

	env, err := newConditionsCELEnv()
	if err != nil {
		t.Fatalf("Failed to create CEL env: %v", err)
	}

	ast, issues := env.Compile(`status(resource, "Ready")`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Failed to compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Failed to create program: %v", err)
	}

	out, _, err := prg.Eval(map[string]interface{}{"resource": resourceMap})
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}

	if out.Value() != "True" {
		t.Errorf("Expected 'True', got %v", out.Value())
	}
}

func TestCELFunctions_ConditionTime(t *testing.T) {
	refTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	resourceMap := newTestResourceMap("True", refTime)

	env, err := newConditionsCELEnv()
	if err != nil {
		t.Fatalf("Failed to create CEL env: %v", err)
	}

	ast, issues := env.Compile(`conditionTime(resource, "Ready")`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Failed to compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Failed to create program: %v", err)
	}

	out, _, err := prg.Eval(map[string]interface{}{"resource": resourceMap})
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}

	parsed, parseErr := time.Parse(time.RFC3339Nano, out.Value().(string))
	if parseErr != nil {
		t.Fatalf("Failed to parse time: %v", parseErr)
	}
	if !parsed.Equal(refTime) {
		t.Errorf("Expected %v, got %v", refTime, parsed)
	}
}

func TestCELFunctions_Condition(t *testing.T) {
	resourceMap := newTestResourceMap("False", time.Now())

	env, err := newConditionsCELEnv()
	if err != nil {
		t.Fatalf("Failed to create CEL env: %v", err)
	}

	ast, issues := env.Compile(`condition(resource, "Ready")`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Failed to compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Failed to create program: %v", err)
	}

	out, _, err := prg.Eval(map[string]interface{}{"resource": resourceMap})
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}

	condMap, ok := out.Value().(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map, got %T", out.Value())
	}
	if condMap["type"] != "Ready" {
		t.Errorf("Expected type 'Ready', got %v", condMap["type"])
	}
	if condMap["status"] != "False" {
		t.Errorf("Expected status 'False', got %v", condMap["status"])
	}
}

func TestCELFunctions_ConditionNotFound(t *testing.T) {
	resourceMap := newTestResourceMap("True", time.Now())

	env, err := newConditionsCELEnv()
	if err != nil {
		t.Fatalf("Failed to create CEL env: %v", err)
	}

	ast, issues := env.Compile(`status(resource, "NonExistent")`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Failed to compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Failed to create program: %v", err)
	}

	_, _, err = prg.Eval(map[string]interface{}{"resource": resourceMap})
	if err == nil {
		t.Fatal("Expected error for non-existent condition, got nil")
	}
}

// ============================================================================
// CompileConditions Tests
// ============================================================================

func TestCompileConditions_Valid(t *testing.T) {
	conditions := defaultTestConditions()

	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}
	if compiled == nil {
		t.Fatal("Expected non-nil compiled conditions")
	}
	if compiled.MinMaxAge() != 10*time.Second {
		t.Errorf("Expected min max age 10s, got %v", compiled.MinMaxAge())
	}
	names := compiled.RuleNames()
	if len(names) != 2 {
		t.Fatalf("Expected 2 rule names, got %d", len(names))
	}
	if names[0] != "isReady" || names[1] != "isNotReady" {
		t.Errorf("Unexpected rule names: %v", names)
	}
}

func TestCompileConditions_InvalidReferenceTime(t *testing.T) {
	conditions := config.Conditions{
		ReferenceTime: "invalid expression !!!",
		Rules: []config.ConditionRule{
			{Name: "test", Expression: `status(resource, "Ready") == "True"`, MaxAge: 30 * time.Minute},
		},
	}

	_, err := CompileConditions(conditions)
	if err == nil {
		t.Fatal("Expected error for invalid reference_time expression")
	}
}

func TestCompileConditions_InvalidRuleExpression(t *testing.T) {
	conditions := config.Conditions{
		ReferenceTime: `conditionTime(resource, "Ready")`,
		Rules: []config.ConditionRule{
			{Name: "bad", Expression: "invalid expression !!!", MaxAge: 30 * time.Minute},
		},
	}

	_, err := CompileConditions(conditions)
	if err == nil {
		t.Fatal("Expected error for invalid rule expression")
	}
}

// ============================================================================
// EvalReferenceTime Tests
// ============================================================================

func TestEvalReferenceTime_Success(t *testing.T) {
	conditions := defaultTestConditions()
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	refTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	resourceMap := newTestResourceMap("True", refTime)

	result, ok := compiled.EvalReferenceTime(resourceMap)
	if !ok {
		t.Fatal("Expected EvalReferenceTime to succeed")
	}
	if !result.Equal(refTime) {
		t.Errorf("Expected %v, got %v", refTime, result)
	}
}

func TestEvalReferenceTime_NoCondition(t *testing.T) {
	conditions := defaultTestConditions()
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	// Resource with no conditions
	resourceMap := map[string]interface{}{
		"status": map[string]interface{}{},
	}

	_, ok := compiled.EvalReferenceTime(resourceMap)
	if ok {
		t.Fatal("Expected EvalReferenceTime to fail for resource without conditions")
	}
}

// ============================================================================
// EvalMaxAge Tests
// ============================================================================

func TestEvalMaxAge_MatchReady(t *testing.T) {
	conditions := defaultTestConditions()
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	resourceMap := newTestResourceMap("True", time.Now())
	maxAge := compiled.EvalMaxAge(resourceMap)

	if maxAge != 30*time.Minute {
		t.Errorf("Expected 30m for ready, got %v", maxAge)
	}
}

func TestEvalMaxAge_MatchNotReady(t *testing.T) {
	conditions := defaultTestConditions()
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	resourceMap := newTestResourceMap("False", time.Now())
	maxAge := compiled.EvalMaxAge(resourceMap)

	if maxAge != 10*time.Second {
		t.Errorf("Expected 10s for not ready, got %v", maxAge)
	}
}

func TestEvalMaxAge_NoMatch_FallbackToMinAge(t *testing.T) {
	conditions := defaultTestConditions()
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	// Resource with Unknown status - no rule matches
	resourceMap := newTestResourceMap("Unknown", time.Now())
	maxAge := compiled.EvalMaxAge(resourceMap)

	if maxAge != 10*time.Second {
		t.Errorf("Expected 10s (min fallback), got %v", maxAge)
	}
}

func TestEvalMaxAge_CompoundExpression(t *testing.T) {
	conditions := config.Conditions{
		ReferenceTime: `conditionTime(resource, "Ready")`,
		Rules: []config.ConditionRule{
			{
				Name:       "readyAndAvailable",
				Expression: `status(resource, "Ready") == "True" && status(resource, "Available") == "True"`,
				MaxAge:     1 * time.Hour,
			},
			{
				Name:       "readyOnly",
				Expression: `status(resource, "Ready") == "True"`,
				MaxAge:     30 * time.Minute,
			},
			{
				Name:       "fallback",
				Expression: `status(resource, "Ready") == "False"`,
				MaxAge:     10 * time.Second,
			},
		},
	}
	compiled, err := CompileConditions(conditions)
	if err != nil {
		t.Fatalf("CompileConditions failed: %v", err)
	}

	// Resource with both Ready=True and Available=True should match first rule
	resourceMap := newTestResourceMapWithConditions([]testCondition{
		{Type: "Ready", Status: "True", LastUpdated: time.Now()},
		{Type: "Available", Status: "True", LastUpdated: time.Now()},
	})

	maxAge := compiled.EvalMaxAge(resourceMap)
	if maxAge != 1*time.Hour {
		t.Errorf("Expected 1h for compound match, got %v", maxAge)
	}

	// Resource with Ready=True but Available=False should match second rule
	resourceMap2 := newTestResourceMapWithConditions([]testCondition{
		{Type: "Ready", Status: "True", LastUpdated: time.Now()},
		{Type: "Available", Status: "False", LastUpdated: time.Now()},
	})

	maxAge2 := compiled.EvalMaxAge(resourceMap2)
	if maxAge2 != 30*time.Minute {
		t.Errorf("Expected 30m for readyOnly match, got %v", maxAge2)
	}
}

// ============================================================================
// Test helpers
// ============================================================================

func defaultTestConditions() config.Conditions {
	return config.Conditions{
		ReferenceTime: `conditionTime(resource, "Ready")`,
		Rules: []config.ConditionRule{
			{Name: "isReady", Expression: `status(resource, "Ready") == "True"`, MaxAge: 30 * time.Minute},
			{Name: "isNotReady", Expression: `status(resource, "Ready") == "False"`, MaxAge: 10 * time.Second},
		},
	}
}

type testCondition struct {
	Type        string
	Status      string
	LastUpdated time.Time
}

func newTestResourceMap(readyStatus string, lastUpdated time.Time) map[string]interface{} {
	return newTestResourceMapWithConditions([]testCondition{
		{Type: "Ready", Status: readyStatus, LastUpdated: lastUpdated},
	})
}

func newTestResourceMapWithConditions(conditions []testCondition) map[string]interface{} {
	condList := make([]interface{}, len(conditions))
	for i, c := range conditions {
		condList[i] = map[string]interface{}{
			"type":                 c.Type,
			"status":               c.Status,
			"last_updated_time":    c.LastUpdated.Format(time.RFC3339Nano),
			"last_transition_time": c.LastUpdated.Format(time.RFC3339Nano),
			"observed_generation":  int32(1),
		}
	}

	ready := false
	if len(conditions) > 0 {
		ready = conditions[0].Status == "True"
	}

	return map[string]interface{}{
		"id":           "test-resource-1",
		"kind":         "Cluster",
		"href":         "/api/v1/clusters/test-resource-1",
		"created_time": time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
		"updated_time": time.Now().Format(time.RFC3339Nano),
		"generation":   int32(1),
		"status": map[string]interface{}{
			"ready":               ready,
			"observed_generation": int32(1),
			"conditions":          condList,
		},
	}
}
