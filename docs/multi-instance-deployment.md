# Deploying Multiple Sentinel Instances
**Status**: Active
**Owner**: HyperFleet Team
**Last Updated**: 2026-03-12
> **Audience:** Operations teams deploying Sentinel at scale.

Sentinel supports horizontal scaling through multiple dimensions: by resource type (separate instances for clusters vs nodepools) and by label-based resource filtering within the same resource type. Deploy multiple Sentinel instances with different `resource_selector` values to distribute the workload.

> **Important**: There is no coordination between Sentinel instances. Operators must ensure all resources are covered by the combined selectors to avoid gaps.

## Using Helm for Multi-Instance Deployment

Deploy multiple Sentinel instances by installing the Helm chart multiple times with different release names and configurations:

```bash
# Instance 1: Watch clusters in us-east region
helm install sentinel-us-east ./charts \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-east

# Instance 2: Watch clusters in us-west region
helm install sentinel-us-west ./charts \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-west

# Instance 3: Watch clusters in eu-central region
helm install sentinel-eu-central ./charts \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=eu-central
```

## Using Values Files for Complex Configurations

For more complex setups, create separate values files for each instance:

**values-us-east.yaml:**
```yaml
config:
  resourceType: clusters
  resourceSelector:
    - label: region
      value: us-east
    - label: environment
      value: production
```

**values-us-west.yaml:**
```yaml
config:
  resourceType: clusters
  resourceSelector:
    - label: region
      value: us-west
    - label: environment
      value: production
```

Deploy using values files:
```bash
helm install sentinel-us-east ./charts \
  --namespace hyperfleet-system \
  -f values-us-east.yaml

helm install sentinel-us-west ./charts \
  --namespace hyperfleet-system \
  -f values-us-west.yaml
```

## Resource Filtering Strategies

| Strategy | Description | Example Labels |
|----------|-------------|----------------|
| Regional | Distribute by geographic region | `region: us-east`, `region: eu-west` |
| Environment | Separate by environment type | `environment: production`, `environment: staging` |
| Numeric subsets | Numeric labels for even distribution | `subset: "0"`, `subset: "1"`, `subset: "2"` |
| Cluster Type | Filter by cluster characteristics | `cluster-type: hypershift`, `cluster-type: standalone` |

Multiple labels use AND logic - all labels must match for a resource to be selected.

## Broker Topic Isolation

When deploying multiple Sentinel instances, consider using separate broker topics to avoid event duplication and enable independent processing:

```bash
# Instance for us-east with dedicated topic
helm install sentinel-us-east ./charts \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-east \
  --set broker.topic=hyperfleet-clusters-us-east

# Instance for us-west with dedicated topic
helm install sentinel-us-west ./charts \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-west \
  --set broker.topic=hyperfleet-clusters-us-west
```

By default, the Helm chart generates topic names using the pattern `{namespace}-{resourceType}`. Override with `broker.topic` when you need per-instance isolation.

## MVP Recommendation

For initial deployments, start with a **single Sentinel instance** watching all resources:

```yaml
config:
  resourceType: clusters
  resourceSelector: []  # Empty = watch all resources
```

Scale to multiple instances as your cluster count grows or when you need regional isolation.

---

## PodDisruptionBudget

**What**: Ensures minimum Sentinel availability during cluster maintenance.

**Configuration for Single-Replica Deployments** (typical topology):
```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: sentinel-pdb
  namespace: hyperfleet-system
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: sentinel
```

**Operational Impact**:
- **Single replica protection**: `minAvailable: 1` blocks voluntary pod eviction when only 1 replica exists
- **Maintenance blocking**: Node drains will be delayed until Sentinel pods are manually drained or scaled up
- **Multiple Sentinels**: Each Sentinel deployment (per resource selector) can have its own PDB
- **Trade-off**: Maintenance operations may require manual intervention for single-replica Sentinels

> **Note**: Cluster maintenance operations respect Sentinel availability requirements.

---

## Operational Guidance

### Resource Requirements

#### Production Recommendations
```yaml
resources:
  requests:
    cpu: 100m      # Baseline for polling every 5s
    memory: 128Mi  # Baseline for ~1000 resources
  limits:
    cpu: 500m      # Handle traffic spikes
    memory: 512Mi  # Memory for large resource sets
```

> **Note**: Resource requirements will be validated and updated based on actual consumption profiling in HYPERFLEET-556.

#### Scaling Guidelines

**CPU Scaling**:
- **Base load**: 50-100m for basic polling
- **Per 1000 resources**: Additional 50m CPU
- **High churn environments**: Additional 100m for frequent events

**Memory Scaling**:
- **Base load**: 64Mi for service overhead
- **Per 1000 resources**: Additional 32Mi memory
- **Complex resource selectors**: Additional 16Mi per selector rule

**Example Calculation**:
```
5000 resources + complex selectors:
CPU: 100m + (5 × 50m) + 100m = 450m
Memory: 64Mi + (5 × 32Mi) + 16Mi = 240Mi
```

### Scaling Strategy

#### Horizontal Scaling (Label Partitioning)

**Approach**: Deploy multiple Sentinel instances with different `resource_selector` configurations.

**Benefits**:
- Linear performance scaling
- Fault isolation (one failure doesn't affect all resources)
- Regional deployment (Sentinel near managed resources)
- Different configurations per environment

**Example Multi-Instance Deployment**:
```
                            ┌───────────────────┐
                            │  HyperFleet API   │
                            └─────────┬─────────┘
                                      │
                              Step 1: fetch resources
                                      │
                                      ▼
┌─────────────────────┐  ┌─────────────────────┐  ┌─────────────────────┐
│   Sentinel US-East  │  │   Sentinel US-West  │  │   Sentinel EU-West  │
│  resource_selector: │  │  resource_selector: │  │  resource_selector: │
│  - label: region    │  │  - label: region    │  │  - label: region    │
│    value: us-east   │  │    value: us-west   │  │    value: eu-west   │
│  max_age_ready=30m  │  │  max_age_ready=1h   │  │  max_age_ready=45m  │
└──────────┬──────────┘  └──────────┬──────────┘  └──────────┬──────────┘
           │                        │                        │
           │                        ▼                        │
           └────────────► Step 2: publish events ◄───────────┘
                                    │
                                    ▼
                            ┌───────────────────┐
                            │  Message Broker   │
                            └───────────────────┘
```

**Important**: This is **NOT leader election**. Multiple Sentinels can overlap resource selectors if needed. Operators must ensure appropriate coverage.

#### Resource Selector Strategies

**Regional Partitioning**:
```yaml
# Sentinel A
resource_selector:
  - label: region
    value: us-east

# Sentinel B
resource_selector:
  - label: region
    value: us-west
```

**Environment Partitioning**:
```yaml
# Production Sentinel
resource_selector:
  - label: environment
    value: production

# Development Sentinel
resource_selector:
  - label: environment
    value: development
```

**Hybrid Partitioning**:
```yaml
# Production US-East
resource_selector:
  - label: region
    value: us-east
  - label: environment
    value: production
```


## Architecture Reference

For more details on the Sentinel architecture and resource filtering design, see the [architecture documentation](https://github.com/openshift-hyperfleet/architecture/tree/main/hyperfleet/components/sentinel).
