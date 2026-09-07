package voice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/events"
)

// VoiceSession is the realtime counterpart to a single conversation bound
// to one machine — see docs/DOMAIN_MODEL.md's VoiceSession entity. It
// owns no state/task/policy logic itself: see this package's doc comment
// (types.go) for the full guarantee.
//
// M7 turn lifecycle: every instruction the user sends belongs to exactly
// one logical turn (a *turn value, defined below). At most one turn is
// ever "current" for a session at a time. Run's frame loop (single
// goroutine, same as M6) owns segmentation; each complete utterance
// starts a turn on its own goroutine (startTurn) so the frame loop keeps
// consuming inbound audio — and can therefore detect a barge-in — while
// that turn is still being processed or spoken. See handleFrame and
// interrupt below for the boundary this creates between an interrupted
// turn and the one that supersedes it.
type VoiceSession struct {
	ID            string
	MachineID     string
	LiveKitRoomID string
	UserID        string
	StartedAt     time.Time
	EndedAt       *time.Time

	agent     *agent.Agent
	stt       STT
	tts       TTS
	transport Transport
	seg       *segmenter
	logger    *slog.Logger

	// mu guards current. Unlike seg (still touched only by Run's single
	// frame-reading goroutine), current is written by both that goroutine
	// (startTurn, interrupt) and each turn's own goroutine (its deferred
	// cleanup), so — unlike M6's single-goroutine session — it needs real
	// synchronization.
	mu      sync.Mutex
	current *turn
}

// turn is one logical voice turn: everything from "utterance handed to
// STT" through "response spoken" that belongs to a single user
// instruction. cancel derives turnCtx (via context.WithCancel) from
// whatever ctx Run was given, so cancelling it propagates into
// agent.Run/tasks.Store exactly like any other ctx cancellation already
// did in M6 (see agent.Agent.Run's runTool) — interruption reuses that
// existing cancellation path rather than inventing a second one.
type turn struct {
	id     string
	cancel context.CancelFunc
}

// NewSession constructs a VoiceSession. ag, stt, tts, and transport are
// required; logger may be nil (a no-op logger is used).
func NewSession(id, machineID, roomID, userID string, ag *agent.Agent, stt STT, tts TTS, transport Transport, logger *slog.Logger) *VoiceSession {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &VoiceSession{
		ID:            id,
		MachineID:     machineID,
		LiveKitRoomID: roomID,
		UserID:        userID,
		StartedAt:     time.Now().UTC(),
		agent:         ag,
		stt:           stt,
		tts:           tts,
		transport:     transport,
		seg:           newSegmenter(DefaultVADConfig()),
		logger:        logger,
	}
}

// Run drives the session loop until ctx is cancelled or the transport's
// Frames channel closes: read inbound frames -> handleFrame, which either
// feeds the frame to the segmenter (no turn active) or treats it as a
// possible barge-in signal (a turn is active). ctx is the parent every
// turn's own cancellable context derives from, so cancelling it (session
// end) also interrupts whatever turn is still active rather than leaving
// it to run/speak unsupervised.
func (s *VoiceSession) Run(ctx context.Context) error {
	frames := s.transport.Frames()
	for {
		select {
		case <-ctx.Done():
			s.interruptCurrent("session_ended")
			return ctx.Err()
		case frame, ok := <-frames:
			if !ok {
				s.interruptCurrent("transport_closed")
				return nil
			}
			s.handleFrame(ctx, frame)
		}
	}
}

// handleFrame is Run's per-frame decision point. If no turn is currently
// active, it behaves exactly as M6's loop did: feed the frame to the
// segmenter, start a turn once a complete utterance is ready. If a turn
// IS active (still being processed, or its response is being
// synthesized/sent), an inbound frame loud enough to count as speech is
// treated as the user interrupting it — the active turn is cancelled
// (interrupt) and the same frame is then fed into the segmenter to begin
// accumulating the new, interrupting utterance. A quiet frame arriving
// while a turn is active (background noise, or the tail of the user's
// own last utterance still arriving) is simply ignored, matching M6's
// prior "drop inbound audio while speaking" behavior for the case where
// nothing rises to the level of an actual interruption.
func (s *VoiceSession) handleFrame(ctx context.Context, frame AudioFrame) {
	if t := s.activeTurn(); t != nil {
		if !s.seg.loud(frame) {
			return
		}
		s.interrupt(t, "barge_in")
		// Fall through: this same frame is the first frame of the
		// interrupting utterance, so it must still reach feed below
		// rather than being consumed only as an interruption signal.
	}

	frameDur := frameDuration(frame)
	utterance, ready := s.seg.feed(frame, frameDur)
	if !ready || len(utterance) == 0 {
		return
	}

	s.startTurn(ctx, utterance, frame.SampleRateHz)
}

// activeTurn returns the session's current turn, or nil if none is
// active (idle session, or between one turn ending and the next
// starting).
func (s *VoiceSession) activeTurn() *turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// clearIfCurrent removes t as the session's active turn only if it is
// still the current one — a turn that already lost a race (e.g. it was
// interrupted, then its own goroutine's deferred cleanup runs afterward)
// must not clear a newer turn that has since become current.
func (s *VoiceSession) clearIfCurrent(t *turn) {
	s.mu.Lock()
	if s.current == t {
		s.current = nil
	}
	s.mu.Unlock()
}

// startTurn registers a new active turn for utterance and runs it on its
// own goroutine via handleUtterance, so Run's frame loop is free to keep
// consuming inbound audio (and therefore detect a barge-in) while this
// turn is still in flight. parentCtx is whatever ctx Run itself was
// given; turnCtx derives from it so cancelling the session also
// interrupts any turn still running.
func (s *VoiceSession) startTurn(parentCtx context.Context, utterance []int16, sampleRateHz int) {
	turnCtx, cancel := context.WithCancel(parentCtx)
	t := &turn{id: newTurnID(), cancel: cancel}

	s.mu.Lock()
	s.current = t
	s.mu.Unlock()

	s.logger.Info("turn_started", "session_id", s.ID, "turn_id", t.id, "machine_id", s.MachineID)

	go func() {
		defer s.clearIfCurrent(t)

		result, err := s.handleUtterance(turnCtx, utterance, sampleRateHz)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// The expected outcome of an interruption — interrupt
				// already logged turn_interrupted/audio_stopped and
				// recorded the UserInterrupted/ResponseInvalidated
				// events; this is just this turn's own goroutine
				// unwinding, not a new failure to report.
				s.logger.Info("response_cancelled", "session_id", s.ID, "turn_id", t.id)
				return
			}
			s.logger.Warn("voice session turn failed", "session_id", s.ID, "turn_id", t.id, "error", err)
			return
		}

		s.logger.Info("turn_completed", "session_id", s.ID, "turn_id", t.id,
			"outcome", string(result.Outcome), "task_id", result.TaskID, "bound_version", result.BoundVersion)
	}()
}

// interrupt is the interruption boundary: it cancels t's context (which
// propagates into agent.Run/tasks.Store, reusing M3's cancellation
// mechanism — see the turn type's doc comment), tells the transport to
// discard any audio t may already have queued for playback, and removes
// t as the session's active turn so a new one can start immediately. It
// does not wait for t's goroutine to actually unwind — cancellation plus
// StopAudio is what makes the boundary immediate; the goroutine's own
// exit is just cleanup.
func (s *VoiceSession) interrupt(t *turn, reason string) {
	t.cancel()

	if err := s.transport.StopAudio(); err != nil {
		s.logger.Warn("voice session failed to stop queued audio", "session_id", s.ID, "turn_id", t.id, "error", err)
	} else {
		s.logger.Info("audio_stopped", "session_id", s.ID, "turn_id", t.id)
	}

	s.clearIfCurrent(t)

	s.logger.Info("turn_interrupted", "session_id", s.ID, "turn_id", t.id, "machine_id", s.MachineID, "reason", reason)

	payload := map[string]any{"session_id": s.ID, "turn_id": t.id, "reason": reason}
	s.logEvent(events.New(events.TypeUserInterrupted, s.MachineID, payload))
	s.logEvent(events.New(events.TypeResponseInvalidated, s.MachineID, payload))
}

// interruptCurrent interrupts the session's active turn, if any. It is a
// safe no-op when the session is idle (nothing to interrupt) — called
// from Run when the session itself is ending, so an active turn doesn't
// keep running/speaking unsupervised after Run returns.
func (s *VoiceSession) interruptCurrent(reason string) {
	if t := s.activeTurn(); t != nil {
		s.interrupt(t, reason)
	}
}

// logEvent is voice's equivalent of tasks.Store's logEvent: there is
// still no persistent event store (same M2/M3/M4 limitation), so a
// constructed events.Event is only ever logged, not appended anywhere,
// via this session's own *slog.Logger.
func (s *VoiceSession) logEvent(ev events.Event) {
	s.logger.Info("voice session event", "event_type", ev.Type, "event_id", ev.ID, "machine_id", ev.MachineID, "payload", ev.Payload)
}

// handleUtterance runs one complete utterance's audio through
// STT -> agent.Run -> TextResponse -> TTS -> Transport. It is called
// directly by tests (see session_test.go) to avoid fighting the
// segmenter's silence-timing in every test, and by startTurn above for
// the real realtime path — both go through this exact same function, so
// there is exactly one place the leak-proof TextResponse boundary is
// invoked from.
//
// ctx.Err() is checked before every user-visible side effect (TTS
// synthesis, Transport.Send), not just once at the top: a turn can be
// interrupted at any point while this function is running (see interrupt
// above), and agent.Run itself can return a nil error with ctx already
// cancelled if the underlying tool happened to finish at the same moment
// interrupt fired (Race A in the M7 test suite) — in every such case this
// function must still never speak Result's content. This is a
// turn-ownership check, not a restatement of policy's staleness check
// (result.BoundVersion == machine.CurrentVersion): that comparison stays
// exclusively inside policy.Evaluator, called from agent.Run — see
// CLAUDE.md's M7 design notes for why the two are deliberately separate
// concerns.
func (s *VoiceSession) handleUtterance(ctx context.Context, audio []int16, sampleRateHz int) (agent.Result, error) {
	text, err := s.stt.Transcribe(ctx, audio, sampleRateHz)
	if err != nil {
		return agent.Result{}, err
	}
	if text == "" {
		return agent.Result{}, nil
	}

	result, err := s.agent.Run(ctx, agent.Instruction{MachineID: s.MachineID, Text: text})
	if err != nil {
		return agent.Result{}, err
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	response := TextResponse(result)
	if response == "" {
		return result, nil
	}

	speechAudio, speechRateHz, err := s.tts.Synthesize(ctx, response)
	if err != nil {
		return result, err
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	if err := s.transport.Send(ctx, speechAudio, speechRateHz); err != nil {
		return result, err
	}
	if ctx.Err() != nil {
		// Race C: interruption landed while Send was in flight or just
		// after it returned. Send only enqueues audio for asynchronous,
		// real-time-paced delivery (see voice/livekit's Transport.Send),
		// so a cancellation observed only now can still leave this turn's
		// audio queued for playback — clear it rather than trust that
		// interrupt's own StopAudio call (fired concurrently on Run's
		// goroutine) happened before this Send landed.
		_ = s.transport.StopAudio()
		return result, ctx.Err()
	}

	return result, nil
}

// frameDuration derives the wall-clock duration an AudioFrame represents
// from its sample count and rate, so segmenter.feed's silence/max-duration
// timers are accurate regardless of how many samples a Transport delivers
// per callback.
func frameDuration(frame AudioFrame) time.Duration {
	if frame.SampleRateHz <= 0 {
		return 0
	}
	return time.Duration(float64(len(frame.Samples)) / float64(frame.SampleRateHz) * float64(time.Second))
}

// newTurnID generates a turn identifier, following the same
// "<prefix>-<6 random bytes hex>" convention as generateSessionID
// (manager.go) and tasks.Store.generateTaskID.
func newTurnID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "turn-" + hex.EncodeToString(b)
}
