package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bodsch.me/probe-service/internal/config"
)

// newTestServer builds a Server with a discarding logger and a zero
// startup delay so probes return 200 immediately.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		Port:         0, // unused; tests do not bind a port
		StartupDelay: 0,
		ServiceName:  "probe-service-test",
		Version:      "0.0.0-test",
		ShutdownWait: time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		IdleTimeout:  time.Second,
		MaxBodyBytes: 1 << 16,
		LogLevel:     slog.LevelInfo,
	}
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv
}

// do executes a request against the Server's root handler.
func do(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// decodeBody parses the response body as a JSON map.
func decodeBody(t *testing.T, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

// TestLivenessRoutes_OK ensures that with a zero startup delay both
// liveness routes return 200 with the expected JSON envelope.
func TestLivenessRoutes_OK(t *testing.T) {
	srv := newTestServer(t)

	for _, p := range []string{"/healthz", "/actuator/health/liveness"} {
		t.Run(p, func(t *testing.T) {
			res := do(t, srv, http.MethodGet, p)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.Code)
			}
			body := decodeBody(t, res)
			if body["status"] != "ok" {
				t.Errorf("status = %v, want ok", body["status"])
			}
			if body["service"] != "probe-service-test" {
				t.Errorf("service = %v, want probe-service-test", body["service"])
			}
			if body["version"] != "0.0.0-test" {
				t.Errorf("version = %v, want 0.0.0-test", body["version"])
			}
		})
	}
}

// TestReadinessRoutes_OK is the readiness counterpart of TestLivenessRoutes_OK.
func TestReadinessRoutes_OK(t *testing.T) {
	srv := newTestServer(t)

	for _, p := range []string{"/readyz", "/actuator/health/readiness"} {
		t.Run(p, func(t *testing.T) {
			res := do(t, srv, http.MethodGet, p)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.Code)
			}
			body := decodeBody(t, res)
			if body["status"] != "ready" {
				t.Errorf("status = %v, want ready", body["status"])
			}
		})
	}
}

// TestProbe_NotReady_WhenDelayActive verifies that with a long startup
// delay, the liveness probe returns 503 and includes retry_after_ms > 0.
func TestProbe_NotReady_WhenDelayActive(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		Port:         0,
		StartupDelay: 5 * time.Second,
		ServiceName:  "probe-service-test",
		Version:      "0.0.0-test",
		ShutdownWait: time.Second,
		MaxBodyBytes: 1 << 16,
	}
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	res := do(t, srv, http.MethodGet, "/healthz")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Code)
	}
	body := decodeBody(t, res)
	if body["status"] != "unhealthy" {
		t.Errorf("status = %v, want unhealthy", body["status"])
	}
	if n, _ := body["retry_after_ms"].(float64); n <= 0 {
		t.Errorf("retry_after_ms = %v, want > 0", body["retry_after_ms"])
	}
}

// TestMethodNotAllowed ensures non-GET on probes and non-POST on admin
// endpoints return 405 with the documented error code.
func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/healthz"},
		{http.MethodPut, "/readyz"},
		{http.MethodGet, "/admin/reset"},
		{http.MethodGet, "/admin/health/reset"},
		{http.MethodGet, "/admin/ready/reset"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			res := do(t, srv, c.method, c.path)
			if res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", res.Code)
			}
			body := decodeBody(t, res)
			if body["error"] != "method_not_allowed" {
				t.Errorf("error = %v, want method_not_allowed", body["error"])
			}
		})
	}
}

// TestServiceVersionHeader confirms the header middleware fires on every
// response, including the 503 case.
func TestServiceVersionHeader(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		Port:         0,
		StartupDelay: 5 * time.Second, // produce 503
		ServiceName:  "probe-service-test",
		Version:      "v9.9.9",
		ShutdownWait: time.Second,
		MaxBodyBytes: 1 << 16,
	}
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	res := do(t, srv, http.MethodGet, "/healthz")
	if got := res.Header().Get("X-Service-Version"); got != "v9.9.9" {
		t.Errorf("X-Service-Version = %q, want v9.9.9", got)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want application/json…", res.Header().Get("Content-Type"))
	}
}

// TestAdminReset_BothFlags exercises POST /admin/reset and verifies the
// response shape: both flags reported with their *_in_ms remaining times.
func TestAdminReset_BothFlags(t *testing.T) {
	srv := newTestServer(t)

	res := do(t, srv, http.MethodPost, "/admin/reset")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	body := decodeBody(t, res)
	for _, key := range []string{"health", "ready", "delay", "time", "health_in_ms", "ready_in_ms"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing %q: %v", key, body)
		}
	}
	if body["health"] != false {
		t.Errorf("health = %v, want false", body["health"])
	}
	if body["ready"] != false {
		t.Errorf("ready = %v, want false", body["ready"])
	}
}

// TestAdminReset_OnlyHealth ensures the targeted reset endpoints only
// touch their own flag in the response payload.
func TestAdminReset_OnlyHealth(t *testing.T) {
	srv := newTestServer(t)

	res := do(t, srv, http.MethodPost, "/admin/health/reset")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	body := decodeBody(t, res)
	if _, ok := body["health"]; !ok {
		t.Error("expected health field in response")
	}
	if _, ok := body["ready"]; ok {
		t.Error("did not expect ready field in /admin/health/reset response")
	}
}
