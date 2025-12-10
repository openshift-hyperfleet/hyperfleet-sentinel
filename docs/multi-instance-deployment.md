# Deploying Multiple Sentinel Instances

Sentinel supports horizontal scaling through multiple dimensions: by resource type (separate instances for clusters vs nodepools) and by label-based resource filtering within the same resource type. Deploy multiple Sentinel instances with different `resource_selector` values to distribute the workload.

> **Important**: There is no coordination between Sentinel instances. Operators must ensure all resources are covered by the combined selectors to avoid gaps.

## Using Helm for Multi-Instance Deployment

Deploy multiple Sentinel instances by installing the Helm chart multiple times with different release names and configurations:

```bash
# Instance 1: Watch clusters in us-east region
helm install sentinel-us-east ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-east

# Instance 2: Watch clusters in us-west region
helm install sentinel-us-west ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-west

# Instance 3: Watch clusters in eu-central region
helm install sentinel-eu-central ./deployments/helm/sentinel \
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
helm install sentinel-us-east ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  -f values-us-east.yaml

helm install sentinel-us-west ./deployments/helm/sentinel \
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
helm install sentinel-us-east ./deployments/helm/sentinel \
  --namespace hyperfleet-system \
  --set config.resourceSelector[0].label=region \
  --set config.resourceSelector[0].value=us-east \
  --set broker.topic=hyperfleet-clusters-us-east

# Instance for us-west with dedicated topic
helm install sentinel-us-west ./deployments/helm/sentinel \
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

## Architecture Reference

For more details on the Sentinel architecture and resource filtering design, see the [architecture documentation](https://github.com/openshift-hyperfleet/architecture/tree/main/hyperfleet/components/sentinel).
