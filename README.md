# hyperfleet-sentinel

HyperFleet Sentinel Service - Kubernetes service that polls HyperFleet API, makes orchestration decisions, and publishes events. Features configurable max age intervals, horizontal sharding via SentinelConfig CRD, and broker abstraction (GCP Pub/Sub, RabbitMQ, Stub). Centralized reconciliation logic.

## Development Setup

### Prerequisites

- Go 1.25 or later
- Docker or Podman
- Make

### Getting Started

1. **Clone the repository**:
   ```bash
   git clone https://github.com/openshift-hyperfleet/hyperfleet-sentinel.git
   cd hyperfleet-sentinel
   ```

2. **Generate the OpenAPI client**:
   ```bash
   make generate
   ```

   This will:
   - Download the official OpenAPI spec from [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) (main branch)
   - Generate Go client code in `pkg/api/openapi/`

   Both the downloaded spec and generated client code are **not committed** to git and must be regenerated locally.

3. **Download dependencies**:
   ```bash
   make download
   ```

4. **Build the binary**:
   ```bash
   make build
   ```

5. **Run tests**:
   ```bash
   make test
   ```

### Common Make Targets

- `make help` - Show all available make targets
- `make generate` - Generate OpenAPI client from spec (Docker/Podman-based)
- `make build` - Build the sentinel binary
- `make test` - Run unit tests with coverage
- `make test-integration` - Run integration tests (requires Docker/Podman)
- `make test-all` - Run all tests (unit + integration)
- `make fmt` - Format Go code
- `make lint` - Run golangci-lint (requires golangci-lint installed)
- `make clean` - Remove build artifacts and generated code

### Testing

The project uses a hybrid testing approach:

- **Unit tests**: Fast, isolated tests using mocks
- **Integration tests**: End-to-end tests with real message brokers via testcontainers

```bash
# Run only unit tests (fast)
make test

# Run integration tests (requires Docker or Podman)
make test-integration

# Run all tests
make test-all
```

Integration tests automatically work with both Docker and Podman. For troubleshooting and advanced configuration, see [docs/testcontainers.md](docs/testcontainers.md).

### OpenAPI Client Generation

This project follows the [rh-trex](https://github.com/openshift-online/rh-trex) pattern for OpenAPI client generation. The OpenAPI specification is automatically downloaded from the official [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) repository (main branch by default) during `make generate`.

The client is generated using Docker/Podman to ensure consistency across development environments.

To use a different branch or tag:
```bash
make generate OPENAPI_SPEC_REF=v1.0.0    # Use a specific tag
make generate OPENAPI_SPEC_REF=develop   # Use a branch
```

For detailed information about OpenAPI client generation, see [openapi/README.md](openapi/README.md).

## Configuration

The Sentinel service uses YAML-based configuration with environment variable overrides for sensitive data (broker credentials).

### Configuration File

Create a configuration file based on the examples in the `configs/` directory:

- **`configs/gcp-pubsub-example.yaml`** - GCP Pub/Sub configuration
- **`configs/rabbitmq-example.yaml`** - RabbitMQ configuration
- **`configs/dev-example.yaml`** - Development configuration

### Configuration Schema

#### Required Fields

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `resource_type` | string | Resource to watch (clusters, nodepools) | `clusters` |
| `hyperfleet_api.endpoint` | string | HyperFleet API base URL (k8s service) | `http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8080` |

#### Optional Fields with Defaults

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `poll_interval` | duration | `5s` | How often to poll the API for resource updates |
| `max_age_not_ready` | duration | `10s` | Max age interval for resources not ready |
| `max_age_ready` | duration | `30m` | Max age interval for ready resources |
| `hyperfleet_api.timeout` | duration | `5s` | Request timeout for API calls |
| `resource_selector` | array | `[]` | Label selectors for filtering resources (enables sharding) |
| `message_data` | map | `{}` | Template fields for CloudEvents data payload |

#### Resource Selector (Sharding)

The `resource_selector` field enables horizontal scaling by having multiple Sentinel instances watch different resource subsets:

```yaml
resource_selector:
  - label: shard
    value: "1"
  - label: region
    value: us-east-1
```

An empty or omitted `resource_selector` means watch all resources. Multiple selectors use AND logic (all labels must match).

#### Message Data Templates

Define custom fields to include in CloudEvents using Go template syntax. Both `.field` and `{{.field}}` formats are supported:

```yaml
message_data:
  resource_id: .id
  resource_type: .kind
  href: .href
  generation: .generation
```

Templates can reference any field from the Resource object returned by the API. The example above follows the `ObjectReference` pattern (id, kind, href) with generation for reconciliation tracking.

### Broker Configuration

Broker configuration is managed by the [hyperfleet-broker library](https://github.com/openshift-hyperfleet/hyperfleet-broker). You can configure the broker using either:

1. **broker.yaml file** (see `broker.yaml` in project root for example)
2. **BROKER_CONFIG_FILE environment variable** (path to your broker config file)
3. **Direct environment variables** (listed below)

#### Environment Variables (Override broker.yaml)

**RabbitMQ:**
| Variable | Description | Example |
|----------|-------------|---------|
| `BROKER_RABBITMQ_URL` | Complete connection URL | `amqp://user:pass@localhost:5672/vhost` |

**Google Pub/Sub:**
| Variable | Description | Example |
|----------|-------------|---------|
| `BROKER_GOOGLEPUBSUB_PROJECT_ID` | GCP project ID | `my-gcp-project` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Service account key path (optional, uses ADC if not set) | `/path/to/key.json` |

For detailed broker configuration options, see the [hyperfleet-broker documentation](https://github.com/openshift-hyperfleet/hyperfleet-broker).

### Running Locally

#### 1. Start a Message Broker

**RabbitMQ (easiest for local development):**
```bash
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management
```

RabbitMQ Management UI: http://localhost:15672 (guest/guest)

#### 2. Set Environment Variables

```bash
# For RabbitMQ (using hyperfleet-broker library)
export BROKER_RABBITMQ_URL=amqp://guest:guest@localhost:5672/

# Or configure via broker.yaml file (see broker.yaml in project root)
```

#### 3. Run Sentinel

```bash
# Copy example config
cp configs/dev-example.yaml config.yaml

# Edit config.yaml to match your local HyperFleet API endpoint
vim config.yaml

# Build and run
make build
./bin/sentinel serve --config=config.yaml
```

### Kubernetes Deployment

Deploy Sentinel to Kubernetes using the Helm chart:

```bash
# Install with default values (RabbitMQ)
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace

# Check status
helm status sentinel -n hyperfleet-system

# View logs
kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel -f
```

> **Note**: The `--create-namespace` flag creates the namespace if it doesn't exist. If the namespace already exists, Helm will use it and this flag has no effect.

See [deployments/helm/sentinel/README.md](deployments/helm/sentinel/README.md) for detailed Helm chart documentation, configuration options, and examples.

### Configuration Validation

The service validates configuration at startup and will fail fast on errors:

- **Required fields present**: `resource_type`, `hyperfleet_api.endpoint`
- **Valid enums**: `resource_type` must be clusters/nodepools
- **Valid durations**: All interval fields must be positive
- **Valid templates**: All `message_data` templates must be valid Go templates
- **Broker configuration**: Managed by hyperfleet-broker library (see broker.yaml)

### Configuration Examples

#### Minimal Configuration

```yaml
resource_type: clusters
hyperfleet_api:
  endpoint: http://localhost:8000
```

This uses all defaults. Broker configuration is managed via `broker.yaml` or environment variables (see Broker Configuration section).

#### Production Configuration with Sharding

```yaml
resource_type: clusters
poll_interval: 5s
max_age_not_ready: 10s
max_age_ready: 30m

resource_selector:
  - label: shard
    value: "1"

hyperfleet_api:
  endpoint: http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8080
  timeout: 30s

message_data:
  resource_id: .id
  resource_type: .kind
  href: .href
  generation: .generation
```

## Observability

The Sentinel service exposes Prometheus metrics on port 8080 at `/metrics` for monitoring and alerting.

### Metrics

Sentinel provides 6 core metrics for comprehensive observability:

| Metric | Type | Description |
|--------|------|-------------|
| `hyperfleet_sentinel_pending_resources` | Gauge | Number of resources pending reconciliation |
| `hyperfleet_sentinel_events_published_total` | Counter | Total events published to message broker |
| `hyperfleet_sentinel_resources_skipped_total` | Counter | Resources skipped (preconditions not met) |
| `hyperfleet_sentinel_poll_duration_seconds` | Histogram | Duration of each polling cycle |
| `hyperfleet_sentinel_api_errors_total` | Counter | Errors when calling HyperFleet API |
| `hyperfleet_sentinel_broker_errors_total` | Counter | Errors when publishing to message broker |

All metrics include `resource_type` and `resource_selector` labels for filtering.

**For detailed metric descriptions, example queries, and alerting rules**, see [docs/metrics.md](docs/metrics.md).

### Grafana Dashboard

A pre-built Grafana dashboard is available at `deployments/dashboards/sentinel-metrics.json` with 8 visualization panels covering all metrics.

To import:
1. Navigate to Grafana → Dashboards → Import
2. Upload `deployments/dashboards/sentinel-metrics.json`
3. Select your Prometheus datasource

### GKE Integration with Google Cloud Managed Prometheus

Sentinel integrates with Google Cloud Managed Prometheus (GMP) for automated metrics collection:

```bash
# Deploy with PodMonitoring enabled (default)
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace

# Verify metrics in Google Cloud Console
# Navigate to: Monitoring → Metrics Explorer
# Query: hyperfleet_sentinel_pending_resources
```

GMP automatically discovers the PodMonitoring resource and begins scraping metrics. No additional configuration required.

#### Alerting

Configure alerts in **Google Cloud Console → Monitoring → Alerting** using the PromQL expressions provided in [docs/metrics.md](docs/metrics.md).

Recommended alerts:
- SentinelHighPendingResources - High number of pending resources
- SentinelAPIErrorRateHigh - High API error rate
- SentinelBrokerErrorRateHigh - High broker error rate
- SentinelSlowPolling - Slow polling cycles
- SentinelNoEventsPublished - No events published despite pending resources
- SentinelHighSkipRatio - High ratio of skipped resources
- SentinelDown - Sentinel service is down

See [docs/metrics.md](docs/metrics.md) for complete alerting rules documentation.

### Accessing Metrics

Access metrics through Google Cloud Console:
1. Navigate to **Monitoring → Metrics Explorer**
2. Select resource type: **Prometheus Target**
3. Query: `hyperfleet_sentinel_pending_resources`

Metrics are automatically collected by Google Cloud Managed Prometheus via the PodMonitoring resource.

## Repository Access

All members of the **hyperfleet** team have write access to this repository.

### Steps to Apply for Repository Access

If you're a team member and need access to this repository:

1. **Verify Organization Membership**: Ensure you're a member of the `openshift-hyperfleet` organization
2. **Check Team Assignment**: Confirm you're added to the hyperfleet team within the organization
3. **Repository Permissions**: All hyperfleet team members automatically receive write access
4. **OWNERS File**: Code reviews and approvals are managed through the OWNERS file

For access issues, contact a repository administrator or organization owner.
