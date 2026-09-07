package deepgram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	interfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
)

func TestTranscribe_SendsExplicitEncodingAndSampleRate(t *testing.T) {
	var gotAuth string
	var gotQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"channels": []map[string]any{
					{
						"alternatives": []map[string]any{
							{"transcript": "check vibration"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	stt := newSTT("test-key", &interfaces.ClientOptions{Host: srv.URL})

	text, err := stt.Transcribe(context.Background(), []int16{1, 2, 3, -1}, 48000)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if text != "check vibration" {
		t.Errorf("Transcribe() = %q, want %q", text, "check vibration")
	}

	if gotAuth != "token test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "token test-key")
	}
	if got := gotQuery.Get("sample_rate"); got != "48000" {
		t.Errorf("sample_rate query param = %q, want %q (must always be sent explicitly)", got, "48000")
	}
	if got := gotQuery.Get("encoding"); got != "linear16" {
		t.Errorf("encoding query param = %q, want %q", got, "linear16")
	}
}

func TestTranscribe_EmptyResultsReturnsEmptyString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"channels": []map[string]any{}}})
	}))
	defer srv.Close()

	stt := newSTT("test-key", &interfaces.ClientOptions{Host: srv.URL})

	text, err := stt.Transcribe(context.Background(), []int16{1}, 48000)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if text != "" {
		t.Errorf("Transcribe() = %q, want empty string for no channels/alternatives", text)
	}
}
