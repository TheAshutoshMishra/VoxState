// Package api wires HTTP routes to thin handlers. Handlers here must not
// contain business logic — that lives in the domain packages introduced in
// later milestones (state, tasks, tools, policy, agent). As of M1 there is
// no domain logic to delegate to yet, so the only route is a health check.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/voice"
)

// NewRouter builds the top-level HTTP handler for the VoxState backend,
// wrapped with minimal structured request logging. state.Store,
// tasks.Store, policy.Evaluator, agent.Agent, and voice.Manager are the
// only dependencies the API layer has as of M6 — it owns machine/state/
// task/result/agent/voice endpoints and delegates all validation/
// business logic to those packages.
func NewRouter(logger *slog.Logger, store *state.Store, taskStore *tasks.Store, evaluator *policy.Evaluator, ag *agent.Agent, voiceManager *voice.Manager) http.Handler {
	mh := &machineHandlers{store: store}
	th := &taskHandlers{store: taskStore}
	ph := &policyHandlers{tasks: taskStore, evaluator: evaluator}
	ah := &agentHandlers{agent: ag}
	vh := &voiceHandlers{manager: voiceManager}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /machines", mh.createMachine)
	mux.HandleFunc("GET /machines/{id}", mh.getMachine)
	mux.HandleFunc("GET /machines/{id}/state", mh.getState)
	mux.HandleFunc("POST /machines/{id}/state", mh.changeState)
	mux.HandleFunc("POST /machines/{id}/tasks", th.createTask)
	mux.HandleFunc("GET /machines/{id}/tasks", th.listTasksForMachine)
	mux.HandleFunc("GET /tasks/{id}", th.getTask)
	mux.HandleFunc("POST /tasks/{id}/start", th.startTask)
	mux.HandleFunc("POST /tasks/{id}/cancel", th.cancelTask)
	mux.HandleFunc("POST /tasks/{id}/result", ph.simulateAndEvaluateResult)
	mux.HandleFunc("POST /machines/{id}/agent/run", ah.run)
	mux.HandleFunc("POST /machines/{id}/voice/sessions", vh.start)
	mux.HandleFunc("GET /voice/sessions/{id}", vh.get)
	mux.HandleFunc("POST /voice/sessions/{id}/end", vh.end)

	return withRequestLogging(logger, mux)
}

// withRequestLogging logs one structured line per request. This is the
// extent of observability M1 introduces; anything beyond this (tracing,
// metrics) is out of scope until a milestone actually needs it.
func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}
