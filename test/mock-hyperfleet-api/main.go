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
)

var adapterNames = []string{"validator", "dns", "provisioner", "networking"}

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
	// Knuth multiplicative hash (2654435761 ≈ 2^32 × φ⁻¹) to spread IDs across the hex space.
	return fmt.Sprintf("1%07x%08x%08x%08x", i%0xFFFFFFF, i*2654435761, i*40503, i*12345678)
}

func fakeUUID(i int) string {
	// Deterministic UUID v4: 0x4000 sets version nibble to 4, 0x8000 sets RFC 4122 variant bits.
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(i*2654435761), (i*40503)&0xFFFF,
		0x4000|((i*12345)&0x0FFF), 0x8000|((i*6789)&0x3FFF),
		(i*1099511627776+i)&0xFFFFFFFFFFFF)
}

func createCluster(i int) map[string]any {
	id := fakeClusterID(i)
	createdAt := time.Date(2020, time.Month(i%12)+1, 1+(i%28), i%24, i%60, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Duration(i%720) * time.Hour)
	generation := int32(1 + i%5)

	return map[string]any{
		"kind": "Cluster",
		"id":   id,
		"href": clustersPath + "/" + id,
		"name": fakeUUID(i),
		"labels": map[string]string{
			"environment": "production",
			"team":        "platform",
		},
		"spec":         map[string]any{},
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
