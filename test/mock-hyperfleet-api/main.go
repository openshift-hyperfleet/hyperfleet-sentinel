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
	ocmBase             = "/api/clusters_mgmt/v1"
	ocmClusters         = ocmBase + "/clusters"
	maxUpgradePatches   = 20
)

var (
	adapterNames = []string{"validator", "dns", "provisioner", "networking"}
	providers    = []string{"aws", "gcp", "azure", "vsphere", "openstack", "libvirt"}
	awsRegions   = []string{"us-east-1", "us-east-2", "us-west-2", "eu-west-1", "eu-central-1"}
	gcpRegions   = []string{"us-central1", "europe-west1", "europe-west4", "asia-east1"}
	displayNames = []string{
		"Home Lab", "Energy Lab v2", "Test CRC", "My Cluster Luigi",
		"Oak Cottage Private", "Home-Lab", "Pietro Cluster", "Test-swatch",
	}
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
		log.Printf("Invalid CLUSTER_COUNT %q: %v, using default %d", s, err,  defaultClusterCount)
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
	h := fmt.Sprintf("%032x", i+1)
	return h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-a" + h[17:20] + "-" + h[20:32]
}

func fakeSubID(i int) string {
	return fmt.Sprintf("%032x", i+100001)
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
		"spec":         buildOCMSpec(i, id, uuid),
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

func buildOCMSpec(i int, id, uuid string) map[string]any {
	minor := 3 + (i % 15) // OCP versions 4.3 – 4.17, the range seen in production
	patch := i % 30
	provider := providers[i%len(providers)]
	createdAt := time.Date(2020, time.Month(i%12)+1, 1+(i%28), i%24, i%60, 0, 0, time.UTC)

	display := uuid
	if i%8 < len(displayNames) && i%5 == 0 { // 20% of clusters get a human-readable display name
		display = displayNames[i%len(displayNames)]
	}

	clusterHref := ocmClusters + "/" + id
	ecHref := clusterHref + "/external_configuration"

	spec := map[string]any{
		"external_id":        uuid,
		"display_name":       display,
		"creation_timestamp": createdAt.Format(time.RFC3339),
		"activity_timestamp": createdAt.Format(time.RFC3339),
		"cloud_provider": map[string]any{
			"kind": "CloudProviderLink",
			"id":   provider,
			"href": ocmBase + "/cloud_providers/" + provider,
		},
		"openshift_version": fmt.Sprintf("4.%d.%d", minor, patch),
		"subscription":      buildSubscription(i),
		"console": map[string]any{
			"url": fmt.Sprintf(
				"https://console-openshift-console.apps.cluster-%d.example.com", i),
		},
		"api": map[string]any{"listening": "external"},
		"nodes": map[string]any{
			"master":  0,
			"infra":   0,
			"compute": 0,
		},
		"state": clusterState(i),
		"groups": map[string]any{
			"kind": "GroupListLink",
			"href": clusterHref + "/groups",
		},
		"network": map[string]any{"type": "OpenShiftSDN", "host_prefix": 23},
		"external_configuration": map[string]any{
			"kind": "ExternalConfiguration",
			"href": ecHref,
			"syncsets": map[string]any{
				"kind": "SyncsetListLink",
				"href": ecHref + "/syncsets",
			},
			"labels": map[string]any{
				"kind": "LabelListLink",
				"href": ecHref + "/labels",
			},
			"manifests": map[string]any{
				"kind": "ManifestListLink",
				"href": ecHref + "/manifests",
			},
		},
		"multi_az": i%3 == 0,
		"managed":  i%2 == 0,
		"ccs":      map[string]any{"enabled": false, "disable_scp_checks": false},
		"storage_quota":       map[string]any{"value": 0, "unit": "B"},
		"load_balancer_quota": 0,
		"identity_providers": map[string]any{
			"kind": "IdentityProviderListLink",
			"href": clusterHref + "/identity_providers",
		},
		"ingresses": map[string]any{
			"kind": "IngressListLink",
			"href": clusterHref + "/ingresses",
		},
		"machine_pools": map[string]any{
			"kind": "MachinePoolListLink",
			"href": clusterHref + "/machine_pools",
		},
		"inflight_checks": map[string]any{
			"kind": "InflightCheckListLink",
			"href": clusterHref + "/inflight_checks",
		},
		"product": map[string]any{
			"kind": "ProductLink", "id": "ocp",
			"href": ocmBase + "/products/ocp",
		},
		"status":                buildOCMStatus(i),
		"node_drain_grace_period": map[string]any{"value": 60, "unit": "minutes"},
		"etcd_encryption":                  false,
		"billing_model":                    "standard",
		"disable_user_workload_monitoring": false,
		"managed_service":                  map[string]any{"enabled": false, "managed": false},
		"hypershift":                       map[string]any{"enabled": false},
		"byo_oidc":                         map[string]any{"enabled": false},
		"delete_protection": map[string]any{
			"href":    clusterHref + "/delete_protection",
			"enabled": false,
		},
		"external_auth_config": map[string]any{
			"kind": "ExternalAuthConfig",
			"href": clusterHref + "/external_auth_config",
			"external_auths": map[string]any{
				"href": clusterHref + "/external_auth_config/external_auths",
			},
			"enabled": false,
		},
		"multi_arch_enabled": false,
		"image_registry":     map[string]any{"state": "enabled"},
		"control_plane": map[string]any{
			"log_forwarders": map[string]any{
				"kind": "LogForwarderListLink",
				"href": clusterHref + "/control_plane/log_forwarders",
			},
		},
	}

	addProviderRegion(spec, provider, i)
	addVersionInfo(spec, i, minor, patch)
	return spec
}

func buildSubscription(i int) map[string]any {
	subID := fakeSubID(i)
	return map[string]any{
		"kind": "SubscriptionLink",
		"id":   subID,
		"href": "/api/accounts_mgmt/v1/subscriptions/" + subID,
	}
}

func clusterState(i int) string {
	states := []string{"ready", "installing", "error", "hibernating"}
	return states[i%len(states)]
}

func buildOCMStatus(i int) map[string]any {
	return map[string]any{
		"state":                        clusterState(i),
		"dns_ready":                    i%4 != 0,
		"oidc_ready":                   false,
		"provision_error_message":      "",
		"provision_error_code":         "",
		"limited_support_reason_count": 0,
	}
}

func addProviderRegion(spec map[string]any, provider string, i int) {
	id, _ := spec["external_id"].(string)
	clusterHref := ocmClusters + "/" + id

	switch provider {
	case "aws":
		region := awsRegions[i%len(awsRegions)]
		spec["region"] = map[string]any{
			"kind": "CloudRegionLink",
			"id":   region,
			"href": ocmBase + "/cloud_providers/aws/regions/" + region,
		}
		spec["aws"] = map[string]any{
			"private_link": false,
			"private_link_configuration": map[string]any{
				"kind": "PrivateLinkConfigurationLink",
				"href": clusterHref + "/aws/private_link_configuration",
			},
			"audit_log":                map[string]any{"role_arn": ""},
			"ec2_metadata_http_tokens": "optional",
		}
		spec["aws_infrastructure_access_role_grants"] = map[string]any{
			"kind": "AWSInfrastructureAccessRoleGrantLink",
			"href": clusterHref + "/aws_infrastructure_access_role_grants",
		}
	case "gcp":
		region := gcpRegions[i%len(gcpRegions)]
		spec["region"] = map[string]any{
			"kind": "CloudRegionLink",
			"id":   region,
			"href": ocmBase + "/cloud_providers/gcp/regions/" + region,
		}
		spec["gcp"] = map[string]any{
			"project_id": "",
			"security":   map[string]any{"secure_boot": false},
			"authentication": map[string]any{
				"kind": "RedHatCloudAccount",
			},
		}
	// azure, vsphere, openstack, libvirt: cloud_provider only, no region
	}
}

func addVersionInfo(spec map[string]any, i, minor, patch int) {
	upgrades := make([]string, 0, i%maxUpgradePatches)
	for p := patch + 1; p <= patch+(i%maxUpgradePatches); p++ {
		upgrades = append(upgrades, fmt.Sprintf("4.%d.%d", minor, p))
	}
	versionID := fmt.Sprintf("openshift-v4.%d.%d", minor, patch)
	spec["version"] = map[string]any{
		"kind":                "VersionLink",
		"id":                  versionID,
		"href":                ocmBase + "/versions/" + versionID,
		"raw_id":              fmt.Sprintf("4.%d.%d", minor, patch),
		"channel_group":       "stable",
		"available_upgrades":  upgrades,
		"available_channels":  []string{"stable-4." + fmt.Sprintf("%d", minor)},
		"end_of_life_timestamp": "2026-12-01T00:00:00Z",
	}
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
