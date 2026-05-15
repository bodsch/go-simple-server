# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-05-15

### Changed (breaking)

- **Project layout**: Code moved from a single `main.go` into Go-idiomatic
  packages under `cmd/probe-service` and `internal/`. The internal types
  (`DelayedFlag`, `cfg`, middleware funcs, helpers) are no longer in
  `package main`. Since they were never importable before, no external Go
  consumer is affected, but anyone vendoring the source must adapt paths.
- **JSON response shape**: All probe / admin responses now consistently
  include `service` and `version` fields. Previously some responses omitted
  `service`. Field order is unspecified (Go map iteration), so clients must
  not depend on it.
- **Config loading**: `config.Load()` now returns `(Config, error)` instead
  of calling `os.Exit` directly. Behaviour on invalid env vars from the
  user's perspective is unchanged (process exits with code 2), but the
  responsibility for terminating moved to `main`.
- **DelayedFlag race fix**: `Reset()` and the internal expiry callback now
  both hold the same mutex. As a side effect, a `Reset()` issued
  concurrently with an expiring timer can no longer be overwritten by the
  stale timer's `val.Store(true)`. Observable change: in such races, the
  flag now stays `false` and the new delay applies (previously it could
  briefly flip to `true` and then back to `false`).

### Added

- `X-Service-Version` response header on every HTTP response.
- `internal/flagx` package with full unit tests, including race-detector
  coverage.
- `internal/server` handler tests using `httptest`.
- Multi-arch Docker build (`linux/amd64`, `linux/arm64`) with non-root user.
- Request latency and response size are now logged in the access log
  (previously they were captured but commented out).

### Removed

- `drainBody` (was dead code).
- Unused `BaseContext` closure on `http.Server` (the default is already
  `context.Background()`).

### Fixed

- Race in `DelayedFlag` where a timer firing concurrently with `Reset()`
  could overwrite the freshly-reset `false` state with `true`.
