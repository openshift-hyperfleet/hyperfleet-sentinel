# HyperFleet Sentinel Operator Guide
**Status**: Active
**Owner**: HyperFleet Team
**Last Updated**: 2026-03-12
> **Audience:** Operators deploying and configuring Sentinel service.

This comprehensive guide teaches operators how to deploy, configure, and operate the HyperFleet Sentinel service—a polling-based event publisher that drives cluster lifecycle orchestration.

## Table of Contents

1. [Introduction](#1-introduction)
   - [What is Sentinel?](#11-what-is-sentinel)
   - [When to Use Sentinel](#12-when-to-use-sentinel)
2. [Core Concepts](#2-core-concepts)
   - [Decision Engine](#21-decision-engine)
      - [State-Based Reconciliation](#211-state-based-reconciliation)
      - [Time-Based Reconciliation (Max Age Intervals)](#212-time-based-reconciliation-max-age-intervals)
   - [Resource Filtering](#22-resource-filtering)
3. [Configuration Reference](#3-configuration-reference)
   - [Configuration File Structure](#31-configuration-file-structure)
   - [Required Fields](#32-required-fields)
   - [Optional Fields](#33-optional-fields)
   - [Resource Selector](#34-resource-selector)
   - [Message Data (CEL Expressions)](#35-message-data-cel-expressions)
   - [Broker Configuration](#36-broker-configuration)
4. [Deployment Checklist](#4-deployment-checklist)
5. [Additional Resources](#additional-resources)

**Appendices:**
- [Appendix A: Troubleshooting](#appendix-a-troubleshooting)

---

## 1. Introduction

### 1.1 What is Sentinel?

**Sentinel is the component within the HyperFleet system responsible for triggering reconciliation events for changes in managed API resources.** It acts as the centralized trigger mechanism for the orchestration platform.

**Key Benefits:**

- **Single source of truth** - Centralized decision logic for when reconciliation should occur
- **Configurable polling strategy** - Different reconciliation rates for stable vs transitional resources
- **Broker abstraction** - Supports multiple message broker backends
- **Horizontal scalability** - Multiple instances can distribute workload through resource filtering

**Core Responsibilities:**

1. **Poll HyperFleet API** for resource updates at configurable intervals
2. **Evaluate resources** using a decision engine to determine when events should be published
3. **Publish CloudEvents** to a message broker
4. **Track metrics** for observability and debugging

**Architecture Overview:**

```mermaid
graph LR
    A[Sentinel] -->|Poll| B[HyperFleet API]
    B -->|Resources| A
    A -->|Evaluate| C[Decision Engine]
    C -->|Publish Events| D[Message Broker]
    D -->|Consume Events| E[Adapters]
    E -->|Update Status| B
```

Sentinel publishes events to a message broker, which fans out messages to downstream adapters. It uses a **dual-trigger reconciliation strategy**:
- **State-based**: Publish immediately when resource state indicates unprocessed spec changes
- **Time-based**: Publish periodically based on max age intervals to ensure eventual consistency

### 1.2 When to Use Sentinel

Deploy Sentinel when you need:

- **Event-driven orchestration** for cluster lifecycle management
- **Centralized reconciliation logic** instead of distributed polling by each adapter
- **Configurable polling intervals** with different rates for ready vs not-ready resources
- **Horizontal scaling** through resource filtering
- **Broker abstraction** to support multiple message broker backends

---

## 2. Core Concepts

### 2.1 Decision Engine

Sentinel's decision engine evaluates resources during each poll cycle to determine when to publish events. It uses a **dual-trigger strategy** that combines two complementary mechanisms to ensure both immediate response to changes and eventual consistency over time:

1. **State-Based Reconciliation** — Immediate event publishing when resource state indicates unprocessed spec changes, which is checked first
2. **Time-Based Reconciliation** — Periodic event publishing to handle drift and failures when state is in sync

**How Sentinel Reads Resource State:**

When Sentinel polls the HyperFleet API, it retrieves cluster or nodepool resources with their current state. 

1. **`resource.Generation`** — Retrieved from the API resource. The HyperFleet API increments this value every time the resource spec is updated.
2. **`resource.status`** — Extracted from the API resource's `type=Ready` condition.

#### 2.1.1 State-Based Reconciliation

State-based reconciliation is a **spec-change detection mechanism** where Sentinel immediately publishes events when resource state indicates the spec has changed but hasn't been fully processed yet.

**How It Works:**

Sentinel detects unprocessed spec changes by comparing the resource's `generation` field with the `ObservedGeneration` from the Ready condition:

1. **Generation Counter**: Every resource has a `generation` field that increments when the spec changes
2. **Ready Condition Aggregation**: The Ready condition (type=Ready) contains an `ObservedGeneration` field which is aggregated from individual adapter conditions
3. **State Comparison**: Sentinel publishes an event when `generation` is greater than the `ObservedGeneration` field from the Ready condition

**Note**: Sentinel uses the Ready condition's `ObservedGeneration` field as a proxy signal for spec changes. While the Ready condition can also be False for other reasons (e.g., adapter-reported infrastructure failures), the `ObservedGeneration` field specifically tracks spec processing, making this an effective spec-change detection mechanism.

**Flow Diagram:**

```mermaid
sequenceDiagram
    participant User
    participant API
    participant Sentinel
    participant Broker
    participant Adapter

    User->>API: Update cluster spec (generation: 1 → 2)
    API->>API: Increment generation
    Sentinel->>API: Poll resources
    API-->>Sentinel: cluster (gen: 2, observed_gen: 1)
    Sentinel->>Sentinel: Evaluate: 2 > 1 → PUBLISH
    Sentinel->>Broker: CloudEvent (reason: state change detected)
    Broker->>Adapter: Consume event
    Adapter->>API: Reconcile cluster
    Adapter->>API: Update status (observed_generation: 2)
```

**Key Properties:**

- **Immediate Response**: No need to wait for max age interval when state indicates unprocessed changes
- **Idempotent**: Adapters can safely process the same generation multiple times
- **Race Prevention**: Ensures spec changes are never missed due to timing
- **Condition-Based**: Uses Ready condition data as a reliable proxy for tracking spec processing status

#### 2.1.2 Time-Based Reconciliation (Max Age Intervals)

Time-based reconciliation ensures **eventual consistency** by publishing events periodically, even when specs haven't changed. This handles external state drift and transient failures.

**How It Works:**

Sentinel uses two configurable max age intervals based on the resource's status (`Ready` condition):

| Resource State | Default Interval | Rationale                                                                              |
|----------------|------------------|----------------------------------------------------------------------------------------|
| **Not Ready** (`Ready` condition status = False) | `10s` | Faster reconciliation for transitional states requires more frequent checks to complete quickly |
| **Ready** (`Ready` condition status = True) | `30m` | Lower frequency for drift detection on stable resources to avoid excessive load            |

**Decision Logic:**

When the resource's `generation` matches the `Ready` condition's `ObservedGeneration` (indicating the condition reflects the current state), Sentinel checks if enough time has elapsed:

1. Calculate reference timestamp:
   - If `status.last_updated` exists → use it (adapter has processed resource)
   - Otherwise → use `created_time` (new resource never processed)

2. Determine max age interval:
   - If resource is ready (`Ready` condition status == True) → use `max_age_ready` (default: 30m)
   - If resource is not ready (`Ready` condition status == False) → use `max_age_not_ready` (default: 10s)

3. Calculate next event time:
   ```text
   next_event = reference_time + max_age_interval
   ```

4. Compare with current time:
   - If `now >= next_event` → **Publish event** (reason: "max age exceeded")
   - Otherwise → **Skip** (reason: "max age not exceeded")

**Flow Diagram:**

```mermaid
graph TD
    A[Determine Reference Time] --> B{last_updated exists?}
    B -->|Yes| C[Use last_updated]
    B -->|No| D[Use created_time]
    C --> E{Resource Ready?}
    D --> E
    E -->|Yes| F[Max Age = 30m]
    E -->|No| G[Max Age = 10s]
    F --> H{now >= reference + max_age?}
    G --> H
    H -->|Yes| I[Publish: max age exceeded]
    H -->|No| J[Skip: within max age]
```

### 2.2 Resource Filtering

Resource filtering enables **horizontal scaling** by allowing operators to distribute resources across multiple Sentinel instances using label-based selectors.

**How It Works:**

The `resource_selector` field defines label key-value pairs that filter which resources a Sentinel instance watches:

```yaml
resource_selector:
  - label: region
    value: us-east-1
  - label: environment
    value: production
```

**Selection Logic:**

- **Empty selector** (`[]`): Watch ALL resources (default behavior)
- **Single selector**: Match resources with that label
- **Multiple selectors**: Match resources with ALL labels (AND logic)

**Common Filtering Patterns:**

| Pattern | Example                                     | Use Case |
|----------|---------------------------------------------|----------|
| **Regional** | `region: us-east`, `region: eu-west`        | Geographic distribution |
| **Environment** | `environment: prod`, `environment: staging` | Isolation by environment |
| **Index-based** | `index: "1"`, `index: "2"`, `index: "3"`    | Numeric distribution for high volume |
| **Combined** | `region: us-east` + `environment: prod`     | Multi-dimensional filtering |

**Architecture Diagram:**

```mermaid
graph TB
    API[HyperFleet API<br/>All Resources]

    API --> S1[Sentinel US-East Clusters<br/>resource_type: clusters<br/>selector: region=us-east]
    API --> S2[Sentinel US-West Clusters<br/>resource_type: clusters<br/>selector: region=us-west]
    API --> S3[Sentinel EU-Central NodePools<br/>resource_type: nodepools<br/>selector: region=eu-central]

    S1 --> B1[Broker Topic:<br/>us-east-clusters]
    S2 --> B2[Broker Topic:<br/>us-west-clusters]
    S3 --> B3[Broker Topic:<br/>eu-central-nodepools]

    B1 --> A1a[Adapter US-East-1]
    B1 --> A1b[Adapter US-East-2]
    B2 --> A2a[Adapter US-West-1]
    B2 --> A2b[Adapter US-West-2]
    B3 --> A3[Adapter EU-Central]
```

**Important Considerations:**

1. **No Coordination**: Sentinel instances operate independently with no coordination
2. **Coverage Responsibility**: Operators must ensure all resources are covered by selectors
3. **Overlap Allowed**: Multiple instances can watch the same resource (events will be duplicated)
4. **Gaps Dangerous**: Resources not matching any selector will never reconcile

**Broker Topic Isolation:**

When using multiple filtered instances, consider using separate broker topics to enable independent processing:

```yaml
# US-East instance
broker:
  topic: hyperfleet-us-east-clusters

# US-West instance
broker:
  topic: hyperfleet-us-west-clusters
```

#### Multi-Region Configuration

For multi-region deployment examples using `resource_selector`, see [Resource Selector
Strategies](multi-instance-deployment.md#resource-filtering-strategies).


For detailed deployment examples, see [docs/multi-instance-deployment.md](multi-instance-deployment.md).

---

## 3. Configuration Reference

### 3.1 Configuration File Structure

Sentinel uses YAML-based configuration with environment variable overrides for sensitive data. Configuration is loaded from a file specified via the `--config` flag.

**Basic Structure:**

```yaml
# Required: Resource type to watch
resource_type: clusters

# Optional: Polling and age intervals
poll_interval: 5s
max_age_not_ready: 10s
max_age_ready: 30m

# Optional: Resource filtering
resource_selector:
  - label: region
    value: us-east-1

# Required: HyperFleet API configuration
hyperfleet_api:
  endpoint: http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8000
  timeout: 5s

# Optional: CloudEvent payload definition
message_data:
  id: "resource.id"
  kind: "resource.kind"
  href: "resource.href"
  generation: "resource.generation"
```

**Configuration Precedence:**

1. Environment variables (highest)
2. Configuration file
3. Built-in defaults (lowest)

### 3.2 Required Fields

These fields MUST be present in the configuration file or Sentinel will fail to start:

| Field | Type | Description | Example                                                          |
|-------|------|-------------|------------------------------------------------------------------|
| `resource_type` | string | Resource to watch (`clusters` or `nodepools`) | `clusters`                                                       |
| `hyperfleet_api.endpoint` | string | HyperFleet API base URL | `http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8000` |

### 3.3 Optional Fields

These fields have sensible defaults and can be omitted:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `poll_interval` | duration | `5s` | How often to poll the API for resource updates |
| `max_age_not_ready` | duration | `10s` | Max age interval for not-ready resources |
| `max_age_ready` | duration | `30m` | Max age interval for ready resources |
| `hyperfleet_api.timeout` | duration | `5s` | Request timeout for API calls |
| `resource_selector` | array | `[]` | Label selectors for filtering (empty = all resources) |
| `message_data` | map | `{}` | CEL expressions for CloudEvents payload |
| `topic` | string | `""` | Override broker topic name (defaults to Helm template) |

### 3.4 Resource Selector

The `resource_selector` field enables horizontal scaling by filtering resources based on labels.

**Structure:**

```yaml
resource_selector:
  - label: <label-key>
    value: <label-value>
  - label: <another-key>
    value: <another-value>
```

**Behavior:**

- **Empty** (`[]` or omitted): Watch ALL resources
- **Single selector**: Match resources with that specific label
- **Multiple selectors**: Match resources with ALL labels (AND logic)

### 3.5 Message Data (CEL Expressions)

The `message_data` field defines the CloudEvents payload structure using **Common Expression Language (CEL)** expressions.

**How It Works:**

1. Each key-value pair in `message_data` becomes a field in the CloudEvent `data` payload
2. Values are CEL expressions evaluated with access to two variables:
   - `resource` - The HyperFleet resource object (cluster or nodepool)
   - `reason` - The decision reason string (e.g., "max age exceeded", "generation changed")
3. Nested maps create nested objects in the payload

**Available CEL Variables:**

| Variable | Type | Description | Example Fields |
|----------|------|-------------|----------------|
| `resource` | Resource | The HyperFleet resource | `id`, `kind`, `href`, `generation`, `status`, `labels`, `created_time` |
| `reason` | string | Decision engine reason | `"max age exceeded"`, `"generation changed"` |

**CEL Expression Syntax:**

```yaml
message_data:
  # Field access
  id: "resource.id"

  # Nested field access
  cluster_id: "resource.owner_references.id"

  # Conditionals (ternary operator)
  ready_status: 'resource.status.ready ? "Ready" : "NotReady"'

  # String literals (must use quotes inside CEL expression)
  source: '"hyperfleet-sentinel"'

  # Numeric/boolean literals
  generation: "resource.generation"
  is_ready: "resource.status.ready"

  # Complex conditionals with CEL filter
  ready_condition: 'resource.status.conditions.filter(c, c.type=="Ready")[0].value == "True" ? "Ready" : "NotReady"'

  # Nested objects
  owner_references: "resource.owner_references"
```

**Cluster Pattern:**
```yaml
message_data:
  id: "resource.id"
  kind: "resource.kind"
  href: "resource.href"
  generation: "resource.generation"
```

**NodePool Pattern with Owner References:**
```yaml
message_data:
  id: "resource.id"
  kind: "resource.kind"
  href: "resource.href"
  generation: "resource.generation"
  owner_references: "resource.owner_references"
```

**Validation:**

- All leaf values MUST be non-empty CEL expression strings
- Empty values or `nil` will cause configuration validation failure:
  ```text
  Error: invalid config: message_data.id: empty CEL expression is not allowed
  ```

**CloudEvents Output:**

The `message_data` configuration produces CloudEvents with the following structure:

```json
{
  "specversion": "1.0",
  "type": "com.redhat.hyperfleet.cluster.reconcile",
  "source": "hyperfleet-sentinel",
  "id": "uuid-generated",
  "time": "2025-01-01T10:00:00Z",
  "datacontenttype": "application/json",
  "data": {
    // Your message_data CEL expressions evaluated here
    "id": "cluster-abc123",
    "kind": "Cluster",
    "href": "/api/v1/clusters/cluster-abc123",
    "generation": 5
  }
}
```

### 3.6 Broker Configuration

Broker configuration is managed by the [hyperfleet-broker library](https://github.com/openshift-hyperfleet/hyperfleet-broker). Configuration is split between:

1. **broker.yaml** - Non-sensitive broker settings (type, project ID, etc.)
2. **Environment variables** - Sensitive credentials and connection strings

**Configuration File: broker.yaml**

```yaml
broker:
  type: rabbitmq  # or googlepubsub

  # RabbitMQ-specific settings
  rabbitmq:
    exchange_type: topic
    # URL should be set via BROKER_RABBITMQ_URL env var

  # Google Pub/Sub-specific settings
  googlepubsub:
    project_id: my-gcp-project
    # Credentials via GOOGLE_APPLICATION_CREDENTIALS or ADC
```

**Environment Variables:**

| Variable | Broker | Description | Example |
|----------|--------|-------------|---------|
| `BROKER_RABBITMQ_URL` | RabbitMQ | Complete connection URL with credentials | `amqp://user:pass@localhost:5672/vhost` |
| `BROKER_GOOGLEPUBSUB_PROJECT_ID` | Pub/Sub | GCP project ID | `my-gcp-project` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Pub/Sub | Service account key path (local dev/testing only, production GKE uses Workload Identity) | `/path/to/key.json` |
| `BROKER_TOPIC` | Both | Topic name for publishing events | `hyperfleet-prod-clusters` |
| `BROKER_CONFIG_FILE` | Both | Path to broker config file | `/app/broker.yaml` |
| `PUBSUB_EMULATOR_HOST` | Pub/Sub | Pub/Sub emulator endpoint (local dev/testing only) | `localhost:8085` |

**Topic Naming:**

The `BROKER_TOPIC` environment variable sets the topic name where events are published. You can use any naming convention that fits your deployment requirements.

Example topic names:
- `hyperfleet-prod-clusters`
- `us-east-clusters`

When using the provided Helm chart, the default template uses `{namespace}-{resource_type}` (e.g., `hyperfleet-dev-clusters`), but this can be overridden by setting the `BROKER_TOPIC` environment variable or the `broker.topic` Helm value.

**Broker Type: RabbitMQ**

```yaml
# broker.yaml
broker:
  type: rabbitmq
  rabbitmq:
    exchange_type: topic
```

```bash
# Environment variables
export BROKER_RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
export BROKER_TOPIC="hyperfleet-dev-clusters"
```

**Broker Type: Google Pub/Sub**

```yaml
# broker.yaml
broker:
  type: googlepubsub
  googlepubsub:
    project_id: hcm-hyperfleet
```

**Local Development Authentication:**

For local development and testing, use one of these authentication methods:

```bash
# Set the topic name
export BROKER_TOPIC="hyperfleet-dev-clusters"

# Option 1: Use personal Application Default Credentials (local development)
gcloud auth application-default login

# Option 2: Use service account key file (local development/testing)
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account-key.json"
```

**Production GKE Authentication:**

When deploying to production GKE, use **Workload Identity Federation** for authentication. This method does not require `GOOGLE_APPLICATION_CREDENTIALS` or `gcloud auth` commands - credentials are automatically provided to the pod:

```bash
# Only the topic name is required via environment variable
export BROKER_TOPIC="hyperfleet-prod-clusters"

# No GOOGLE_APPLICATION_CREDENTIALS needed - Workload Identity handles authentication
```

For Workload Identity setup instructions, see [Configure Workload Identity](running-sentinel.md#5-configure-workload-identity).

**Broker Configuration Reference:**

For complete broker configuration options, see the [hyperfleet-broker documentation](https://github.com/openshift-hyperfleet/hyperfleet-broker).

---

## 4. Deployment Checklist

Follow this checklist to ensure successful Sentinel deployment and operation.

### Phase 1: Configuration Planning

**Define Resource Monitoring Scope**

- [ ] Determine `resource_type` to monitor: `clusters` or `nodepools`
- [ ] Define `resource_selector` labels for filtering resources
  - Leave empty (`[]`) to monitor all resources
  - Use label selectors for horizontal scaling
  - Reference: [Resource Filtering](#22-resource-filtering)

**Configure Reconciliation Parameters**

- [ ] Review and adjust polling intervals:
  - `poll_interval` - How often to poll the HyperFleet API (default: `5s`)
  - `max_age_not_ready` - Reconciliation interval for not-ready resources (default: `10s`)
  - `max_age_ready` - Reconciliation interval for ready resources (default: `30m`)
  - Reference: [Optional Fields](#optional-fields)

**Design CloudEvents Payload**

- [ ] Define `message_data` CEL expressions for event payload structure
- [ ] Reference: [Message Data (CEL Expressions)](#message-data-cel-expressions)

**Configure HyperFleet API Connection**

- [ ] **Ensure HyperFleet API is deployed and accessible**
    - **Critical:** Sentinel performs connectivity verification at startup and **will fail to start if the API is unavailable**
    - **Rolling updates will not succeed** until API connectivity is restored
- [ ] Set `hyperfleet_api.endpoint` to HyperFleet API URL
- [ ] Adjust `hyperfleet_api.timeout` if needed (default: `5s`)
- [ ] Reference: [Required Fields](#required-fields)

### Phase 2: Broker Preparation

**Select and Configure Broker**

- [ ] Choose broker type: `rabbitmq` or `googlepubsub`
- [ ] Reference: [Broker Configuration](#broker-configuration)

**Provision Broker Infrastructure**

- [ ] **RabbitMQ:**
  - Create exchange and queues
  - Configure topic routing keys
  - Set retention and delivery policies
- [ ] **Google Pub/Sub:**
  - Create topic with your chosen naming convention (e.g., `hyperfleet-prod-clusters`)
  - Configure message retention duration
  - Set up dead-letter topic (optional)

**Configure Authentication and Permissions**

- [ ] **RabbitMQ:**
  - Create credentials for Sentinel service
  - Grant publish permissions to exchange
  - Prepare `BROKER_RABBITMQ_URL` connection string
- [ ] **Google Pub/Sub:**
  - Configure Workload Identity Federation (GKE) or service account
  - Grant `roles/pubsub.publisher` role to Sentinel service account

### Phase 3: Deployment

**Deploy with Helm**

- [ ] Update Helm chart `values.yaml` with:
  - Sentinel configuration (`config` section)
  - Broker configuration (`broker` section)
  - Sensitive credentials in `secrets` or reference to existing secrets
- [ ] Install Sentinel using Helm chart:
  ```bash
  helm install sentinel ./charts \
    --namespace hyperfleet-system \
    --values values.yaml
  ```
- [ ] Verify deployment:
  ```bash
  kubectl get deployment -n hyperfleet-system sentinel
  kubectl get pods -n hyperfleet-system -l app.kubernetes.io/name=sentinel
  ```
- [ ] Reference: [Helm Chart README](../charts/README.md)

### Phase 4: Post-Deployment Validation

**Verify Service Health**

- [ ] Check health endpoint: `curl http://<sentinel-service>:8080/healthz`
- [ ] Check readiness endpoint: `curl http://<sentinel-service>:8080/readyz`
  - **Note:** The `/readyz` endpoint returns `false` until the first successful poll completes and broker health checks pass. Pods intentionally stay unready during initial startup.
  - If startup latency causes false-positive readiness probe failures, tune the Kubernetes readiness probe timing (e.g., increase `initialDelaySeconds` or `periodSeconds`) in your Helm values.
- [ ] Review pod logs for startup errors:
  ```bash
  kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel
  ```
- [ ] Verify Sentinel is publishing events:
  ```bash
  kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel | grep -E "Fetched resources|Trigger cycle completed"
  ```
  Expected log output when Sentinel is operating correctly:
  ```text
  Fetched resources count=15 label_selectors=1 topic=hyperfleet-dev-clusters subset=clusters
  Trigger cycle completed total=15 published=3 skipped=12 duration=0.125s topic=hyperfleet-dev-clusters subset=clusters
  ```
  - `count` - Number of resources fetched from the API matching the resource selector
  - `published` - Number of events published (generation changed or max age exceeded)
  - `skipped` - Number of resources skipped (no reconciliation needed)

For detailed deployment guidance, see [docs/running-sentinel.md](running-sentinel.md)

---

## Additional Resources

### Documentation

- **[Running Sentinel](running-sentinel.md)** - Detailed guide for local and GKE deployments
- **[Metrics Documentation](metrics.md)** - Complete metrics catalog with PromQL examples
- **[Multi-Instance Deployment](multi-instance-deployment.md)** - Horizontal scaling strategies
- **[Testcontainers Documentation](testcontainers.md)** - Integration testing with testcontainers
- **[Helm Chart README](../charts/README.md)** - Helm chart configuration reference

### Tools and Libraries

- **[Common Expression Language (CEL)](https://github.com/google/cel-spec)** - CEL specification for `message_data`
- **[CloudEvents](https://cloudevents.io/)** - CloudEvents specification

---

## Appendix A: Troubleshooting

| Symptom                                                                                                                      | Likely Cause | Solution                                                                                                                                                                                                                                                                                                                 |
|------------------------------------------------------------------------------------------------------------------------------|--------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Events not published, resources not found** | Resource selector mismatch | Verify `resource_selector` matches resource labels. Empty selector watches ALL resources. Check logs: `kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel`                                                                                                                                             |
| **Events not published, resources found but skipped**                                                                           | Max age not exceeded | Normal behavior. Events publish when `generation > observed_generation` OR max age interval elapsed (`max_age_ready`: 30m, `max_age_not_ready`: 10s).                                                                                                                                                                    |
| **API connection errors, DNS lookup fails**                                                                                  | Wrong service name or namespace | Verify endpoint format: `http://<service>.<namespace>.svc.cluster.local:8000`. Check API is running: `kubectl get pods -n hyperfleet-system -l app=hyperfleet-api`                                                                                                                                                       |
| **API returns 401 Unauthorized**                                                                                             | Missing authentication | Add auth headers to `hyperfleet_api` config if API requires authentication.                                                                                                                                                                                                                                              |
| **API returns 404 Not Found**                                                                                                | Wrong API version in path | Verify endpoint uses correct API version: `/api/v1/clusters` or `/api/hyperfleet/v1/clusters`                                                                                                                                                                                                                            |
| **Broker PermissionDenied (Pub/Sub)**                                                                                        | Missing publisher role | Grant role: `gcloud projects add-iam-policy-binding ${GCP_PROJECT} --role="roles/pubsub.publisher" --member="principal://iam.googleapis.com/..."`                                                                                                                                                                        |
| **Broker Topic not found (Pub/Sub)**                                                                                         | Topic doesn't exist | Create topic: `gcloud pubsub topics create hyperfleet-prod-clusters --project=${GCP_PROJECT}`                                                                                                                                                                                                                            |
| **Broker type mismatch**                                                                                                     | Config doesn't match actual broker | Ensure `broker.type` matches: `rabbitmq` or `googlepubsub`. Check: `kubectl get configmap sentinel -o jsonpath='{.data.broker\.yaml}'`                                                                                                                                                                                   |
| **High CPU/memory usage**                                                                                                    | Too many resources or slow API | Check `kubectl top pod -n hyperfleet-system -l app=sentinel`. Consider horizontal scaling with `resource_selector` or increase poll intervals.                                                                                                                                                                           |
| **Error: resource_type is required**                                                                                         | Missing required config field | Add `resource_type: clusters` or `resource_type: nodepools` to configuration.                                                                                                                                                                                                                                            |
| **Error: invalid resource_type**                                                                                             | Invalid value | Use only `clusters` or `nodepools`.                                                                                                                                                                                                                                                                                      |
| **Error: hyperfleet_api.endpoint is required**                                                                               | Missing required config field | Add `hyperfleet_api.endpoint: http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8000`                                                                                                                                                                                                                            |
| **Error: poll_interval must be positive**                                                                                    | Zero or negative interval | Set `poll_interval: 5s` (must be > 0).                                                                                                                                                                                                                                                                                   |
| **Error: OpenAPI client not generated**                                                                                      | Missing generated code | Run `make generate && make build` before starting Sentinel.                                                                                                                                                                                                                                                              |
| **Pods stay unready after startup**                                                                                         | Normal startup behavior | The `/readyz` endpoint returns `false` until the first successful poll completes and broker health checks pass. This is expected. If readiness probe failures persist beyond initial startup, check pod logs and broker connectivity. Tune probe timing (e.g., increase `initialDelaySeconds`) in Helm values if needed. |
| **Health/readiness endpoints return errors**                                                                                 | Configuration validation failed | Check pod logs for startup errors: `kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel`. Verify all required config fields.                                                                                                                                                                            |

---
