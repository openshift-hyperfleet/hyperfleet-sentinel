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

   The OpenAPI client code is generated from `openapi/openapi.yaml` and placed in `pkg/api/openapi/`. These generated files are **not committed** to git and must be regenerated locally.

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
- `make fmt` - Format Go code
- `make lint` - Run golangci-lint (requires golangci-lint installed)
- `make clean` - Remove build artifacts and generated code

### OpenAPI Client Generation

This project follows the [rh-trex](https://github.com/openshift-online/rh-trex) pattern for OpenAPI client generation. The client is generated using Docker/Podman to ensure consistency across development environments.

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

## Repository Access

All members of the **hyperfleet** team have write access to this repository.

### Steps to Apply for Repository Access

If you're a team member and need access to this repository:

1. **Verify Organization Membership**: Ensure you're a member of the `openshift-hyperfleet` organization
2. **Check Team Assignment**: Confirm you're added to the hyperfleet team within the organization
3. **Repository Permissions**: All hyperfleet team members automatically receive write access
4. **OWNERS File**: Code reviews and approvals are managed through the OWNERS file

For access issues, contact a repository administrator or organization owner.
