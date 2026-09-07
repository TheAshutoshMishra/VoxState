// Package server owns HTTP server construction and its lifecycle
// (listen, graceful shutdown). It knows nothing about routes or domain
// logic — those are supplied as an http.Handler by the caller.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// New builds an *http.Server with the given address and handler, and
// reasonable timeouts to avoid slow-client resource exhaustion.
func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Run starts srv and blocks until ctx is cancelled, at which point it
// attempts a graceful shutdown (allowing in-flight requests to finish)
// within shutdownTimeout. It returns nil on a clean shutdown, or the
// error that caused the server to stop.
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	serveErr := make(chan error, 1)

	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining in-flight requests")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}

		logger.Info("http server stopped cleanly")
		return nil
	}
}
