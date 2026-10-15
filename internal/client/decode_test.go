package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const (
	recordedResourceListWithLabelsAndReferences = `{
		"page": 1,
		"size": 1,
		"total": 1,
		"items": [
			{
				"id": "cluster-rec-1",
				"href": "/api/hyperfleet/v1/clusters/cluster-rec-1",
				"kind": "Cluster",
				"name": "cluster-rec-1",
				"generation": 7,
				"created_by": "test-user@example.com",
				"created_time": "2025-06-01T09:00:00Z",
				"updated_by": "test-user@example.com",
				"updated_time": "2025-06-01T10:00:00Z",
				"deleted_by": null,
				"deleted_time": null,
				"tenancy": {"org_id": "org-1"},
				"labels": {"region": "us-east", "env": "production"},
				"spec": {"version": "4.18"},
				"references": {
					"wif_config": [
						{"id": "wc-xyz", "href": "/api/hyperfleet/v1/wifconfigs/wc-xyz", "kind": "WifConfig"}
					],
					"network": [
						{"id": "net-1", "href": "/api/hyperfleet/v1/networks/net-1", "kind": "Network"},
						{"id": "net-2", "href": "/api/hyperfleet/v1/networks/net-2", "kind": "Network"}
					]
				},
				"status": {
					"conditions": [
						{
							"type": "Reconciled",
							"status": "True",
							"reason": "AllAdaptersReported",
							"message": "all adapters reported success",
							"created_time": "2025-06-01T09:05:00Z",
							"last_transition_time": "2025-06-01T09:10:00Z",
							"last_updated_time": "2025-06-01T09:15:00Z",
							"observed_generation": 7
						}
					]
				}
			}
		]
	}`

	recordedResourceListWithOwnerReference = `{
		"page": 1,
		"size": 1,
		"total": 1,
		"items": [
			{
				"id": "nodepool-rec-1",
				"href": "/api/hyperfleet/v1/nodepools/nodepool-rec-1",
				"kind": "NodePool",
				"name": "workers",
				"generation": 3,
				"created_time": "2025-06-01T09:00:00Z",
				"updated_time": "2025-06-01T10:00:00Z",
				"owner_references": {
					"id": "cluster-rec-1",
					"href": "/api/hyperfleet/v1/clusters/cluster-rec-1",
					"kind": "Cluster"
				},
				"spec": {"replicas": 3},
				"status": {
					"conditions": [
						{
							"type": "Reconciled",
							"status": "True",
							"created_time": "2025-06-01T09:05:00Z",
							"last_transition_time": "2025-06-01T09:10:00Z",
							"last_updated_time": "2025-06-01T09:15:00Z",
							"observed_generation": 3
						}
					]
				}
			}
		]
	}`

	recordedResourceListEmptyPage = `{"page": 1, "size": 0, "total": 0, "items": []}`

	recordedResourceListPage1Of2 = `{
		"page": 1,
		"size": 2,
		"total": 3,
		"items": [
			{
				"id": "cluster-page-1", "href": "/api/hyperfleet/v1/clusters/cluster-page-1",
				"kind": "Cluster", "name": "cluster-page-1", "generation": 1,
				"created_time": "2025-06-01T09:00:00Z", "updated_time": "2025-06-01T10:00:00Z",
				"spec": {}, "status": {"conditions": []}
			},
			{
				"id": "cluster-page-2", "href": "/api/hyperfleet/v1/clusters/cluster-page-2",
				"kind": "Cluster", "name": "cluster-page-2", "generation": 1,
				"created_time": "2025-06-01T09:00:00Z", "updated_time": "2025-06-01T10:00:00Z",
				"spec": {}, "status": {"conditions": []}
			}
		]
	}`

	recordedResourceListPage2Of2 = `{
		"page": 2,
		"size": 1,
		"total": 3,
		"items": [
			{
				"id": "cluster-page-3", "href": "/api/hyperfleet/v1/clusters/cluster-page-3",
				"kind": "Cluster", "name": "cluster-page-3", "generation": 1,
				"created_time": "2025-06-01T09:00:00Z", "updated_time": "2025-06-01T10:00:00Z",
				"spec": {}, "status": {"conditions": []}
			}
		]
	}`
)

func TestFetchResources_RecordedResponse(t *testing.T) {
	tests := []struct {
		check            func(t *testing.T, resources []Resource)
		name             string
		resourceType     string
		pages            []string
		wantRequestCount int
	}{
		{
			name:             "labels and references",
			resourceType:     "clusters",
			pages:            []string{recordedResourceListWithLabelsAndReferences},
			wantRequestCount: 1,
			check: func(t *testing.T, resources []Resource) {
				t.Helper()
				if len(resources) != 1 {
					t.Fatalf("Expected 1 resource, got %d", len(resources))
				}
				r := resources[0]
				if r.ID != "cluster-rec-1" {
					t.Errorf("Expected ID cluster-rec-1, got %s", r.ID)
				}
				if r.Href != "/api/hyperfleet/v1/clusters/cluster-rec-1" {
					t.Errorf("Unexpected Href: %s", r.Href)
				}
				if r.Generation != 7 {
					t.Errorf("Expected generation 7, got %d", r.Generation)
				}
				if r.Labels["region"] != "us-east" || r.Labels["env"] != "production" {
					t.Errorf("Unexpected labels: %v", r.Labels)
				}
				if r.OwnerReferences != nil {
					t.Errorf("Expected no owner references, got %v", r.OwnerReferences)
				}
				if len(r.References["wif_config"]) != 1 || r.References["wif_config"][0].ID != "wc-xyz" {
					t.Errorf("Unexpected wif_config references: %v", r.References["wif_config"])
				}
				if len(r.References["network"]) != 2 {
					t.Errorf("Expected 2 network references, got %d", len(r.References["network"]))
				}
				if len(r.Status.Conditions) != 1 {
					t.Fatalf("Expected 1 condition, got %d", len(r.Status.Conditions))
				}
				cond := r.Status.Conditions[0]
				if cond.Type != "Reconciled" || cond.Status != "True" || cond.Reason != "AllAdaptersReported" {
					t.Errorf("Unexpected condition: %+v", cond)
				}
				if cond.ObservedGeneration != 7 {
					t.Errorf("Expected observed generation 7, got %d", cond.ObservedGeneration)
				}
			},
		},
		{
			name:             "owner reference",
			resourceType:     "nodepools",
			pages:            []string{recordedResourceListWithOwnerReference},
			wantRequestCount: 1,
			check: func(t *testing.T, resources []Resource) {
				t.Helper()
				if len(resources) != 1 {
					t.Fatalf("Expected 1 resource, got %d", len(resources))
				}
				r := resources[0]
				if r.OwnerReferences == nil {
					t.Fatal("Expected owner references to be set")
				}
				if r.OwnerReferences.ID != "cluster-rec-1" || r.OwnerReferences.Kind != "Cluster" {
					t.Errorf("Unexpected owner reference: %+v", r.OwnerReferences)
				}
			},
		},
		{
			name:             "empty page",
			resourceType:     "clusters",
			pages:            []string{recordedResourceListEmptyPage},
			wantRequestCount: 1,
			check: func(t *testing.T, resources []Resource) {
				t.Helper()
				if len(resources) != 0 {
					t.Errorf("Expected 0 resources, got %d", len(resources))
				}
			},
		},
		{
			name:             "multi-page pagination",
			resourceType:     "clusters",
			pages:            []string{recordedResourceListPage1Of2, recordedResourceListPage2Of2},
			wantRequestCount: 2,
			check: func(t *testing.T, resources []Resource) {
				t.Helper()
				if len(resources) != 3 {
					t.Fatalf("Expected 3 resources across pages, got %d", len(resources))
				}
				if resources[0].ID != "cluster-page-1" || resources[2].ID != "cluster-page-3" {
					t.Errorf("Unexpected resource ordering: %v", resources)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rawPage := r.URL.Query().Get("page")
				page, err := strconv.Atoi(rawPage)
				if err != nil {
					t.Errorf("Invalid page requested: %q", rawPage)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if page < 1 || page > len(tt.pages) {
					t.Errorf("Unexpected page requested: %d", page)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requestCount++
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tt.pages[page-1])); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, 10*time.Second)
			resources, err := client.FetchResources(context.Background(), tt.resourceType, nil)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if requestCount != tt.wantRequestCount {
				t.Errorf("Expected %d requests, got %d", tt.wantRequestCount, requestCount)
			}
			tt.check(t, resources)
		})
	}
}
