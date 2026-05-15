// Package httpx provides reusable HTTP plumbing: JSON response helpers,
// middleware (request-id, panic recovery, body limits, access logging)
// and a status-capturing ResponseWriter.
package httpx

import (
	"encoding/json"
	"net/http"
	"time"
)

// WriteJSON serialises payload as JSON and writes it with the given status.
// The "Content-Type" header is always set. Encoding errors are intentionally
// ignored because no useful recovery is possible after WriteHeader.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteError writes a small, consistent JSON error response with the
// provided machine-readable code.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]any{
		"error": code,
		"time":  NowRFC3339(),
	})
}

// NowRFC3339 returns the current UTC time formatted as RFC3339 (no fractional
// seconds). Centralised so the format stays consistent across responses.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
