package voice

import (
	"context"
	"log/slog"
	"time"

	"voxstate/backend/internal/agent"
)

// VoiceSession is the M6 realtime counterpart to a single conversation
// bound to one machine — see docs/DOMAIN_MODEL.md's VoiceSession entity.
// It owns no state/task/policy logic itself: see this package's doc
// comment (types.go) for the full guarantee.
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

	// speaking guards inbound audio while a response is being synthesized
	// and sent: while true, Run drops inbound frames instead of feeding
	// them to the segmenter. This is a deliberate M6 behavior decision,
	// not an oversight — queuing audio that arrives mid-response for
	// later processing is exactly the kind of interruption-awareness M7
	// is responsible for introducing. Run is single-goroutine per
	// session, so a plain bool is sufficient; no mutex is needed.
	speaking bool
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
// Frames channel closes: read inbound frames -> segment into utterances
// (segmenter.go) -> handleUtterance (STT -> agent.Run -> TTS/Transport).
// Run does not implement interruption/barge-in (M7): while speaking is
// true it does not attempt to detect or react to the user talking over a
// response beyond simply not processing that audio as a new instruction.
func (s *VoiceSession) Run(ctx context.Context) error {
	frames := s.transport.Frames()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			if s.speaking {
				continue
			}
			frameDur := frameDuration(frame)
			utterance, ready := s.seg.feed(frame, frameDur)
			if !ready || len(utterance) == 0 {
				continue
			}
			if _, err := s.handleUtterance(ctx, utterance, frame.SampleRateHz); err != nil {
				s.logger.Warn("voice session utterance handling failed", "session_id", s.ID, "error", err)
			}
		}
	}
}

// handleUtterance runs one complete utterance's audio through
// STT -> agent.Run -> TextResponse -> TTS -> Transport.Send. It is called
// directly by tests (see session_test.go) to avoid fighting the
// segmenter's silence-timing in every test, and by Run's frame loop above
// for the real realtime path — both go through this exact same function,
// so there is exactly one place the leak-proof TextResponse boundary is
// invoked from.
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

	response := TextResponse(result)
	if response == "" {
		return result, nil
	}

	s.speaking = true
	defer func() { s.speaking = false }()

	speechAudio, speechRateHz, err := s.tts.Synthesize(ctx, response)
	if err != nil {
		return result, err
	}
	if err := s.transport.Send(ctx, speechAudio, speechRateHz); err != nil {
		return result, err
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
