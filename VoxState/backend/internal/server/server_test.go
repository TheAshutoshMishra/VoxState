package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestRun_GracefulShutdown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New("127.0.0.1:0", http.NewServeMux())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, srv, logger)
	}()

	// Give the listener goroutine a moment to call ListenAndServe before
	// triggering shutdown.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned error after graceful shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of context cancellation")
	}
}

func TestNew_SetsTimeouts(t *testing.T) {
	srv := New("127.0.0.1:0", http.NewServeMux())

	if srv.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout is unset")
	}
	if srv.ReadTimeout == 0 {
		t.Error("ReadTimeout is unset")
	}
	if srv.WriteTimeout == 0 {
		t.Error("WriteTimeout is unset")
	}
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout is unset")
	}
}
