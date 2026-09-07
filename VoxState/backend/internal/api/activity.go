package api

import (
	"net/http"

	"voxstate/backend/internal/activity"
)

// activityHandlers exposes internal/activity's ring buffer over HTTP for
// M8's frontend event stream. Like every other handler group in this
// package, it contains no logic of its own — see internal/activity's doc
// comment for what is and isn't captured and why.
type activityHandlers struct {
	log *activity.Handler
}

type activityEntryResponse struct {
	Message string         `json:"message"`
	Time    string         `json:"time"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// list returns recently captured activity entries, most recent first. If
// no activity.Handler was wired in (e.g. a test router), it returns an
// empty list rather than a nil-pointer error — the endpoint degrades
// gracefully instead of being a hard dependency.
func (h *activityHandlers) list(w http.ResponseWriter, r *http.Request) {
	var entries []activity.Entry
	if h.log != nil {
		entries = h.log.List()
	}

	out := make([]activityEntryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, activityEntryResponse{
			Message: e.Message,
			Time:    e.Time.UTC().Format(timeFormat),
			Attrs:   e.Attrs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
