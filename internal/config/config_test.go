package config

import (
	"log/slog"
	"testing"
	"time"
)

// TestLoad_Defaults verifies that calling Load with no env vars yields
// the documented defaults.
func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("STARTUP_DELAY", "")
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("VERSION", "")
	t.Setenv("SHUTDOWN_WAIT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("IDLE_TIMEOUT", "")
	t.Setenv("MAX_BODY_BYTES", "")
	t.Setenv("LOG_LEVEL", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if c.Port != 8080 {
		t.Errorf("Port = %d, want 8080", c.Port)
	}
	if c.StartupDelay != 30*time.Second {
		t.Errorf("StartupDelay = %v, want 30s", c.StartupDelay)
	}
	if c.ServiceName != "probe-service" {
		t.Errorf("ServiceName = %q, want %q", c.ServiceName, "probe-service")
	}
	if c.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", c.Version, "1.0.0")
	}
	if c.MaxBodyBytes != 1<<20 {
		t.Errorf("MaxBodyBytes = %d, want %d", c.MaxBodyBytes, 1<<20)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want Info", c.LogLevel)
	}
}

// TestLoad_Overrides verifies that all supported variables are honoured.
func TestLoad_Overrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("STARTUP_DELAY", "5s")
	t.Setenv("SERVICE_NAME", "probe")
	t.Setenv("VERSION", "2.3.4")
	t.Setenv("MAX_BODY_BYTES", "2048")
	t.Setenv("LOG_LEVEL", "debug")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if c.Port != 9090 {
		t.Errorf("Port = %d, want 9090", c.Port)
	}
	if c.StartupDelay != 5*time.Second {
		t.Errorf("StartupDelay = %v, want 5s", c.StartupDelay)
	}
	if c.ServiceName != "probe" {
		t.Errorf("ServiceName = %q, want %q", c.ServiceName, "probe")
	}
	if c.Version != "2.3.4" {
		t.Errorf("Version = %q, want %q", c.Version, "2.3.4")
	}
	if c.MaxBodyBytes != 2048 {
		t.Errorf("MaxBodyBytes = %d, want 2048", c.MaxBodyBytes)
	}
	if c.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want Debug", c.LogLevel)
	}
}

// TestLoad_InvalidValues verifies that bad input produces an error
// instead of crashing the process.
func TestLoad_InvalidValues(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
	}{
		{"port not int", "PORT", "abc"},
		{"port out of range", "PORT", "70000"},
		{"port zero", "PORT", "0"},
		{"duration garbage", "STARTUP_DELAY", "not-a-duration"},
		{"duration negative", "STARTUP_DELAY", "-1s"},
		{"max body zero", "MAX_BODY_BYTES", "0"},
		{"max body garbage", "MAX_BODY_BYTES", "huge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("Load(%s=%q) returned nil error", tc.key, tc.val)
			}
		})
	}
}

// TestParseLogLevel checks the level-name mapping including fallback.
func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLogLevel(in); got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
