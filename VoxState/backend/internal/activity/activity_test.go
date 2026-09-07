package activity

import (
	"io"
	"log/slog"
	"testing"
)

func TestHandler_CapturesOnlyKnownMessages(t *testing.T) {
	h := NewHandler(slog.NewTextHandler(io.Discard, nil), 10)
	logger := slog.New(h)

	logger.Info("task lifecycle event", "event_type", "DiagnosticStarted", "machine_id", "m1")
	logger.Info("http request", "method", "GET") // not captured
	logger.Info("turn_interrupted", "session_id", "s1", "turn_id", "t1")

	entries := h.List()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2: %+v", len(entries), entries)
	}
	// Most recent first.
	if entries[0].Message != "turn_interrupted" {
		t.Errorf("entries[0].Message = %q, want turn_interrupted", entries[0].Message)
	}
	if entries[0].Attrs["turn_id"] != "t1" {
		t.Errorf("entries[0].Attrs[turn_id] = %v, want t1", entries[0].Attrs["turn_id"])
	}
	if entries[1].Message != "task lifecycle event" {
		t.Errorf("entries[1].Message = %q, want task lifecycle event", entries[1].Message)
	}
}

func TestHandler_RingBufferCapsAtCapacity(t *testing.T) {
	h := NewHandler(slog.NewTextHandler(io.Discard, nil), 3)
	logger := slog.New(h)

	for i := 0; i < 5; i++ {
		logger.Info("audio_stopped", "seq", i)
	}

	entries := h.List()
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3 (capped)", len(entries))
	}
	// Most recent first: seq 4, 3, 2.
	if got := entries[0].Attrs["seq"]; got != int64(4) {
		t.Errorf("entries[0].Attrs[seq] = %v, want 4", got)
	}
}

func TestHandler_WithAttrsSharesUnderlyingBuffer(t *testing.T) {
	h := NewHandler(slog.NewTextHandler(io.Discard, nil), 10)
	logger := slog.New(h).With("app_env", "test")

	logger.Info("response_cancelled", "turn_id", "t1")

	if got := len(h.List()); got != 1 {
		t.Fatalf("len(h.List()) = %d, want 1 (With() must not lose capture capability)", got)
	}
}
