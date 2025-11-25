# OpenAPI Specification

This directory contains the OpenAPI specification for the HyperFleet API, fetched from the official [hyperfleet-api-spec](https://github.com/openshift-hyperfleet/hyperfleet-api-spec) repository.

## OpenAPI Spec Source

The `openapi.yaml` file is **automatically downloaded** during `make generate` from:
- **Repository**: https://github.com/openshift-hyperfleet/hyperfleet-api-spec
- **Version**: Controlled by `OPENAPI_SPEC_VERSION` in Makefile (default: v1.0.0)
- **File**: `core-openapi.yaml` from the release

**Important**: The `openapi.yaml` file is **NOT committed** to git. It is downloaded fresh on every `make generate` to ensure you're always using the official specification.

## Generating the Client

To generate the Go client from the latest OpenAPI spec:

```bash
make generate
```

This will:
1. Download `core-openapi.yaml` from hyperfleet-api-spec v1.0.0 release
2. Generate Go client code in `pkg/api/openapi/`
3. Format the generated code

**Important**: Generated files in `pkg/api/openapi/` are also **NOT committed** to git. They must be regenerated locally during development.

## Using a Different Spec Version

To use a different version of the hyperfleet-api-spec:

```bash
# Use a specific version
make generate OPENAPI_SPEC_VERSION=v1.1.0

# Use the latest release
make generate OPENAPI_SPEC_VERSION=latest
```

You can also set the version in your environment:

```bash
export OPENAPI_SPEC_VERSION=v1.1.0
make generate
```

## Generator Details

- **Tool**: OpenAPI Generator CLI v7.16.0
- **Language**: Go
- **Output**: `pkg/api/openapi/` (not committed to git)
- **Docker-based**: Uses `Dockerfile.openapi` for consistent generation across environments
- **Wrapper**: `internal/client/client.go` provides a simplified interface to the generated client

The generator configuration follows the same pattern as [rh-trex](https://github.com/openshift-online/rh-trex).

## Updating the Client

When the hyperfleet-api-spec repository releases a new version:

1. Update `OPENAPI_SPEC_VERSION` in the Makefile (or use the environment variable)
2. Run `make generate` to download the new spec and regenerate the client
3. Update `internal/client/client.go` wrapper if needed to support new endpoints/models
4. Run tests to ensure compatibility: `make test`
