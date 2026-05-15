package server

import (
	"net/http"

	"bodsch.me/probe-service/internal/config"
	"bodsch.me/probe-service/internal/flagx"
)

// registerRoutes attaches all HTTP routes to mux. Liveness and readiness
// each have two URL aliases (the Kubernetes-style /healthz | /readyz and
// the Spring Actuator-style paths) but share a single handler closure.
func registerRoutes(mux *http.ServeMux, cfg config.Config, health, ready *flagx.DelayedFlag) {
	liveness := probeHandler(health, livenessLabels, cfg.ServiceName, cfg.Version)
	readiness := probeHandler(ready, readinessLabels, cfg.ServiceName, cfg.Version)

	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/actuator/health/liveness", liveness)
	mux.HandleFunc("/readyz", readiness)
	mux.HandleFunc("/actuator/health/readiness", readiness)

	delayStr := cfg.StartupDelay.String()

	mux.HandleFunc("/admin/reset", resetHandler(delayStr,
		resetTarget{stateKey: "health", remainingKey: "health_in_ms", flag: health},
		resetTarget{stateKey: "ready", remainingKey: "ready_in_ms", flag: ready},
	))
	mux.HandleFunc("/admin/health/reset", resetHandler(delayStr,
		resetTarget{stateKey: "health", remainingKey: "health_in_ms", flag: health},
	))
	mux.HandleFunc("/admin/ready/reset", resetHandler(delayStr,
		resetTarget{stateKey: "ready", remainingKey: "ready_in_ms", flag: ready},
	))
}
