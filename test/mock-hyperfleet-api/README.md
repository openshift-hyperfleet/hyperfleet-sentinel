# Mock HyperFleet API

A lightweight mock server that simulates the HyperFleet API, returning configurable resource responses.

The server listens on port **8888**.

## Prerequisites

- Go 1.25
- Docker
- kubectl
- `envsubst`

## Environment Variables

| Variable        | Default | Description                  |
|-----------------|---------|------------------------------|
| `CLUSTER_COUNT` | `100`   | Number of clusters to return |

## Run

**Option 1 - Locally:**

```bash
CLUSTER_COUNT=1000 go run test/mock-hyperfleet-api/main.go
```

**Option 2 - Docker:**

```bash
docker build -t mock-hyperfleet-api test/mock-hyperfleet-api/
docker run -p 8888:8888 -e CLUSTER_COUNT=1000 mock-hyperfleet-api
```

**Verify:**

```bash
curl http://localhost:8888/api/hyperfleet/v1/clusters
```

## Deploy

> **Note:** The Quay repo must be public. If using a private repo, see [Troubleshooting](#image-pull-fails-with-401-unauthorized).

```bash
# Build and push (linux/amd64 for cluster compatibility)
docker build --platform linux/amd64 -t quay.io/${USER}/mock-hyperfleet-api test/mock-hyperfleet-api/
docker push quay.io/${USER}/mock-hyperfleet-api

# Deploy (envsubst substitutes environment variables like $USER in the YAML)
envsubst < test/mock-hyperfleet-api/deployment.yaml | kubectl apply -n hyperfleet -f -

# Verify
kubectl logs -l app=mock-hyperfleet-api -n hyperfleet

# If you need to update cluster count (triggers automatic pod restart)
kubectl set env deployment/mock-hyperfleet-api CLUSTER_COUNT=5000 -n hyperfleet
```

## Point Sentinel at the Mock Hyperfleet API

Edit the ConfigMap and set `hyperfleet_api.endpoint` to `http://mock-hyperfleet-api:8888`:

```bash
kubectl edit configmap hyperfleet-sentinel-clusters-config -n hyperfleet
```

Then restart the pod and verify:

```bash
kubectl rollout restart deployment hyperfleet-sentinel-clusters -n hyperfleet
kubectl logs -l app.kubernetes.io/name=sentinel -n hyperfleet -f
```

## Clean Up

```bash
kubectl delete deployment mock-hyperfleet-api -n hyperfleet
```

## Troubleshooting

### Image Pull Fails with 401 UNAUTHORIZED

Create an image pull secret:

```bash
# Step 1: Enter your Quay password (input is hidden)
echo -n "Enter Quay password: "; read -r -s QUAY_PASSWORD; echo

# Step 2: Create the secret
kubectl create secret docker-registry quay-pull-secret \
  --docker-server=quay.io \
  --docker-username=$USER \
  --docker-password="$QUAY_PASSWORD" \
  -n hyperfleet \
  --dry-run=client -o yaml | kubectl apply -f -

# Step 3: Verify the secret was created
kubectl get secret quay-pull-secret -n hyperfleet
```

Then uncomment `imagePullSecrets` in `test/mock-hyperfleet-api/deployment.yaml` and redeploy.
