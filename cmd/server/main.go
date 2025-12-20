package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

type cfg struct {
	Port         int
	ReadyAfter   time.Duration
	ServiceName  string
	Version      string
	ShutdownWait time.Duration
}

var readyFlag atomic.Bool

func main() {
	c := loadCfg()

	readyFlag.Store(false)
	if c.ReadyAfter <= 0 {
		readyFlag.Store(true)
	} else {
		go func() {
			time.Sleep(c.ReadyAfter)
			readyFlag.Store(true)
		}()
	}

	mux := http.NewServeMux()

	// Liveness: always OK if process is alive
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	// Readiness: OK only if readyFlag is true
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !readyFlag.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not-ready",
				"time":   time.Now().UTC().Format(time.RFC3339Nano),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ready",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	// Minimal API
	mux.HandleFunc("/api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": c.ServiceName,
			"version": c.Version,
			"pong":    true,
			"time":    time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	mux.HandleFunc("/api/v1/echo", func(w http.ResponseWriter, r *http.Request) {
		msg := r.URL.Query().Get("msg")
		if msg == "" {
			msg = "hello"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"echo": msg,
			"time": time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	// Optional: readiness toggles (useful to test Argo/K8s behavior)
	mux.HandleFunc("/admin/ready", func(w http.ResponseWriter, r *http.Request) {
		readyFlag.Store(true)
		writeJSON(w, http.StatusOK, map[string]any{"ready": true})
	})
	mux.HandleFunc("/admin/not-ready", func(w http.ResponseWriter, r *http.Request) {
		readyFlag.Store(false)
		writeJSON(w, http.StatusOK, map[string]any{"ready": false})
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.Port),
		Handler:           logging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("listen %s: %v", srv.Addr, err)
	}

	log.Printf("service=%s version=%s port=%d readyAfter=%s",
		c.ServiceName, c.Version, c.Port, c.ReadyAfter)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server error: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), c.ShutdownWait)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Printf("shutdown complete")
}

func loadCfg() cfg {
	port := envInt("PORT", 8080)
	readyAfter := envDuration("READY_AFTER", 0*time.Second)
	serviceName := envStr("SERVICE_NAME", "simple-api")
	version := envStr("VERSION", "0.1.0")
	shutdownWait := envDuration("SHUTDOWN_WAIT", 10*time.Second)

	return cfg{
		Port:         port,
		ReadyAfter:   readyAfter,
		ServiceName:  serviceName,
		Version:      version,
		ShutdownWait: shutdownWait,
	}
}

func envStr(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s ua=%q dur=%s", r.Method, r.URL.Path, r.UserAgent(), time.Since(start))
	})
}
