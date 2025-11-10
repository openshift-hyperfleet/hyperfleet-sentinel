package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/api/openapi"
)

// HyperFleetClient wraps the OpenAPI-generated client
type HyperFleetClient struct {
	apiClient *openapi.APIClient
}

// NewHyperFleetClient creates a new HyperFleet API client using OpenAPI-generated client
func NewHyperFleetClient(endpoint string, timeout time.Duration) *HyperFleetClient {
	cfg := openapi.NewConfiguration()
	cfg.Servers = openapi.ServerConfigurations{
		{
			URL:         endpoint,
			Description: "HyperFleet API",
		},
	}
	cfg.HTTPClient = &http.Client{
		Timeout: timeout,
	}

	return &HyperFleetClient{
		apiClient: openapi.NewAPIClient(cfg),
	}
}

// Resource represents a HyperFleet resource (cluster, nodepool, etc.)
type Resource struct {
	ID       string                 `json:"id"`
	Href     string                 `json:"href"`
	Kind     string                 `json:"kind"`
	Labels   map[string]string      `json:"labels"`
	Status   ResourceStatus         `json:"status"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ResourceStatus represents the status of a resource
type ResourceStatus struct {
	Phase              string      `json:"phase"`
	LastTransitionTime time.Time   `json:"lastTransitionTime"` // Updates only when status.phase changes
	LastUpdated        time.Time   `json:"lastUpdated"`        // Updates every time an adapter checks the resource
	Conditions         []Condition `json:"conditions,omitempty"`
}

// Condition represents a status condition
type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
}

// FetchResources fetches resources from the HyperFleet API
func (c *HyperFleetClient) FetchResources(ctx context.Context, resourceType string, labelSelector map[string]string) ([]Resource, error) {
	// TODO: Update this when real spec supports different resource types
	// For now, only clusters endpoint is defined in the placeholder spec

	req := c.apiClient.DefaultAPI.ListClusters(ctx)
	if len(labelSelector) > 0 {
		req = req.Labels(labelSelector)
	}

	resourceList, resp, err := req.Execute()
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("failed to fetch resources: status %d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("failed to fetch resources: %w", err)
	}

	// Convert OpenAPI models to internal models
	resources := make([]Resource, 0, len(resourceList.Items))
	for _, item := range resourceList.Items {
		resource := Resource{
			ID:     item.GetId(),
			Href:   item.GetHref(),
			Kind:   item.GetKind(),
			Labels: item.GetLabels(),
			Status: ResourceStatus{
				Phase:              item.Status.GetPhase(),
				LastTransitionTime: item.Status.GetLastTransitionTime(),
				LastUpdated:        item.Status.GetLastUpdated(),
			},
			Metadata: item.GetMetadata(),
		}

		// Convert conditions
		if conditions := item.Status.GetConditions(); len(conditions) > 0 {
			resource.Status.Conditions = make([]Condition, 0, len(conditions))
			for _, cond := range conditions {
				resource.Status.Conditions = append(resource.Status.Conditions, Condition{
					Type:               cond.GetType(),
					Status:             cond.GetStatus(),
					LastTransitionTime: cond.GetLastTransitionTime(),
					Reason:             cond.GetReason(),
					Message:            cond.GetMessage(),
				})
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}
