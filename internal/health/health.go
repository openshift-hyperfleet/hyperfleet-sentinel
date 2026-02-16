package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

// CheckFunc is a function that checks a specific dependency.
// It returns nil if the dependency is healthy, or an error describing the failure.
type CheckFunc func() error

// ReadinessChecker tracks the readiness state of the application and
// evaluates registered health checks on each /readyz request.
// It is goroutine-safe.
type ReadinessChecker struct {
	ready  atomic.Bool
	mu     sync.RWMutex
	checks map[string]CheckFunc
	logger logger.HyperFleetLogger
}

// NewReadinessChecker creates a new ReadinessChecker with ready=false and no checks.
func NewReadinessChecker(log logger.HyperFleetLogger) *ReadinessChecker {
	return &ReadinessChecker{
		checks: make(map[string]CheckFunc),
		logger: log,
	}
}

// AddCheck registers a named check function that will be evaluated on each /readyz request.
func (r *ReadinessChecker) AddCheck(name string, fn CheckFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks[name] = fn
}

// SetReady sets the readiness state. When set to false (e.g. during shutdown),
// /readyz returns 503 immediately without evaluating checks.
func (r *ReadinessChecker) SetReady(ready bool) {
	r.ready.Store(ready)
}

// IsReady returns the current readiness state.
func (r *ReadinessChecker) IsReady() bool {
	return r.ready.Load()
}

// healthResponse is the JSON response for /healthz.
type healthResponse struct {
	Status string `json:"status"`
}

// readyResponse is the JSON response for /readyz.
type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// writeJSON writes a JSON response with the given status code.
func (r *ReadinessChecker) writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		r.logger.Errorf(context.Background(), "Failed to encode health JSON response: %v", err)
	}
}

// HealthzHandler returns an http.HandlerFunc for the /healthz liveness endpoint.
// It always returns 200 OK with {"status":"ok"} since liveness only checks
// that the process can respond to HTTP requests.
func (r *ReadinessChecker) HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		r.writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}
}

// ReadyzHandler returns an http.HandlerFunc for the /readyz readiness endpoint.
// When ready=false (shutdown), it returns 503 immediately without running checks.
// When ready=true, it evaluates all registered checks and returns 200 if all pass,
// or 503 with details of which checks failed.
func (r *ReadinessChecker) ReadyzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !r.IsReady() {
			r.writeJSON(w, http.StatusServiceUnavailable, readyResponse{
				Status: "error",
				Checks: r.allChecksStatus("unavailable"),
			})
			return
		}

		checks := r.runChecks()
		allOK := true
		for _, v := range checks {
			if v != "ok" {
				allOK = false
				break
			}
		}

		if allOK {
			r.writeJSON(w, http.StatusOK, readyResponse{
				Status: "ok",
				Checks: checks,
			})
			return
		}

		r.writeJSON(w, http.StatusServiceUnavailable, readyResponse{
			Status: "error",
			Checks: checks,
		})
	}
}

// runChecks evaluates all registered check functions and returns a map of results.
func (r *ReadinessChecker) runChecks() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]string, len(r.checks))
	for name, fn := range r.checks {
		if err := fn(); err != nil {
			results[name] = err.Error()
		} else {
			results[name] = "ok"
		}
	}
	return results
}

// allChecksStatus returns a map with all registered check names set to the given status.
func (r *ReadinessChecker) allChecksStatus(status string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]string, len(r.checks))
	for name := range r.checks {
		results[name] = status
	}
	return results
}
