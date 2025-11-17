# Kubernetes Deployment

This directory contains Kubernetes manifests for deploying the Sentinel service.

## Files

- `deployment.yaml` - Complete deployment example with ConfigMap, Secret, Deployment, and ServiceAccount

## Deployment Pattern

The Sentinel service uses a ConfigMap for non-sensitive configuration and Secrets for broker credentials:

1. **ConfigMap** (`sentinel-config`): Contains the main YAML configuration file
2. **Secret** (`sentinel-broker-credentials`): Contains broker credentials and connection details
3. **Deployment**: Mounts ConfigMap as `/etc/sentinel/config.yaml` and injects Secret values as environment variables
4. **ServiceAccount**: Used for cloud provider authentication (Workload Identity, IRSA)

## Prerequisites

- Kubernetes cluster (v1.20+)
- kubectl configured to access your cluster
- HyperFleet API deployed and accessible
- Message broker (RabbitMQ, GCP Pub/Sub, or AWS SQS) configured

## Quick Start

### 1. Create Namespace

```bash
kubectl create namespace hyperfleet-system
```

### 2. Update Secret

**Important**: Never commit real credentials to git. Use external secrets management solutions like:
- [External Secrets Operator](https://external-secrets.io/)
- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
- [Vault](https://www.vaultproject.io/)

Edit `deployment.yaml` and update the `sentinel-broker-credentials` Secret with your actual broker credentials:

```bash
# Edit the Secret section with your credentials
vim deployments/kubernetes/deployment.yaml
```

### 3. Deploy

```bash
kubectl apply -f deployments/kubernetes/deployment.yaml
```

### 4. Verify Deployment

```bash
# Check pods
kubectl get pods -n hyperfleet-system

# Check logs
kubectl logs -n hyperfleet-system -l app=sentinel -f

# Check configuration loaded correctly
kubectl logs -n hyperfleet-system -l app=sentinel | grep "Configuration loaded successfully"
```

## Horizontal Scaling with Sharding

To scale horizontally, deploy multiple Sentinel instances with different shard selectors:

### Example: 3 Shards

Create three ConfigMaps with different `resource_selector` values:

**Shard 1:**
```yaml
resource_selector:
  - label: shard
    value: "1"
```

**Shard 2:**
```yaml
resource_selector:
  - label: shard
    value: "2"
```

**Shard 3:**
```yaml
resource_selector:
  - label: shard
    value: "3"
```

Deploy three separate Deployments, each referencing its respective ConfigMap:

```bash
kubectl apply -f sentinel-shard-1.yaml
kubectl apply -f sentinel-shard-2.yaml
kubectl apply -f sentinel-shard-3.yaml
```

Resources in the HyperFleet API should be labeled with `shard=1`, `shard=2`, or `shard=3` to be picked up by the corresponding Sentinel instance.

## Configuration Reloading

The Sentinel service does **not** support dynamic configuration reloading. To apply configuration changes:

1. Update the ConfigMap or Secret
2. Restart the pods:

```bash
kubectl rollout restart deployment/sentinel -n hyperfleet-system
```

## Broker Configuration Examples

### RabbitMQ (Default)

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: sentinel-broker-credentials
stringData:
  BROKER_TYPE: "rabbitmq"
  BROKER_HOST: "rabbitmq.hyperfleet-system.svc.cluster.local"
  BROKER_PORT: "5672"
  BROKER_EXCHANGE: "hyperfleet-events"
  BROKER_VHOST: "/hyperfleet"
  RABBITMQ_USERNAME: "sentinel-user"
  RABBITMQ_PASSWORD: "secret-password"
```

### GCP Pub/Sub

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: sentinel-broker-credentials
stringData:
  BROKER_TYPE: "pubsub"
  BROKER_PROJECT_ID: "your-gcp-project-id"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sentinel
  annotations:
    # Enable Workload Identity
    iam.gke.io/gcp-service-account: sentinel@your-project.iam.gserviceaccount.com
```

### AWS SQS

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: sentinel-broker-credentials
stringData:
  BROKER_TYPE: "awsSqs"
  BROKER_REGION: "us-east-1"
  BROKER_QUEUE_URL: "https://sqs.us-east-1.amazonaws.com/123456789012/hyperfleet-events"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sentinel
  annotations:
    # Enable IRSA
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/sentinel-role
```

## Troubleshooting

### Configuration Errors

```bash
# Check if configuration loaded successfully
kubectl logs -n hyperfleet-system -l app=sentinel | grep -i "config"

# Check for validation errors
kubectl logs -n hyperfleet-system -l app=sentinel | grep -i "error"
```

### Broker Connection Issues

```bash
# For RabbitMQ, check connectivity
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  nc -zv rabbitmq.hyperfleet-system.svc.cluster.local 5672

# Check broker credentials in Secret
kubectl get secret sentinel-broker-credentials -n hyperfleet-system -o yaml
```

### Resource Selection

```bash
# Verify resource_selector is working
# Check Sentinel logs for which resources it's processing
kubectl logs -n hyperfleet-system -l app=sentinel | grep "Processing resource"
```
