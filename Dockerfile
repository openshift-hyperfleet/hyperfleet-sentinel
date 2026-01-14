ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot

FROM golang:1.25 AS builder

ARG GIT_SHA=unknown
ARG GIT_DIRTY=""

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux make build

# Runtime stage
FROM ${BASE_IMAGE}

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
