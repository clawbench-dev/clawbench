package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// defaultAgentSessionsPageSize is the page size when the client does not ask for
// one. Small enough that the first page lands fast on a phone, large enough that
// a typical agent's whole list arrives in one request.
const defaultAgentSessionsPageSize = 50

// maxAgentSessionsPageSize bounds what a client can request, so one call cannot
// pull an unbounded list.
const maxAgentSessionsPageSize = 200

// ServeAgentSessions handles GET /api/agents/{id}/sessions.
//
// It lists the conversations that use the agent — 1:1 chats, groups where it is
// a member, and groups where it is the host — across ALL projects, archived
// chats included. That scope is not a convenience: the settings page shows the
// agent's session COUNT, which is computed without a project predicate and
// without an archived filter, so a narrower list would contradict the number
// printed right above it.
//
// Keyset pagination: `limit` (default 50, max 200), `cursor` + `cursor_id` taken
// from the previous response. The response carries `total` (the same count the
// settings page displays) so the header cannot drift from the rows.
func ServeAgentSessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/sessions")
	if !isValidAgentID(agentID) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.RLock()
	agent := model.GetAgent(agentID)
	configMutex.RUnlock()
	if agent == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	limit := defaultAgentSessionsPageSize
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	// Clamp AFTER parsing so an oversized value cannot pull an unbounded list.
	if limit > maxAgentSessionsPageSize {
		limit = maxAgentSessionsPageSize
	}

	// Normalize the cursor the same way ServeSessions does: the client may send
	// the value back as ISO 8601 (it is a timestamp after all), but SQLite stores
	// "2006-01-02 15:04:05", and the keyset predicate compares text.
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		cursor = strings.ReplaceAll(cursor, "T", " ")
		cursor = strings.TrimSuffix(cursor, "Z")
		cursor = strings.TrimSuffix(cursor, "+00:00")
	}

	page, err := service.ListAgentSessions(
		agentID,
		limit,
		cursor,
		r.URL.Query().Get("cursor_id"),
	)
	if err != nil {
		slog.Error("failed to list sessions for agent", "agent", agentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		jsonSessions:   page.Sessions,
		jsonHasMore:    page.HasMore,
		"nextCursor":   page.NextCursor,
		"nextCursorId": page.NextCursorID,
		jsonTotal:      page.Total,
	})
}
