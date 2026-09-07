package api

import (
	"io"
	"log/slog"
	"net/http"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
	"voxstate/backend/internal/voice"
)

// newTestRouter builds a router backed by fresh, empty in-memory stores,
// with logging discarded so test output stays quiet. Demo tools run with
// zero artificial delay so tests stay fast. The voice.Manager is backed
// entirely by fakes (see voice_test.go) — this package's tests never
// import voice/livekit, voice/rime, or voice/deepgram, matching the "no
// third-party SDK in tests" convention voice's own tests also follow.
func newTestRouter() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	stateStore := state.NewStore()
	taskStore := tasks.NewStore(stateStore, logger)
	evaluator := policy.NewEvaluator(stateStore)
	registry := tools.NewRegistry(tools.NewVibrationScan(0), tools.NewTemperatureScan(0))
	ag := agent.New(stateStore, taskStore, evaluator, registry, nil)
	voiceManager := voice.NewManager(stateStore, ag, fakeRoomProvisioner{}, fakeSTTFactory, fakeTTSFactory, fakeTransportFactory, logger)
	return NewRouter(logger, stateStore, taskStore, evaluator, ag, voiceManager, nil)
}
