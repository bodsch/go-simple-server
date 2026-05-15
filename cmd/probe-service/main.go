// Command probe-service runs an HTTP service that exposes liveness and
// readiness probes with a configurable startup delay and admin endpoints
// to reset probe state. It is intended for orchestration testing
// (Kubernetes readiness/liveness, load-balancer health checks, etc.).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"bodsch.me/probe-service/internal/config"
	"bodsch.me/probe-service/internal/logging"
	"bodsch.me/probe-service/internal/server"
)

// Build metadata, populated at link time by GoReleaser via:
//
//	go build -ldflags "-X main.version=... -X main.commit=... -X main.date=..."
//
// They are intentionally exported as plain package-level strings so any
// build tool (GoReleaser, Make, Bazel) can fill them without reflection.
// The runtime VERSION env var (handled by config.Load) still wins for the
// HTTP responses; these values exist primarily for the startup log line
// and for embedding build provenance into the binary.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// main is the process entrypoint. It loads configuration, builds a logger
// and server, wires signal-based cancellation, and forwards non-trivial
// errors to the OS as a non-zero exit code.
func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	log := logging.New(cfg.LogLevel)
	log.Info("build info",
		"version", version,
		"commit", commit,
		"date", date,
	)

	srv, err := server.New(cfg, log)
	if err != nil {
		log.Error("server build failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server terminated", "err", err)
		os.Exit(1)
	}
}
