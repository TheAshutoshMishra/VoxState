// Command devserver is a local-development-only alternative to
// cmd/server, added for M8. It wires the exact same state/task/policy/
// agent stack and HTTP router as cmd/server, but replaces the real
// LiveKit/Rime/Deepgram provider implementations with trivial in-memory
// fakes so the binary builds and runs without the pkg-config/libopus
// system dependency internal/voice/livekit requires (see CLAUDE.md's
// "Environment note"), and without needing real LiveKit/Rime/Deepgram
// credentials.
//
// This exists so the M8 frontend can be developed and manually verified
// end to end (machine/state/task/policy/activity panels, all real data)
// on a machine that doesn't have libopus installed or third-party voice
// credentials configured. Voice session start/end lifecycle is
// demonstrable through this binary (the HTTP endpoints behave
// identically); actual LiveKit audio is not, since there is no real
// realtime server behind the fake room/token this binary returns. Use
// cmd/server for anything beyond frontend development against the
// non-voice parts of the API.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"voxstate/backend/internal/activity"
	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/api"
	"voxstate/backend/internal/config"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/server"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
	"voxstate/backend/internal/voice"
)

const demoToolDelay = 3 * time.Second
const activityLogCapacity = 200

func main() {
	cfg := config.Load()

	activityLog := activity.NewHandler(slog.NewJSONHandler(os.Stdout, nil), activityLogCapacity)
	logger := slog.New(activityLog).With("app_env", cfg.AppEnv, "mode", "devserver")

	stateStore := state.NewStore()
	taskStore := tasks.NewStore(stateStore, logger)
	evaluator := policy.NewEvaluator(stateStore)
	toolRegistry := tools.NewRegistry(
		tools.NewVibrationScan(demoToolDelay),
		tools.NewTemperatureScan(demoToolDelay),
	)
	ag := agent.New(stateStore, taskStore, evaluator, toolRegistry, nil)

	voiceManager := voice.NewManager(stateStore, ag, fakeProvisioner{}, fakeSTTFactory, fakeTTSFactory, fakeTransportFactory, logger)

	handler := api.NewRouter(logger, stateStore, taskStore, evaluator, ag, voiceManager, activityLog)
	srv := server.New(cfg.Addr(), handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Warn("starting voxstate devserver — voice is FAKED, not a real LiveKit connection; use cmd/server for real voice", "addr", cfg.Addr())

	if err := server.Run(ctx, srv, logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// --- trivial fakes, dev-harness only — mirror internal/voice/*_test.go's
// fakes in spirit, but exported/standalone since they live outside the
// voice package's own test files. ---

type fakeProvisioner struct{}

// Provision returns a syntactically valid but unreachable LiveKit URL
// (nothing listens on this port in this binary) — a real client's
// connection attempt fails with a genuine network error, matching what a
// misconfigured/unavailable LiveKit deployment would look like, rather
// than a URL-parsing error a real deployment would never hit.
func (fakeProvisioner) Provision(_ context.Context, sessionID string) (roomName, serverURL, token string, err error) {
	return "dev-room-" + sessionID, "ws://localhost:7880", "fake-token", nil
}

func (fakeProvisioner) Teardown(context.Context, string) error { return nil }

type fakeSTT struct{}

func (fakeSTT) Transcribe(context.Context, []int16, int) (string, error) { return "", nil }

func fakeSTTFactory() voice.STT { return fakeSTT{} }

type fakeTTS struct{}

func (fakeTTS) Synthesize(context.Context, string) ([]int16, int, error) { return nil, 0, nil }

func fakeTTSFactory() voice.TTS { return fakeTTS{} }

// fakeTransport never delivers inbound frames (Frames() is an empty,
// never-written channel) and treats every outbound call as a no-op — this
// binary has no real audio path, only the session-lifecycle HTTP
// endpoints.
type fakeTransport struct {
	frames chan voice.AudioFrame
}

func (f *fakeTransport) Send(context.Context, []int16, int) error { return nil }
func (f *fakeTransport) StopAudio() error                         { return nil }
func (f *fakeTransport) Frames() <-chan voice.AudioFrame          { return f.frames }
func (f *fakeTransport) Close() error                             { close(f.frames); return nil }

func fakeTransportFactory(context.Context, string, string) (voice.Transport, error) {
	return &fakeTransport{frames: make(chan voice.AudioFrame)}, nil
}
