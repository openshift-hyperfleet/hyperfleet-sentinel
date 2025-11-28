# HyperFleet Sentinel Metrics

This document describes the Prometheus metrics exposed by the HyperFleet Sentinel service for monitoring and observability.

## Metrics Overview

Sentinel exposes metrics on port 8080 at the `/metrics` endpoint. All metrics follow the `hyperfleet_sentinel_*` naming convention and include common labels for filtering and aggregation.

## Common Labels

All metrics include the following labels:

| Label | Description | Example Values |
|-------|-------------|----------------|
| `resource_type` | Type of resource being monitored | `clusters`, `nodepools` |
| `resource_selector` | Label selector for resource filtering | `shard:1`, `env:prod`, `all` |

Additional labels may be present depending on the specific metric (see individual metric descriptions below).

## Metrics Catalog

### 1. `hyperfleet_sentinel_pending_resources`

**Type:** Gauge

**Description:** Current number of resources pending reconciliation based on max age intervals or generation mismatches. This gauge provides a snapshot of resources that need processing.

**Labels:**
- `resource_type`: Type of resource (e.g., `clusters`, `nodepools`)
- `resource_selector`: Label selector applied to the resources

**Use Cases:**
- Monitor backlog of pending resources
- Alert on high resource queue sizes
- Track resource processing efficiency

**Example Query:**
```promql
# Total pending resources across all types
sum(hyperfleet_sentinel_pending_resources)

# Pending resources by type
sum by (resource_type) (hyperfleet_sentinel_pending_resources)
```

---

### 2. `hyperfleet_sentinel_events_published_total`

**Type:** Counter

**Description:** Total number of reconciliation events successfully published to the message broker. Tracks event publication activity and throughput.

**Labels:**
- `resource_type`: Type of resource
- `resource_selector`: Label selector
- `reason`: Reason for publishing the event (e.g., `max_age_exceeded`, `generation_mismatch`)

**Use Cases:**
- Monitor event publishing rate
- Track reconciliation triggers by reason
- Measure overall system activity

**Example Query:**
```promql
# Events published per second
rate(hyperfleet_sentinel_events_published_total[5m])

# Events by reason
sum by (reason) (rate(hyperfleet_sentinel_events_published_total[5m]))
```

---

### 3. `hyperfleet_sentinel_resources_skipped_total`

**Type:** Counter

**Description:** Total number of resources skipped during evaluation because they didn't meet the criteria for event publication (e.g., within max age, generation already reconciled).

**Labels:**
- `resource_type`: Type of resource
- `resource_selector`: Label selector
- `reason`: Reason for skipping (e.g., `within_max_age`, `generation_match`)

**Use Cases:**
- Monitor decision engine effectiveness
- Track resource skip rate vs publish rate
- Debug reconciliation loop behavior

**Example Query:**
```promql
# Resources skipped per second
rate(hyperfleet_sentinel_resources_skipped_total[5m])

# Skip ratio (skipped / total processed)
rate(hyperfleet_sentinel_resources_skipped_total[5m]) /
  (rate(hyperfleet_sentinel_resources_skipped_total[5m]) +
   rate(hyperfleet_sentinel_events_published_total[5m]))
```

---

### 4. `hyperfleet_sentinel_poll_duration_seconds`

**Type:** Histogram

**Description:** Duration of each polling cycle in seconds, including API calls, decision evaluation, and event publishing. Provides latency distribution for performance monitoring.

**Labels:**
- `resource_type`: Type of resource
- `resource_selector`: Label selector

**Buckets:** Default Prometheus buckets (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10)

**Use Cases:**
- Monitor polling loop performance
- Detect API latency issues
- Set SLO targets for reconciliation speed

**Example Query:**
```promql
# 95th percentile poll duration
histogram_quantile(0.95,
  rate(hyperfleet_sentinel_poll_duration_seconds_bucket[5m]))

# Average poll duration
rate(hyperfleet_sentinel_poll_duration_seconds_sum[5m]) /
  rate(hyperfleet_sentinel_poll_duration_seconds_count[5m])
```

---

### 5. `hyperfleet_sentinel_api_errors_total`

**Type:** Counter

**Description:** Total number of errors when calling the HyperFleet API. Tracks API connectivity and availability issues.

**Labels:**
- `resource_type`: Type of resource
- `resource_selector`: Label selector
- `error_type`: Type of error (e.g., `fetch_error`, `timeout`, `auth_error`)

**Use Cases:**
- Alert on API availability issues
- Track error rates by type
- Monitor API health

**Example Query:**
```promql
# API error rate
rate(hyperfleet_sentinel_api_errors_total[5m])

# Errors by type
sum by (error_type) (rate(hyperfleet_sentinel_api_errors_total[5m]))
```

---

### 6. `hyperfleet_sentinel_broker_errors_total`

**Type:** Counter

**Description:** Total number of errors when publishing events to the message broker. Tracks broker connectivity and delivery issues.

**Labels:**
- `resource_type`: Type of resource
- `resource_selector`: Label selector
- `error_type`: Type of error (e.g., `publish_error`, `connection_error`, `timeout`)

**Use Cases:**
- Alert on message delivery failures
- Monitor broker health
- Track error rates by broker type

**Example Query:**
```promql
# Broker error rate
rate(hyperfleet_sentinel_broker_errors_total[5m])

# Errors by type
sum by (error_type) (rate(hyperfleet_sentinel_broker_errors_total[5m]))
```

---

## Recommended Alerting Rules

### Alerting with Google Cloud Managed Prometheus

**Note**: Google Cloud Managed Prometheus uses Google Cloud Alerting for alert management, not PrometheusRule CRDs. The alerting rules below are provided as PromQL expressions that you can configure in Google Cloud Console → Monitoring → Alerting.

For Prometheus Operator compatibility, the Helm chart can optionally deploy a PrometheusRule resource when `monitoring.prometheusRule.enabled=true` (disabled by default for GMP). The following alert examples can be configured in either system:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: sentinel-alerts
  namespace: hyperfleet-system
spec:
  groups:
  - name: sentinel.rules
    interval: 30s
    rules:
    - alert: SentinelHighPendingResources
      expr: |
        sum(hyperfleet_sentinel_pending_resources) > 100
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "High number of pending resources"
        description: "{{ $value }} resources are pending reconciliation for more than 10 minutes."

    - alert: SentinelAPIErrorRateHigh
      expr: |
        rate(hyperfleet_sentinel_api_errors_total[5m]) > 0.1
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "High API error rate detected"
        description: "API error rate is {{ $value | humanize }} errors/sec for resource_type {{ $labels.resource_type }}."

    - alert: SentinelBrokerErrorRateHigh
      expr: |
        rate(hyperfleet_sentinel_broker_errors_total[5m]) > 0.05
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "High broker error rate detected"
        description: "Broker error rate is {{ $value | humanize }} errors/sec for resource_type {{ $labels.resource_type }}."

    - alert: SentinelSlowPolling
      expr: |
        histogram_quantile(0.95,
          rate(hyperfleet_sentinel_poll_duration_seconds_bucket[5m])) > 5
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "Polling cycles are slow"
        description: "95th percentile poll duration is {{ $value | humanize }}s for {{ $labels.resource_type }}."

    - alert: SentinelNoEventsPublished
      expr: |
        rate(hyperfleet_sentinel_events_published_total[15m]) == 0
        AND hyperfleet_sentinel_pending_resources > 0
      for: 15m
      labels:
        severity: warning
      annotations:
        summary: "No events published despite pending resources"
        description: "Sentinel has pending resources but hasn't published events in 15 minutes."
```

To configure these alerts in **Google Cloud Console**:
1. Navigate to **Monitoring → Alerting**
2. Click **Create Policy**
3. Use the PromQL expressions above in the condition configuration
4. Configure notification channels and documentation

For Prometheus Operator users, enable PrometheusRule in values.yaml and verify:

```bash
kubectl get prometheusrule -n hyperfleet-system
```

### Individual Alert Rules (YAML Format)

For reference, here are the individual alert rules in YAML format:

#### High Pending Resources

Alert when the number of pending resources exceeds a threshold for an extended period.

```yaml
- alert: SentinelHighPendingResources
  expr: |
    sum(hyperfleet_sentinel_pending_resources) > 100
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "High number of pending resources"
    description: "{{ $value }} resources are pending reconciliation for more than 10 minutes."
```

### API Error Rate High

Alert when the API error rate exceeds acceptable limits.

```yaml
- alert: SentinelAPIErrorRateHigh
  expr: |
    rate(hyperfleet_sentinel_api_errors_total[5m]) > 0.1
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "High API error rate detected"
    description: "API error rate is {{ $value | humanize }} errors/sec for resource_type {{ $labels.resource_type }}."
```

### Broker Error Rate High

Alert when broker errors indicate message delivery issues.

```yaml
- alert: SentinelBrokerErrorRateHigh
  expr: |
    rate(hyperfleet_sentinel_broker_errors_total[5m]) > 0.05
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "High broker error rate detected"
    description: "Broker error rate is {{ $value | humanize }} errors/sec for resource_type {{ $labels.resource_type }}."
```

### Slow Poll Duration

Alert when polling cycles are taking too long, indicating performance issues.

```yaml
- alert: SentinelSlowPolling
  expr: |
    histogram_quantile(0.95,
      rate(hyperfleet_sentinel_poll_duration_seconds_bucket[5m])) > 5
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Polling cycles are slow"
    description: "95th percentile poll duration is {{ $value | humanize }}s for {{ $labels.resource_type }}."
```

### No Events Published

Alert when no events have been published recently, which may indicate a stuck sentinel.

```yaml
- alert: SentinelNoEventsPublished
  expr: |
    rate(hyperfleet_sentinel_events_published_total[15m]) == 0
    AND hyperfleet_sentinel_pending_resources > 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: "No events published despite pending resources"
    description: "Sentinel has {{ $value }} pending resources but hasn't published events in 15 minutes."
```

### High Skip Ratio

Alert when too many resources are being skipped, which may indicate configuration issues.

```yaml
- alert: SentinelHighSkipRatio
  expr: |
    rate(hyperfleet_sentinel_resources_skipped_total[10m]) /
    (rate(hyperfleet_sentinel_resources_skipped_total[10m]) +
     rate(hyperfleet_sentinel_events_published_total[10m])) > 0.95
  for: 30m
  labels:
    severity: info
  annotations:
    summary: "High resource skip ratio"
    description: "{{ $value | humanizePercentage }} of resources are being skipped."
```

---

## Grafana Dashboard

A pre-built Grafana dashboard is available at `deployments/dashboards/sentinel-metrics.json`. The dashboard includes:

1. **Pending Resources** - Current backlog gauge and time series
2. **Events Published Rate** - Event publication throughput by reason
3. **Resources Skipped Rate** - Resource skip rate by reason
4. **Poll Duration Percentiles** - p50, p95, p99 latency tracking
5. **Poll Rate by Resource Type** - Processing rate table
6. **API Errors Rate** - API error tracking by error type
7. **Broker Errors Rate** - Broker error tracking by error type

### Import Dashboard

To import the dashboard into Grafana:

1. Navigate to Grafana → Dashboards → Import
2. Upload `deployments/dashboards/sentinel-metrics.json`
3. Select your Prometheus datasource
4. Click "Import"

---

## Prometheus Integration

### Google Cloud Managed Prometheus (GMP) (Recommended)

Google Cloud Managed Prometheus is a fully managed Prometheus-compatible monitoring service for GKE. It automatically collects metrics using PodMonitoring resources.

#### 1. Enable Managed Prometheus

GMP is available on GKE clusters. Ensure managed collection is enabled:

```bash
# For GKE Autopilot, managed collection is enabled by default
# For GKE Standard, enable it with:
gcloud container clusters update CLUSTER_NAME \
  --enable-managed-prometheus \
  --zone=ZONE
```

#### 2. Deploy Sentinel with PodMonitoring

The Helm chart automatically creates a PodMonitoring resource when deployed:

```bash
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace \
  --set monitoring.podMonitoring.enabled=true
```

The PodMonitoring resource scrapes metrics from Sentinel pods automatically.

#### 3. Verify and Access Metrics

```bash
# Check PodMonitoring was created
kubectl get podmonitoring -n hyperfleet-system

# Verify the PodMonitoring details
kubectl describe podmonitoring sentinel -n hyperfleet-system
```

Access metrics in **Google Cloud Console → Metrics Explorer**:
1. Navigate to **Monitoring → Metrics Explorer**
2. In the resource type, select **Prometheus Target**
3. Query your metrics:
   ```promql
   hyperfleet_sentinel_pending_resources
   ```


---

## Querying Metrics

### Google Cloud Console

Query metrics through the Google Cloud Console:

1. **Navigate to the Google Cloud Console**
2. **Go to: Monitoring → Metrics Explorer**
3. **Query your metrics**, for example:
   ```promql
   hyperfleet_sentinel_pending_resources
   rate(hyperfleet_sentinel_events_published_total[5m])
   ```

This uses the integrated Metrics Explorer and provides a native GKE experience.

### Verify Metrics Collection

Check that metrics are being collected:

```bash
# Verify PodMonitoring exists
kubectl get podmonitoring sentinel -n hyperfleet-system

# Check Service endpoints
kubectl get endpoints sentinel -n hyperfleet-system

# View recent metrics in Google Cloud Console
# Navigate to: Monitoring → Metrics Explorer
# Query: hyperfleet_sentinel_pending_resources
```

### Metrics Architecture

```text
[Sentinel Pod] → [Service (ClusterIP):8080] → [PodMonitoring] → [Prometheus] → [Metrics Explorer] → [Google Cloud Console]
```

Metrics are collected internally by Google Cloud Managed Prometheus (GMP) via the PodMonitoring.

---

## Best Practices

1. **Set up alerts** for critical metrics (API errors, broker errors, high pending resources)
2. **Monitor trends** over time to understand baseline behavior
3. **Use labels** to filter metrics by shard or resource type for granular visibility
4. **Correlate metrics** with application logs for debugging
5. **Review dashboards** regularly during incidents and normal operations
6. **Adjust thresholds** based on your specific SLO/SLA requirements

---

## Troubleshooting

### Metrics not appearing in Google Cloud Console

**GKE with Google Cloud Managed Prometheus:**

1. **Verify Managed Prometheus is enabled:**
   ```bash
   gcloud container clusters describe CLUSTER_NAME \
     --zone=ZONE \
     --format="value(monitoringConfig.managedPrometheusConfig.enabled)"
   # Should return: true
   ```

2. **Check if PodMonitoring is created:**
   ```bash
   kubectl get podmonitoring -n hyperfleet-system
   kubectl describe podmonitoring sentinel -n hyperfleet-system
   ```

3. **Verify Service and endpoints:**
   ```bash
   kubectl get svc sentinel -n hyperfleet-system
   kubectl get endpoints sentinel -n hyperfleet-system
   # Should show pod IP:8080
   ```

4. **Review GMP collector logs:**
   ```bash
   kubectl logs -n gmp-system -l app.kubernetes.io/name=collector
   # Look for scrape errors or connection issues
   ```

5. **Test metrics endpoint directly:**
   ```bash
   kubectl port-forward -n hyperfleet-system svc/sentinel 8080:8080
   # In another terminal:
   curl http://localhost:8080/metrics | grep hyperfleet_sentinel
   ```

6. **Check if metrics appear in Google Cloud Console:**
   - Navigate to: **Monitoring → Metrics Explorer**
   - Select resource type: **Prometheus Target**
   - Query: `hyperfleet_sentinel_pending_resources`


### Dashboard shows no data

1. Verify the datasource is configured correctly
2. Check the time range in Grafana
3. Ensure metrics are being scraped by Prometheus
4. Verify namespace and resource_type variables are set correctly

### High cardinality warnings

If you see warnings about high cardinality:
- Review the number of unique label combinations
- Consider reducing the number of shards or resource selectors
- Contact the HyperFleet team for guidance

---

## Additional Resources

- [Prometheus Query Language (PromQL)](https://prometheus.io/docs/prometheus/latest/querying/basics/)
- [Grafana Dashboards](https://grafana.com/docs/grafana/latest/dashboards/)
- [Google Cloud Managed Prometheus](https://cloud.google.com/stackdriver/docs/managed-prometheus)
- [PodMonitoring API Reference](https://github.com/GoogleCloudPlatform/prometheus-engine/blob/main/doc/api.md#podmonitoring)
- [HyperFleet Architecture Documentation](https://github.com/openshift-hyperfleet/architecture)
