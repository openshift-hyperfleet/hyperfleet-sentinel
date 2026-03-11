package engine

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/config"
)

// compiledRule holds a pre-compiled CEL program for a condition rule
type compiledRule struct {
	name    string
	program cel.Program
	maxAge  time.Duration
}

// CompiledConditions holds pre-compiled CEL programs for the condition evaluation
type CompiledConditions struct {
	referenceTime cel.Program
	rules         []compiledRule
	minMaxAge     time.Duration
}

// newConditionsCELEnv creates a CEL environment with custom functions for condition evaluation.
//
// Variables:
//   - resource (DynType) - the resource map
//
// Functions:
//   - condition(resource, type) → map  - returns the full condition map for the given type
//   - status(resource, type) → string  - returns the status string of a condition
//   - conditionTime(resource, type) → string - returns the last_updated_time (RFC3339) of a condition
func newConditionsCELEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("resource", cel.DynType),

		cel.Function("condition",
			cel.Overload("condition_resource_type",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(celCondition),
			),
		),

		cel.Function("status",
			cel.Overload("status_resource_type",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(celStatus),
			),
		),

		cel.Function("conditionTime",
			cel.Overload("conditionTime_resource_type",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(celConditionTime),
			),
		),
	)
}

// celCondition implements the condition(resource, type) CEL function.
// It finds the condition with the given type in resource.status.conditions and returns the full map.
func celCondition(lhs ref.Val, rhs ref.Val) ref.Val {
	condType := string(rhs.(types.String))

	cond, err := findCondition(lhs, condType)
	if err != nil {
		return types.NewErr("condition(%s): %s", condType, err)
	}

	return types.DefaultTypeAdapter.NativeToValue(cond)
}

// celStatus implements the status(resource, type) CEL function.
// It returns the status string of the condition with the given type.
func celStatus(lhs ref.Val, rhs ref.Val) ref.Val {
	condType := string(rhs.(types.String))

	cond, err := findCondition(lhs, condType)
	if err != nil {
		return types.NewErr("status(%s): %s", condType, err)
	}

	statusVal, ok := cond["status"]
	if !ok {
		return types.NewErr("status(%s): condition has no status field", condType)
	}

	statusStr, ok := statusVal.(string)
	if !ok {
		return types.NewErr("status(%s): status is not a string", condType)
	}

	return types.String(statusStr)
}

// celConditionTime implements the conditionTime(resource, type) CEL function.
// It returns the last_updated_time string of the condition with the given type.
func celConditionTime(lhs ref.Val, rhs ref.Val) ref.Val {
	condType := string(rhs.(types.String))

	cond, err := findCondition(lhs, condType)
	if err != nil {
		return types.NewErr("conditionTime(%s): %s", condType, err)
	}

	timeVal, ok := cond["last_updated_time"]
	if !ok {
		return types.NewErr("conditionTime(%s): condition has no last_updated_time field", condType)
	}

	timeStr, ok := timeVal.(string)
	if !ok {
		return types.NewErr("conditionTime(%s): last_updated_time is not a string", condType)
	}

	return types.String(timeStr)
}

// findCondition extracts the condition map matching the given type from a resource map.
func findCondition(resourceVal ref.Val, condType string) (map[string]interface{}, error) {
	resourceMap, ok := resourceVal.Value().(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("resource is not a map")
	}

	statusVal, ok := resourceMap["status"]
	if !ok {
		return nil, fmt.Errorf("resource has no status field")
	}

	statusMap, ok := statusVal.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("status is not a map")
	}

	conditionsVal, ok := statusMap["conditions"]
	if !ok {
		return nil, fmt.Errorf("no conditions found")
	}

	conditions, ok := conditionsVal.([]interface{})
	if !ok {
		return nil, fmt.Errorf("conditions is not a list")
	}

	for _, c := range conditions {
		condMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if condMap["type"] == condType {
			return condMap, nil
		}
	}

	return nil, fmt.Errorf("condition type %q not found", condType)
}

// CompileConditions pre-compiles all CEL expressions from the conditions config.
// This should be called at startup and will fail fast on invalid expressions.
func CompileConditions(conditions config.Conditions) (*CompiledConditions, error) {
	if len(conditions.Rules) == 0 {
		return nil, fmt.Errorf("conditions.Rules must have at least one rule")
	}

	env, err := newConditionsCELEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Compile reference_time expression
	refAst, issues := env.Compile(conditions.ReferenceTime)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile reference_time expression %q: %w", conditions.ReferenceTime, issues.Err())
	}
	// Enforce result type: reference_time must produce a string (or dyn)
	if refOutType := refAst.OutputType(); !refOutType.IsEquivalentType(cel.StringType) && !refOutType.IsEquivalentType(cel.DynType) {
		return nil, fmt.Errorf("reference_time expression %q must return string, got %s", conditions.ReferenceTime, refOutType)
	}
	refPrg, err := env.Program(refAst)
	if err != nil {
		return nil, fmt.Errorf("failed to create program for reference_time %q: %w", conditions.ReferenceTime, err)
	}

	// Compile rule expressions
	compiled := make([]compiledRule, len(conditions.Rules))
	var minAge time.Duration
	for i, rule := range conditions.Rules {
		if rule.MaxAge <= 0 {
			return nil, fmt.Errorf("rule %q has non-positive max_age %v", rule.Name, rule.MaxAge)
		}
		ast, issues := env.Compile(rule.Expression)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("failed to compile rule %q expression %q: %w", rule.Name, rule.Expression, issues.Err())
		}
		// Enforce result type: rule expressions must produce a bool (or dyn)
		if outType := ast.OutputType(); !outType.IsEquivalentType(cel.BoolType) && !outType.IsEquivalentType(cel.DynType) {
			return nil, fmt.Errorf("rule %q expression %q must return bool, got %s", rule.Name, rule.Expression, outType)
		}
		prg, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("failed to create program for rule %q expression %q: %w", rule.Name, rule.Expression, err)
		}
		compiled[i] = compiledRule{
			name:    rule.Name,
			program: prg,
			maxAge:  rule.MaxAge,
		}
		if i == 0 || rule.MaxAge < minAge {
			minAge = rule.MaxAge
		}
	}

	return &CompiledConditions{
		referenceTime: refPrg,
		rules:         compiled,
		minMaxAge:     minAge,
	}, nil
}

// EvalReferenceTime evaluates the reference_time expression against the resource map.
// Returns the parsed time and true if successful, or zero time and false if evaluation fails.
func (cc *CompiledConditions) EvalReferenceTime(resourceMap map[string]interface{}) (time.Time, bool) {
	if cc.referenceTime == nil {
		return time.Time{}, false
	}
	out, _, err := cc.referenceTime.Eval(map[string]interface{}{
		"resource": resourceMap,
	})
	if err != nil {
		return time.Time{}, false
	}

	timeStr, ok := out.Value().(string)
	if !ok {
		return time.Time{}, false
	}

	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		return time.Time{}, false
	}

	return t, true
}

// EvalMaxAge evaluates rules in order against the resource map.
// Returns the max_age of the first matching rule.
// If no rule matches, returns the smallest max_age (most conservative fallback).
func (cc *CompiledConditions) EvalMaxAge(resourceMap map[string]interface{}) time.Duration {
	activation := map[string]interface{}{
		"resource": resourceMap,
	}

	for _, rule := range cc.rules {
		out, _, err := rule.program.Eval(activation)
		if err != nil {
			continue
		}
		if out.Value() == true {
			return rule.maxAge
		}
	}

	return cc.minMaxAge
}

// MinMaxAge returns the smallest max age across all rules (most conservative fallback).
func (cc *CompiledConditions) MinMaxAge() time.Duration {
	return cc.minMaxAge
}

// RuleNames returns the names of all compiled rules.
func (cc *CompiledConditions) RuleNames() []string {
	names := make([]string, len(cc.rules))
	for i, r := range cc.rules {
		names[i] = r.name
	}
	return names
}
