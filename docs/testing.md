# Testing and Deployment Guide

> **IMPORTANT**: This documentation is for **TESTING purposes only**. Production deployments are handled via CI/CD pipelines.

This guide enables developers to test Sentinel both locally (for development) and on GKE (for integration testing) before merging code changes.

## Table of Contents

- [Local Testing (Development Environment)](#local-testing-development-environment)
  - [Prerequisites](#prerequisites-for-local-testing)
  - [Setting Up Local Broker](#1-setting-up-local-broker)
  - [Configuring Sentinel](#2-configuring-sentinel-for-local-testing)
  - [Running Sentinel](#3-running-sentinel-locally)
  - [Verification Steps](#4-verification-steps-for-local-testing)
  - [Running Tests](#5-running-tests-locally)
- [GKE Testing (Integration Environment)](#gke-testing-integration-environment)
  - [Prerequisites](#prerequisites-for-gke-testing)
  - [Building Container Image](#1-building-container-image-for-gke)
  - [Authentication and Image Push](#2-authentication-and-image-push)
  - [Helm Deployment](#3-helm-deployment-test-environment)
  - [Verification Steps](#4-verification-steps-for-gke)
  - [Cleanup](#5-cleanup)
- [Troubleshooting](#troubleshooting)

---

## Local Testing (Development Environment)

### Prerequisites for Local Testing

- Go 1.25+ installed
- Podman (for running broker locally and integration tests)
- Make utility
- Access to a message broker (RabbitMQ recommended for local development)
- HyperFleet API accessible (local or remote instance)

### 1. Setting Up Local Broker

#### Option A: RabbitMQ via Podman (Recommended)

```bash
podman run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management
```

Verify: Access RabbitMQ management console at http://localhost:15672 (guest/guest)

#### Option B: Google Pub/Sub Emulator

```bash
# Start the emulator (runs on port 8085 by default)
gcloud beta emulators pubsub start --project=test-project --host-port=localhost:8085
```

> **Note**: The emulator runs in the foreground. Open a new terminal for subsequent commands.

### 2. Configuring Sentinel for Local Testing

#### Step 1: Generate the OpenAPI Client

Before running Sentinel, you must generate the OpenAPI client:

```bash
make generate
```

This downloads the OpenAPI spec from [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) and generates the Go client code.

#### Step 2: Set Broker Environment Variable

For RabbitMQ:

```bash
export BROKER_RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
```

For Google Pub/Sub Emulator:

First, create a broker configuration file `configs/broker-pubsub.yaml`:

```yaml
broker:
  type: googlepubsub
  googlepubsub:
    project_id: test-project
```

Then set the environment variables:

```bash
export PUBSUB_EMULATOR_HOST=localhost:8085
export BROKER_CONFIG_FILE=configs/broker-pubsub.yaml
```

### 3. Running Sentinel Locally

#### Option A: Build and Run Binary

```bash
# Build the binary
make build

# Run Sentinel
./sentinel serve --config=configs/dev-example.yaml
```

#### Option B: Run Directly with Go

```bash
go run ./cmd/sentinel serve --config=configs/dev-example.yaml
```

### 4. Verification Steps for Local Testing

#### Check Health Endpoint

```bash
curl http://localhost:8080/health
```

Expected response:

```
OK
```

#### Check Metrics Endpoint

```bash
curl http://localhost:8080/metrics | grep hyperfleet_sentinel
```

**Without HyperFleet API running**, you will see error metrics:

```
hyperfleet_sentinel_api_errors_total{error_type="fetch_error",resource_selector="all",resource_type="clusters"} 1
```

> **Note**: This is expected behavior when testing locally without a HyperFleet API instance. The `api_errors_total` metric indicates the Sentinel is running correctly but cannot reach the API.

**With HyperFleet API running**, you will see additional metrics:

```
hyperfleet_sentinel_pending_resources{...} 0
hyperfleet_sentinel_events_published_total{...} 0
hyperfleet_sentinel_poll_duration_seconds_bucket{...}
```

#### Monitor Logs

Watch console output for startup and broker connection messages.

**For RabbitMQ**, you should see the broker connection log:

```
[watermill] 2025/12/01 15:28:26.051755 connection.go:99: level=INFO msg="Connected to AMQP"
```

**For Google Pub/Sub**, there is no explicit connection log (see [HYPERFLEET-276](https://issues.redhat.com/browse/HYPERFLEET-276)). The publisher initializes silently. You can verify it's working by checking the health endpoint (`curl http://localhost:8080/health`) and metrics.

> **Note**: If the HyperFleet API is not running, Sentinel will still start but API polling will fail silently (visible in metrics as `api_errors_total`). This is expected for local broker testing.

#### Verify Broker Integration

**For RabbitMQ:**
1. Open management console at http://localhost:15672
2. Login with guest/guest
3. Check Exchanges tab for `hyperfleet.sentinel` exchange
4. Check Queues tab for any bound queues

**For Pub/Sub Emulator:**
- Check emulator logs for published messages

### 5. Running Tests Locally

#### Unit Tests

```bash
make test
```

#### Integration Tests

Integration tests use testcontainers to automatically spin up broker containers:

```bash
make test-integration
```

> **Note**: Integration tests require Podman. See [docs/testcontainers.md](testcontainers.md) for Podman-specific configuration.

#### All Tests

```bash
make test-all
```

#### Check Test Coverage

```bash
# Run tests with coverage
go test -coverprofile=coverage.out ./...

# View coverage report in browser
go tool cover -html=coverage.out
```

---

## GKE Testing (Integration Environment)

### Prerequisites for GKE Testing

- GKE cluster with Google Cloud Managed Prometheus (GMP) enabled
- `gcloud` CLI configured and authenticated
- `kubectl` configured for the cluster
- `podman` for building images
- `helm` for deploying the chart
- Access to `gcr.io/hcm-hyperfleet` container registry (or your own registry)

### 1. Building Container Image for GKE

```bash
# Build for AMD64 (required for GKE)
podman build --platform linux/amd64 -t gcr.io/hcm-hyperfleet/sentinel:test-$(git rev-parse --abbrev-ref HEAD) .
```

> **Note**: If building on ARM64 Mac for AMD64 GKE, you must use `--platform linux/amd64` to avoid architecture mismatch errors.

### 2. Authentication and Image Push

#### Configure Authentication with GCR

```bash
gcloud auth configure-docker gcr.io
```

#### Push Test Image to Registry

```bash
podman push gcr.io/hcm-hyperfleet/sentinel:test-$(git rev-parse --abbrev-ref HEAD)
```

### 3. Helm Deployment (Test Environment)

#### Basic Deployment

```bash
# Get your branch name
BRANCH=$(git rev-parse --abbrev-ref HEAD)

# Deploy Sentinel with your test image
helm install sentinel-test ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace \
  --set image.repository=gcr.io/hcm-hyperfleet/sentinel \
  --set image.tag=test-${BRANCH} \
  --set monitoring.podMonitoring.enabled=true
```

#### Deployment with RabbitMQ

```bash
helm install sentinel-test ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace \
  --set image.repository=gcr.io/hcm-hyperfleet/sentinel \
  --set image.tag=test-${BRANCH} \
  --set broker.type=rabbitmq \
  --set broker.rabbitmq.url="amqp://user:pass@rabbitmq.hyperfleet-system.svc.cluster.local:5672/" \
  --set monitoring.podMonitoring.enabled=true
```

#### Deployment with Google Pub/Sub

```bash
helm install sentinel-test ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace \
  --set image.repository=gcr.io/hcm-hyperfleet/sentinel \
  --set image.tag=test-${BRANCH} \
  --set broker.type=googlepubsub \
  --set broker.googlepubsub.projectId=your-gcp-project-id \
  --set monitoring.podMonitoring.enabled=true
```

### 4. Verification Steps for GKE

#### Check Pod Status

```bash
kubectl get pods -n hyperfleet-system -l app.kubernetes.io/name=sentinel
```

#### View Pod Logs

```bash
kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel -f
```

#### Verify Health Endpoint

```bash
# Port-forward to access health endpoint
kubectl port-forward -n hyperfleet-system svc/sentinel-test 8080:8080 &

# Check health
curl http://localhost:8080/health

# Check metrics
curl http://localhost:8080/metrics | grep hyperfleet_sentinel
```

#### Check PodMonitoring Status

```bash
# List PodMonitoring resources
kubectl get podmonitoring -n hyperfleet-system

# Describe PodMonitoring
kubectl describe podmonitoring -n hyperfleet-system -l app.kubernetes.io/name=sentinel
```

#### Verify Metrics in Google Cloud Console

1. Navigate to: **Monitoring > Metrics Explorer**
2. Select resource type: **Prometheus Target**
3. Query: `hyperfleet_sentinel_pending_resources`

You should see metrics from your test deployment.

### 5. Cleanup

Remove the test deployment when done:

```bash
helm uninstall sentinel-test -n hyperfleet-system
```

Optionally, delete the test image from the registry:

```bash
gcloud container images delete gcr.io/hcm-hyperfleet/sentinel:test-$(git rev-parse --abbrev-ref HEAD) --quiet
```

---

## Troubleshooting

### Exec format error

**Problem**: Container fails to start with `exec format error`

**Cause**: Architecture mismatch - image was built for a different CPU architecture than the target

**Solution**: Ensure `--platform linux/amd64` is used when building:

```bash
podman build --platform linux/amd64 -t your-image:tag .
```

### Broker connection refused

**Problem**: Sentinel logs show "connection refused" errors for broker

**Cause**: Broker URL is incorrect or broker service is not accessible

**Solution**:
1. Verify broker URL is correct
2. Ensure broker service is running and accessible from the pod
3. For Kubernetes, verify the service name and namespace are correct:
   ```bash
   kubectl get svc -n hyperfleet-system
   ```

### Metrics not appearing in GMP

**Problem**: Metrics are not visible in Google Cloud Metrics Explorer

**Cause**: PodMonitoring not configured correctly or GMP collector not scraping

**Solution**:
1. Verify PodMonitoring is created:
   ```bash
   kubectl get podmonitoring -n hyperfleet-system
   ```
2. Check GMP collector logs:
   ```bash
   kubectl logs -n gmp-system -l app.kubernetes.io/name=collector
   ```
3. Ensure the metrics endpoint is accessible:
   ```bash
   kubectl port-forward -n hyperfleet-system pod/<pod-name> 8080:8080
   curl http://localhost:8080/metrics
   ```

### ConfigMap vs Environment Variable Configuration

**Problem**: Broker credentials not being picked up

**Cause**: Broker credentials must be set via environment variables, not ConfigMap

**Solution**: Use `--set` flags or a values file to set broker credentials:
```bash
--set broker.rabbitmq.url="amqp://user:pass@host:5672/"
```

### HyperFleet API Connection Errors

**Problem**: Sentinel cannot connect to HyperFleet API

**Solution**:
1. Verify the API endpoint is correct in your config
2. For local testing, ensure the API is running
3. For GKE, use the in-cluster service name:
   ```yaml
   hyperfleet_api:
     endpoint: http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8080
   ```

### OpenAPI Client Not Generated

**Problem**: Build fails with missing package errors

**Cause**: OpenAPI client was not generated

**Solution**: Run the generate target before building:
```bash
make generate
make build
```

### Integration Tests Failing with Container Errors

**Problem**: Integration tests fail to start containers

**Solution**:
1. Ensure Podman is running
2. See [docs/testcontainers.md](testcontainers.md) for Podman-specific configuration
3. Check available disk space and memory
