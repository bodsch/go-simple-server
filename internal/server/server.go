package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"bodsch.me/probe-service/internal/config"
	"bodsch.me/probe-service/internal/flagx"
	"bodsch.me/probe-service/internal/httpx"
)

// Server is the runnable application. Public callers should treat it as
// opaque except for Run.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	http   *http.Server
	health *flagx.DelayedFlag
	ready  *flagx.DelayedFlag
}

// New builds a Server with all routes and middleware in place. It does
// not bind the listening socket; that happens in Run so that test code
// can construct a Server in process without holding a port.
func New(cfg config.Config, log *slog.Logger) (*Server, error) {
	if log == nil {
		return nil, errors.New("server.New: nil logger")
	}

	health := flagx.NewDelayedFlag(cfg.StartupDelay)
	ready := flagx.NewDelayedFlag(cfg.StartupDelay)

	mux := http.NewServeMux()
	registerRoutes(mux, cfg, health, ready)

	// Middleware order matters:
	//   RequestID is outermost so the ID is in r.Context() for every layer
	//   below it (otherwise the WithContext rebind inside RequestID is
	//   invisible to outer middlewares' deferred log statements).
	//   AccessLog then Recoverer follow, so panic responses are still logged
	//   with status 500 and the request ID. ServiceVersion sets a response
	//   header and therefore must run before any WriteHeader. MaxBody only
	//   affects the inner handler.
	handler := httpx.Chain(mux,
		httpx.RequestID(),
		httpx.AccessLog(log),
		httpx.Recoverer(log),
		httpx.ServiceVersion(cfg.Version),
		httpx.MaxBody(cfg.MaxBodyBytes),
	)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	return &Server{
		cfg:    cfg,
		log:    log,
		http:   srv,
		health: health,
		ready:  ready,
	}, nil
}

// Handler returns the fully composed root http.Handler, primarily for
// tests that want to drive the server via httptest without binding a port.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run binds the listener and serves until ctx is cancelled, then performs
// a graceful shutdown bounded by cfg.ShutdownWait.
//
// Run returns nil on a clean shutdown caused by ctx cancellation, and a
// non-nil error if either the listener could not be bound, the server
// terminated with an error other than http.ErrServerClosed, or the
// shutdown itself failed.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.http.Addr, err)
	}

	s.log.Info("starting",
		"service", s.cfg.ServiceName,
		"version", s.cfg.Version,
		"addr", s.http.Addr,
		"startup_delay", s.cfg.StartupDelay.String(),
	)

	errCh := make(chan error, 1)
	go func() {
		err := s.http.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		s.log.Info("shutdown requested")
	case err := <-errCh:
		if err != nil {
			s.log.Error("server error", "err", err)
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownWait)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.log.Error("shutdown failed", "err", err)
		return fmt.Errorf("shutdown: %w", err)
	}
	s.log.Info("shutdown complete")
	return nil
}
