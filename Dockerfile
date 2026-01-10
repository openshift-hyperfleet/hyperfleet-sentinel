# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies (make, curl for downloading OpenAPI spec, git for version info)
RUN apk add --no-cache make curl git

WORKDIR /build

# OpenAPI spec configuration from hyperfleet-api repository
ARG OPENAPI_SPEC_URL=https://raw.githubusercontent.com/openshift-hyperfleet/hyperfleet-api/main/openapi/openapi.yaml
ENV OPENAPI_SPEC_URL=${OPENAPI_SPEC_URL}

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build configuration
COPY . .

# Build binary using Makefile (includes make generate for OpenAPI client generation)
RUN CGO_ENABLED=0 GOOS=linux make build

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/bin/sentinel /app/sentinel

# Config will be provided via Helm ConfigMap mount
# COPY configs/sentinel.yaml /app/configs/sentinel.yaml

ENTRYPOINT ["/app/sentinel"]
CMD ["serve"]

LABEL name="hyperfleet-sentinel" \
      vendor="Red Hat" \
      version="0.1.0" \
      summary="HyperFleet Sentinel - Resource polling and event publishing service" \
      description="Watches HyperFleet API resources and publishes reconciliation events to message brokers"
