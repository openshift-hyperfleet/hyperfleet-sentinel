# HyperFleet Sentinel Runbook

**Status**: Active
**Owner**: HyperFleet Team
**Last Updated**: 2026-03-12

**Audience:** **Platform Operations teams** and **SREs** responsible for HyperFleet Sentinel deployments

---

## Purpose

This runbook provides operational guidance for teams deploying and managing HyperFleet Sentinel in production environments. It serves as the primary reference for:

---

## Reliability Features

The Sentinel service is designed with multiple layers of reliability to ensure continuous reconciliation of HyperFleet resources.

### Stateless Design

**What**: Sentinel maintains no persistent state between polling cycles.

**Implementation**:
- All reconciliation decisions are made based on current resource state from the HyperFleet API
- No local databases or persistent storage requirements
- Configuration loaded once at startup from YAML files and environment variables
- Each polling cycle starts fresh from API data

**Benefits**:
- Simple horizontal scaling (no state coordination needed)
- Fast recovery after restarts (no state reconstruction)
- Eliminates state corruption issues
- Simplified deployment (no persistent volumes)

**Operational Impact**: Sentinel instances can be stopped/started without data loss. Resource reconciliation continues from the last adapter-reported status.

### Graceful Shutdown

**What**: Sentinel responds to SIGTERM/SIGINT signals with controlled shutdown.

**Implementation**:
- Listens for termination signals during main polling loop
- Completes current polling cycle before exiting
- Maximum shutdown time: 20 seconds for HTTP server shutdown
- Publishes any pending events before shutdown
- Cleans up broker connections gracefully

**Configuration**:
```yaml
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 30
```

**Operational Impact**: Graceful shutdown minimizes event loss by attempting to publish pending events before exit, subject to the grace period.

### API Retry Logic

**What**: Automatic retry with exponential backoff for HyperFleet API calls.

**Implementation**:
- **Timeout**: 5 seconds per API call (configurable via `hyperfleet_api.timeout`)
- **Initial interval**: 500ms (first retry after 500ms)
- **Max interval**: 8 seconds (maximum retry interval)
- **Multiplier**: 2.0 (doubles interval each retry: 500ms → 1s → 2s → 4s → 8s)
- **Randomization**: 10% jitter added to prevent thundering herd
- **Max elapsed time**: 30 seconds total (time-based retry, not attempt-based)
- **Failure handling**: Logs errors, continues with next resource after max elapsed time

**Configuration**:
```yaml
hyperfleet_api:
  endpoint: http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8000
  timeout: 5s
```

**Metrics**: Failed API calls tracked via `hyperfleet_sentinel_api_errors_total` metric.

**Operational Impact**: Transient API issues don't stop reconciliation. Service continues polling after API recovery.

### Broker Publish Retry

**What**: Automatic retry for message broker publishing failures.

**Implementation**:
- **External library**: Retry behavior handled by `hyperfleet-broker` library
- **Broker support**: GCP Pub/Sub and RabbitMQ with library-managed retry logic
- **Failure isolation**: Failed events logged but don't stop processing of other resources
- **Error handling**: Log error, record metric, continue to next resource

> **Note**: Specific retry parameters (attempts, timeouts, backoff strategy) are implemented in the external [hyperfleet-broker](https://github.com/openshift-hyperfleet/hyperfleet-broker) library and not configurable at the Sentinel level.

**Configuration Example (GCP Pub/Sub)**:
```yaml
# Via environment variables or ConfigMap
BROKER_TYPE: "pubsub"
BROKER_PROJECT_ID: "hyperfleet-prod"
```

**Metrics**: Publishing failures tracked via `hyperfleet_sentinel_broker_errors_total` metric.

**Operational Impact**: Temporary broker outages don't cause event loss. Events are retried by the broker library, but durability depends on broker availability and Sentinel remaining active.

### Per-Resource Error Isolation

**What**: Failures processing one resource don't affect processing of other resources.

**Implementation**:
- Each resource evaluated independently in the polling loop
- Decision engine errors logged but processing continues
- Event publishing failures logged but don't stop the polling cycle
- API errors for specific resources don't abort the entire fetch operation

**Example Flow**:
```
Polling Cycle:
├── Fetch 100 clusters from API
├── Process cluster-1 → Event published
├── Process cluster-2 → Log error, continue
├── Process cluster-3 → Event published
└── Complete cycle, sleep, repeat
```

**Operational Impact**: Problematic resources (e.g., malformed data) don't prevent reconciliation of healthy resources.

## Health Checks

**What**: Kubernetes readiness and liveness probes that verify actual service functionality.

**Implementation**:

**Liveness Probe** (`/healthz`):
- Verifies main polling goroutine is running
- Checks broker connection status
- Returns 200 OK if service can perform reconciliation
- **Failure threshold**: 3 consecutive failures
- **Period**: 20 seconds

**Readiness Probe** (`/readyz`):
- Verifies configuration loaded successfully
- Validates HyperFleet API connectivity
- Confirms broker configuration is valid
- Returns 200 OK when ready to process traffic
- **Period**: 10 seconds

**Configuration**:
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 15
  periodSeconds: 20
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

**Operational Impact**: Kubernetes automatically restarts unhealthy pods and removes unready pods from service.

## Common Failure Modes and Recovery Procedures

### 1. Sentinel Pod Crash Loop
**Symptoms**: Pod restart count increasing, CrashLoopBackOff status

**Diagnosis**: Check pod logs, resource constraints, configuration errors

**Recovery**:
1. Check logs: `kubectl logs -l app.kubernetes.io/name=sentinel --previous`
2. Verify resource limits: `kubectl describe pods -l app.kubernetes.io/name=sentinel`
3. Validate configuration: `kubectl get configmap -l app.kubernetes.io/name=sentinel -o yaml`

**Alternative commands for specific deployment:**
```bash
# If you know the Helm release name (e.g., "my-sentinel")
kubectl logs deployment/my-sentinel-sentinel --previous
kubectl get configmap my-sentinel-sentinel-config -o yaml
```

### 2. API Connectivity Loss
**Symptoms**: High API error rate, no events published

**Diagnosis**: API health, network connectivity, authentication

**Recovery**:
1. Test API connectivity: `kubectl exec -l app.kubernetes.io/name=sentinel -- curl hyperfleet-api:8000/health`
2. Check API pod status: `kubectl get pods -l app.kubernetes.io/name=hyperfleet-api`
3. Verify service endpoints: `kubectl get endpoints hyperfleet-api`
4. Check API service: `kubectl get service hyperfleet-api`

**Note**: API endpoint uses port 8000 as configured in values.yaml

### 3. Broker Publishing Failures
**Symptoms**: High broker error rate, events not reaching adapters

**Diagnosis**: Broker connectivity, credentials, topic configuration

**Recovery**:
1. Check broker credentials: `kubectl get secret -l app.kubernetes.io/name=sentinel -o yaml`
2. Test RabbitMQ connectivity: `kubectl exec -l app.kubernetes.io/name=sentinel -- nslookup rabbitmq.hyperfleet-system.svc.cluster.local`
3. Check broker health: `kubectl get pods -l app.kubernetes.io/name=rabbitmq`
4. Validate broker config: `kubectl exec -l app.kubernetes.io/name=sentinel -- cat /etc/sentinel/broker.yaml`

**For specific secret (if you know the Helm release name):**
```bash
kubectl get secret my-sentinel-sentinel-broker-credentials -o yaml
```

## Additional Diagnostic Commands

### Check Overall Sentinel Health
```bash
# View all Sentinel resources
kubectl get all -l app.kubernetes.io/name=sentinel

# Check health endpoints
kubectl exec -l app.kubernetes.io/name=sentinel -- wget -qO- http://localhost:8080/healthz
kubectl exec -l app.kubernetes.io/name=sentinel -- wget -qO- http://localhost:8080/readyz

# View metrics endpoint
kubectl exec -l app.kubernetes.io/name=sentinel -- curl -s http://localhost:9090/metrics
```

### Monitor Sentinel Activity
```bash
# Follow logs in real-time
kubectl logs -l app.kubernetes.io/name=sentinel -f

# Check recent polling cycles
kubectl logs -l app.kubernetes.io/name=sentinel --tail=50 | grep -E "(polling|published|error)"

# Monitor resource usage
kubectl top pods -l app.kubernetes.io/name=sentinel
```

### Validate Configuration
```bash
# Check mounted config
kubectl exec -l app.kubernetes.io/name=sentinel -- cat /etc/sentinel/config.yaml

# Check broker configuration
kubectl exec -l app.kubernetes.io/name=sentinel -- cat /etc/sentinel/broker.yaml

# Verify environment variables
kubectl exec -l app.kubernetes.io/name=sentinel -- env | grep -E "(OTEL|TRACING|BROKER)"
```

### Network Connectivity Tests
```bash
# Test HyperFleet API connectivity
kubectl exec -l app.kubernetes.io/name=sentinel -- wget -qO- http://hyperfleet-api:8000/health

# Test DNS resolution
kubectl exec -l app.kubernetes.io/name=sentinel -- nslookup hyperfleet-api
kubectl exec -l app.kubernetes.io/name=sentinel -- nslookup rabbitmq.hyperfleet-system.svc.cluster.local

# Check cluster networking
kubectl get endpoints hyperfleet-api
kubectl get service hyperfleet-api
```
