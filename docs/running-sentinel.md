# Running Sentinel

> **IMPORTANT**: This documentation covers running Sentinel for **development and testing purposes**. Production deployments are handled via CI/CD pipelines.

This guide enables developers to run Sentinel both locally (for development) and on GKE (for integration) before merging code changes.

## Table of Contents

- [Running Locally](#running-locally)
  - [Prerequisites](#prerequisites-for-running-locally)
  - [Setting Up a Message Broker](#1-setting-up-a-message-broker)
  - [Configuring Sentinel](#2-configuring-sentinel)
  - [Running Sentinel](#3-running-sentinel)
  - [Verification Steps](#4-verification-steps)
- [Running on GKE](#running-on-gke)
  - [Prerequisites](#prerequisites-for-gke)
  - [Building Container Image](#1-building-container-image)
  - [Authentication and Image Push](#2-authentication-and-image-push)
  - [Helm Deployment](#3-helm-deployment)
  - [Verification Steps](#4-verification-steps-for-gke)
  - [Cleanup](#5-cleanup)
- [Troubleshooting](#troubleshooting)

---

## Running Locally

### Prerequisites for Running Locally

- Go 1.25+ installed
- Podman (for running broker locally and integration tests)
- Make utility
- Access to a message broker (RabbitMQ recommended for local development)
- HyperFleet API accessible (local or remote instance)

### 1. Setting Up a Message Broker

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

### 2. Configuring Sentinel

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

```bash
export PUBSUB_EMULATOR_HOST=localhost:8085
export BROKER_GOOGLEPUBSUB_PROJECT_ID=test-project
```

### 3. Running Sentinel

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

### 4. Verification Steps

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

> **Note**: This is expected behavior when running locally without a HyperFleet API instance. The `api_errors_total` metric indicates the Sentinel is running correctly but cannot reach the API.

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

> **Note**: If the HyperFleet API is not running, Sentinel will still start but API polling will fail silently (visible in metrics as `api_errors_total`). This is expected for local broker validation.

---

## Running on GKE

### Prerequisites for GKE

- GKE cluster with Google Cloud Managed Prometheus (GMP) enabled
- `gcloud` CLI configured and authenticated
- `kubectl` configured for the cluster
- `podman` for building images
- `helm` for deploying the chart
- Access to `gcr.io/hcm-hyperfleet` container registry (or your own registry)

### 1. Building Container Image

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

#### Push Image to Registry

```bash
podman push gcr.io/hcm-hyperfleet/sentinel:test-$(git rev-parse --abbrev-ref HEAD)
```

### 3. Helm Deployment

#### Basic Deployment

```bash
# Get your branch name
BRANCH=$(git rev-parse --abbrev-ref HEAD)

# Deploy Sentinel with your image
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

You should see metrics from your deployment.

### 5. Cleanup

Remove the deployment when done:

```bash
helm uninstall sentinel-test -n hyperfleet-system
```

Optionally, delete the image from the registry:

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
2. For local execution, ensure the API is running
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
