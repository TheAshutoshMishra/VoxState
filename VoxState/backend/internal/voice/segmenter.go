package voice

import (
	"math"
	"time"
)

// vadConfig tunes segmenter's energy/silence heuristic. Populated from
// config.Config in main.go; see NewSegmenterConfig for defaults.
type vadConfig struct {
	// EnergyThreshold is a per-frame RMS level (out of int16 full scale,
	// 32767) above which a frame counts as "speech". This is a fixed,
	// hand-tuned value, not adaptive to ambient noise.
	EnergyThreshold float64
	// SilenceTimeout is how long RMS must stay at or below
	// EnergyThreshold, continuously, after speech has started, before the
	// accumulated buffer is emitted as a complete utterance.
	SilenceTimeout time.Duration
	// MaxUtteranceDuration forces emission even without silence, so a
	// stuck-open input can't block a session forever.
	MaxUtteranceDuration time.Duration
}

// DefaultVADConfig returns M6's fixed segmentation thresholds.
func DefaultVADConfig() vadConfig {
	return vadConfig{
		EnergyThreshold:      500,
		SilenceTimeout:       700 * time.Millisecond,
		MaxUtteranceDuration: 15 * time.Second,
	}
}

// segmenter accumulates inbound AudioFrames into utterances using a
// simple energy-threshold + silence-timeout heuristic: frames whose RMS
// energy exceeds EnergyThreshold are "speech" and get appended to the
// current utterance buffer, resetting the silence timer; once
// SilenceTimeout of continuous quiet follows a non-empty buffer, that
// buffer is emitted as one utterance.
//
// This is NOT production-grade VAD: no noise-floor adaptation, no
// spectral analysis, no barge-in awareness, fixed thresholds not tuned
// against any real microphone. It is a known, documented M6 limitation
// (see CLAUDE.md's M6 design notes) — good enough to demonstrate "you can
// talk to it and it responds," not a component to build production voice
// UX on. It is pure and deterministic (no I/O, no goroutines), which is
// what makes it directly unit-testable by feeding a synthetic sequence of
// frames — see segmenter_test.go.
type segmenter struct {
	cfg vadConfig

	buf              []int16
	sampleRateHz     int
	speaking         bool
	silenceElapsed   time.Duration
	utteranceElapsed time.Duration
}

func newSegmenter(cfg vadConfig) *segmenter {
	return &segmenter{cfg: cfg}
}

// feed appends one frame's samples and returns (utterance, true) if a
// complete utterance is ready to hand to STT, else (nil, false). The
// caller supplies frameDur (the wall-clock duration this frame
// represents) since AudioFrame carries only samples, not timing.
func (s *segmenter) feed(frame AudioFrame, frameDur time.Duration) ([]int16, bool) {
	loud := rms(frame.Samples) > s.cfg.EnergyThreshold

	if !s.speaking && !loud {
		return nil, false
	}

	if !s.speaking {
		s.speaking = true
		s.sampleRateHz = frame.SampleRateHz
		s.silenceElapsed = 0
		s.utteranceElapsed = 0
	}

	s.buf = append(s.buf, frame.Samples...)
	s.utteranceElapsed += frameDur

	if loud {
		s.silenceElapsed = 0
	} else {
		s.silenceElapsed += frameDur
	}

	if s.silenceElapsed >= s.cfg.SilenceTimeout || s.utteranceElapsed >= s.cfg.MaxUtteranceDuration {
		return s.emit(), true
	}
	return nil, false
}

// loud reports whether frame's energy alone would count as speech,
// without mutating segmenter state. VoiceSession.handleFrame (session.go)
// uses this to decide whether an inbound frame arriving while a turn is
// active is a barge-in signal, before ever handing the frame to feed —
// feed is only ever called by the single Run-loop goroutine that owns
// s.buf, so this stays a plain query with no locking, same as feed.
func (s *segmenter) loud(frame AudioFrame) bool {
	return rms(frame.Samples) > s.cfg.EnergyThreshold
}

func (s *segmenter) emit() []int16 {
	out := s.buf
	s.buf = nil
	s.speaking = false
	s.silenceElapsed = 0
	s.utteranceElapsed = 0
	return out
}

// rms returns the root-mean-square amplitude of samples, as a float in
// the same scale as int16 (0..32768).
func rms(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSquares float64
	for _, s := range samples {
		v := float64(s)
		sumSquares += v * v
	}
	return math.Sqrt(sumSquares / float64(len(samples)))
}
