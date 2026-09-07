// Package api wires HTTP routes to thin handlers. Handlers here must not
// contain business logic — that lives in the domain packages introduced in
// later milestones (state, tasks, tools, policy, agent). As of M1 there is
// no domain logic to delegate to yet, so the only route is a health check.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"voxstate/backend/internal/activity"
	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/voice"
)

// NewRouter builds the top-level HTTP handler for the VoxState backend,
// wrapped with minimal structured request logging and (M8) permissive
// CORS so the Next.js frontend (a separate origin in dev) can call it.
// state.Store, tasks.Store, policy.Evaluator, agent.Agent, and
// voice.Manager are the only domain dependencies the API layer has — it
// owns machine/state/task/result/agent/voice endpoints and delegates all
// validation/business logic to those packages. activityLog is optional
// (nil is safe, see activityHandlers.list) — M8's read-only event-stream
// endpoint for the frontend; see internal/activity's doc comment for what
// it captures and why introducing it did not require touching any
// existing domain package's code.
func NewRouter(logger *slog.Logger, store *state.Store, taskStore *tasks.Store, evaluator *policy.Evaluator, ag *agent.Agent, voiceManager *voice.Manager, activityLog *activity.Handler) http.Handler {
	mh := &machineHandlers{store: store}
	th := &taskHandlers{store: taskStore}
	ph := &policyHandlers{tasks: taskStore, evaluator: evaluator}
	ah := &agentHandlers{agent: ag}
	vh := &voiceHandlers{manager: voiceManager}
	acth := &activityHandlers{log: activityLog}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /machines", mh.listMachines)
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
	mux.HandleFunc("GET /activity", acth.list)

	return withCORS(withRequestLogging(logger, mux))
}

// withCORS allows any origin to call this API. This is a hackathon-demo
// posture (the frontend and backend run on different localhost ports in
// dev, and there is no user-auth/session model anywhere in this codebase
// yet to scope origins against more precisely) — see CLAUDE.md's M8 notes
// for the explicit trade-off. It handles preflight OPTIONS requests
// itself rather than registering them per-route.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
