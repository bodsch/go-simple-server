// Package main implements a small HTTP service that exposes liveness/readiness probes
// with a configurable startup delay and admin endpoints to reset probe state.
// The service is intended for debugging and orchestration testing (e.g., Kubernetes).
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// cfg holds all runtime configuration derived from environment variables.
// Values are intentionally kept simple and service-centric (timeouts, port, limits).
type cfg struct {
	Port         int
	StartupDelay time.Duration // applies to BOTH health+ready
	ServiceName  string
	Version      string
	ShutdownWait time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodyBytes int64
	LogLevel     slog.Level
}

// main configures and starts the HTTP server, sets up delayed health/readiness flags,
// registers endpoints, and performs graceful shutdown on SIGINT/SIGTERM.
func main() {
	c := loadCfg()
	log := newLogger(c)

	health := NewDelayedFlag(c.StartupDelay)
	ready := NewDelayedFlag(c.StartupDelay)

	mux := http.NewServeMux()

	// health: 200 only if "healthy" flag is true
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if !health.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":         "unhealthy",
				"service":        c.ServiceName,
				"version":        c.Version,
				"time":           time.Now().UTC().Format(time.RFC3339Nano),
				"retry_after_ms": health.Remaining().Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": c.ServiceName,
			"version": c.Version,
			"time":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	// ready: 200 only if "ready" flag is true
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if !ready.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":         "not-ready",
				"service":        c.ServiceName,
				"version":        c.Version,
				"time":           time.Now().UTC().Format(time.RFC3339Nano),
				"retry_after_ms": ready.Remaining().Milliseconds(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ready",
			"service": c.ServiceName,
			"version": c.Version,
			"time":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	// Admin: reset both flags back to "false" and re-apply the startup delay
	// (Intentionally POST-only. Add auth if you ever expose this beyond localhost.)
	mux.HandleFunc("/admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		health.Reset()
		ready.Reset()
		writeJSON(w, http.StatusOK, map[string]any{
			"health":       false,
			"ready":        false,
			"delay":        c.StartupDelay.String(),
			"time":         time.Now().UTC().Format(time.RFC3339Nano),
			"health_in_ms": health.Remaining().Milliseconds(),
			"ready_in_ms":  ready.Remaining().Milliseconds(),
		})
	})

	// /admin/health/reset resets only the health flag to false and restarts its delay timer.
	mux.HandleFunc("/admin/health/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		health.Reset()
		writeJSON(w, http.StatusOK, map[string]any{
			"health":       false,
			"delay":        c.StartupDelay.String(),
			"time":         time.Now().UTC().Format(time.RFC3339Nano),
			"health_in_ms": health.Remaining().Milliseconds(),
		})
	})

	// /admin/ready/reset resets only the ready flag to false and restarts its delay timer.
	mux.HandleFunc("/admin/ready/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		ready.Reset()
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":       false,
			"delay":       c.StartupDelay.String(),
			"time":        time.Now().UTC().Format(time.RFC3339Nano),
			"ready_in_ms": ready.Remaining().Milliseconds(),
		})
	})

	// srv is the configured HTTP server instance with timeouts and middleware.
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.Port),
		Handler:           withMiddleware(mux, log, c.MaxBodyBytes),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       c.ReadTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			return context.Background()
		},
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Error("listen failed", "addr", srv.Addr, "err", err)
		os.Exit(1)
	}

	log.Info("starting",
		"service", c.ServiceName,
		"version", c.Version,
		"addr", srv.Addr,
		"startup_delay", c.StartupDelay.String(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case err := <-errCh:
		if err != nil {
			log.Error("server error", "err", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), c.ShutdownWait)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}

/*
Delayed flag (atomic + safe reset)
*/

// DelayedFlag represents a boolean state that becomes true only after a configured delay.
// It is safe for concurrent use and supports Reset() without old timers flipping the flag.
type DelayedFlag struct {
	delay    time.Duration
	val      atomic.Bool
	deadline atomic.Int64 // unix nanos; 0 = none
	gen      atomic.Uint64

	mu    sync.Mutex
	timer *time.Timer
}

// NewDelayedFlag creates a DelayedFlag and immediately schedules it to flip to true
// after the given delay. A non-positive delay makes the flag true immediately.
func NewDelayedFlag(delay time.Duration) *DelayedFlag {
	f := &DelayedFlag{delay: delay}
	f.Reset()
	return f
}

// Load returns the current state of the flag.
func (f *DelayedFlag) Load() bool { return f.val.Load() }

// Reset sets the flag to false and schedules it to flip to true after the configured delay.
// It can be called repeatedly; a generation guard ensures older timers do not win.
func (f *DelayedFlag) Reset() {
	g := f.gen.Add(1)

	f.val.Store(false)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.timer != nil {
		_ = f.timer.Stop()
		f.timer = nil
	}

	if f.delay <= 0 {
		f.deadline.Store(0)
		f.val.Store(true)
		return
	}

	dl := time.Now().Add(f.delay).UnixNano()
	f.deadline.Store(dl)

	f.timer = time.AfterFunc(f.delay, func() {
		if f.gen.Load() != g {
			return
		}
		f.val.Store(true)
		f.deadline.Store(0)
	})
}

// Remaining returns the remaining time until the flag becomes true.
// If the flag is already true (or no deadline is set), it returns 0.
func (f *DelayedFlag) Remaining() time.Duration {
	dl := f.deadline.Load()
	if dl <= 0 {
		return 0
	}
	rem := time.Until(time.Unix(0, dl))
	if rem < 0 {
		return 0
	}
	return rem
}

/*
Middleware + logging (JSON)
*/

// withMiddleware composes all HTTP middlewares in a fixed order:
// body size limit -> request ID -> panic recovery -> access logging.
func withMiddleware(next http.Handler, log *slog.Logger, maxBodyBytes int64) http.Handler {
	var h http.Handler = next
	h = maxBody(maxBodyBytes)(h)
	h = requestID()(h)
	h = recoverer(log)(h)
	h = accessLog(log)(h)
	return h
}

// middleware is a function that wraps an http.Handler with additional behavior.
type middleware func(http.Handler) http.Handler

// requestID adds/propagates a request ID for each request and exposes it as X-Request-Id.
// The ID is stored in the request context for use by downstream handlers/middlewares.
func requestID() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set("X-Request-Id", id)
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID{}, id))
			next.ServeHTTP(w, r)
		})
	}
}

// recoverer converts panics into HTTP 500 responses and logs the panic.
// This prevents crashing the whole process due to a single request handler bug.
func recoverer(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"panic", rec,
						"request_id", requestIDFromContext(r.Context()),
					)
					writeError(w, http.StatusInternalServerError, "internal_error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// maxBody applies a maximum request body size using http.MaxBytesReader.
// A non-positive max disables body limiting.
func maxBody(max int64) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if max > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// accessLog logs request/response metadata in structured JSON form.
func accessLog(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			log.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"ua", r.UserAgent(),
				"remote", r.RemoteAddr,
				"xff", r.Header.Get("X-Forwarded-For"),
				"request_id", requestIDFromContext(r.Context()),
			)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture status code and number of bytes written.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

// WriteHeader captures the status code before passing it to the underlying ResponseWriter.
func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the number of bytes written and forwards the write to the underlying ResponseWriter.
func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

/*
JSON responses
*/

// writeJSON writes a JSON response with the given status code.
// Encoding errors are intentionally ignored for simplicity (best-effort response).
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError writes a small, consistent JSON error response.
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{
		"error": code,
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

/*
Request-ID context
*/

// ctxKeyRequestID is a private context key type to avoid collisions with other packages.
type ctxKeyRequestID struct{}

// requestIDFromContext returns the request ID stored in the context or an empty string.
func requestIDFromContext(ctx context.Context) string {
	v := ctx.Value(ctxKeyRequestID{})
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// newRequestID creates a URL-safe, compact, random request ID.
func newRequestID() string {
	var b [18]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

/*
Config + logger
*/

// loadCfg reads configuration from environment variables and applies defaults.
// Some values are validated and will cause the process to exit on invalid input.
func loadCfg() cfg {
	port := mustEnvInt("PORT", 8080)
	startupDelay := mustEnvDuration("STARTUP_DELAY", 30*time.Second)

	serviceName := envStr("SERVICE_NAME", "simple-api")
	version := envStr("VERSION", "0.1.0")

	shutdownWait := mustEnvDuration("SHUTDOWN_WAIT", 10*time.Second)

	readTimeout := mustEnvDuration("READ_TIMEOUT", 15*time.Second)
	writeTimeout := mustEnvDuration("WRITE_TIMEOUT", 15*time.Second)
	idleTimeout := mustEnvDuration("IDLE_TIMEOUT", 60*time.Second)

	maxBody := mustEnvInt64("MAX_BODY_BYTES", 1<<20) // 1 MiB default

	level := parseSlogLevel(envStr("LOG_LEVEL", "info"))

	return cfg{
		Port:         port,
		StartupDelay: startupDelay,
		ServiceName:  serviceName,
		Version:      version,
		ShutdownWait: shutdownWait,

		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		MaxBodyBytes: maxBody,
		LogLevel:     level,
	}
}

// newLogger constructs a JSON slog logger writing to stdout with the configured level.
func newLogger(c cfg) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogLevel})
	return slog.New(h)
}

// envStr returns the environment variable value for k, or def if it is empty/whitespace.
func envStr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

// mustEnvInt reads an integer environment variable and validates a sane port-like range.
// On invalid value, the process exits with a non-zero status.
func mustEnvInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > 65535 {
		fmt.Fprintf(os.Stderr, "invalid %s=%q\n", k, v)
		os.Exit(2)
	}
	return n
}

// mustEnvInt64 reads an int64 environment variable and enforces it is positive.
// On invalid value, the process exits with a non-zero status.
func mustEnvInt64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "invalid %s=%q\n", k, v)
		os.Exit(2)
	}
	return n
}

// mustEnvDuration reads a duration environment variable (time.ParseDuration) and validates it is non-negative.
// On invalid value, the process exits with a non-zero status.
func mustEnvDuration(k string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		fmt.Fprintf(os.Stderr, "invalid %s=%q\n", k, v)
		os.Exit(2)
	}
	return d
}

// parseSlogLevel converts a string into a slog.Level with a conservative default of info.
func parseSlogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

/*
(Optional) drain body for keep-alive safety in some handlers
*/

// drainBody reads and closes the request body to allow connection reuse in keep-alive scenarios.
// It is currently unused but can be helpful for handlers that abort early.
func drainBody(r *http.Request) {
	if r.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
}
