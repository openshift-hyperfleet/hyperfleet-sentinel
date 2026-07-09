# Decision Engine Reference

> **Audience:** Developers working on or extending the Sentinel decision engine.

This document provides the detailed design reference for the Sentinel decision engine, including the CEL function reference, status tracking semantics, adapter status contract, and test scenarios. For operator-focused configuration guidance, see the [Operator Guide](sentinel-operator-guide.md). For configuration field reference, see [config.md](config.md).

## Table of Contents

- [Message Decision](#message-decision)
  - [How It Works](#how-it-works)
  - [Default Configuration](#default-configuration)
  - [Why Debounce?](#why-debounce)
  - [Key Design Decisions](#key-design-decisions)
- [CEL Function Reference](#cel-function-reference)
  - [condition(name)](#conditionname)
- [Status Tracking](#status-tracking)
  - [Resource Status Conditions](#resource-status-conditions)
  - [Field Semantics](#field-semantics)
  - [How Generation Changes Flow Through the System](#how-generation-changes-flow-through-the-system)
  - [Why last_updated_time for Age Calculation](#why-last_updated_time-for-age-calculation)
- [Adapter Status Update Contract](#adapter-status-update-contract)
  - [Why This Matters](#why-this-matters)
  - [Required Adapter Behavior](#required-adapter-behavior)
  - [Integration Testing Requirements](#integration-testing-requirements)
- [Service Components](#service-components)
  - [Config Loader](#config-loader)
  - [Resource Watcher](#resource-watcher)
  - [Decision Engine](#decision-engine)
  - [Message Publisher](#message-publisher)
  - [Main Reconciler](#main-reconciler)
- [Decision Engine Test Scenarios](#decision-engine-test-scenarios)
  - [Message Decision Tests](#message-decision-tests)
  - [Edge Cases](#edge-cases)
  - [Test Requirements](#test-requirements)

---

## Message Decision

The Sentinel uses a `message_decision` configuration with named **params** and a boolean **result** expression. This is the **sole decision mechanism** — there are no hardcoded checks.

### How It Works

1. **Params** are named variables defined as CEL expressions
2. Params can reference other params (evaluated in authored order — params must be declared after their dependencies)
3. The **result** expression combines params using standard CEL logical operators (`&&`, `||`) to produce a boolean
4. If result is `true`, a reconciliation event is published

### Default Configuration

When `message_decision` is omitted from the config file, Sentinel uses the following defaults:

| Param Name | Type | Expression | Purpose |
|------------|------|------------|---------|
| `ref_time` | CEL → string | `condition("Reconciled").last_updated_time` | Reference timestamp for age calculation |
| `is_reconciled` | CEL → bool | `condition("Reconciled").status == "True"` | Whether resource is reconciled |
| `has_ref_time` | CEL → bool | `ref_time != ""` | Guard: Reconciled condition exists |
| `is_new_resource` | CEL → bool | `resource.generation == 1 && !has_ref_time` | Brand-new resource that needs immediate reconciliation |
| `generation_mismatch` | CEL → bool | `resource.generation > condition("Reconciled").observed_generation` | Resource spec changed since last reconciliation |
| `reconciled_and_stale` | CEL → bool | `is_reconciled && has_ref_time && now - timestamp(ref_time) > duration("30m")` | Reconciled resource whose last check is stale |
| `not_reconciled_and_debounced` | CEL → bool | `!is_reconciled && has_ref_time && now - timestamp(ref_time) > duration("10s")` | Not-reconciled resource, debounce period elapsed |

**Result**: `is_new_resource || generation_mismatch || reconciled_and_stale || not_reconciled_and_debounced`

**Key Insight — Why No Hardcoded Generation Check:**

The API already aggregates adapter statuses into the `Reconciled` condition. When a user changes the resource spec (incrementing `generation`), the API sets `Reconciled` to `False` because not all adapters have reconciled the new generation yet. This means `Reconciled == False` already covers the generation mismatch case — there is no need for the Sentinel to duplicate this logic with a separate generation check. However, the default configuration includes `generation_mismatch` as an explicit param for clarity and to handle edge cases where the API may not yet have aggregated the condition.

### Why Debounce?

The Sentinel polls every cycle (5s by default) and will publish a message for not-ready resources. This provides fast reaction time to changes including newly created resources, but can create unnecessary load in downstream services.

Debouncing limits the rate at which events fire by delaying execution until a specified amount of time has passed since the last event. It groups multiple rapid, sequential events into a single action, improving performance by reducing unnecessary API calls to adapters.

By introducing a debounce interval (10s default), the Sentinel limits the messages published for a resource — ensuring adapters have time to complete their work before triggering the next reconciliation cycle. Brand-new resources (`is_new_resource`) bypass this debounce because no adapter has processed them yet — there is no "previous work" to wait for.

### Key Design Decisions

- All CEL expressions are compiled at startup (fail-fast on invalid configuration)
- Params are evaluated in authored order; if param B references param A, A must be declared first
- The `now` variable (current timestamp) is available in all expressions
- The `result` is the **sole decision maker** — all time-based checks, condition evaluations, and reconciliation triggers are encoded in params (no hardcoded checks)
- The `result` expression uses standard CEL logical operators (`&&`, `||`). No aliases or custom operator syntax — pure CEL
- A single custom helper function `condition(name)` provides access to resource status data (see [CEL Function Reference](#cel-function-reference)). Fields are accessed directly (e.g., `condition("Reconciled").status`), keeping the API surface minimal
- This aligns with the adapter framework's preconditions pattern (CEL-based evaluation)

---

## CEL Function Reference

### condition(name)

A single custom function is registered at startup. The `resource` parameter is implicit — the function already knows the resource structure.

**Signature:** `condition(name string) → Condition`

Returns the full condition object matching the given `type` name.

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `.status` | string | `"True"`, `"False"`, or `"Unknown"` |
| `.observed_generation` | int | Which generation this condition last reconciled |
| `.last_updated_time` | string | ISO 8601 timestamp of last adapter check |
| `.last_transition_time` | string | ISO 8601 timestamp of last status change |
| `.reason` | string | Machine-readable reason for the condition |
| `.message` | string | Human-readable message |

**When condition is missing:** Returns a zero-value Condition (`.status = ""`, `.observed_generation = 0`, `.last_updated_time` = empty string). This follows Go's zero-value convention and allows safe field access without null checks.

**Examples:**

```cel
condition("Reconciled").status == "True"         # check if resource is reconciled
condition("Reconciled").last_updated_time        # get timestamp for age calculation
condition("Available").observed_generation       # get last reconciled generation
condition("Applied").reason                      # get reason for condition
```

**Notes:**

- Searches `resource.status.conditions[]` by the `type` field
- Works with **any** condition type present on the resource (e.g., `"Reconciled"`, `"LastKnownReconciled"`, `"Applied"`, `"Health"`, or custom conditions)
- When accessing `.last_updated_time` on a missing condition, the zero time value will cause `timestamp()` conversion to produce a very old timestamp, which naturally triggers age-exceeded checks — acting as a fail-safe that ensures new or unknown resources get reconciled (see [Test 7](#test-7-missing-reconciled-condition-on-resource-zero-value-fail-safe))
- Guard against missing conditions with `ref_time != ""` (the `has_ref_time` param in the default config) to avoid unintended timestamp conversions

**Available CEL Variables:**

| Variable | Type | Description |
|----------|------|-------------|
| `resource` | map | The API resource (`id`, `kind`, `href`, `generation`, `created_time`, `updated_time`, `labels`, `owner_references`, `metadata`) |
| `now` | timestamp | Current evaluation timestamp |
| `condition(name)` | function | Look up a status condition by type name |
| `timestamp(string)` | function | Standard CEL time conversion |
| `duration(string)` | function | Standard CEL duration parsing |

---

## Status Tracking

### Resource Status Conditions

The Sentinel reads the resource's status conditions to evaluate the message decision rules. The default configuration relies on the `Reconciled` condition, but custom rules can reference any condition:

```json
{
  "id": "cls-123",
  "generation": 2,
  "status": {
    "conditions": [
      {
        "type": "Available",
        "status": "True",
        "observed_generation": 1,
        "last_updated_time": "2025-10-21T12:00:00Z",
        "last_transition_time": "2025-10-21T10:00:00Z"
      },
      {
        "type": "Reconciled",
        "status": "False",
        "observed_generation": 1,
        "last_updated_time": "2025-10-21T12:00:00Z",
        "last_transition_time": "2025-10-21T10:00:00Z"
      }
    ]
  }
}
```

### Field Semantics

- **`generation`**: User's desired state version. Increments when the resource spec changes (e.g., user scales nodes from 3 to 5). This is the "what the user wants" field.

- **`condition.observed_generation`**: Which generation was last reconciled by a given adapter. The API uses this to compute the aggregated `Reconciled` condition — when any adapter's `observed_generation` is behind `resource.generation`, the API sets `Reconciled` to `False`.

- **`condition.last_transition_time`**: Updates ONLY when the condition status changes (e.g., Reconciled False → True).

- **`condition.last_updated_time`**: Updates EVERY time an adapter checks the resource, regardless of whether status changed.

### How Generation Changes Flow Through the System

When a user changes the cluster spec (e.g., scales nodes), `generation` increments (1 → 2). The API detects that not all adapters have reconciled this generation and sets `Reconciled` to `False`. The Sentinel's default rules also evaluate `generation_mismatch` explicitly (`resource.generation > condition("Reconciled").observed_generation`), while the API's aggregated `Reconciled` condition remains the source of truth for adapter progress.

### Why last_updated_time for Age Calculation

If a cluster stays in "Provisioning" state for 2 hours, `last_transition_time` would remain at the time it entered "Provisioning" (e.g., 10:00), even though adapters check it at 11:00, 11:30, 12:00. Using `last_transition_time` for age calculation would incorrectly trigger events too frequently. Using `last_updated_time` ensures age is calculated from the last adapter check, not the last status change.

For complete details on generation and observed_generation semantics, see the [HyperFleet Status Guide](https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/docs/status-guide.md).

---

## Adapter Status Update Contract

**CRITICAL REQUIREMENT:** For the Sentinel message decision to work correctly, adapters MUST update their status on EVERY evaluation, regardless of whether they take action.

### Why This Matters

Without this requirement, adapters that skip work due to unmet preconditions would create an infinite event loop:

```text
Time 10:00 - DNS adapter receives event
Time 10:00 - DNS checks preconditions: Validation not complete
Time 10:00 - DNS does NOT update status (skips work)
            ❌ cluster.status.last_updated_time remains at 09:50
Time 10:10 - Sentinel sees last_updated_time=09:50, age threshold exceeded (10s)
Time 10:10 - Sentinel publishes ANOTHER event
Time 10:10 - DNS receives event AGAIN...
            ↻ INFINITE LOOP until validation completes
```

### Required Adapter Behavior

Adapters MUST update status in ALL scenarios:

1. **Preconditions Met** → Create Job → Report status with `observed_time=now`
2. **Preconditions NOT Met** → Skip work → Report status anyway with:

   ```json
   {
     "adapter": "dns",
     "observed_generation": 1,
     "observed_time": "2025-10-17T10:00:00Z",
     "conditions": [
       {
         "type": "Available",
         "status": "False",
         "reason": "PreconditionsNotMet",
         "message": "Waiting for validation to complete"
       },
       {
         "type": "Applied",
         "status": "False",
         "reason": "PreconditionsNotMet",
         "message": "Waiting for validation adapter"
       },
       {
         "type": "Health",
         "status": "True",
         "reason": "NoErrors",
         "message": "Adapter is healthy"
       }
     ]
   }
   ```

Adapters send `observed_time` in the request. The API uses this to update `last_report_time` in AdapterStatus and aggregates to `last_updated_time` in ClusterStatus.

### Integration Testing Requirements

Integration tests MUST verify that:

- Adapters send `observed_time` when preconditions are met
- Adapters send `observed_time` when preconditions are NOT met
- Sentinel correctly calculates age from `cluster.status.last_updated_time` (aggregated from adapter reports) for message decision evaluation

---

## Service Components

### Config Loader

**Responsibility:** Load configuration from YAML files with environment variable overrides.

**Key Functions:**

- `LoadConfig(configFile, flags)` — Load and merge Sentinel configuration (YAML file + CLI flags + env vars)
- `Validate()` — Validate required fields, compile CEL expressions at startup (fail-fast)

**Implementation Details:**

- Configuration loaded from YAML file path specified via `--config` flag
- Precedence: CLI flags > env vars (`HYPERFLEET_*`) > YAML file > defaults
- CEL expressions in `message_decision` and `message_data` are compiled at startup
- Broker configuration loaded separately via `broker.yaml` or `BROKER_CONFIG_FILE` env var (handled by hyperfleet-broker library)

### Resource Watcher

**Responsibility:** Fetch resources from HyperFleet API that need reconciliation.

**Key Functions:**

- `FetchResources(ctx, resourceType, selector)` — Fetch all resources matching the `resource_type` and `resource_selector` label filter

The Resource Watcher fetches all resources matching the label selector on every poll cycle, then the Decision Engine evaluates each one in-memory to decide publish/skip. Server-side condition-based pre-filtering is a planned optimization (see HYPERFLEET-805) but not yet implemented.

### Decision Engine

**Responsibility:** Configurable decision logic via CEL-based message decision.

**Key Functions:**

- `Evaluate(resource, now)` — Determine if resource needs an event

**Decision Logic:**

1. **Evaluate message decision params** in dependency order, building an activation map:
   - CEL expression params are evaluated with access to `resource`, `now`, and previously evaluated params

2. **Evaluate result expression** with all params in scope:
   - Result uses standard CEL logical operators (`&&`, `||`)

3. **Return decision based on result:**
   - If result is `true` → publish event (reason: "message decision matched")
   - If result is `false` → skip (reason: "message decision result is false")

**Implementation Requirements:**

- All CEL expressions compiled at startup (fail-fast on invalid expressions)
- Params are evaluated in authored order; dependencies must be declared before use
- Clear logging of decision reasoning

### Message Publisher

**Responsibility:** Publish CloudEvents to message broker.

Publishing happens inline in `Sentinel.trigger()` — builds the CloudEvent and calls the `hyperfleet-broker` library's `Publisher.Publish(ctx, topic, event)`.

**CloudEvent Format** (CloudEvents 1.0):

```json
{
  "specversion": "1.0",
  "type": "com.redhat.hyperfleet.cluster.reconcile",
  "source": "hyperfleet-sentinel",
  "id": "evt-abc123",
  "time": "2025-10-21T12:00:00Z",
  "datacontenttype": "application/json",
  "data": {
    "resource_id": "cls-123",
    "resource_type": "cluster",
    "region": "us-east",
    "status": "Provisioning"
  }
}
```

**Message Data Composition (CEL Expressions):**

The `data` field structure is defined by the `message_data` configuration using CEL expressions. This allows Sentinel to be generic across different resource types (clusters, nodepools, etc.) by configuring which fields to extract and include in CloudEvents. See [config.md — Message Data](config.md#message-data-cloudevent-payload) for configuration details.

**Implementation Requirements:**

Broker abstraction is handled by the [hyperfleet-broker](https://github.com/openshift-hyperfleet/hyperfleet-broker) library (supports GCP Pub/Sub and RabbitMQ). On publish failure, the error is logged, a metric is recorded, and the loop continues to the next resource.

### Main Reconciler

**Responsibility:** Orchestrate reconciliation loop with periodic polling.

The main loop lives in `Sentinel.trigger()` (`internal/sentinel/sentinel.go`).

**Initialization Steps** (executed once at startup):

1. Load Sentinel configuration from YAML file specified via `--config` flag
2. Load broker configuration from `broker.yaml` or `BROKER_CONFIG_FILE`
3. Parse `message_decision` params/result, resource selector, `message_data`, and resource type
4. Apply environment variable overrides for sensitive fields
5. Initialize MessagePublisher with broker config
6. Log configuration details and validate all required fields

**Polling Loop Steps** (repeated every `poll_interval`):

1. **Fetch Resources:** Build label selector from `resource_selector` configuration, determine resource endpoint from `resource_type` (e.g., `/clusters`, `/nodepools`), fetch matching resources
2. **Evaluate Each Resource:** For each resource, call `DecisionEngine.Evaluate(resource, now)`. Publish event or skip based on the decision result. Continue to next resource on publishing errors.
3. **Sleep and Repeat:** Sleep for configured `poll_interval` (default: 5 seconds)

**Service Architecture:**

- **Single-phase initialization:** Load configuration once during startup, resolve param dependencies, compile CEL expressions, fail fast if invalid
- **Stateless polling loop:** No configuration reloading during runtime
- **Simple service model:** No Kubernetes controller pattern, just periodic polling
- **Graceful shutdown:** Support clean termination on SIGTERM/SIGINT

**Error Handling:**

- On config load failure: exit with error code
- On resource fetch failure: log error, wait poll interval, retry
- On event publishing failure: log error, record metric, continue to next resource

---

## Decision Engine Test Scenarios

The following test scenarios document the expected behavior of the Decision Engine for the default `message_decision` configuration.

### Message Decision Tests

#### Test 1: Reconciled resource with recent check — skip

```text
Given:
  - Resource Reconciled condition status: True
  - resource.generation = 2
  - condition("Reconciled").observed_generation = 2
  - condition("Reconciled").last_updated_time = now() - 5m (age < 30m)
Then:
  - Decision: SKIP
  - Reason: "message decision not matched"
  - Params evaluated: ref_time, is_reconciled=true, is_new_resource=false,
    generation_mismatch=false, reconciled_and_stale=false, not_reconciled_and_debounced=false
  - Result: false || false || false || false = false
```

#### Test 2: Not-reconciled resource with debounce elapsed — publish

```text
Given:
  - Resource Reconciled condition status: False
  - resource.generation = 2
  - condition("Reconciled").observed_generation = 2
  - condition("Reconciled").last_updated_time = now() - 15s (age > 10s)
Then:
  - Decision: PUBLISH
  - Reason: "message decision matched"
  - Params evaluated: ref_time, is_reconciled=false, is_new_resource=false,
    generation_mismatch=false, reconciled_and_stale=false, not_reconciled_and_debounced=true
  - Result: false || false || false || true = true
```

#### Test 3: Not-reconciled resource within debounce period — skip

```text
Given:
  - Resource Reconciled condition status: False
  - resource.generation = 2
  - condition("Reconciled").observed_generation = 2
  - condition("Reconciled").last_updated_time = now() - 5s (age < 10s)
Then:
  - Decision: SKIP
  - Reason: "message decision not matched"
  - Params evaluated: ref_time, is_reconciled=false, is_new_resource=false,
    generation_mismatch=false, reconciled_and_stale=false, not_reconciled_and_debounced=false
  - Result: false || false || false || false = false
```

#### Test 4: Reconciled resource with stale check — publish (periodic health check)

```text
Given:
  - Resource Reconciled condition status: True
  - resource.generation = 2
  - condition("Reconciled").observed_generation = 2
  - condition("Reconciled").last_updated_time = now() - 31m (age > 30m)
Then:
  - Decision: PUBLISH
  - Reason: "message decision matched"
  - Params evaluated: ref_time, is_reconciled=true, is_new_resource=false,
    generation_mismatch=false, reconciled_and_stale=true, not_reconciled_and_debounced=false
  - Result: false || false || true || false = true
```

#### Test 5: Brand-new resource (generation 1, not ready) — publish immediately

```text
Given:
  - Resource has no Reconciled condition (brand-new, no adapter has reported)
  - resource.generation = 1
Then:
  - Decision: PUBLISH
  - Reason: "message decision matched"
  - Params evaluated: ref_time="", is_reconciled=false, has_ref_time=false,
    is_new_resource=true, generation_mismatch=true (1 > 0),
    reconciled_and_stale=false, not_reconciled_and_debounced=false
  - Result: true || true || false || false = true
Note:
  - Brand-new resources bypass the debounce because no adapter has
    processed them yet — there is no "previous work" to wait for.
  - Both is_new_resource and generation_mismatch are true here;
    is_new_resource fires first in the result expression.
```

#### Test 6: Not-reconciled resource due to generation mismatch — publish

```text
Given:
  - resource.generation = 2 (user changed spec)
  - API has set Reconciled condition status: False (because adapters haven't reconciled generation 2)
  - condition("Reconciled").observed_generation = 1
  - condition("Reconciled").last_updated_time = now() - 15s (debounce elapsed)
Then:
  - Decision: PUBLISH
  - Reason: "message decision matched"
  - Params evaluated: ref_time, is_reconciled=false, is_new_resource=false,
    generation_mismatch=true, reconciled_and_stale=false, not_reconciled_and_debounced=true
  - Result: false || true || false || true = true
Note:
  - The default rules evaluate generation_mismatch directly
    (resource.generation > condition("Reconciled").observed_generation).
    In this scenario both generation_mismatch and not_reconciled_and_debounced
    are true — either path alone would trigger the publish.
```

### Edge Cases

#### Test 7: Missing Reconciled condition on resource (zero-value fail-safe)

```text
Given:
  - Resource has no Reconciled condition
  - resource.generation = 1
Then:
  - condition("Reconciled") returns zero-value Condition
  - is_reconciled = false (zero-value .status == "" != "True")
  - is_new_resource = true (generation == 1 && !has_ref_time)
  - Decision: PUBLISH
  - Reason: "message decision matched"
Note:
  - Brand-new resources with no conditions are caught by is_new_resource.
    The default config guards timestamp() behind has_ref_time, so this case
    publishes via is_new_resource rather than the debounce branch.
```

#### Test 8: CEL expression compilation failure at startup

```text
Given:
  - Configuration contains invalid CEL expression in params or result
Then:
  - Sentinel exits with error at startup (fail-fast)
  - Clear error message indicating which param/expression failed
```

#### Test 9: Circular param dependency at startup

```text
Given:
  - Param A references Param B, but Param B is declared after Param A
Then:
  - CEL compilation fails at startup because Param B is not yet in scope
  - Sentinel exits with error (fail-fast)
Note:
  - Params are evaluated in authored order. There is no topological sort
    or circular dependency detection — dependencies must be declared first.
```

#### Test 10: Brand-new resource with no Reconciled condition and generation > 1

```text
Given:
  - Resource has no Reconciled condition (no adapter has reported yet)
  - resource.generation = 2 (created with a spec update before any adapter ran)
Then:
  - condition("Reconciled") returns zero-value Condition
  - is_reconciled = false (.status == "" != "True")
  - is_new_resource = false (generation != 1)
  - ref_time = "" → has_ref_time = false
  - generation_mismatch = true (generation 2 > observed_generation 0)
  - Decision: PUBLISH
  - Reason: "message decision matched"
Note:
  - Resources with generation > 1 and no conditions are caught by generation_mismatch.
```

#### Test 11: message_decision omitted from configuration

```text
Given:
  - Configuration YAML has no message_decision section
Then:
  - Sentinel uses the default message_decision configuration
  - Service starts normally with default CEL params and result
Note:
  - The default configuration covers the standard reconciliation scenarios.
    See DefaultMessageDecision() in internal/config/config.go.
```

#### Test 12: Params reference non-Reconciled condition types

```text
Given:
  - Configuration uses custom condition types:
    params:
      - name: ref_time
        expr: 'condition("Applied").last_updated_time'
      - name: is_applied
        expr: 'condition("Applied").status == "True"'
      - name: age_exceeded
        expr: 'is_applied && now - timestamp(ref_time) > duration("5m")'
    result: age_exceeded
  - Resource has Applied condition with status "True" and last_updated_time = now() - 10m
Then:
  - Decision: PUBLISH
  - Reason: "message decision matched"
  - Params evaluated: ref_time=Applied.last_updated_time, is_applied=true, age_exceeded=true
Note:
  - The condition() function works with ANY condition type present in
    resource.status.conditions[], not just "Reconciled".
  - If Applied is missing, condition() returns a zero-value Condition
    (status=""), so is_applied=false. The && in age_exceeded short-circuits
    before timestamp(ref_time) is evaluated — result is SKIP, not PUBLISH.
```

### Test Requirements

#### Unit Tests (Decision Engine)

The Decision Engine logic should be tested with unit tests covering:

- All decision paths: param evaluation → result evaluation → publish/skip
- CEL expression compilation (valid and invalid expressions)
- CEL custom function behavior (`condition()` with zero-value handling)
- Param ordering (dependencies must be declared before use)
- Result expression evaluation with logical operators
- Zero-value Condition handling (missing conditions naturally trigger reconciliation)
- Edge cases handled gracefully (missing conditions, brand-new resources, etc.)

#### Integration Tests (End-to-End)

Integration tests should verify the complete Sentinel workflow:

1. **Event Publishing:** Sentinel successfully publishes CloudEvents to the message broker when message decision result is true
2. **Not-reconciled triggers reconciliation:** When a resource's Reconciled condition is False (including after spec changes that increment generation), Sentinel publishes an event based on message decision rules
3. **Message decision evaluation:** Sentinel evaluates `message_decision` params and result to determine whether to publish
4. **Adapter feedback loop:** Adapters receive events, process resources, and update conditions correctly, which the API aggregates into the `Reconciled` condition for Sentinel to read in subsequent polls
