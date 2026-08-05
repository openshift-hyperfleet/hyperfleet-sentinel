package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testContentType = "application/json"
	testStatusError = "error"
)

func TestNewReadinessChecker_DefaultNotReady(t *testing.T) {
	rc := NewReadinessChecker()
	if rc.IsReady() {
		t.Error("Expected new ReadinessChecker to be not ready")
	}
}

func TestReadinessChecker_SetReady(t *testing.T) {
	rc := NewReadinessChecker()

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
	rc := NewReadinessChecker()
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

func TestHealthzHandler_InvalidParameters(t *testing.T) {
	tests := []struct {
		lastPollFn   func() time.Time
		name         string
		expectedBody string
		threshold    time.Duration
		expectedCode int
	}{
		{
			name:         "nil lastPollFn",
			lastPollFn:   nil,
			threshold:    15 * time.Second,
			expectedCode: http.StatusInternalServerError,
			expectedBody: "invalid health configuration",
		},
		{
			name:         "non-positive threshold",
			lastPollFn:   time.Now,
			threshold:    0,
			expectedCode: http.StatusInternalServerError,
			expectedBody: "invalid health configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewReadinessChecker()
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			w := httptest.NewRecorder()

			rc.HealthzHandler(tt.lastPollFn, tt.threshold).ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Fatalf("expected status %d, got %d", tt.expectedCode, w.Code)
			}

			var resp healthResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Status != tt.expectedBody {
				t.Fatalf("expected status %q, got %q", tt.expectedBody, resp.Status)
			}
		})
	}
}

func TestHealthzHandler_PollStates(t *testing.T) {
	tests := []struct {
		lastPoll       time.Time
		name           string
		expectedStatus string
		expectedCode   int
	}{
		{
			name:           "healthy poll",
			lastPoll:       time.Now().Add(-1 * time.Second),
			expectedCode:   http.StatusOK,
			expectedStatus: "ok",
		},
		{
			name:           "stale poll",
			lastPoll:       time.Now().Add(-20 * time.Second),
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "poll stale",
		},
		{
			name:           "pre-first poll",
			lastPoll:       time.Time{},
			expectedCode:   http.StatusOK,
			expectedStatus: "ok",
		},
	}

	// 3 * poll_interval (default 5s) = 15s;
	threshold := 15 * time.Second

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewReadinessChecker()
			lastPollFn := func() time.Time { return tt.lastPoll }

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			w := httptest.NewRecorder()

			rc.HealthzHandler(lastPollFn, threshold).ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
			}

			if ct := w.Header().Get("Content-Type"); ct != testContentType {
				t.Errorf("Expected Content-Type %s, got %s", testContentType, ct)
			}

			var resp healthResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
			if resp.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got '%s'", tt.expectedStatus, resp.Status)
			}
		})
	}
}

func TestReadyzHandler_PreFirstPollNotReady(t *testing.T) {
	rc := NewReadinessChecker()
	rc.AddCheck("broker", func() error { return nil })
	rc.AddCheck("sentinel_poll", func() error {
		return fmt.Errorf("no successful poll completed yet")
	})

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
	if resp.Status != testStatusError {
		t.Errorf("Expected status %q, got %s", testStatusError, resp.Status)
	}
	got, ok := resp.Checks["sentinel_poll"]
	if !ok {
		t.Fatalf("Expected sentinel_poll check to be present")
	}
	if got == "ok" {
		t.Errorf("Expected sentinel_poll check to fail before first poll")
	}
	if resp.Checks["broker"] != "ok" {
		t.Errorf("Expected broker 'ok', got %s", resp.Checks["broker"])
	}
}

func TestReadyzHandler_WhenNotReady(t *testing.T) {
	rc := NewReadinessChecker()
	rc.AddCheck("broker", func() error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != testContentType {
		t.Errorf("Expected Content-Type %s, got %s", testContentType, ct)
	}

	var resp readyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Status != testStatusError {
		t.Errorf("Expected status %q, got '%s'", testStatusError, resp.Status)
	}
	if resp.Checks["broker"] != "unavailable" {
		t.Errorf("Expected broker check 'unavailable', got '%s'", resp.Checks["broker"])
	}
}

func TestReadyzHandler_WhenReady(t *testing.T) {
	rc := NewReadinessChecker()
	rc.AddCheck("broker", func() error { return nil })
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != testContentType {
		t.Errorf("Expected Content-Type %s, got %s", testContentType, ct)
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
	rc := NewReadinessChecker()
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
	rc := NewReadinessChecker()
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
	if resp.Status != testStatusError {
		t.Errorf("Expected status %q, got '%s'", testStatusError, resp.Status)
	}
	if resp.Checks["broker"] != "connection refused" {
		t.Errorf("Expected broker check 'connection refused', got '%s'", resp.Checks["broker"])
	}
	if resp.Checks["config"] != "ok" {
		t.Errorf("Expected config check 'ok', got '%s'", resp.Checks["config"])
	}
}

func TestReadyzHandler_NoChecksRegistered(t *testing.T) {
	rc := NewReadinessChecker()
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
	rc := NewReadinessChecker()
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

func withLogCapture(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	return &buf, func() { slog.SetDefault(prev) }
}

func TestHealthzHandler_LogsOnInvalidConfig(t *testing.T) {
	buf, restore := withLogCapture(t)
	defer restore()

	rc := NewReadinessChecker()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	rc.HealthzHandler(nil, 15*time.Second).ServeHTTP(w, req)

	if !strings.Contains(buf.String(), "invalid configuration") {
		t.Errorf("Expected log to contain 'invalid configuration', got: %s", buf.String())
	}
}

func TestHealthzHandler_LogsOnStalePoll(t *testing.T) {
	buf, restore := withLogCapture(t)
	defer restore()

	rc := NewReadinessChecker()
	lastPollFn := func() time.Time { return time.Now().Add(-20 * time.Second) }
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	rc.HealthzHandler(lastPollFn, 15*time.Second).ServeHTTP(w, req)

	if !strings.Contains(buf.String(), "poll stale") {
		t.Errorf("Expected log to contain 'poll stale', got: %s", buf.String())
	}
}

func TestReadyzHandler_LogsOnCheckFailure(t *testing.T) {
	buf, restore := withLogCapture(t)
	defer restore()

	rc := NewReadinessChecker()
	rc.AddCheck("broker", func() error { return fmt.Errorf("connection refused") })
	rc.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	rc.ReadyzHandler().ServeHTTP(w, req)

	if !strings.Contains(buf.String(), "Readyz check failed") {
		t.Errorf("Expected log to contain 'Readyz check failed', got: %s", buf.String())
	}
}

func TestReadyzHandler_MultipleChecksAllPass(t *testing.T) {
	rc := NewReadinessChecker()
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
