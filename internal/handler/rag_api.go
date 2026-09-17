//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"
)

// normalizeCursorTime converts a frontend cursor timestamp to the format SQLite
// stores in chat_sessions.created_at ("2006-01-02 15:04:05"). The frontend
// sends RFC3339 (e.g. "2026-05-16T15:25:50Z"); the T separator and zone suffix
// would otherwise make the lexicographic comparison miss.
func normalizeCursorTime(cursor string) string {
	if cursor == "" {
		return ""
	}
	cursor = strings.ReplaceAll(cursor, "T", " ")
	cursor = strings.TrimSuffix(cursor, "Z")
	cursor = strings.TrimSuffix(cursor, "+00:00")
	return cursor
}

// normalizeTimeBound converts a frontend time-range bound into the UTC
// "2006-01-02 15:04:05" text SQLite compares created_at against.
//
// Note the two search paths apply the window to different columns: search mode
// filters rag_chunks.created_at (when the matching message was written), while
// browse mode filters chat_sessions.created_at (when the session was created).
// A session created months ago whose only match is today therefore appears in
// search mode but not in browse mode. This mirrors the two modes' semantics —
// browse lists sessions, search lists matches — and is called out in the spec.
//
// The stored timestamps are UTC, not local:
//   - chat_sessions.created_at is filled by DEFAULT CURRENT_TIMESTAMP, which
//     SQLite evaluates in UTC ("2026-09-10 12:12:54").
//   - rag_chunks.created_at is bound from a time.Time, which the modernc driver
//     renders as UTC with a suffix ("2026-09-10 12:16:35 +0000 UTC").
//
// The user picks days in their own calendar, so a date-only bound is read as a
// local day and converted to the matching UTC instant. Parsing it with
// time.Parse would silently use UTC and shift the window by the zone offset —
// in UTC+8 the "today" preset would span local 08:00 → next-day 07:59 and drop
// every session created in the local small hours.
//
// Two input shapes are accepted:
//   - date-only "2024-03-01" (from <input type="date">): `endOfDay` expands it
//     to the last second of that local day so the whole day is included;
//     otherwise it becomes the start of the local day.
//   - RFC3339 "2024-03-01T10:00:00Z": an absolute instant, converted to UTC.
//
// Unparseable input is returned trimmed so callers never bind garbage that
// would silently match nothing.
func normalizeTimeBound(value string, endOfDay bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const layout = "2006-01-02 15:04:05"
	// An upper bound gets a literal fractional suffix so it sorts after a row
	// whose text continues past the second — rag_chunks stores
	// "…15:59:59 +0000 UTC", which is lexicographically greater than a bare
	// "…15:59:59" and would otherwise drop the final second of the range.
	// A literal, not a ".999999" format verb: Go omits trailing zeros, so the
	// verb would emit the bare second and defeat the purpose.
	const endSuffix = ".999999"
	render := func(t time.Time) string {
		if endOfDay {
			return t.UTC().Format(layout) + endSuffix
		}
		return t.UTC().Format(layout)
	}

	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		if endOfDay {
			// Expand to the last second of the selected local day. AddDate (not
			// Add 24h) so a DST transition day still lands on the next local
			// midnight before stepping back.
			t = t.AddDate(0, 0, 1).Add(-time.Second)
		}
		return render(t)
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return render(t)
	}
	return value
}

// Rebuild mutual exclusion lives in rag.RebuildCoordinator (single slot for all
// three rebuild kinds), so no handler-local guard is needed.

// ServeRAGSearch handles POST /api/rag/search — hybrid/FTS/vector search.
// Auth: a local AI token bypasses auth; remote requires cookie.
// Project isolation: remote requests require the project cookie; a local AI
// token may omit it for global search.
func ServeRAGSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Remote requests require the project cookie; a local AI token may omit it for global search.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	var req struct {
		Query            string `json:"q"`
		Limit            int    `json:"limit"`
		Backend          string `json:"backend"`
		Role             string `json:"role"`
		SessionID        string `json:"session_id"`
		ExcludeSessionID string `json:"exclude_session_id"`
		FromTime         string `json:"from"`
		ToTime           string `json:"to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Query == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return
	}

	defaultLimit := 5
	effectiveLimit := defaultLimit
	if req.Limit > 0 {
		effectiveLimit = req.Limit
	}

	// Project isolation: use cookie-derived project path when set.
	// Empty projectPath (AI-token global search) searches across all projects.
	params := rag.SearchParams{
		Query:            req.Query,
		ProjectPath:      projectPath,
		Backend:          req.Backend,
		Role:             req.Role,
		SessionID:        req.SessionID,
		ExcludeSessionID: req.ExcludeSessionID,
		FromTime:         req.FromTime,
		ToTime:           req.ToTime,
	}

	searchPoolSize := model.ConfigInstance.RAG.SearchPoolSize
	result, err := rag.RAGSearch(r.Context(), rag.GlobalStore, rag.GlobalEmbedder, params, effectiveLimit, searchPoolSize)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGSearchFailed")
		return
	}

	if result.Results == nil {
		result.Results = []rag.SearchHit{}
	}
	writeJSON(w, http.StatusOK, result)
}

// ServeRAGMessage handles GET /api/rag/message?id=<id> — get full message by ID.
// Project isolation: remote requires the project cookie; a local AI token may omit it for cross-project access.
func ServeRAGMessage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Remote requests require the project cookie; a local AI token may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageIdRequired")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	msg, err := service.GetMessageByID(id)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "MessageNotFound")
		return
	}

	// Verify the message belongs to the authenticated project (skip for AI-token global access)
	if projectPath != "" && msg.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

// ServeMessageSummarize handles POST /api/rag/message/summarize?id=<id> —
// generates a reading summary for a chat message on demand and returns it.
// Project isolation: remote requires the project cookie; a local AI token may omit it.
func ServeMessageSummarize(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Remote requests require the project cookie; a local AI token may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageIdRequired")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	msg, err := service.GetMessageByID(id)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "MessageNotFound")
		return
	}

	// Verify the message belongs to the authenticated project (skip for AI-token global access)
	if projectPath != "" && msg.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	summary, cards, ok, err := service.GenerateMessageSummaryOnDemand(id)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "SummarizeFailed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary":      summary,
		"summaryCards": cards,
		"hasSummary":   ok,
	})
}

// ServeRAGMessageIndexStatus handles GET /api/rag/message-index-status?id=<id> —
// returns FTS and vector embedding status for a specific message.
// Project isolation: remote requires the project cookie; a local AI token may omit it for cross-project access.
func ServeRAGMessageIndexStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageIdRequired")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	// Verify message exists and belongs to the authenticated project
	msg, err := service.GetMessageByID(id)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "MessageNotFound")
		return
	}
	if projectPath != "" && msg.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Query RAG store for index status
	ftsIndexed, vecIndexed := false, false
	if rag.GlobalStore != nil {
		fts, vec, err := rag.GlobalStore.GetMessageIndexStatus(id)
		if err != nil {
			slog.Warn("rag: failed to get message index status", slog.Int64("message_id", id), slog.String("err", err.Error()))
		} else {
			ftsIndexed, vecIndexed = fts, vec
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"fts_indexed": ftsIndexed,
		"vec_indexed": vecIndexed,
	})
}

// ServeRAGSession handles GET /api/rag/session?id=<id> — get all messages in a session.
// Project isolation: remote requires the project cookie; a local AI token may omit it for cross-project access.
func ServeRAGSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Remote requests require the project cookie; a local AI token may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the authenticated project (skip for AI-token global access)
	if projectPath != "" {
		if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != projectPath {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
	}

	messages, err := service.GetMessagesBySessionID(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	if messages == nil {
		messages = []model.ChatMessage{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"messages":   messages,
		"total":      len(messages),
	})
}

// ServeRAGRebuild handles POST /api/rag/rebuild — starts an asynchronous index
// rebuild of the requested kind ("fts" | "vector" | "full").
//
// All three kinds share one shape: mark the target layer stale, then let the
// indexer redo that work in bounded batches. The handler therefore only marks and
// returns 202; clients poll GET /api/rag/rebuild/status for progress.
//
// The work is never performed inline. Re-segmentation alone measured ~149s on a
// 44k-chunk production store, far beyond the frontend's 10s request timeout, so
// an inline rebuild completed successfully on the server while the UI reported
// failure. See rag.RebuildKind for what each kind discards.
//
// No project-scoping: the RAG store is shared across all projects, so the index
// is rebuilt globally.
func ServeRAGRebuild(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalStore == nil || rag.GlobalRebuildCoordinator == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
		return
	}

	var req struct {
		Kind string `json:"kind"`
	}
	// An empty body is accepted and treated as the default kind, so a bare POST
	// (and the older FTS-only client) keeps working.
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	kind := rag.RebuildKind(req.Kind)
	if req.Kind == "" {
		kind = rag.RebuildFTS
	}

	err := rag.GlobalRebuildCoordinator.Start(rag.GlobalStore, kind)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status": "accepted",
			"kind":   string(kind),
		})
	case errors.Is(err, rag.ErrRebuildInProgress):
		writeLocalizedErrorf(w, r, http.StatusConflict, "RAGResetInProgress")
	case errors.Is(err, rag.ErrSegmenterUnavailable):
		// Refused before anything was modified: SegmentText would fall back to
		// returning raw text and overwrite correctly segmented CJK.
		slog.Error("rag: rebuild refused, segmenter unavailable")
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGSegmenterUnavailable")
	case errors.Is(err, rag.ErrIndexerUnavailable):
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
	case errors.Is(err, rag.ErrStoreUnavailable):
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
	default:
		slog.Error("rag: rebuild start failed", slog.String("kind", req.Kind), slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
	}
}

// ServeRAGRebuildStatus handles GET /api/rag/rebuild/status — returns the
// progress of the background rebuild so the UI can poll instead of holding a
// request open for the duration.
func ServeRAGRebuildStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if rag.GlobalRebuildCoordinator == nil {
		// No coordinator (RAG disabled): report idle rather than an error so the
		// client can treat it as "nothing running" uniformly.
		writeJSON(w, http.StatusOK, rag.RebuildStatus{Status: "idle"})
		return
	}

	writeJSON(w, http.StatusOK, rag.GlobalRebuildCoordinator.GetStatus())
}

// ServeRAGStatus handles GET /api/rag/status — returns RAG availability status and indexing progress.
func ServeRAGStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Mode determination: config-based, not data-based.
	// FTS is always enabled when store exists; VectorEnabled controls vector mode.
	// Empty data (e.g. after rebuild) doesn't change mode — indexer will fill data.
	vectorEnabled := model.ConfigInstance.RAG.VectorEnabled

	mode := "none"
	if rag.GlobalStore != nil {
		if vectorEnabled && rag.EmbedderHealthy() {
			mode = "hybrid"
		} else {
			mode = "fts"
		}
	}

	hasFTSData := rag.GlobalStore != nil && rag.GlobalStore.HasFTSData()
	embedderHealthy := rag.EmbedderHealthy()

	// Progress counters — combined queries to reduce round trips
	totalMessages, indexedMessages, err := service.MessageIndexCounts()
	if err != nil {
		slog.Warn("rag: failed to count messages", slog.String("err", err.Error()))
	}
	var embeddedMessages int
	if rag.GlobalStore != nil {
		embeddedMessages, err = rag.GlobalStore.EmbeddedMessageCount()
		if err != nil {
			slog.Warn("rag: failed to count embedded messages", slog.String("err", err.Error()))
		}
	}
	hasVecData := embeddedMessages > 0 && vectorEnabled

	// Per-index disk footprint. Best-effort: dbstat may be unavailable, in
	// which case sizes stay at 0 and the UI simply hides them.
	var ftsBytes, vecBytes int64
	if rag.GlobalStore != nil {
		ftsBytes, vecBytes, err = rag.GlobalStore.IndexDiskUsage()
		if err != nil {
			slog.Warn("rag: failed to compute index disk usage", slog.String("err", err.Error()))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"available":         hasFTSData || hasVecData,
		"mode":              mode,
		"has_fts_data":      hasFTSData,
		"has_vec_data":      hasVecData,
		"embedder_healthy":  embedderHealthy,
		"total_messages":    totalMessages,
		"indexed_messages":  indexedMessages,
		"embedded_messages": embeddedMessages,
		"fts_size_bytes":    ftsBytes,
		"vec_size_bytes":    vecBytes,
	})
}

// ServeRAGSessionFirstMessage handles GET /api/rag/session-first-message?session_id=<id>
// — lazily returns the earliest message of a session for the session-search
// browse detail view. The browse list intentionally omits message content for
// performance, so the detail preview is fetched on demand. Archived sessions
// are supported. Project isolation: remote requires the project cookie.
func ServeRAGSessionFirstMessage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the authenticated project (skip for
	// AI-token global access). Archived sessions are allowed.
	if projectPath != "" {
		sessionProject := service.GetSessionProjectPathIncludeArchived(sessionID)
		if sessionProject == "" || sessionProject != projectPath {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
	}

	msg, err := service.GetSessionFirstMessage(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "MessageNotFound")
		return
	}

	resp := struct {
		MessageID int64      `json:"message_id"`
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		CreatedAt *time.Time `json:"created_at"`
	}{}
	if msg != nil {
		resp.MessageID = msg.MessageID
		resp.Role = msg.Role
		resp.Content = msg.Content
		resp.CreatedAt = &msg.CreatedAt
	}

	writeJSON(w, http.StatusOK, resp)
}

// ServeRAGSessionSearch handles POST /api/rag/session-search — session-aggregated RAG search.
func ServeRAGSessionSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsAITokenRequest(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	var req struct {
		Query            string `json:"q"`
		Backend          string `json:"backend"`
		Role             string `json:"role"`
		SessionID        string `json:"session_id"`
		ExcludeSessionID string `json:"exclude_session_id"`
		FromTime         string `json:"from"`
		ToTime           string `json:"to"`
		PreferMode       string `json:"prefer_mode"`
		Archived         string `json:"archived"`
		SortOrder        string `json:"sort"`
		SessionType      string `json:"session_type"`
		Cursor           string `json:"cursor"`
		CursorID         string `json:"cursor_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Normalize the time-range bounds once: date-only inputs expand to the
	// start/end of the selected day in local time, RFC3339 inputs convert to
	// local, so both line up with the "2006-01-02 15:04:05" text SQLite stores.
	fromTime := normalizeTimeBound(req.FromTime, false)
	toTime := normalizeTimeBound(req.ToTime, true)

	searchLimit := model.ConfigInstance.RAG.SearchLimit
	if searchLimit <= 0 {
		searchLimit = 100
	}

	// Empty query → "browse all" mode: list the project's sessions instead of
	// rejecting the request. Archive filter, type filter, time range, time sort
	// and cursor pagination apply here too. The frontend requests pages of
	// searchLimit rows and scrolls to load more, so there is no hard cap on the
	// number shown.
	if req.Query == "" {
		cursor := normalizeCursorTime(req.Cursor)
		result, err := rag.RecentSessions(r.Context(), projectPath, searchLimit, req.Archived, req.SessionType, req.SortOrder, fromTime, toTime, cursor, req.CursorID)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGSearchFailed")
			return
		}
		if result.Sessions == nil {
			result.Sessions = []rag.SessionSearchResult{}
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	searchPoolSize := model.ConfigInstance.RAG.SearchPoolSize

	params := rag.SearchParams{
		Query:            req.Query,
		ProjectPath:      projectPath,
		Backend:          req.Backend,
		Role:             req.Role,
		SessionID:        req.SessionID,
		ExcludeSessionID: req.ExcludeSessionID,
		FromTime:         fromTime,
		ToTime:           toTime,
		PreferMode:       req.PreferMode,
		Archived:         req.Archived,
		SortOrder:        req.SortOrder,
		SessionType:      req.SessionType,
	}

	result, err := rag.RAGSessionSearch(r.Context(), rag.GlobalStore, rag.GlobalEmbedder, params, searchLimit, searchPoolSize)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGSearchFailed")
		return
	}

	if result.Sessions == nil {
		result.Sessions = []rag.SessionSearchResult{}
	}
	writeJSON(w, http.StatusOK, result)
}
