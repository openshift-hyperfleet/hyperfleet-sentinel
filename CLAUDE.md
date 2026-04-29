# CLAUDE.md

## Project Identity

HyperFleet Sentinel is a **Kubernetes resource watcher** that polls the HyperFleet API for cluster/nodepool updates, makes orchestration decisions based on max age intervals, and publishes CloudEvents to message brokers. It is stateless, horizontally scalable via label-based sharding, and delegates all state persistence to the API.

- **Language**: Go 1.25+
- **Messaging**: Broker abstraction supporting RabbitMQ, GCP Pub/Sub, and Stub implementations
- **API Client**: Generated from [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) OpenAPI spec
- **Deployment**: Helm chart with PodMonitoring (GKE) and ServiceMonitor (Prometheus Operator)

## Critical First Steps

**Generated OpenAPI client is NOT committed to git.** Before any build, test, or development task:

```bash
make generate    # Downloads spec from hyperfleet-api and generates Go client
```

Setup sequence for a fresh clone:
1. `make generate` — generate OpenAPI client in `pkg/api/openapi/`
2. `make download` — fetch Go dependencies
3. `make build` — build `bin/sentinel` binary
4. `make test` — verify unit tests pass

## Verification Commands

| Command | What it does |
|---|---|
| `make verify` | go vet + format check (fast) |
| `make lint` | golangci-lint (comprehensive) |
| `make test` | unit tests only (no external deps) |
| `make test-integration` | integration tests with testcontainers (RabbitMQ, Pub/Sub) |
| `make test-helm` | Helm chart lint and validation |
| `make test-all` | lint + unit + integration + helm tests |

Use `make verify && make test` for fast local feedback. Use `make test-all` before pushing.

## Code Conventions

### Commit Messages
Format: `HYPERFLEET-### - type: description`

Example:
```
HYPERFLEET-427 - feat: add standard metrics labels

Adds resource_type and resource_selector labels to all
Prometheus metrics for consistent querying.

Co-Authored-By: Claude <noreply@anthropic.com>
```

### Import Ordering
1. Standard library
2. External packages (`github.com/google/cel-go`, `github.com/prometheus/client_golang`)
3. HyperFleet packages (`github.com/openshift-hyperfleet/hyperfleet-broker`, etc.)
4. Internal packages (`github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/...`)

### Configuration
- Config lives in `internal/config/config.go` — struct tags for YAML, validation via `Validate()`
- All durations use `time.Duration` with YAML `duration` format (e.g., `5s`, `30m`)
- Environment variables override YAML only for broker credentials (via hyperfleet-broker library)
- Config validation fails fast at startup — never run with invalid config

### Error Handling
- Errors propagate with context: `fmt.Errorf("failed to poll API: %w", err)`
- Log errors at the boundary (main service loop), not deep in call stack
- Use structured logging: `logger.Error("msg", "key", value, "error", err)`

### Metrics
- All metrics defined in `pkg/metrics/metrics.go` — use Prometheus client conventions
- Standard labels on all metrics: `resource_type`, `resource_selector`
- Counter: `_total` suffix (e.g., `hyperfleet_sentinel_events_published_total`)
- Gauge: no suffix (e.g., `hyperfleet_sentinel_pending_resources`)
- Histogram: `_seconds` suffix (e.g., `hyperfleet_sentinel_poll_duration_seconds`)

### Testing
- Unit tests: mock external dependencies (API client, broker), fast, deterministic
- Integration tests: testcontainers for real RabbitMQ/Pub/Sub, slower, covers end-to-end flows
- Test file naming: `*_test.go` alongside implementation
- Integration tests: `test/integration/*_test.go` with build tag `//go:build integration`

### CloudEvents Structure
Events use CEL expressions from `message_data` config to build payloads:
```yaml
message_data:
  id: resource.id            # CEL expressions, not static values
  kind: resource.kind
  href: resource.href
  generation: resource.generation
```

CEL context includes:
- `resource` — the cluster/nodepool object from API
- `reason` — decision string ("not_reconciled", "reconciled_stale", "reconciled_fresh")

## Project Boundaries

**DO NOT**:
- Modify generated code in `pkg/api/openapi/` — regenerate via `make generate` instead
- Add dependencies without checking licenses (`go-licenses` reports in CI)
- Commit broker credentials or GCP service account keys
- Add business logic to Sentinel — orchestration decisions only, execution belongs in adapters
- Store state in Sentinel — it is stateless, API is the source of truth
- Poll faster than API can handle — respect backpressure and rate limits

**DO**:
- Use `make generate` after any hyperfleet-api spec changes
- Add tests for new features (unit + integration if broker/API interaction)
- Update Prometheus metrics when adding observable behaviors
- Update CHANGELOG.md for user-visible changes
- Follow the ObjectReference pattern for CloudEvents payloads (id, kind, href)
- Use broker abstraction (`hyperfleet-broker`) — never import RabbitMQ/Pub/Sub clients directly

## Architecture Context

Sentinel is one component in the HyperFleet control plane:
- **API** persists cluster/nodepool state (source of truth)
- **Sentinel** watches API, decides when resources need reconciliation, publishes events
- **Adapters** consume events, execute provisioning/deprovisioning, report status back to API
- **Broker** (RabbitMQ or Pub/Sub) decouples Sentinel from adapters

Sentinel's job: **decide when**, not **execute how**. Max age intervals define "when":
- `max_age_not_reconciled`: poll frequently for unstable resources
- `max_age_reconciled`: poll infrequently for stable resources

## Local Development

```bash
# 1. Start HyperFleet API (see hyperfleet-api repo) and RabbitMQ
docker run -d -p 5672:5672 rabbitmq:3-management

# 2. Configure (see configs/dev-example.yaml and broker.yaml for templates)
# 3. Run Sentinel
./bin/sentinel serve --config config.yaml

# Watch events at http://localhost:15672 (guest/guest)
```

For detailed local/GKE deployment, see [docs/running-sentinel.md](docs/running-sentinel.md).

## Helm Chart

Chart lives in `charts/` with values for:
- Multiple Sentinel instances with different `resource_selector` (sharding)
- Monitoring: PodMonitoring (GKE/GMP) or ServiceMonitor (Prometheus Operator)
- Broker config via ConfigMap (type, topic) + Secret (credentials)

Example: deploy 2 Sentinels watching different shards:
```bash
helm install sentinel-shard-1 ./charts \
  --set config.resourceSelector[0].label=shard \
  --set config.resourceSelector[0].value=1 \
  --set broker.topic=hyperfleet-prod-clusters

helm install sentinel-shard-2 ./charts \
  --set config.resourceSelector[0].label=shard \
  --set config.resourceSelector[0].value=2 \
  --set broker.topic=hyperfleet-prod-clusters
```

Both read from the same API and publish to the same topic, but watch different label-filtered subsets.

## Validation Checklist

Before submitting a PR:
1. `make generate` — ensure OpenAPI client is current
2. `make fmt` — format code
3. `make verify` — vet and format check
4. `make lint` — pass golangci-lint
5. `make test` — pass unit tests
6. `make test-integration` — pass integration tests (if broker/API changes)
7. `make test-helm` — validate Helm chart
8. Update CHANGELOG.md for user-visible changes
9. Add metrics if new observable behavior
10. Commit message follows `HYPERFLEET-### - type: description` format
