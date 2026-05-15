// Package server assembles the HTTP server, its handlers, routing and
// graceful-shutdown logic.
package server

import (
	"net/http"

	"bodsch.me/probe-service/internal/flagx"
	"bodsch.me/probe-service/internal/httpx"
)

// probeLabels carries the two textual labels used in a probe handler's
// JSON output, one for the success case and one for the failure case.
type probeLabels struct {
	// up is the value of "status" when the underlying flag is true (200).
	up string
	// down is the value of "status" when the flag is false (503).
	down string
}

// livenessLabels are used by /healthz and /actuator/health/liveness.
var livenessLabels = probeLabels{up: "ok", down: "unhealthy"}

// readinessLabels are used by /readyz and /actuator/health/readiness.
var readinessLabels = probeLabels{up: "ready", down: "not-ready"}

// probeHandler builds a GET-only handler that reports the state of the
// supplied DelayedFlag. When the flag is true the handler returns 200
// and labels.up; when it is false it returns 503, labels.down, and the
// remaining time until the flag would flip.
//
// All probe responses share the same JSON envelope so that monitoring
// systems can parse them uniformly:
//
//	{
//	  "status":         "<labels.up | labels.down>",
//	  "service":        "<service name>",
//	  "version":        "<service version>",
//	  "retry_after_ms": <int, only present when not-up>,
//	  "time":           "<RFC3339>"
//	}
func probeHandler(flag *flagx.DelayedFlag, labels probeLabels, service, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if !flag.Load() {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":         labels.down,
				"service":        service,
				"version":        version,
				"retry_after_ms": flag.Remaining().Milliseconds(),
				"time":           httpx.NowRFC3339(),
			})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status":  labels.up,
			"service": service,
			"version": version,
			"time":    httpx.NowRFC3339(),
		})
	}
}

// resetTarget pairs a DelayedFlag with the JSON key under which its
// remaining delay should be reported by the reset handler.
type resetTarget struct {
	// stateKey is the JSON field name for the boolean state, e.g. "health".
	stateKey string
	// remainingKey is the JSON field name for the millisecond countdown,
	// e.g. "health_in_ms".
	remainingKey string
	// flag is the DelayedFlag to be reset by this handler.
	flag *flagx.DelayedFlag
}

// resetHandler builds a POST-only handler that calls Reset() on every
// target and returns a JSON description of the new state.
//
// The response always contains a "delay" field (the configured delay as
// a Go duration string), a "time" field, and for each target a state
// field set to false and a *_in_ms field with the remaining time.
func resetHandler(delayStr string, targets ...resetTarget) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		for _, t := range targets {
			t.flag.Reset()
		}

		body := map[string]any{
			"delay": delayStr,
			"time":  httpx.NowRFC3339(),
		}
		for _, t := range targets {
			body[t.stateKey] = false
			body[t.remainingKey] = t.flag.Remaining().Milliseconds()
		}
		httpx.WriteJSON(w, http.StatusOK, body)
	}
}
