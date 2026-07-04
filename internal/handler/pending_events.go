package handler

import (
	"net/http"

	"clawbench/internal/service"
)

// ServePendingEvents returns pending (missed) events for offline clients.
// GET /api/ai/events/pending?after=evt_12345
// The "after" query parameter is the last event_id the client has seen.
func ServePendingEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{strError: "method not allowed"})
		return
	}

	after := r.URL.Query().Get("after")

	events, err := service.GetPendingEvents(after)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{strError: "failed to fetch pending events"})
		return
	}

	if events == nil {
		events = []service.PendingEvent{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
	})
}
