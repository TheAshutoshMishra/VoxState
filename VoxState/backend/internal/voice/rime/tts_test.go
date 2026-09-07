package rime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildWAV constructs a minimal, valid RIFF/WAVE payload carrying samples
// at sampleRateHz, mirroring what a real Rime response looks like closely
// enough for parseWAV to exercise its real chunk-scanning logic against.
func buildWAV(samples []int16, sampleRateHz int) []byte {
	dataLen := len(samples) * 2
	buf := make([]byte, 0, 44+dataLen)
	buf = append(buf, []byte("RIFF")...)
	buf = appendUint32(buf, uint32(36+dataLen))
	buf = append(buf, []byte("WAVE")...)
	buf = append(buf, []byte("fmt ")...)
	buf = appendUint32(buf, 16)
	buf = appendUint16(buf, 1) // PCM
	buf = appendUint16(buf, 1) // mono
	buf = appendUint32(buf, uint32(sampleRateHz))
	byteRate := sampleRateHz * 2
	buf = appendUint32(buf, uint32(byteRate))
	buf = appendUint16(buf, 2)  // block align
	buf = appendUint16(buf, 16) // bits per sample
	buf = append(buf, []byte("data")...)
	buf = appendUint32(buf, uint32(dataLen))
	for _, s := range samples {
		buf = appendUint16(buf, uint16(s))
	}
	return buf
}

func appendUint32(b []byte, v uint32) []byte {
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, v)
	return append(b, tmp...)
}

func appendUint16(b []byte, v uint16) []byte {
	tmp := make([]byte, 2)
	binary.LittleEndian.PutUint16(tmp, v)
	return append(b, tmp...)
}

func TestSynthesize_SendsExplicitSamplingRateAndParsesWAV(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")

		w.Header().Set("Content-Type", "audio/wav")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buildWAV([]int16{100, -100, 200, -200}, 22050))
	}))
	defer srv.Close()

	tts := NewTTS(Config{
		APIKey:       "test-key",
		ModelID:      "coda",
		Speaker:      "astra",
		SampleRateHz: 24000,
		Endpoint:     srv.URL,
	})

	samples, sampleRateHz, err := tts.Synthesize(context.Background(), "hello from rime")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}

	// The request must always state samplingRate explicitly — Rime's own
	// docs are inconsistent about what an implicit default would be, so
	// this client never relies on one.
	if rate, ok := gotBody["samplingRate"].(float64); !ok || int(rate) != 24000 {
		t.Errorf("request samplingRate = %v, want 24000 explicitly present", gotBody["samplingRate"])
	}
	if gotBody["audioFormat"] != "wav" {
		t.Errorf("request audioFormat = %v, want %q", gotBody["audioFormat"], "wav")
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotAccept != "audio/wav" {
		t.Errorf("Accept header = %q, want %q", gotAccept, "audio/wav")
	}

	// Synthesize must report the rate the WAV's fmt chunk actually states
	// (22050, what the fake server sent), not the 24000 requested — this
	// is the point of parsing the real fmt chunk rather than echoing the
	// request's SampleRateHz back unchecked.
	if sampleRateHz != 22050 {
		t.Errorf("sampleRateHz = %d, want 22050 (the WAV's actual rate, not the requested one)", sampleRateHz)
	}
	want := []int16{100, -100, 200, -200}
	if len(samples) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(samples), len(want))
	}
	for i := range want {
		if samples[i] != want[i] {
			t.Errorf("samples[%d] = %d, want %d", i, samples[i], want[i])
		}
	}
}

func TestSynthesize_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	tts := NewTTS(Config{APIKey: "bad-key", SampleRateHz: 24000, Endpoint: srv.URL})

	if _, _, err := tts.Synthesize(context.Background(), "hello"); err == nil {
		t.Fatal("Synthesize() error = nil, want an error for a non-200 response")
	}
}

func TestParseWAV_RejectsNonWAVPayload(t *testing.T) {
	if _, _, err := parseWAV([]byte("not a wav file")); err == nil {
		t.Fatal("parseWAV() error = nil, want an error for a non-WAV payload")
	}
}

func TestParseWAV_RejectsUnsupportedBitDepth(t *testing.T) {
	buf := buildWAV([]int16{1, 2}, 16000)
	// Overwrite the bits-per-sample field (offset 34) to 8, an unsupported depth.
	binary.LittleEndian.PutUint16(buf[34:36], 8)

	if _, _, err := parseWAV(buf); err == nil {
		t.Fatal("parseWAV() error = nil, want an error for an unsupported bit depth")
	}
}
