// Package activity is M8's minimal, additive mechanism for making
// backend-originated events visible to the frontend: a bounded, in-memory
// ring buffer fed by a slog.Handler that taps the exact same structured
// log lines tasks.Store and voice.VoiceSession already emit (M3/M7) — no
// domain package's code or method signatures change to support this. It
// is deliberately not a database or an event bus (per the M8 brief): a
// single mutex-guarded slice, the same simplicity level as every other
// in-memory store in this codebase (state.Store, tasks.Store,
// voice.Manager).
//
// Coverage: task lifecycle events (DiagnosticStarted/Cancelled/Completed,
// via tasks.Store's "task lifecycle event" log line) and every M7 voice
// log line (turn_started/turn_interrupted/turn_completed/
// response_cancelled/audio_stopped, plus the UserInterrupted/
// ResponseInvalidated events voice logs as "voice session event") are
// captured here, since both packages already log via a shared
// *slog.Logger. MachineStateChanged/TechnicianReported/ToolResultRejected
// are NOT captured here — state.Store and policy.Evaluator don't log at
// all (by design, see CLAUDE.md's M2/M4 notes) — the frontend instead
// reads those directly from the HTTP response body of the request that
// produced them (every handler that constructs one already returns it
// inline). This package only fills the gap for events that happen on a
// background goroutine with no HTTP caller to hand them to.
package activity

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// capturedMessages is the allow-list of slog record messages this
// package treats as a domain event worth surfacing to the frontend.
// Everything else (HTTP request logs, warnings, etc.) still passes
// through to the wrapped handler unchanged, just isn't buffered here.
var capturedMessages = map[string]bool{
	"task lifecycle event": true, // tasks.Store
	"voice session event":  true, // voice.VoiceSession (UserInterrupted/ResponseInvalidated)
	"turn_started":         true, // voice.VoiceSession (M7)
	"turn_interrupted":     true,
	"turn_completed":       true,
	"response_cancelled":   true,
	"audio_stopped":        true,
}

// Entry is one captured log line, reduced to what the frontend needs:
// when it happened, what kind of thing it was, and its structured
// attributes exactly as the producing package logged them.
type Entry struct {
	Message string         `json:"message"`
	Time    time.Time      `json:"time"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

type ring struct {
	mu       sync.Mutex
	entries  []Entry
	capacity int
}

func (r *ring) append(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	if len(r.entries) > r.capacity {
		r.entries = r.entries[len(r.entries)-r.capacity:]
	}
}

// List returns captured entries, most recent first.
func (r *ring) List() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.entries))
	for i, e := range r.entries {
		out[len(r.entries)-1-i] = e
	}
	return out
}

// Handler wraps another slog.Handler, forwarding every record to it
// unchanged (stdout logging behavior is identical to before this package
// existed) while additionally buffering records whose Message is in
// capturedMessages. Construct one with NewHandler and keep the returned
// *Handler to call List(); pass slog.New(handler) around as the shared
// logger exactly as before — WithAttrs/WithGroup return a Handler backed
// by the same underlying ring, so logger.With(...) (main.go already does
// this) doesn't lose capture capability.
type Handler struct {
	next slog.Handler
	r    *ring
}

// NewHandler wraps next, buffering up to capacity captured entries.
func NewHandler(next slog.Handler, capacity int) *Handler {
	return &Handler{next: next, r: &ring{capacity: capacity}}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if capturedMessages[r.Message] {
		attrs := make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Any()
			return true
		})
		h.r.append(Entry{Message: r.Message, Time: r.Time, Attrs: attrs})
	}
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), r: h.r}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), r: h.r}
}

// List returns captured entries, most recent first, capped at whatever
// capacity NewHandler was constructed with.
func (h *Handler) List() []Entry {
	return h.r.List()
}
