package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/openshift-hyperfleet/hyperfleet-sentinel/pkg/logger"
)

func TestNewReadinessChecker_DefaultNotReady(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	if rc.IsReady() {
		t.Error("Expected new ReadinessChecker to be not ready")
	}
}

func TestReadinessChecker_SetReady(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())

	rc.SetReady(true)
	if !rc.IsReady() {
		t.Error("Expected IsReady() to return true after SetReady(true)")
	}

	rc.SetReady(false)
	if rc.IsReady() {
		t.Error("Expected IsReady() to return false after SetReady(false)")
	}
}

func TestReadinessChecker_ConcurrentAccess(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rc.SetReady(true)
		}()
		go func() {
			defer wg.Done()
			rc.IsReady()
		}()
	}
	wg.Wait()
}

func TestHealthzHandler_AlwaysReturns200(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())

	tests := []struct {
		name  string
		ready bool
	}{
		{"when not ready", false},
		{"when ready", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc.SetReady(tt.ready)
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			w := httptest.NewRecorder()

			rc.HealthzHandler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", ct)
			}

			var resp healthResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
			if resp.Status != "ok" {
				t.Errorf("Expected status 'ok', got '%s'", resp.Status)
			}
		})
	}
}

func TestReadyzHandler_WhenNotReady(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", resp.Status)
	}
	if resp.Checks["broker"] != "unavailable" {
		t.Errorf("Expected broker check 'unavailable', got '%s'", resp.Checks["broker"])
	}
}

func TestReadyzHandler_WhenReady(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error { return nil })
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}
	if resp.Checks["broker"] != "ok" {
		t.Errorf("Expected broker check 'ok', got '%s'", resp.Checks["broker"])
	}
}

func TestReadyzHandler_TransitionOnShutdown(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error { return nil })
	rc.SetReady(true)

	// Verify ready
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	rc.ReadyzHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 when ready, got %d", w.Code)
	}

	// Simulate shutdown
	rc.SetReady(false)

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w = httptest.NewRecorder()
	rc.ReadyzHandler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 after shutdown, got %d", w.Code)
	}
}

func TestReadyzHandler_CheckFails(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error { return fmt.Errorf("connection refused") })
	rc.AddCheck("config", func() error { return nil })
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", resp.Status)
	}
	if resp.Checks["broker"] != "connection refused" {
		t.Errorf("Expected broker check 'connection refused', got '%s'", resp.Checks["broker"])
	}
	if resp.Checks["config"] != "ok" {
		t.Errorf("Expected config check 'ok', got '%s'", resp.Checks["config"])
	}
}

func TestReadyzHandler_NoChecksRegistered(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}
}

func TestReadyzHandler_ShutdownSkipsChecks(t *testing.T) {
	checkCalled := false
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error {
		checkCalled = true
		return nil
	})
	// ready=false means shutdown, checks should NOT be executed
	rc.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
	if checkCalled {
		t.Error("Expected checks NOT to be called during shutdown")
	}
}

func TestReadyzHandler_MultipleChecksAllPass(t *testing.T) {
	rc := NewReadinessChecker(logger.NewHyperFleetLogger())
	rc.AddCheck("broker", func() error { return nil })
	rc.AddCheck("config", func() error { return nil })
	rc.AddCheck("hyperfleet_api", func() error { return nil })
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(resp.Checks) != 3 {
		t.Errorf("Expected 3 checks, got %d", len(resp.Checks))
	}
	for name, status := range resp.Checks {
		if status != "ok" {
			t.Errorf("Expected check '%s' to be 'ok', got '%s'", name, status)
		}
	}
}
