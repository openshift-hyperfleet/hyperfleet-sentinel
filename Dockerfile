# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

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
