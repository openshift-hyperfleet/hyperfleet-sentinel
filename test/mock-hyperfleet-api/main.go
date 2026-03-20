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

	maxUpgradePatches = 20
)

var (
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
		log.Printf("Invalid CLUSTER_COUNT %q, using default %d", s, defaultClusterCount)
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
	return fmt.Sprintf("1%07x%08x%08x%08x", i%0xFFFFFFF, i*2654435761, i*40503, i*12345678)
}

func fakeUUID(i int) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		i*2654435761, (i*40503)&0xFFFF,
		0x4000|((i*12345)&0x0FFF), 0x8000|((i*6789)&0x3FFF),
		i*1099511627776+i)
}

func fakeSubID(i int) string {
	return fmt.Sprintf("1%06x%04x%04x%04x%06x",
		i%0xFFFFFF, (i*7919)&0xFFFF, (i*104729)&0xFFFF,
		(i*6271)&0xFFFF, (i*31337)&0xFFFFFF)
}

func createCluster(i int) map[string]any {
	id := fakeClusterID(i)
	uuid := fakeUUID(i)
	minor := 3 + (i % 15)
	patch := i % 30

	display := uuid
	if i%8 < len(displayNames) && i%5 == 0 {
		display = displayNames[i%len(displayNames)]
	}

	cluster := map[string]any{
		"kind":               "Cluster",
		"id":                 id,
		"href":               ocmClusters + "/" + id,
		"name":               uuid,
		"external_id":        uuid,
		"display_name":       display,
		"creation_timestamp": time.Date(2020, time.Month(i%12)+1, 1+(i%28), i%24, i%60, 0, 0, time.UTC).Format(time.RFC3339),
		"activity_timestamp": time.Date(2020, time.Month(i%12)+1, 1+(i%28), i%24, i%60, 0, 0, time.UTC).Format(time.RFC3339),
		"subscription":       buildSubscription(i),
		"nodes": map[string]any{
			"master":  0,
			"infra":   0,
			"compute": 0,
			"compute_machine_type": map[string]any{
				"kind": "MachineTypeLink",
				"id":   "m5.xlarge",
				"href": ocmBase + "/machine_types/m5.xlarge",
			},
		},
		"state":                  clusterState(i),
		"groups":                 linkList("GroupListLink", id+"/groups"),
		"network":                map[string]any{"type": "OpenShiftSDN", "host_prefix": 23},
		"external_configuration": buildExternalConfig(id),
		"multi_az":               i%3 == 0,
		"managed":                i%2 == 0,
		"ccs":                    map[string]any{"enabled": false, "disable_scp_checks": false},
		"identity_providers":     linkList("IdentityProviderListLink", id+"/identity_providers"),
		"ingresses":              linkList("IngressListLink", id+"/ingresses"),
		"machine_pools":          linkList("MachinePoolListLink", id+"/machine_pools"),
		"inflight_checks":        linkList("InflightCheckListLink", id+"/inflight_checks"),
		"product": map[string]any{
			"kind": "ProductLink", "id": "ocp",
			"href": ocmBase + "/products/ocp",
		},
		"status":                           buildStatus(i),
		"node_drain_grace_period":          map[string]any{"value": 60, "unit": "minutes"},
		"etcd_encryption":                  false,
		"billing_model":                    "standard",
		"disable_user_workload_monitoring": false,
		"managed_service":                  map[string]any{"enabled": false, "managed": false},
		"hypershift":                       map[string]any{"enabled": false},
		"byo_oidc":                         map[string]any{"enabled": false},
		"delete_protection": map[string]any{
			"href":    ocmClusters + "/" + id + "/delete_protection",
			"enabled": false,
		},
		"external_auth_config": map[string]any{
			"kind":    "ExternalAuthConfig",
			"href":    ocmClusters + "/" + id + "/external_auth_config",
			"enabled": false,
			"external_auths": map[string]any{
				"href": ocmClusters + "/" + id + "/external_auth_config/external_auths",
			},
		},
		"multi_arch_enabled": false,
		"image_registry":     map[string]any{"state": "enabled"},
		"control_plane": map[string]any{
			"log_forwarders": linkList("LogForwarderListLink", id+"/control_plane/log_forwarders"),
		},
		"openshift_version": fmt.Sprintf("4.%d.%d", minor, patch),
		"api":               map[string]any{"listening": "external"},
		"console": map[string]any{
			"url": fmt.Sprintf(
				"https://console-openshift-console.apps.cluster-%d.example.com", i),
		},
		"storage_quota":      map[string]any{"value": 0, "unit": "B"},
		"load_balancer_quota": 0,
	}

	provider := providers[i%len(providers)]
	addProviderFields(cluster, id, provider, i)
	addVersionInfo(cluster, i, minor, patch)

	return cluster
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
	if i%7 == 0 {
		return "unknown"
	}
	return "ready"
}

func buildExternalConfig(id string) map[string]any {
	base := ocmClusters + "/" + id + "/external_configuration"
	return map[string]any{
		"kind":      "ExternalConfiguration",
		"href":      base,
		"syncsets":  map[string]any{"kind": "SyncsetListLink", "href": base + "/syncsets"},
		"labels":    map[string]any{"kind": "LabelListLink", "href": base + "/labels"},
		"manifests": map[string]any{"kind": "ManifestListLink", "href": base + "/manifests"},
	}
}

func buildStatus(i int) map[string]any {
	state := clusterState(i)
	return map[string]any{
		"state":                        state,
		"dns_ready":                    i%4 != 0,
		"oidc_ready":                   false,
		"provision_error_message":      "",
		"provision_error_code":         "",
		"limited_support_reason_count": 0,
	}
}

func linkList(kind, path string) map[string]any {
	return map[string]any{
		"kind": kind,
		"href": ocmClusters + "/" + path,
	}
}

func addProviderFields(cluster map[string]any, id, provider string, i int) {
	cluster["cloud_provider"] = cloudProviderLink(provider)

	// azure, vsphere, openstack, libvirt: cloud_provider only, no region
	switch provider {
	case "aws":
		region := awsRegions[i%len(awsRegions)]
		cluster["region"] = cloudRegion("aws", region)
		cluster["aws"] = map[string]any{
			"private_link": false,
			"private_link_configuration": map[string]any{
				"kind": "PrivateLinkConfigurationLink",
				"href": ocmClusters + "/" + id + "/aws/private_link_configuration",
			},
			"audit_log":                map[string]any{"role_arn": ""},
			"ec2_metadata_http_tokens": "optional",
		}
		cluster["aws_infrastructure_access_role_grants"] = map[string]any{
			"kind": "AWSInfrastructureAccessRoleGrantLink",
			"href": ocmClusters + "/" + id + "/aws_infrastructure_access_role_grants",
		}
	case "gcp":
		region := gcpRegions[i%len(gcpRegions)]
		cluster["region"] = cloudRegion("gcp", region)
		cluster["gcp"] = map[string]any{
			"project_id":     "",
			"security":       map[string]any{"secure_boot": false},
			"authentication": map[string]any{"kind": "RedHatCloudAccount"},
		}
	}
}

func cloudProviderLink(id string) map[string]any {
	return map[string]any{
		"kind": "CloudProviderLink",
		"id":   id,
		"href": ocmBase + "/cloud_providers/" + id,
	}
}

func cloudRegion(provider, region string) map[string]any {
	return map[string]any{
		"kind": "CloudRegionLink",
		"id":   region,
		"href": ocmBase + "/cloud_providers/" + provider + "/regions/" + region,
	}
}

func addVersionInfo(cluster map[string]any, i, minor, patch int) {
	upgrades := make([]string, 0, maxUpgradePatches+23)
	for p := patch + 1; p <= patch+maxUpgradePatches; p++ {
		upgrades = append(upgrades, fmt.Sprintf("4.%d.%d", minor, p))
	}
	nextMinor := minor + 1
	for p := 37; p <= 59; p += 1 + (i % 3) {
		upgrades = append(upgrades, fmt.Sprintf("4.%d.%d", nextMinor, p))
	}
	cluster["version"] = map[string]any{
		"kind":                  "Version",
		"id":                    fmt.Sprintf("openshift-v4.%d.%d", minor, patch),
		"href":                  fmt.Sprintf("%s/versions/openshift-v4.%d.%d", ocmBase, minor, patch),
		"raw_id":                fmt.Sprintf("4.%d.%d", minor, patch),
		"channel_group":         "stable",
		"available_upgrades":    upgrades,
		"available_channels":    nil,
		"end_of_life_timestamp": fmt.Sprintf("2023-%02d-10T00:00:00Z", 1+(i%12)),
	}
}
