package api

import (
	"net/http"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
	Time   string `json:"time"`
}

// healthHandler reports that the backend process is up. It intentionally
// does not check downstream dependencies (e.g. Postgres) — no such
// dependency is wired into the backend yet as of M1.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Time:   time.Now().UTC().Format(time.RFC3339),
	})
}
