# Sentinel Helm Chart

This Helm chart deploys the HyperFleet Sentinel service to Kubernetes.

## Prerequisites

- Kubernetes 1.20+
- Helm 3.0+
- HyperFleet API deployed and accessible
- Message broker (RabbitMQ, GCP Pub/Sub, or AWS SQS) configured

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
| `image.repository` | Container image repository | `quay.io/hyperfleet/sentinel` |
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

| Parameter | Description | Default |
|-----------|-------------|---------|
| `broker.type` | Broker type (rabbitmq, pubsub, awsSqs) | `rabbitmq` |
| `broker.rabbitmq.host` | RabbitMQ host | `rabbitmq.hyperfleet-system.svc.cluster.local` |
| `broker.rabbitmq.port` | RabbitMQ port | `5672` |
| `broker.rabbitmq.exchange` | RabbitMQ exchange | `hyperfleet-events` |
| `broker.rabbitmq.vhost` | RabbitMQ virtual host | `/hyperfleet` |
| `broker.rabbitmq.exchangeType` | RabbitMQ exchange type | `fanout` |
| `broker.rabbitmq.username` | RabbitMQ username | `sentinel-user` |
| `broker.rabbitmq.password` | RabbitMQ password | `change-me-in-production` |
| `existingSecret` | Use existing secret for broker credentials | `""` |

## Examples

### Using RabbitMQ

```yaml
# values-rabbitmq.yaml
broker:
  type: rabbitmq
  rabbitmq:
    host: rabbitmq.messaging.svc.cluster.local
    port: "5672"
    exchange: hyperfleet-production
    vhost: /prod
    username: sentinel-prod
    password: super-secret-password

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

### Using GCP Pub/Sub

```yaml
# values-pubsub.yaml
broker:
  type: pubsub
  pubsub:
    projectId: my-gcp-project

serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: sentinel@my-gcp-project.iam.gserviceaccount.com
```

```bash
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-pubsub.yaml
```

### Using AWS SQS

```yaml
# values-sqs.yaml
broker:
  type: awsSqs
  sqs:
    region: us-east-1
    queueUrl: https://sqs.us-east-1.amazonaws.com/123456789012/hyperfleet-events

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/sentinel-role
```

```bash
helm install sentinel ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --values values-sqs.yaml
```

### Using Existing Secret

```yaml
# values-existing-secret.yaml
existingSecret: my-broker-credentials

# Create secret separately:
kubectl create secret generic my-broker-credentials \
  --namespace hyperfleet-system \
  --from-literal=BROKER_TYPE=rabbitmq \
  --from-literal=BROKER_HOST=rabbitmq.local \
  --from-literal=BROKER_PORT=5672 \
  --from-literal=BROKER_EXCHANGE=events \
  --from-literal=RABBITMQ_USERNAME=user \
  --from-literal=RABBITMQ_PASSWORD=pass
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
