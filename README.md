# hyperfleet-sentinel

HyperFleet Sentinel Service - Kubernetes service that polls HyperFleet API, makes orchestration decisions, and publishes events. Features configurable backoff strategies, horizontal sharding via SentinelConfig CRD, and broker abstraction (GCP Pub/Sub, RabbitMQ, Stub). Centralized reconciliation logic.

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

   The OpenAPI client code is generated from `openapi/hyperfleet-api.yaml` and placed in `pkg/api/openapi/`. These generated files are **not committed** to git and must be regenerated locally.

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

## Repository Access

All members of the **hyperfleet** team have write access to this repository.

### Steps to Apply for Repository Access

If you're a team member and need access to this repository:

1. **Verify Organization Membership**: Ensure you're a member of the `openshift-hyperfleet` organization
2. **Check Team Assignment**: Confirm you're added to the hyperfleet team within the organization
3. **Repository Permissions**: All hyperfleet team members automatically receive write access
4. **OWNERS File**: Code reviews and approvals are managed through the OWNERS file

For access issues, contact a repository administrator or organization owner.
