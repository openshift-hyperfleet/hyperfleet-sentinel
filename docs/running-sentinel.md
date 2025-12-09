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
  - [Set Up Environment Variables](#1-set-up-environment-variables)
  - [Connect to GKE Cluster](#2-connect-to-gke-cluster)
  - [Building Container Image](#3-building-container-image)
  - [Authentication and Image Push](#4-authentication-and-image-push)
  - [Helm Deployment](#5-helm-deployment)
  - [Verification Steps](#6-verification-steps)
  - [Cleanup](#7-cleanup)
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

Verify: Access RabbitMQ management console at `http://localhost:15672` (guest/guest)

#### Option B: Google Pub/Sub Emulator (gcloud CLI)

```bash
# Start the emulator (runs on port 8085 by default)
gcloud beta emulators pubsub start --project=test-project --host-port=localhost:8085
```

> **Note**: The emulator runs in the foreground. Open a new terminal for subsequent commands.

#### Option C: Google Pub/Sub Emulator via Podman

You can also run the emulator in Podman, as documented in the [broker library](https://github.com/openshift-hyperfleet/hyperfleet-broker?tab=readme-ov-file#running-rabbitmq-and-pubsub-emulator-in-containers):

```bash
export PUBSUB_PROJECT_ID=test-project
export PUBSUB_EMULATOR_HOST=localhost:8085

podman run --rm --name pubsub-emulator -d -p 8085:8085 google/cloud-sdk:emulators \
  /bin/bash -c "gcloud beta emulators pubsub start --project=test-project --host-port='0.0.0.0:8085'"
```

### 2. Configuring Sentinel

Broker configuration happens in two places:

- **`broker.yaml`** - For non-sensitive settings (broker type, project ID, etc.)
- **Environment variables** - For sensitive settings (credentials, URLs with passwords)
  - For the Pub/Sub emulator, environment variables are also required for the Google SDK to work properly

> **Note**: If using real Google Pub/Sub (not the emulator), you need GCP credentials in place via `gcloud auth application-default login` or by setting `GOOGLE_APPLICATION_CREDENTIALS` to a service account key file.

#### Step 1: Generate the OpenAPI Client

Before running Sentinel, you must generate the OpenAPI client:

```bash
make generate
```

This downloads the OpenAPI spec from [hyperfleet-api](https://github.com/openshift-hyperfleet/hyperfleet-api) and generates the Go client code.

#### Step 2: Configure Broker

The default `broker.yaml` is configured for RabbitMQ. Choose your broker below:

**For RabbitMQ** (default, no changes needed to `broker.yaml`):

```bash
export BROKER_RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
```

**For Google Pub/Sub Emulator** (requires `broker.yaml` modification):

1. Edit `broker.yaml` to use `googlepubsub`:
   ```yaml
   broker:
     type: googlepubsub
     googlepubsub:
       project_id: test-project
   ```

2. Set the emulator host (required for the Google SDK):
   ```bash
   export PUBSUB_EMULATOR_HOST=localhost:8085
   ```

#### Step 3: Set Topic Name

Set the topic name for event publishing:

```bash
# For clusters
export BROKER_TOPIC=hyperfleet-dev-${USER}-clusters

# For nodepools
export BROKER_TOPIC=hyperfleet-dev-${USER}-nodepools
```

This sets the full topic name where events will be published (e.g., `hyperfleet-dev-rafael-clusters`). See [Naming Strategy](https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/components/sentinel/sentinel-naming-strategy.md) for details.

### 3. Running Sentinel

#### Option A: Build and Run Binary

```bash
# Build the binary
make build

# Run Sentinel (uses broker.yaml from current directory)
./sentinel serve --config=configs/dev-example.yaml
```

#### Option B: Run Directly with Go

```bash
# Run with explicit broker config path
BROKER_CONFIG_FILE=broker.yaml go run ./cmd/sentinel serve --config=configs/dev-example.yaml
```

### 4. Verification Steps

#### Check Health Endpoint

```bash
curl http://localhost:8080/health
```

Expected response:

```text
OK
```

#### Check Metrics Endpoint

```bash
curl http://localhost:8080/metrics | grep hyperfleet_sentinel
```

**Without HyperFleet API running**, you will see error metrics:

```text
hyperfleet_sentinel_api_errors_total{error_type="fetch_error",resource_selector="all",resource_type="clusters"} 1
```

> **Note**: This is expected behavior when running locally without a HyperFleet API instance. The `api_errors_total` metric indicates the Sentinel is running correctly but cannot reach the API.

**With HyperFleet API running**, you will see additional metrics:

```text
hyperfleet_sentinel_pending_resources{...} 0
hyperfleet_sentinel_events_published_total{...} 0
hyperfleet_sentinel_poll_duration_seconds_bucket{...}
```

#### Monitor Logs

Watch console output for startup and broker connection messages.

**Startup messages** (always visible):

```log
I1208 15:30:00.123456   12345 config.go:82] Loading configuration from configs/dev-example.yaml
I1208 15:30:00.123789   12345 config.go:111] Configuration loaded successfully: resource_type=clusters
I1208 15:30:00.123800   12345 logger.go:96] Starting HyperFleet Sentinel version=dev commit=abc1234
```

**For RabbitMQ**, you should also see the broker connection log:

```log
[watermill] 2025/12/01 15:28:26.051755 connection.go:99: level=INFO msg="Connected to AMQP"
```

**For Google Pub/Sub**, there is no explicit connection log. The Google Pub/Sub SDK does not expose connection events, so the publisher initializes silently. You can verify it's working by checking the health endpoint (`curl http://localhost:8080/health`) and metrics. For debugging, you can enable SDK debug logging with these environment variables:

```bash
export GOOGLE_SDK_GO_LOGGING_LEVEL=debug
export GRPC_GO_LOG_VERBOSITY_LEVEL=99
export GODEBUG=http2debug=1
```

> **Note**: If the HyperFleet API is not running, Sentinel will still start but API polling will fail silently (visible in metrics as `api_errors_total`). This is expected for local broker validation.

---

## Running on GKE

### Prerequisites for GKE

- GKE cluster access (use the shared cluster below or your own)
- `gcloud` CLI configured and authenticated
- `kubectl` configured for the cluster
- `podman` for building images
- `helm` for deploying the chart
- Access to Google Container Registry (GCR) for your project

### 1. Set Up Environment Variables

Set these variables once and use them throughout the deployment:

```bash
# GCP project ID
export GCP_PROJECT=hcm-hyperfleet

# Your namespace: hyperfleet-{env}-{username}
export NAMESPACE=hyperfleet-dev-${USER}

# Image tag: {namespace}-{git-sha-short} (follows naming convention)
export IMAGE_TAG=${NAMESPACE}-$(git rev-parse --short HEAD)
# Example: hyperfleet-dev-rafael-a1b2c3d (if USER=rafael)
```

> **Note**: The image tag format `{namespace}-{git-sha-short}` follows the [Naming Strategy](https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/components/sentinel/sentinel-naming-strategy.md) convention to prevent collisions between developers.

### 2. Connect to GKE Cluster

A shared GKE cluster with Config Connector enabled is available for development and testing:

```bash
gcloud container clusters get-credentials hyperfleet-dev --zone=us-central1-a --project=${GCP_PROJECT}
```

**Usage guidelines:**
- For personal work, create a namespace named after yourself to isolate resources
- For team collaboration, use a designated namespace to separate resources among members

> **Note**: This environment is scheduled for deletion every Friday at 8:00 PM (EST). See [GKE deployment docs](https://github.com/openshift-hyperfleet/architecture/tree/main/hyperfleet/deployment/GKE) for more details.

### 3. Building Container Image

#### Option A: Using Makefile Targets (Recommended for Dev)

For pushing to your personal Quay registry:

```bash
# Build and push to quay.io/${QUAY_USER}/sentinel:dev-<commit>
QUAY_USER=${USER} make image-dev
```

This will output the image tag to use in your terraform.tfvars.

#### Option B: Manual Build for GCR

For pushing to Google Container Registry:

```bash
# Build for AMD64 (required for GKE)
podman build --platform linux/amd64 -t gcr.io/${GCP_PROJECT}/sentinel:${IMAGE_TAG} .
```

> **Note**: If building on ARM64 Mac for AMD64 GKE, you must use `--platform linux/amd64` to avoid architecture mismatch errors.

### 4. Authentication and Image Push

> **Note**: If you used `make image-dev` (Option A above), authentication and push are handled automatically. Skip to [Helm Deployment](#5-helm-deployment). For Quay.io, ensure you've run `podman login quay.io` first.

#### Configure Authentication with GCR

```bash
gcloud auth configure-docker gcr.io
```

#### Push Image to Registry

```bash
podman push gcr.io/${GCP_PROJECT}/sentinel:${IMAGE_TAG}
```

### 5. Helm Deployment

```bash
# Deploy Sentinel with Google Pub/Sub (default topic: {namespace}-{resourceType})
helm install sentinel-test ./deployments/helm/sentinel \
  --namespace ${NAMESPACE} \
  --create-namespace \
  --set image.repository=gcr.io/${GCP_PROJECT}/sentinel \
  --set image.tag=${IMAGE_TAG} \
  --set broker.type=googlepubsub \
  --set broker.googlepubsub.projectId=${GCP_PROJECT} \
  --set monitoring.podMonitoring.enabled=true
```

> **Tip**: The default topic is `{namespace}-{resourceType}` (e.g., `hyperfleet-dev-rafael-clusters`). You can override with `--set broker.topic=custom-topic`. See [Naming Strategy](https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/components/sentinel/sentinel-naming-strategy.md) for details.

### 6. Verification Steps

#### Check Pod Status

```bash
kubectl get pods -n ${NAMESPACE} -l app.kubernetes.io/name=sentinel
```

#### View Pod Logs

```bash
kubectl logs -n ${NAMESPACE} -l app.kubernetes.io/name=sentinel -f
```

You should see the startup messages:

```log
I1208 15:30:00.123456   1 config.go:82] Loading configuration from /app/configs/sentinel.yaml
I1208 15:30:00.123789   1 config.go:111] Configuration loaded successfully: resource_type=clusters
I1208 15:30:00.123800   1 logger.go:96] Starting HyperFleet Sentinel version=0.1.0 commit=abc1234
```

> **Note**: Sentinel outputs minimal logs during normal operation. Use the health endpoint and metrics to verify the service is running correctly.

#### Verify Health Endpoint

Start port-forward in a separate terminal:

```bash
kubectl port-forward -n ${NAMESPACE} svc/sentinel-test 8080:8080
```

Check health:

```bash
curl http://localhost:8080/health
```

Check metrics:

```bash
curl http://localhost:8080/metrics | grep hyperfleet_sentinel
```

#### Check PodMonitoring Status

List PodMonitoring resources:

```bash
kubectl get podmonitoring -n ${NAMESPACE}
```

Describe PodMonitoring:

```bash
kubectl describe podmonitoring -n ${NAMESPACE} -l app.kubernetes.io/name=sentinel
```

#### Verify Metrics in Google Cloud Console

1. Open the [Metrics Explorer](https://console.cloud.google.com/monitoring/metrics-explorer) for your project
2. In "Select a metric", search for `hyperfleet_sentinel`
3. Select **Prometheus Target** > **Hyperfleet** > choose a metric (e.g., `api_errors_total`)

### 7. Cleanup

Remove the deployment when done:

```bash
helm uninstall sentinel-test -n ${NAMESPACE}
```

Optionally, delete the image from the registry:

```bash
gcloud container images delete gcr.io/${GCP_PROJECT}/sentinel:${IMAGE_TAG} --quiet --force-delete-tags
```

---

## Troubleshooting

### Exec format error

**Problem**: Container fails to start with `exec format error`

**Cause**: Architecture mismatch - image was built for a different CPU architecture than the target

**Solution**: Ensure `--platform linux/amd64` is used when building:

```bash
podman build --platform linux/amd64 -t gcr.io/${GCP_PROJECT}/sentinel:${IMAGE_TAG} .
```

### Broker connection refused

**Problem**: Sentinel fails to start with "connection refused" errors for broker

**Cause**: Broker is not running or `broker.yaml` is configured for the wrong broker type

**Solution**:
1. Verify the broker is running (RabbitMQ or Pub/Sub emulator)
2. Ensure `broker.yaml` has the correct `type` (rabbitmq or googlepubsub)
3. For Pub/Sub emulator, ensure `PUBSUB_EMULATOR_HOST` is set
4. For RabbitMQ, ensure `BROKER_RABBITMQ_URL` is set or the URL in `broker.yaml` is correct

### Metrics not appearing in GMP

**Problem**: Metrics are not visible in Google Cloud Metrics Explorer

**Cause**: PodMonitoring not configured correctly or GMP collector not scraping

**Solution**:
1. Verify PodMonitoring is created:
   ```bash
   kubectl get podmonitoring -n ${NAMESPACE}
   ```
2. Check GMP collector logs:
   ```bash
   kubectl logs -n gmp-system -l app.kubernetes.io/name=collector
   ```
3. Ensure the metrics endpoint is accessible:
   ```bash
   kubectl port-forward -n ${NAMESPACE} svc/sentinel-test 8080:8080
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
