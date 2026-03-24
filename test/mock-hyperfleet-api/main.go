package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	port                = 8888
	defaultClusterCount = 100
	clustersPath        = "/api/hyperfleet/v1/clusters"
	releaseImageBase    = "quay.io/openshift-release-dev/ocp-release"
)

var (
	adapterNames = []string{"validator", "dns", "provisioner", "networking"}
	platforms    = []string{"AWS", "GCP", "Azure", "KubeVirt"}
	awsRegions   = []string{"us-east-1", "us-east-2", "us-west-2", "eu-west-1", "eu-central-1"}
	gcpRegions   = []string{"us-central1", "europe-west1", "europe-west4", "asia-east1"}
	azureRegions = []string{"eastus", "westus2", "westeurope", "northeurope"}
	networkTypes = []string{"OVNKubernetes", "OpenShiftSDN"}
	availPolicy  = []string{"HighlyAvailable", "SingleReplica"}
)

func main() {
	clusterCount := clusterCountFromEnv()
	if clusterCount <= 0 {
		log.Fatalf("CLUSTER_COUNT must be greater than 0, got %d", clusterCount)
	}

	clusterList := createClusterList(clusterCount)

	clusterBytes, err := json.Marshal(clusterList)
	if err != nil {
		log.Fatalf("Failed to marshal clusters: %v", err)
	}

	http.HandleFunc(clustersPath, jsonHandler(clusterBytes))

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Mock HyperFleet API on %s — %d clusters, %d bytes (~%d/cluster)",
		addr, clusterCount, len(clusterBytes), len(clusterBytes)/clusterCount)

	server := &http.Server{
		Addr:         addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func clusterCountFromEnv() int {
	s := os.Getenv("CLUSTER_COUNT")
	if s == "" {
		return defaultClusterCount
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Printf("Invalid CLUSTER_COUNT %q: %v, using default %d", s, err, defaultClusterCount)
		return defaultClusterCount
	}
	return n
}

func jsonHandler(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(data); err != nil {
			log.Printf("failed to write response: %v", err)
		}
	}
}

func createClusterList(count int) map[string]any {
	clusters := make([]map[string]any, 0, count)
	for i := range count {
		clusters = append(clusters, createCluster(i))
	}
	return map[string]any{
		"kind":  "ClusterList",
		"page":  1,
		"size":  count,
		"total": count,
		"items": clusters,
	}
}

func fakeClusterID(i int) string {
	return fmt.Sprintf("%032x", i+1)
}

func fakeUUID(i int) string {
	return fmt.Sprintf("00000000-0000-4000-a000-%012x", i+1)
}

func fakeInfraID(i int) string {
	return fmt.Sprintf("hc-%012x", i+1)
}

func createCluster(i int) map[string]any {
	id := fakeClusterID(i)
	uuid := fakeUUID(i)
	createdAt := time.Date(2020, time.Month(i%12)+1, 1+(i%28), i%24, i%60, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Duration(i%720) * time.Hour)
	generation := int32(1 + i%5)

	return map[string]any{
		"kind": "Cluster",
		"id":   id,
		"href": clustersPath + "/" + id,
		"name": uuid,
		"labels": map[string]string{
			"environment": "production",
			"team":        "platform",
		},
		"spec":         buildHostedClusterSpec(i, uuid),
		"created_time": createdAt.Format(time.RFC3339),
		"updated_time": updatedAt.Format(time.RFC3339),
		"created_by":   fmt.Sprintf("user-%d@example.com", i%20),
		"updated_by":   fmt.Sprintf("user-%d@example.com", i%20),
		"generation":   generation,
		"status": map[string]any{
			"conditions": buildConditions(generation),
		},
	}
}

func buildHostedClusterSpec(i int, clusterID string) map[string]any {
	minor := 3 + (i % 15) // OCP versions 4.3 – 4.17
	patch := i % 30
	platform := platforms[i%len(platforms)]
	netType := networkTypes[i%len(networkTypes)]
	infraID := fakeInfraID(i)

	spec := map[string]any{
		"release": map[string]any{
			"image": fmt.Sprintf("%s:4.%d.%d-x86_64", releaseImageBase, minor, patch),
		},
		"clusterID":    clusterID,
		"infraID":      infraID,
		"channel":      fmt.Sprintf("stable-4.%d", minor),
		"platform":     buildPlatform(platform, i),
		"dns":          map[string]any{"baseDomain": fmt.Sprintf("cluster-%d.example.com", i)},
		"networking":   buildNetworking(netType, i),
		"etcd": map[string]any{
			"managementType": "Managed",
			"managed": map[string]any{
				"storage": map[string]any{
					"type": "PersistentVolume",
					"persistentVolume": map[string]any{
						"size": "8Gi",
					},
				},
			},
		},
		"services":     buildServices(),
		"pullSecret":   map[string]any{"name": "pull-secret"},
		"sshKey":       map[string]any{"name": "ssh-key"},
		"issuerURL":    fmt.Sprintf("https://oidc.example.com/%s", infraID),
		"fips":         i%10 == 0,
		"controllerAvailabilityPolicy":     availPolicy[i%len(availPolicy)],
		"infrastructureAvailabilityPolicy": availPolicy[(i+1)%len(availPolicy)],
		"secretEncryption": map[string]any{
			"type": "aescbc",
			"aescbc": map[string]any{
				"activeKey": map[string]any{"name": fmt.Sprintf("etcd-encryption-key-%d", i)},
			},
		},
		"configuration": map[string]any{
			"apiServer": map[string]any{
				"audit": map[string]any{"profile": "Default"},
			},
		},
	}

	return spec
}

func buildPlatform(platformType string, i int) map[string]any {
	p := map[string]any{"type": platformType}
	switch platformType {
	case "AWS":
		region := awsRegions[i%len(awsRegions)]
		arnPrefix := fmt.Sprintf("arn:aws:iam::123456789012:role/hc-%d", i)
		p["aws"] = map[string]any{
			"region": region,
			"rolesRef": map[string]any{
				"ingressARN":              arnPrefix + "-ingress",
				"imageRegistryARN":        arnPrefix + "-image-registry",
				"storageARN":              arnPrefix + "-storage",
				"networkARN":              arnPrefix + "-network",
				"kubeCloudControllerARN":  arnPrefix + "-kube-controller",
				"nodePoolManagementARN":   arnPrefix + "-nodepool-mgmt",
				"controlPlaneOperatorARN": arnPrefix + "-cpo",
			},
			"endpointAccess": "Public",
		}
	case "GCP":
		region := gcpRegions[i%len(gcpRegions)]
		p["gcp"] = map[string]any{
			"projectID": fmt.Sprintf("hyperfleet-project-%d", i%5),
			"region":    region,
		}
	case "Azure":
		region := azureRegions[i%len(azureRegions)]
		p["azure"] = map[string]any{
			"location":       region,
			"subscriptionID": fmt.Sprintf("00000000-0000-0000-0000-%012x", i+1),
			"resourceGroup":  fmt.Sprintf("hc-rg-%d", i),
		}
	case "KubeVirt":
		p["kubevirt"] = map[string]any{
			"baseDomainPassthrough": i%2 == 0,
		}
	}
	return p
}

func buildNetworking(netType string, i int) map[string]any {
	clusterNet := fmt.Sprintf("10.%d.0.0/14", 128+(i%32)*4)
	serviceNet := fmt.Sprintf("172.%d.0.0/16", 16+i%16)
	return map[string]any{
		"networkType": netType,
		"clusterNetwork": []map[string]any{
			{"cidr": clusterNet, "hostPrefix": 23},
		},
		"serviceNetwork": []map[string]any{
			{"cidr": serviceNet},
		},
	}
}

func buildServices() []map[string]any {
	routeStrategy := map[string]any{"type": "Route"}
	services := []string{"APIServer", "OAuthServer", "OIDC", "Konnectivity", "Ignition"}
	result := make([]map[string]any, len(services))
	for i, svc := range services {
		result[i] = map[string]any{"service": svc, "servicePublishingStrategy": routeStrategy}
	}
	return result
}

func buildConditions(generation int32) []map[string]any {
	now := time.Now().UTC()
	conditionCreated := now.Add(-24 * time.Hour).Format(time.RFC3339)
	lastTransition := now.Add(-10 * time.Minute).Format(time.RFC3339)
	lastUpdated := now.Format(time.RFC3339)

	makeCondition := func(typ, status, reason, message string) map[string]any {
		return map[string]any{
			"type":                 typ,
			"status":               status,
			"reason":               reason,
			"message":              message,
			"observed_generation":  generation,
			"created_time":         conditionCreated,
			"last_updated_time":    lastUpdated,
			"last_transition_time": lastTransition,
		}
	}

	conditions := []map[string]any{
		makeCondition("Ready", "True", "AllAdaptersReady",
			"All adapters reported Ready True for the current generation"),
		makeCondition("Available", "True", "AllAdaptersAvailable",
			"All adapters reported Available True for the same generation"),
	}

	for _, adapter := range adapterNames {
		conditions = append(conditions, makeCondition(adapter+"Successful", "True",
			adapter+"Completed", fmt.Sprintf("Adapter %s completed successfully", adapter)))
	}

	return conditions
}
