package resource

import (
	"time"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/internal/client"
)

// ToMap converts a Resource into a plain map[string]interface{} for CEL evaluation.
// Time fields are formatted as RFC3339Nano strings to match their JSON representation.
// Returns a zero-value map if r is nil.
func ToMap(r *client.Resource) map[string]interface{} {
	if r == nil {
		return map[string]interface{}{
			"id":           "",
			"href":         "",
			"kind":         "",
			"created_time": "",
			"updated_time": "",
			"generation":   int32(0),
			"status": map[string]interface{}{
				"ready":                false,
				"last_transition_time": "",
				"last_updated":         "",
				"observed_generation":  int32(0),
			},
		}
	}

	status := map[string]interface{}{
		"ready":                r.Status.Ready,
		"last_transition_time": r.Status.LastTransitionTime.Format(time.RFC3339Nano),
		"last_updated":         r.Status.LastUpdated.Format(time.RFC3339Nano),
		"observed_generation":  r.Status.ObservedGeneration,
	}
	if len(r.Status.Conditions) > 0 {
		conditions := make([]interface{}, len(r.Status.Conditions))
		for i, c := range r.Status.Conditions {
			cond := map[string]interface{}{
				"type":                 c.Type,
				"status":               c.Status,
				"last_transition_time": c.LastTransitionTime.Format(time.RFC3339Nano),
				"last_updated_time":    c.LastUpdatedTime.Format(time.RFC3339Nano),
				"observed_generation":  c.ObservedGeneration,
			}
			if c.Reason != "" {
				cond["reason"] = c.Reason
			}
			if c.Message != "" {
				cond["message"] = c.Message
			}
			conditions[i] = cond
		}
		status["conditions"] = conditions
	}

	m := map[string]interface{}{
		"id":           r.ID,
		"href":         r.Href,
		"kind":         r.Kind,
		"created_time": r.CreatedTime.Format(time.RFC3339Nano),
		"updated_time": r.UpdatedTime.Format(time.RFC3339Nano),
		"generation":   r.Generation,
		"status":       status,
	}

	if len(r.Labels) > 0 {
		labels := make(map[string]interface{}, len(r.Labels))
		for k, v := range r.Labels {
			labels[k] = v
		}
		m["labels"] = labels
	}

	if r.OwnerReferences != nil {
		m["owner_references"] = map[string]interface{}{
			"id":   r.OwnerReferences.ID,
			"href": r.OwnerReferences.Href,
			"kind": r.OwnerReferences.Kind,
		}
	}

	if r.Metadata != nil {
		m["metadata"] = r.Metadata
	}

	return m
}
