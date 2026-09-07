package voice

import (
	"testing"
	"time"
)

func loudFrame(n, sampleRateHz int) AudioFrame {
	samples := make([]int16, n)
	for i := range samples {
		samples[i] = 20000
	}
	return AudioFrame{Samples: samples, SampleRateHz: sampleRateHz}
}

func quietFrame(n, sampleRateHz int) AudioFrame {
	return AudioFrame{Samples: make([]int16, n), SampleRateHz: sampleRateHz}
}

func TestSegmenter_EmitsAfterSilenceFollowingSpeech(t *testing.T) {
	cfg := vadConfig{EnergyThreshold: 500, SilenceTimeout: 50 * time.Millisecond, MaxUtteranceDuration: time.Second}
	s := newSegmenter(cfg)

	for i := 0; i < 3; i++ {
		if _, ready := s.feed(loudFrame(160, 16000), 10*time.Millisecond); ready {
			t.Fatalf("emitted early on speech frame %d", i)
		}
	}

	var utterance []int16
	var ready bool
	for i := 0; i < 10 && !ready; i++ {
		utterance, ready = s.feed(quietFrame(160, 16000), 10*time.Millisecond)
	}
	if !ready {
		t.Fatal("expected an utterance to be emitted after the silence timeout")
	}
	if len(utterance) == 0 {
		t.Error("emitted utterance is empty")
	}
}

func TestSegmenter_ForcesEmissionAtMaxDuration(t *testing.T) {
	cfg := vadConfig{EnergyThreshold: 500, SilenceTimeout: time.Hour, MaxUtteranceDuration: 50 * time.Millisecond}
	s := newSegmenter(cfg)

	var ready bool
	for i := 0; i < 20 && !ready; i++ {
		_, ready = s.feed(loudFrame(160, 16000), 10*time.Millisecond)
	}
	if !ready {
		t.Fatal("expected forced emission once MaxUtteranceDuration was exceeded, even without silence")
	}
}

func TestSegmenter_NeverEmitsOnSilenceOnly(t *testing.T) {
	s := newSegmenter(DefaultVADConfig())

	for i := 0; i < 100; i++ {
		if _, ready := s.feed(quietFrame(160, 16000), 20*time.Millisecond); ready {
			t.Fatal("emitted an utterance from silence-only input")
		}
	}
}

func TestSegmenter_ResetsAfterEmission(t *testing.T) {
	cfg := vadConfig{EnergyThreshold: 500, SilenceTimeout: 20 * time.Millisecond, MaxUtteranceDuration: time.Second}
	s := newSegmenter(cfg)

	// First utterance.
	s.feed(loudFrame(160, 16000), 10*time.Millisecond)
	var ready bool
	for i := 0; i < 10 && !ready; i++ {
		_, ready = s.feed(quietFrame(160, 16000), 10*time.Millisecond)
	}
	if !ready {
		t.Fatal("first utterance was never emitted")
	}

	// After emission, silence alone must not immediately re-trigger.
	if _, ready := s.feed(quietFrame(160, 16000), 10*time.Millisecond); ready {
		t.Fatal("emitted again from silence right after a prior emission")
	}
}
