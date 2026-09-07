package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestActivity_HTTP covers M8's new GET /activity endpoint. newTestRouter
// wires a nil activity.Handler (matching a discard-everything logger, per
// its own doc comment), so this only asserts the degrade-gracefully path
// (empty list, not an error) — internal/activity's own tests cover actual
// capture behavior.
func TestActivity_HTTP(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodGet, "/activity", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var list []activityEntryResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if list == nil {
		t.Error("decoded list is nil, want an empty slice (frontend should never need to special-case null)")
	}
}

// TestCORS_HeadersPresent covers M8's permissive CORS middleware, needed
// for the Next.js frontend (a different origin in dev) to call this API.
func TestCORS_HeadersPresent(t *testing.T) {
	router := newTestRouter()

	rec := doJSON(t, router, http.MethodGet, "/health", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}
