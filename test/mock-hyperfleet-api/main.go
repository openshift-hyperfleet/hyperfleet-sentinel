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
	defaultCreatedTime  = "2025-01-01T09:00:00Z"
	defaultUpdatedTime  = "2025-01-01T10:00:00Z"
	defaultUser         = "test-user@example.com"
)

func main() {
	clusterCount := defaultClusterCount

	if envCount := os.Getenv("CLUSTER_COUNT"); envCount != "" {
		n, err := strconv.Atoi(envCount)
		if err != nil {
			log.Printf("Invalid CLUSTER_COUNT %q: %v, using default %d", envCount, err, clusterCount)
		} else {
			clusterCount = n
		}
	}

	clusterList := createClusterList(clusterCount)

	clusterBytes, err := json.Marshal(clusterList)
	if err != nil {
		log.Fatalf("Failed to marshal clusters: %v", err)
	}

	addr := fmt.Sprintf(":%d", port)

	http.HandleFunc(clustersPath, jsonHandler(clusterBytes))

	log.Printf("Mock Hyperfleet API Server running on %s", addr)
	log.Printf("clusterCount=%d", clusterCount)

	server := &http.Server{
		Addr:         addr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
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
	now := time.Now()
	clusters := make([]map[string]any, 0, count)
	lastUpdated := now.Add(-35 * time.Minute).Format(time.RFC3339)

	for i := range count {
		id := fmt.Sprintf("cluster-%d", i)

		condition := func(condType string) map[string]any {
			return map[string]any{
				"type":                 condType,
				"status":               "True",
				"created_time":         defaultCreatedTime,
				"last_transition_time": defaultUpdatedTime,
				"last_updated_time":    lastUpdated,
				"observed_generation":  1,
			}
		}

		cluster := map[string]any{
			"id":           id,
			"href":         clustersPath + "/" + id,
			"kind":         "Cluster",
			"name":         id,
			"generation":   1,
			"created_time": defaultCreatedTime,
			"updated_time": defaultUpdatedTime,
			"created_by":   defaultUser,
			"updated_by":   defaultUser,
			"spec":         map[string]any{},
			"status": map[string]any{
				"conditions": []map[string]any{
					condition("Ready"),
					condition("Available"),
				},
			},
		}

		clusters = append(clusters, cluster)
	}

	return map[string]any{
		"kind":  "ClusterList",
		"page":  1,
		"size":  count,
		"total": count,
		"items": clusters,
	}
}
