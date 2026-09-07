// Package rime is voice's only real integration with Rime's TTS API.
// Rime has no official SDK in any language except Python framework
// plugins (confirmed by inspecting the rimelabs GitHub org during the M6
// research pass — the only Go artifact there is rime-cli, a release
// download tool, not a client library) — this package is a hand-rolled
// net/http client against Rime's documented REST endpoint.
package rime

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"voxstate/backend/internal/voice"
)

const endpoint = "https://users.rime.ai/v1/rime-tts"

// Config is the subset of config.Config voice/rime needs.
type Config struct {
	APIKey  string
	ModelID string // e.g. "coda" (Rime's flagship model; mistv3 is the low-latency alternative)
	Speaker string // e.g. "astra"
	Lang    string // e.g. "eng"
	// SampleRateHz is sent explicitly on every request — see TTS's doc
	// comment for why an implicit default is never trusted. Rime accepts
	// 4000-44100.
	SampleRateHz int
	HTTPClient   *http.Client // optional, defaults to http.DefaultClient
	// Endpoint overrides the request URL; empty uses Rime's real REST
	// endpoint. Exists so tests can point this client at an
	// httptest.Server instead of making a live network call.
	Endpoint string
}

// TTS implements voice.TTS against Rime's REST endpoint, requesting WAV
// output with Accept: audio/wav (Rime's quickstart docs are explicit that
// this returns raw audio bytes directly — a JSON envelope with a
// base64-encoded "audioContent" field is only what you get back if the
// Accept header is missing/wrong, which this client always sets
// correctly).
//
// Rime's own documentation states different default values for
// samplingRate on different pages (16000, 22050, and 24000 have all been
// observed as "the default" across different doc pages during the M6
// research pass) — this client never relies on an implicit default:
// every request sets samplingRate explicitly from Config, and Synthesize
// reports back whatever sample rate the returned WAV's fmt chunk actually
// states, not the requested value, so a mismatch between what we asked
// for and what Rime actually produced can never silently propagate.
type TTS struct {
	cfg    Config
	client *http.Client
}

func NewTTS(cfg Config) *TTS {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = endpoint
	}
	return &TTS{cfg: cfg, client: client}
}

var _ voice.TTS = (*TTS)(nil)

type ttsRequest struct {
	Text         string `json:"text"`
	Speaker      string `json:"speaker"`
	ModelID      string `json:"modelId"`
	Lang         string `json:"lang,omitempty"`
	AudioFormat  string `json:"audioFormat"`
	SamplingRate int    `json:"samplingRate"`
}

// jsonEnvelope is only used to produce a readable error message if Rime
// ever falls back to its JSON-envelope response shape (which happens if
// the Accept header this client sends is somehow not honored) — this
// client's happy path never parses JSON out of the body.
type jsonEnvelope struct {
	AudioContent string `json:"audioContent"`
}

func (t *TTS) Synthesize(ctx context.Context, text string) ([]int16, int, error) {
	reqBody, err := json.Marshal(ttsRequest{
		Text:         text,
		Speaker:      t.cfg.Speaker,
		ModelID:      t.cfg.ModelID,
		Lang:         t.cfg.Lang,
		AudioFormat:  "wav",
		SamplingRate: t.cfg.SampleRateHz,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("rime: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, fmt.Errorf("rime: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/wav")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("rime: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("rime: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("rime: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		var env jsonEnvelope
		if jsonErr := json.Unmarshal(body, &env); jsonErr == nil && env.AudioContent != "" {
			return nil, 0, fmt.Errorf("rime: received JSON envelope instead of raw audio (Accept header not honored) — audioContent field was present but this client does not decode it")
		}
	}

	return parseWAV(body)
}

// parseWAV extracts 16-bit PCM samples and the actual sample rate from a
// RIFF/WAVE payload, scanning chunks rather than assuming a fixed header
// length — the fmt chunk's SampleRate field is the authoritative rate
// Rime actually produced, which is what Synthesize reports back rather
// than the value requested (see TTS's doc comment on why an assumed
// default/echo is never trusted).
func parseWAV(b []byte) ([]int16, int, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("rime: response is not a valid WAV payload (%d bytes)", len(b))
	}

	var sampleRateHz, bitsPerSample, dataOffset, dataLen int
	pos := 12
	for pos+8 <= len(b) {
		chunkID := string(b[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		chunkStart := pos + 8
		if chunkStart+chunkSize > len(b) {
			break
		}

		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				sampleRateHz = int(binary.LittleEndian.Uint32(b[chunkStart+4 : chunkStart+8]))
				bitsPerSample = int(binary.LittleEndian.Uint16(b[chunkStart+14 : chunkStart+16]))
			}
		case "data":
			dataOffset = chunkStart
			dataLen = chunkSize
		}

		pos = chunkStart + chunkSize
		if chunkSize%2 == 1 {
			pos++ // RIFF chunks are word-aligned
		}
	}

	if dataLen == 0 {
		return nil, 0, fmt.Errorf("rime: WAV payload has no data chunk")
	}
	if bitsPerSample != 0 && bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("rime: unsupported WAV bit depth %d (want 16)", bitsPerSample)
	}

	raw := b[dataOffset : dataOffset+dataLen]
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}
	return samples, sampleRateHz, nil
}
