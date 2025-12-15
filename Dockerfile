FROM openapitools/openapi-generator-cli:v7.16.0 AS openapi-gen

WORKDIR /local

# OpenAPI spec configuration from hyperfleet-api repository
ARG OPENAPI_SPEC_REF=main
ARG OPENAPI_SPEC_URL=https://raw.githubusercontent.com/openshift-hyperfleet/hyperfleet-api/${OPENAPI_SPEC_REF}/openapi/openapi.yaml

# Fetch OpenAPI spec from hyperfleet-api
RUN echo "Fetching OpenAPI spec from hyperfleet-api (ref: ${OPENAPI_SPEC_REF})..." && \
    mkdir -p openapi && \
    wget -O openapi/openapi.yaml "${OPENAPI_SPEC_URL}" || \
    (echo "Failed to download OpenAPI spec from ${OPENAPI_SPEC_URL}" && exit 1)


# Generate Go client/models from OpenAPI spec
RUN bash /usr/local/bin/docker-entrypoint.sh generate \
    -i /local/openapi/openapi.yaml \
    -g go \
    -o /local/pkg/api/openapi && \
    rm -f /local/pkg/api/openapi/go.mod /local/pkg/api/openapi/go.sum && \
    rm -rf /local/pkg/api/openapi/test

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy generated OpenAPI client from openapi-gen stage
COPY --from=openapi-gen /local/pkg/api/openapi ./pkg/api/openapi

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o sentinel ./cmd/sentinel

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/sentinel /app/sentinel

# Config will be provided via Helm ConfigMap mount
# COPY configs/sentinel.yaml /app/configs/sentinel.yaml

ENTRYPOINT ["/app/sentinel"]
CMD ["serve"]

LABEL name="hyperfleet-sentinel" \
      vendor="Red Hat" \
      version="0.1.0" \
      summary="HyperFleet Sentinel - Resource polling and event publishing service" \
      description="Watches HyperFleet API resources and publishes reconciliation events to message brokers"
