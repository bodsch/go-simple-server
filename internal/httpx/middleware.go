package httpx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"time"
)

// Middleware is a standard HTTP middleware constructor.
type Middleware func(http.Handler) http.Handler

// Chain composes the given middlewares around next in the order they are
// passed: the first middleware in the slice is the outermost wrapper, which
// makes the call order at request time identical to the slice order.
//
// Example: Chain(h, A, B, C) → A(B(C(h))).
func Chain(next http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		next = mws[i](next)
	}
	return next
}

// ctxKeyRequestID is a private context key type used to attach the request
// ID to a request's context without colliding with other packages.
type ctxKeyRequestID struct{}

// RequestIDFromContext returns the request ID stored in ctx, or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID{}).(string); ok {
		return v
	}
	return ""
}

// newRequestID generates a URL-safe, compact, 18-byte random identifier.
func newRequestID() string {
	var b [18]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// RequestID assigns a new identifier to every request, attaches it to the
// request context, and exposes it as the X-Request-Id response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set("X-Request-Id", id)
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID{}, id))
			next.ServeHTTP(w, r)
		})
	}
}

// Recoverer converts panics from downstream handlers into a JSON 500
// response and logs the panic value together with the request ID.
func Recoverer(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"panic", rec,
						"request_id", RequestIDFromContext(r.Context()),
					)
					WriteError(w, http.StatusInternalServerError, "internal_error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody caps the request body size using http.MaxBytesReader.
// A non-positive max disables body limiting.
func MaxBody(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if max > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ServiceVersion sets the X-Service-Version response header on every reply,
// for both success and error responses (because the header is set before
// the inner handler writes the status code).
func ServiceVersion(version string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Service-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog logs request/response metadata (method, path, status, bytes,
// latency, request ID, user agent, remote addr) in structured form.
func AccessLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := NewStatusWriter(w)

			next.ServeHTTP(sw, r)

			log.Info("probe",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.Status(),
				"bytes", sw.Bytes(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
				"user_agent", r.UserAgent(),
				"remote", r.RemoteAddr,
			)
		})
	}
}
