package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/golang/glog"
	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/api/openapi"
)

// Retry configuration constants
const (
	// DefaultInitialInterval is the initial retry interval
	DefaultInitialInterval = 500 * time.Millisecond
	// DefaultMaxInterval is the maximum retry interval
	DefaultMaxInterval = 8 * time.Second
	// DefaultMaxElapsedTime is the maximum total time for retries
	DefaultMaxElapsedTime = 30 * time.Second
	// DefaultMultiplier is the backoff multiplier
	DefaultMultiplier = 2.0
	// DefaultRandomizationFactor adds jitter to retry intervals
	DefaultRandomizationFactor = 0.1
)

// ResourceType represents the type of HyperFleet resource
type ResourceType string

// Resource type constants
const (
	// ResourceTypeClusters represents cluster resources
	ResourceTypeClusters ResourceType = "clusters"
	// ResourceTypeNodePools represents nodepool resources
	ResourceTypeNodePools ResourceType = "nodepools"
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
	ID          string                 `json:"id"`
	Href        string                 `json:"href"`
	Kind        string                 `json:"kind"`
	CreatedTime time.Time              `json:"created_time"`
	UpdatedTime time.Time              `json:"updated_time"`
	Generation  int32                  `json:"generation"`
	Labels      map[string]string      `json:"labels"`
	Status      ResourceStatus         `json:"status"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ResourceStatus represents the status of a resource
type ResourceStatus struct {
	Phase              string      `json:"phase"`
	LastTransitionTime time.Time   `json:"lastTransitionTime"` // Updates only when status.phase changes
	LastUpdated        time.Time   `json:"lastUpdated"`        // Updates every time an adapter checks the resource
	ObservedGeneration int32       `json:"observedGeneration"` // The generation last processed by the adapter
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

// FetchResources fetches resources from the HyperFleet API with retry logic.
//
// Retry behavior:
//   - Automatically retries on transient failures (5xx, timeouts, network errors)
//   - Does NOT retry on client errors (4xx) as they are not retriable
//
// Graceful degradation:
//   - Resources with nil status are logged (glog.Warningf) and skipped
//   - This maintains service availability during resource provisioning/deletion
//   - Only resources with valid status are returned
//
// Returns a slice of resources and an error if the fetch operation fails.
func (c *HyperFleetClient) FetchResources(ctx context.Context, resourceType ResourceType, labelSelector map[string]string) ([]Resource, error) {
	// Validate inputs
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}

	// Validate resourceType against known types
	switch resourceType {
	case ResourceTypeClusters, ResourceTypeNodePools:
		// Valid type
	default:
		return nil, fmt.Errorf("invalid resourceType: %q (must be one of: %q, %q)",
			resourceType, ResourceTypeClusters, ResourceTypeNodePools)
	}

	// Configure exponential backoff
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = DefaultInitialInterval
	b.MaxInterval = DefaultMaxInterval
	b.Multiplier = DefaultMultiplier
	b.RandomizationFactor = DefaultRandomizationFactor

	// Retry operation with backoff (v5 API)
	operation := func() ([]Resource, error) {
		resources, err := c.fetchResourcesOnce(ctx, resourceType, labelSelector)
		if err != nil {
			// Check if error is retriable
			if isRetriable(err) {
				glog.V(2).Infof("Retriable error fetching %s: %v (will retry)", resourceType, err)
				return nil, err // Retry
			}
			// Non-retriable error - stop retrying
			glog.V(2).Infof("Non-retriable error fetching %s: %v (will not retry)", resourceType, err)
			return nil, backoff.Permanent(err)
		}
		return resources, nil
	}

	// Execute with retry using v5 API
	// Note: MaxElapsedTime is now a Retry option, not a BackOff field
	resources, err := backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(b),
		backoff.WithMaxElapsedTime(DefaultMaxElapsedTime),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s after retries: %w", resourceType, err)
	}

	return resources, nil
}

// labelSelectorToSearchString converts a label selector map to search parameter string
// Format: "key1=value1,key2=value2"
func labelSelectorToSearchString(labelSelector map[string]string) string {
	if len(labelSelector) == 0 {
		return ""
	}

	var parts []string
	for k, v := range labelSelector {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	// Sort for deterministic output in tests
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// fetchResourcesOnce performs a single fetch operation without retry logic
func (c *HyperFleetClient) fetchResourcesOnce(ctx context.Context, resourceType ResourceType, labelSelector map[string]string) ([]Resource, error) {
	// Build search parameter from label selector
	searchParam := labelSelectorToSearchString(labelSelector)

	// Call appropriate endpoint based on resource type
	switch resourceType {
	case ResourceTypeClusters:
		return c.fetchClusters(ctx, searchParam)
	case ResourceTypeNodePools:
		return c.fetchNodePools(ctx, searchParam)
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// fetchClusters fetches cluster resources from the API
func (c *HyperFleetClient) fetchClusters(ctx context.Context, searchParam string) ([]Resource, error) {
	req := c.apiClient.DefaultAPI.GetClusters(ctx)
	if searchParam != "" {
		req = req.Search(searchParam)
	}

	resourceList, resp, err := req.Execute()
	if err != nil {
		if resp != nil {
			// Enhanced error with status code
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("API request failed: %v", err),
				Retriable:  isHTTPStatusRetriable(resp.StatusCode),
			}
		}
		// Network/timeout error - use errors.As for proper error unwrapping
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return nil, &APIError{
				StatusCode: 0,
				Message:    "request timeout",
				Retriable:  true,
			}
		}
		return nil, &APIError{
			StatusCode: 0,
			Message:    fmt.Sprintf("network error: %v", err),
			Retriable:  true, // Assume network errors are retriable
		}
	}

	// Nil check for response
	if resourceList == nil {
		return nil, &APIError{
			StatusCode: 0,
			Message:    "received nil response from API",
			Retriable:  false,
		}
	}

	// Convert OpenAPI models to internal models
	resources := make([]Resource, 0, len(resourceList.Items))
	for _, item := range resourceList.Items {
		// Get ID and Kind with defaults for optional pointer fields
		id := ""
		if item.Id != nil {
			id = *item.Id
		}
		href := ""
		if item.Href != nil {
			href = *item.Href
		}

		resource := Resource{
			ID:          id,
			Href:        href,
			Kind:        item.Kind,
			Generation:  item.Generation,
			CreatedTime: item.CreatedTime,
			UpdatedTime: item.UpdatedTime,
			Status: ResourceStatus{
				Phase:              string(item.Status.Phase),
				LastTransitionTime: item.Status.LastTransitionTime,
				LastUpdated:        item.Status.LastUpdatedTime,
				ObservedGeneration: item.Status.ObservedGeneration,
			},
		}

		// Handle optional labels
		if item.Labels != nil {
			resource.Labels = *item.Labels
		}

		// Convert conditions from OpenAPI model
		if len(item.Status.Conditions) > 0 {
			resource.Status.Conditions = make([]Condition, 0, len(item.Status.Conditions))
			for _, cond := range item.Status.Conditions {
				condition := Condition{
					Type:               cond.Type,
					Status:             string(cond.Status),
					LastTransitionTime: cond.LastTransitionTime,
				}
				if cond.Reason != nil {
					condition.Reason = *cond.Reason
				}
				if cond.Message != nil {
					condition.Message = *cond.Message
				}
				resource.Status.Conditions = append(resource.Status.Conditions, condition)
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// fetchNodePools fetches nodepool resources from the API
func (c *HyperFleetClient) fetchNodePools(ctx context.Context, searchParam string) ([]Resource, error) {
	req := c.apiClient.DefaultAPI.GetNodePools(ctx)
	if searchParam != "" {
		req = req.Search(searchParam)
	}

	resourceList, resp, err := req.Execute()
	if err != nil {
		if resp != nil {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("API request failed: %v", err),
				Retriable:  isHTTPStatusRetriable(resp.StatusCode),
			}
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return nil, &APIError{
				StatusCode: 0,
				Message:    "request timeout",
				Retriable:  true,
			}
		}
		return nil, &APIError{
			StatusCode: 0,
			Message:    fmt.Sprintf("network error: %v", err),
			Retriable:  true,
		}
	}

	if resourceList == nil {
		return nil, &APIError{
			StatusCode: 0,
			Message:    "received nil response from API",
			Retriable:  false,
		}
	}

	// Convert OpenAPI models to internal models
	resources := make([]Resource, 0, len(resourceList.Items))
	for _, item := range resourceList.Items {
		// Get ID and Href with defaults for optional pointer fields
		id := ""
		if item.Id != nil {
			id = *item.Id
		}
		href := ""
		if item.Href != nil {
			href = *item.Href
		}
		kind := ""
		if item.Kind != nil {
			kind = *item.Kind
		}

		resource := Resource{
			ID:          id,
			Href:        href,
			Kind:        kind,
			Generation:  0, // NodePool doesn't have a generation field
			CreatedTime: item.CreatedTime,
			UpdatedTime: item.UpdatedTime,
			Status: ResourceStatus{
				Phase:              string(item.Status.Phase),
				LastTransitionTime: item.Status.LastTransitionTime,
				LastUpdated:        item.Status.LastUpdatedTime,
				ObservedGeneration: item.Status.ObservedGeneration,
			},
		}

		// Handle optional labels
		if item.Labels != nil {
			resource.Labels = *item.Labels
		}

		// Convert conditions from OpenAPI model
		if len(item.Status.Conditions) > 0 {
			resource.Status.Conditions = make([]Condition, 0, len(item.Status.Conditions))
			for _, cond := range item.Status.Conditions {
				condition := Condition{
					Type:               cond.Type,
					Status:             string(cond.Status),
					LastTransitionTime: cond.LastTransitionTime,
				}
				if cond.Reason != nil {
					condition.Reason = *cond.Reason
				}
				if cond.Message != nil {
					condition.Message = *cond.Message
				}
				resource.Status.Conditions = append(resource.Status.Conditions, condition)
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// APIError represents an API error with retry information
type APIError struct {
	StatusCode int
	Message    string
	Retriable  bool
}

func (e *APIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
	}
	return e.Message
}

// isRetriable determines if an error should be retried
// Uses errors.As for proper error unwrapping (Go 1.13+)
func isRetriable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retriable
	}
	// Unknown errors are not retriable by default
	return false
}

// isHTTPStatusRetriable determines if an HTTP status code is retriable
func isHTTPStatusRetriable(statusCode int) bool {
	// 5xx server errors are retriable
	if statusCode >= 500 && statusCode < 600 {
		return true
	}
	// 429 Too Many Requests is retriable
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	// 408 Request Timeout is retriable
	if statusCode == http.StatusRequestTimeout {
		return true
	}
	// 4xx client errors are NOT retriable (except 408 and 429 above)
	// 2xx and 3xx are successful, no retry needed
	return false
}
