// Package config loads runtime configuration from environment variables
// and validates it. It returns errors instead of calling os.Exit so that
// the calling main package retains control over process termination and
// so that the loader is unit-testable.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration derived from environment variables.
type Config struct {
	// Port is the TCP port the HTTP server binds to.
	Port int
	// StartupDelay is applied to BOTH the liveness and readiness flags
	// after process start and after every admin reset.
	StartupDelay time.Duration
	// ServiceName is reported in JSON responses (json: "service").
	ServiceName string
	// Version is reported in JSON responses and the X-Service-Version header.
	Version string
	// ShutdownWait is the maximum time the server is given to drain in-flight
	// requests during graceful shutdown.
	ShutdownWait time.Duration
	// ReadTimeout, WriteTimeout, IdleTimeout map to the corresponding fields
	// on http.Server.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	// MaxBodyBytes caps the request body size. Non-positive disables the cap.
	MaxBodyBytes int64
	// LogLevel is the minimum slog level emitted by the logger.
	LogLevel slog.Level
}

// Load reads environment variables and returns a validated Config.
// On any invalid value, Load returns an error describing the offending key.
//
// Recognised variables and defaults:
//
//	PORT             (int 1-65535)         default 8080
//	STARTUP_DELAY    (time.Duration)       default 30s
//	SERVICE_NAME     (string)              default "probe-service"
//	VERSION          (string)              default "1.0.0"
//	SHUTDOWN_WAIT    (time.Duration)       default 10s
//	READ_TIMEOUT     (time.Duration)       default 15s
//	WRITE_TIMEOUT    (time.Duration)       default 15s
//	IDLE_TIMEOUT     (time.Duration)       default 60s
//	MAX_BODY_BYTES   (int64 > 0)           default 1 MiB
//	LOG_LEVEL        (debug|info|warn|error) default info
func Load() (Config, error) {
	port, err := envInt("PORT", 8080, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	startupDelay, err := envDuration("STARTUP_DELAY", 30*time.Second, false)
	if err != nil {
		return Config{}, err
	}
	shutdownWait, err := envDuration("SHUTDOWN_WAIT", 10*time.Second, false)
	if err != nil {
		return Config{}, err
	}
	readTimeout, err := envDuration("READ_TIMEOUT", 15*time.Second, false)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := envDuration("WRITE_TIMEOUT", 15*time.Second, false)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := envDuration("IDLE_TIMEOUT", 60*time.Second, false)
	if err != nil {
		return Config{}, err
	}
	maxBody, err := envInt64("MAX_BODY_BYTES", 1<<20, 1)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:         port,
		StartupDelay: startupDelay,
		ServiceName:  envStr("SERVICE_NAME", "probe-service"),
		Version:      envStr("VERSION", "1.0.0"),
		ShutdownWait: shutdownWait,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		MaxBodyBytes: maxBody,
		LogLevel:     parseLogLevel(envStr("LOG_LEVEL", "info")),
	}, nil
}

// envStr returns the trimmed environment variable for key, or def if empty.
func envStr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

// envInt parses an int env var and ensures it lies within [min, max].
func envInt(key string, def, minVal, maxVal int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < minVal || n > maxVal {
		return 0, fmt.Errorf("invalid %s=%q (expected int in [%d,%d])", key, v, minVal, maxVal)
	}
	return n, nil
}

// envInt64 parses an int64 env var and ensures it is >= minVal.
func envInt64(key string, def, minVal int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < minVal {
		return 0, fmt.Errorf("invalid %s=%q (expected int64 >= %d)", key, v, minVal)
	}
	return n, nil
}

// envDuration parses a time.Duration env var. If allowNegative is false,
// negative durations are rejected.
func envDuration(key string, def time.Duration, allowNegative bool) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || (!allowNegative && d < 0) {
		return 0, fmt.Errorf("invalid %s=%q (expected duration like 30s, 1m)", key, v)
	}
	return d, nil
}

// parseLogLevel maps a string to a slog.Level. Unknown values fall back to info.
func parseLogLevel(s string) slog.Level {
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
