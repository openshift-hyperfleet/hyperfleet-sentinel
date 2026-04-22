# Contributing to hyperfleet-sentinel

## Development Setup

Complete local environment setup including prerequisites, installation, and first run.

```bash
# 1. Clone the repository
git clone https://github.com/openshift-hyperfleet/hyperfleet-sentinel.git
cd hyperfleet-sentinel

# 2. Install prerequisites
# - Go 1.25 or later
# - Docker or Podman
# - Make
# - pre-commit

# 3. Generate OpenAPI client (required before any other commands)
make generate

# 4. Install dependencies
make download

# 5. Build the project
make build

# 6. Run tests to verify setup
make test

# 7. Install git hooks
make install-hooks
```

**First-time setup notes:**
- The OpenAPI client code is **not committed** to git and must be generated locally via `make generate`
- Generated code appears in `pkg/api/openapi/` and must be regenerated after any spec updates
- Integration tests require Docker/Podman for testcontainers (RabbitMQ, GCP Pub/Sub emulators)
- `make install-hooks` installs pre-commit hooks configured in `.pre-commit-config.yaml` for commit message and code quality validation on every commit
- See [docs/testcontainers.md](docs/testcontainers.md) for Docker/Podman configuration
- See [docs/running-sentinel.md](docs/running-sentinel.md) for detailed runtime instructions

## Repository Structure

Overview of key directories and files - what's where and why.

```
hyperfleet-sentinel/
├── cmd/
│   └── sentinel/           # Main application entry point
├── internal/               # Private application code
│   ├── client/            # HyperFleet API client with retry logic
│   ├── config/            # Configuration loading and validation
│   ├── engine/            # Core polling and reconciliation engine
│   ├── health/            # Health and readiness probes
│   ├── publisher/         # CloudEvents publishing logic
│   └── sentinel/          # Main service orchestration
├── pkg/                   # Public library code
│   ├── api/openapi/       # Generated OpenAPI client (not committed)
│   └── metrics/           # Prometheus metrics definitions
├── test/
│   └── integration/       # Integration tests with testcontainers
├── charts/                # Helm chart for deployment
├── configs/               # Example configuration files
├── deployments/           # Kubernetes manifests and Grafana dashboards
├── openapi/               # OpenAPI client generation config
├── docs/                  # Additional documentation
├── Makefile               # Build automation and development tasks
├── README.md              # Getting started guide
└── CONTRIBUTING.md        # This file
```

## Testing

How to run unit tests, integration tests, linting, and validation.

### Unit Tests
```bash
# Run all unit tests (fast, no external dependencies)
make test

# Run unit tests with coverage
make test-coverage

# Run unit tests for a specific package
go test -v ./internal/config/
```

### Integration Tests
```bash
# Run integration tests (requires Docker/Podman)
make test-integration

# Integration tests use testcontainers to spin up:
# - RabbitMQ broker
# - GCP Pub/Sub emulator
```

### Linting and Quality Checks
```bash
# Run all verification checks (vet + format check)
make verify

# Run golangci-lint
make lint

# Check code formatting
make fmt-check

# Auto-format code
make fmt

# Run all quality gates (lint + all tests)
make test-all
```

### Helm Chart Testing
```bash
# Lint and validate Helm chart
make test-helm
```

## Common Development Tasks

Build commands, running locally, generating code, etc.

### Building
```bash
# Clean build
make clean && make build

# Build container image
make image

# Build and push to registry
make image-push

# Build and push to personal Quay registry
QUAY_USER=myusername make image-dev
```

### Running Locally
```bash
# Run with example configuration
./bin/sentinel serve --config configs/dev-example.yaml

# Run with custom broker configuration
BROKER_CONFIG_FILE=broker.yaml ./bin/sentinel serve --config configs/dev-example.yaml

# For detailed local/GKE deployment instructions, see:
# docs/running-sentinel.md
```

### Code Generation
```bash
# Generate OpenAPI client from hyperfleet-api spec (main branch)
make generate

# Generate from a specific branch or tag
make generate OPENAPI_SPEC_REF=v1.0.0
make generate OPENAPI_SPEC_REF=develop

# Clean generated code
make clean
```

## Commit Standards

All commits must follow the HyperFleet commit message format:

```
HYPERFLEET-### - type: description

Optional longer description.

Co-Authored-By: Name <email@example.com>
```

**Types**: `feat`, `fix`, `chore`, `docs`, `test`, `refactor`

**Examples**:
- `HYPERFLEET-123 - feat: add support for nested payload fields`
- `HYPERFLEET-456 - fix: handle nil pointer in status polling`
- `HYPERFLEET-789 - chore: update hyperfleet-broker to v1.1.0`

For detailed commit conventions, see the [HyperFleet Architecture Repository](https://github.com/openshift-hyperfleet/architecture).

## Release Process

Releases are created manually via GitHub Releases and follow Semantic Versioning.

1. Update CHANGELOG.md - move [Unreleased] items to new version section
2. Create and push version tag: `git tag v0.x.y && git push origin v0.x.y`
3. Create GitHub Release with tag, referencing CHANGELOG entries
4. Container images are built and pushed to `quay.io/openshift-hyperfleet/sentinel:v0.x.y`
5. Update [hyperfleet-chart](https://github.com/openshift-hyperfleet/hyperfleet-chart) umbrella chart with new version

Deployment is managed via the hyperfleet-chart umbrella Helm chart which includes Sentinel as a subchart.
