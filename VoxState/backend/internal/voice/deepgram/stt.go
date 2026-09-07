// Package deepgram is voice's only real integration with Deepgram's STT
// API, using Deepgram's official Go SDK (github.com/deepgram/deepgram-go-sdk/v3).
//
// This client calls Deepgram's prerecorded REST endpoint once per
// utterance segmenter.go already produced, rather than opening a
// persistent streaming websocket connection. LiveKit's Go path gives us a
// raw, continuous audio stream with no built-in endpointing, so voice
// already has to do its own energy/silence segmentation regardless of
// which Deepgram API style is used — given that, a streaming connection
// would just be redundant endpointing on top of our own, for no latency
// benefit once an utterance is already a complete, bounded buffer. One
// blocking request per utterance is simpler, symmetric with voice/rime's
// REST TTS call, and directly testable against an httptest.Server.
package deepgram

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	restapi "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/rest"
	interfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	listen "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/listen"

	"voxstate/backend/internal/voice"
)

// defaultModel is Deepgram's current general-purpose model as of the M6
// research pass. Config does not expose overriding it — M6 has no
// requirement to make the STT model configurable beyond what's needed to
// demonstrate the pipeline, and adding a knob nothing calls would be
// premature.
const defaultModel = "nova-3"

// STT implements voice.STT against Deepgram's prerecorded REST endpoint.
type STT struct {
	client *restapi.Client
}

// NewSTT constructs an STT using apiKey (the DEEPGRAM_API_KEY env var —
// see .env.example). Deepgram's SDK also supports reading a key from its
// own environment-variable default, but this client always passes it
// explicitly, matching the rest of VoxState's config-driven dependency
// injection rather than a package silently reading process env vars on
// its own.
func NewSTT(apiKey string) *STT {
	return newSTT(apiKey, nil)
}

// newSTT is the shared constructor; opts is exposed only to
// stt_test.go (via the Host override) so tests can point this client at
// an httptest.Server instead of making a live network call.
func newSTT(apiKey string, opts *interfaces.ClientOptions) *STT {
	client := listen.NewREST(apiKey, opts)
	return &STT{client: restapi.New(client)}
}

var _ voice.STT = (*STT)(nil)

// Transcribe submits one already-segmented utterance's raw PCM16 samples
// to Deepgram as linear16-encoded audio, always stating sampleRateHz
// explicitly (Deepgram's options struct accepts an arbitrary rate rather
// than requiring one fixed value, confirmed against its Go SDK's
// PreRecordedTranscriptionOptions during the M6 research pass — so the
// inbound path needs no resampling: it's fed whatever rate the Transport
// actually captured at, e.g. LiveKit's native 48kHz).
func (s *STT) Transcribe(ctx context.Context, audio []int16, sampleRateHz int) (string, error) {
	buf := new(bytes.Buffer)
	buf.Grow(len(audio) * 2)
	for _, sample := range audio {
		if err := binary.Write(buf, binary.LittleEndian, sample); err != nil {
			return "", fmt.Errorf("deepgram: encode PCM: %w", err)
		}
	}

	res, err := s.client.FromStream(ctx, buf, &interfaces.PreRecordedTranscriptionOptions{
		Model:      defaultModel,
		Encoding:   "linear16",
		SampleRate: sampleRateHz,
		Channels:   1,
		Punctuate:  true,
	})
	if err != nil {
		return "", fmt.Errorf("deepgram: transcribe: %w", err)
	}

	if res == nil || res.Results == nil || len(res.Results.Channels) == 0 || len(res.Results.Channels[0].Alternatives) == 0 {
		return "", nil
	}
	return res.Results.Channels[0].Alternatives[0].Transcript, nil
}
