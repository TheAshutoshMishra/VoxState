// Command server is the VoxState backend entry point. It wires up
// configuration, logging, HTTP routing, graceful shutdown, and the
// in-memory state, task, policy, agent/tool, and (M6) voice engines.
// There is still no LLM, database, or frontend integration.
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
	"voxstate/backend/internal/voice/deepgram"
	"voxstate/backend/internal/voice/livekit"
	"voxstate/backend/internal/voice/rime"
)

// demoToolDelay governs how long the HTTP-exposed demo tools "run" for
// before completing on their own — long enough that a demo operator can
// interleave a POST .../state call to change the machine mid-flight and
// observe the M5 stale-result rejection path live, short enough not to
// make the happy path annoyingly slow.
const demoToolDelay = 3 * time.Second

// activityLogCapacity bounds the M8 in-memory activity ring buffer (see
// internal/activity) — large enough to cover a demo session's worth of
// task/voice events, small enough to never be a meaningful memory
// concern for a process that already holds all state in memory anyway.
const activityLogCapacity = 200

func main() {
	cfg := config.Load()

	// activityLog taps the same structured log lines already written to
	// stdout (tasks.Store/voice.VoiceSession's lifecycle events) into a
	// bounded buffer the M8 frontend can poll via GET /activity — see
	// internal/activity's doc comment for why this needed no changes to
	// any domain package.
	activityLog := activity.NewHandler(slog.NewJSONHandler(os.Stdout, nil), activityLogCapacity)
	logger := slog.New(activityLog).With(
		"app_env", cfg.AppEnv,
	)

	stateStore := state.NewStore()
	taskStore := tasks.NewStore(stateStore, logger)
	evaluator := policy.NewEvaluator(stateStore)
	toolRegistry := tools.NewRegistry(
		tools.NewVibrationScan(demoToolDelay),
		tools.NewTemperatureScan(demoToolDelay),
	)
	ag := agent.New(stateStore, taskStore, evaluator, toolRegistry, nil)

	// Voice (M6): the bot joins LiveKit rooms as a raw participant (no Go
	// support exists for LiveKit's Agents framework — see
	// internal/voice/livekit's doc comment), synthesizes speech via Rime,
	// and transcribes inbound audio via Deepgram. main.go is the only
	// place these concrete provider clients are constructed and handed to
	// voice.Manager as interface values — see internal/voice's doc
	// comment for the dependency direction this preserves.
	roomProvisioner := livekit.NewProvisioner(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	transportFactory := livekit.NewTransportFactory(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret, cfg.RimeSampleRateHz)
	ttsFactory := func() voice.TTS {
		return rime.NewTTS(rime.Config{
			APIKey:       cfg.RimeAPIKey,
			ModelID:      cfg.RimeModelID,
			Speaker:      cfg.RimeSpeaker,
			SampleRateHz: cfg.RimeSampleRateHz,
		})
	}
	sttFactory := func() voice.STT {
		return deepgram.NewSTT(cfg.DeepgramAPIKey)
	}
	voiceManager := voice.NewManager(stateStore, ag, roomProvisioner, sttFactory, ttsFactory, transportFactory, logger)

	handler := api.NewRouter(logger, stateStore, taskStore, evaluator, ag, voiceManager, activityLog)
	srv := server.New(cfg.Addr(), handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting voxstate backend", "addr", cfg.Addr())

	if err := server.Run(ctx, srv, logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
