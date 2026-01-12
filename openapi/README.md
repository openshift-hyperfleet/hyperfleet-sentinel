# OpenAPI Specification

This directory contains the OpenAPI specification for the HyperFleet API, fetched from the official [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) repository.

## OpenAPI Spec Source

The `openapi.yaml` file is **automatically downloaded** during `make generate` from:
- **Repository**: https://github.com/openshift-hyperfleet/hyperfleet-api
- **Default ref**: main (configurable via `OPENAPI_SPEC_REF`)
- **File**: `openapi/openapi.yaml`

**Important**: The `openapi.yaml` file is **NOT committed** to git. It is downloaded fresh on every `make generate` to ensure you're always using the official specification.

## Generating the Client

To generate the Go client from the latest OpenAPI spec:

```bash
make generate
```

This will:
1. Download `openapi.yaml` from hyperfleet-api (main branch by default)
2. Generate Go client code in `pkg/api/openapi/`
3. Format the generated code

**Important**: Generated files in `pkg/api/openapi/` are also **NOT committed** to git. They must be regenerated locally during development.

## Using a Different Branch or Tag

To use a specific branch or tag:

```bash
# Use a specific tag
make generate OPENAPI_SPEC_REF=v1.0.0

# Use a different branch
make generate OPENAPI_SPEC_REF=develop

# Use a commit SHA
make generate OPENAPI_SPEC_REF=abc123
```

You can also set it as an environment variable:

```bash
export OPENAPI_SPEC_REF=v1.0.0
make generate
```

## Generator Details

- **Tool**: OAPI Codegen https://github.com/oapi-codegen/oapi-codegen
- **Language**: Go
- **Output**: `pkg/api/openapi/` (not committed to git)
- **Go-based**: Uses oapi-codegen to generate go types
- **Wrapper**: `internal/client/client.go` provides a simplified interface to the generated client

The generator configuration follows the same pattern as [rh-trex](https://github.com/openshift-online/rh-trex).

## Updating the Client

When the hyperfleet-api repository is updated:

1. Run `make generate` (or `make generate OPENAPI_SPEC_REF=<ref>`) to download the spec and regenerate the client
2. Update `internal/client/client.go` wrapper if needed to support new endpoints/models
3. Run tests to ensure compatibility: `make test`

By default, the spec is fetched from the main branch. Use `OPENAPI_SPEC_REF` to pin to a specific version.
