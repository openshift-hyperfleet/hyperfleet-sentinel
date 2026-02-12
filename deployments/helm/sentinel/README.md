# Sentinel Helm Chart

This Helm chart deploys the HyperFleet Sentinel service to Kubernetes.

## Prerequisites

- Kubernetes 1.20+
- Helm 3.0+
- HyperFleet API deployed and accessible
- Message broker (RabbitMQ or GCP Pub/Sub) configured

## Installing the Chart

```bash
# Install with default values (RabbitMQ)
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace

# Install with custom values
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --create-namespace \
  --values my-values.yaml
```

> **Note**: The `--create-namespace` flag creates the namespace if it doesn't exist. If the namespace already exists, Helm will use it and this flag has no effect. You can omit this flag if you've already created the namespace.

## Upgrading the Chart

```bash
helm upgrade sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values my-values.yaml
```

## Uninstalling the Chart

```bash
helm uninstall sentinel --namespace hyperfleet-system
```

## Configuration

The following table lists the configurable parameters of the Sentinel chart and their default values.

### General Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of Sentinel replicas | `1` |
| `image.registry` | Container image registry | `CHANGE_ME` |
| `image.repository` | Container image repository | `hyperfleet-sentinel` |
| `image.tag` | Container image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `Always` |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override fully qualified app name | `""` |

### ServiceAccount Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Resource Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Sentinel Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.resourceType` | Resource type to watch | `clusters` |
| `config.pollInterval` | Polling interval | `5s` |
| `config.maxAgeNotReady` | Max age for not ready resources | `10s` |
| `config.maxAgeReady` | Max age for ready resources | `30m` |
| `config.resourceSelector` | Resource selector for sharding | See values.yaml |
| `config.hyperfleetApi.endpoint` | HyperFleet API endpoint | `http://hyperfleet-api.hyperfleet-system.svc.cluster.local:8080` |
| `config.hyperfleetApi.timeout` | API timeout | `5s` |
| `config.messageData` | CloudEvents data payload fields | See values.yaml |

### Broker Configuration

> **Note**: Broker configuration uses the [hyperfleet-broker library](https://github.com/openshift-hyperfleet/hyperfleet-broker).

| Parameter | Description | Default |
|-----------|-------------|---------|
| `broker.type` | Broker type (`rabbitmq` or `googlepubsub`) | `rabbitmq` |
| `broker.topic` | Topic name for broker publishing (supports Helm templates) | `{{ .Release.Namespace }}-{{ .Values.config.resourceType }}` |
| `broker.rabbitmq.url` | RabbitMQ connection URL (format: `amqp://user:pass@host:port/vhost`) | `amqp://sentinel-user:change-me-in-production@rabbitmq.hyperfleet-system.svc.cluster.local:5672/hyperfleet` |
| `broker.rabbitmq.exchangeType` | RabbitMQ exchange type | `topic` |
| `broker.googlepubsub.projectId` | GCP project ID (for Pub/Sub) | `your-gcp-project-id` |
| `broker.googlepubsub.maxOutstandingMessages` | Max outstanding messages (for Pub/Sub) | `1000` |
| `broker.googlepubsub.numGoroutines` | Number of goroutines (for Pub/Sub) | `10` |
| `broker.googlepubsub.createTopicIfMissing` | Auto-create topic if it doesn't exist (for Pub/Sub) | `false` |
| `subscriber.parallelism` | Number of parallel workers for message processing | `1` |
| `existingSecret` | Use existing secret for broker credentials | `""` |

## Examples

### Using RabbitMQ

```yaml
# values-rabbitmq.yaml
broker:
  type: rabbitmq
  rabbitmq:
    # Connection URL with credentials, host, port, and vhost
    url: amqp://sentinel-prod:super-secret-password@rabbitmq.messaging.svc.cluster.local:5672/prod
    exchangeType: topic

config:
  resourceSelector:
    - label: environment
      value: production
```

```bash
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-rabbitmq.yaml
```

### Using Google Cloud Pub/Sub

```yaml
# values-googlepubsub.yaml
broker:
  type: googlepubsub
  googlepubsub:
    projectId: my-gcp-project
    maxOutstandingMessages: 1000
    numGoroutines: 10
```

```bash
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-googlepubsub.yaml
```

### Using Existing Secret

```yaml
# values-existing-secret.yaml
existingSecret: my-broker-credentials

# Create secret separately (only for RabbitMQ):
kubectl create secret generic my-broker-credentials \
  --namespace hyperfleet-system \
  --from-literal=BROKER_RABBITMQ_URL=amqp://user:pass@rabbitmq.local:5672/

# Note: Google Pub/Sub doesn't require Secret
# projectId is configured in values.yaml (not sensitive)
# Authentication uses Workload Identity in GKE
```

### Horizontal Scaling with Sharding

Deploy multiple Sentinel instances watching different resource shards:

```yaml
# values-shard-1.yaml
config:
  resourceSelector:
    - label: shard
      value: "1"
```

```yaml
# values-shard-2.yaml
config:
  resourceSelector:
    - label: shard
      value: "2"
```

```bash
helm install sentinel-shard-1 ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-shard-1.yaml

helm install sentinel-shard-2 ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-shard-2.yaml
```

## Security Considerations

### Broker Credentials

**WARNING**: Never commit real credentials to git!

Use one of these approaches for production:

1. **External Secrets Operator**:

   ```yaml
   apiVersion: external-secrets.io/v1beta1
   kind: ExternalSecret
   metadata:
     name: sentinel-broker-credentials
   spec:
     secretStoreRef:
       name: vault-backend
       kind: SecretStore
     target:
       name: sentinel-broker-credentials
     data:
       - secretKey: BROKER_TYPE
         remoteRef:
           key: sentinel/broker
           property: type
       # ... other fields
   ```

2. **Sealed Secrets**:

   ```bash
   kubectl create secret generic sentinel-broker-credentials \
     --dry-run=client -o yaml \
     --from-literal=BROKER_TYPE=rabbitmq \
     --from-literal=RABBITMQ_PASSWORD=super-secret | \
     kubeseal -o yaml > sealed-secret.yaml
   ```

3. **HashiCorp Vault**:

   ```yaml
   serviceAccount:
     annotations:
       vault.hashicorp.com/agent-inject: "true"
       vault.hashicorp.com/role: "sentinel"
       vault.hashicorp.com/agent-inject-secret-broker: "secret/data/sentinel/broker"
   ```

## Monitoring

Add Prometheus annotations for scraping metrics:

```yaml
podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n hyperfleet-system -l app.kubernetes.io/name=sentinel
```

### View Logs

```bash
kubectl logs -n hyperfleet-system -l app.kubernetes.io/name=sentinel -f
```

### Describe Pod

```bash
kubectl describe pod -n hyperfleet-system -l app.kubernetes.io/name=sentinel
```

### Check ConfigMap

```bash
kubectl get configmap -n hyperfleet-system
kubectl describe configmap sentinel-config -n hyperfleet-system
```

### Verify Secret

```bash
kubectl get secret -n hyperfleet-system
kubectl describe secret sentinel-broker-credentials -n hyperfleet-system
```

## License

See the parent [LICENSE](../../../LICENSE) file for details.
