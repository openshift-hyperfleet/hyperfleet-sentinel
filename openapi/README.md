# OpenAPI Specification

This directory contains the OpenAPI specification for the HyperFleet API.

## Current Status

The file `hyperfleet-api.yaml` is currently a **placeholder** with a minimal schema. It will be replaced with the actual HyperFleet API specification when available.

The codebase currently uses the OpenAPI-generated client through a wrapper in `internal/client/client.go`.

## Generating the Client

When you update the OpenAPI spec:

1. Edit `hyperfleet-api.yaml` with new/updated spec
2. Run the generator:
   ```bash
   make generate
   ```
3. The generated client will be updated in `pkg/api/openapi/`
4. Update `internal/client/client.go` wrapper if needed to support new endpoints/models

## Generator Details

- **Tool**: OpenAPI Generator CLI v7.16.0
- **Language**: Go
- **Output**: `pkg/api/openapi/`
- **Docker-based**: Uses `Dockerfile.openapi` for consistent generation
- **Wrapper**: `internal/client/client.go` provides a simplified interface to the generated client

The generator configuration follows the same pattern as [rh-trex](https://github.com/openshift-online/rh-trex).
