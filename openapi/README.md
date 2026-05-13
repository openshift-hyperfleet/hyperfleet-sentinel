# OpenAPI Specification

This directory contains the oapi-codegen configuration and the extracted OpenAPI specification
for the HyperFleet API, sourced from the
[hyperfleet-api-spec](https://github.com/openshift-hyperfleet/hyperfleet-api-spec) Go module.

## OpenAPI Spec Source

The `openapi.yaml` file is **copied during `make generate`** from the `hyperfleet-api-spec`
Go module cache (located via `go list -m`). The module version is pinned in `go.mod`:

```
github.com/openshift-hyperfleet/hyperfleet-api-spec v1.0.12
```

**Important**: The `openapi.yaml` file is **NOT committed** to git. It is copied fresh on
every `make generate` from the module cache.

## Generating the Client

To generate the Go client from the pinned spec:

```bash
make generate
```

This will:
1. Copy `$(VARIANT)/openapi.yaml` from the `hyperfleet-api-spec` module cache (located via `go list -m`)
2. Generate Go client code in `pkg/api/openapi/`

**Important**: Generated files in `pkg/api/openapi/` are also **NOT committed** to git. They
must be regenerated locally during development.

## Updating the Spec Version

Sentinel is a client of `hyperfleet-api`, so both services must use the **compatible** `hyperfleet-api-spec`
version. Before upgrading, check which version `hyperfleet-api` currently [pins](https://github.com/openshift-hyperfleet/hyperfleet-api/blob/main/go.mod).

Once you have the target version:

```bash
go get github.com/openshift-hyperfleet/hyperfleet-api-spec@vX.Y.Z
go mod tidy
make generate
```

Then update `internal/client/client.go` if needed to support new endpoints or models, and run
`make test` to verify compatibility.

## Generator Details

- **Tool**: [OAPI Codegen](https://github.com/oapi-codegen/oapi-codegen)
- **Language**: Go
- **Output**: `pkg/api/openapi/` (not committed to git)
- **Schema extraction**: `go list -m -f '{{.Dir}}'` locates the module cache directory; `$(VARIANT)/openapi.yaml` is copied to `openapi/openapi.yaml` for oapi-codegen input
- **Wrapper**: `internal/client/client.go` provides a simplified interface to the generated client
