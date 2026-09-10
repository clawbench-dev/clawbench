//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
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

// normalizeTimeBound converts a frontend time-range bound into the local
// "2006-01-02 15:04:05" text SQLite compares created_at against.
//
// Two input shapes are accepted:
//   - date-only "2024-03-01" (from <input type="date">): `endOfDay` expands it
//     to 23:59:59 so the whole selected day is included; otherwise it becomes
//     00:00:00. Without this a date-only upper bound would lexicographically
//     sort before any same-day timestamp ("2024-03-01" < "2024-03-01 10:00:00").
//   - RFC3339 "2024-03-01T10:00:00Z": parsed and converted to local time so it
//     lines up with the local timestamps SQLite stores.
//
// Unparseable input is returned trimmed so callers never bind garbage that
// would silently match nothing.
func normalizeTimeBound(value string, endOfDay bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const layout = "2006-01-02 15:04:05"
	if t, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t.Format(layout)
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.Local().Format(layout)
	}
	return value
}

// ragResetting prevents concurrent reset requests.
var ragResetting atomic.Bool

// ServeRAGSearch handles POST /api/rag/search — hybrid/FTS/vector search.
// Auth: localhost bypasses auth (CLI); remote requires cookie.
// Project isolation: remote requests require project cookie; localhost (CLI) may omit it for global search.
func ServeRAGSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Remote requests require project cookie; localhost (CLI) may omit it for global search.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
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
	// Empty projectPath (CLI global search) searches across all projects.
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
// Project isolation: remote requires project cookie; localhost may omit it for cross-project access.
func ServeRAGMessage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Remote requests require project cookie; localhost (CLI) may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
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

	// Verify the message belongs to the authenticated project (skip for localhost global access)
	if projectPath != "" && msg.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

// ServeMessageSummarize handles POST /api/rag/message/summarize?id=<id> —
// generates a reading summary for a chat message on demand and returns it.
// Project isolation: remote requires project cookie; localhost may omit it.
func ServeMessageSummarize(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Remote requests require project cookie; localhost (CLI) may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
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

	// Verify the message belongs to the authenticated project (skip for localhost global access)
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
// Project isolation: remote requires project cookie; localhost may omit it for cross-project access.
func ServeRAGMessageIndexStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
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
// Project isolation: remote requires project cookie; localhost may omit it for cross-project access.
func ServeRAGSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Remote requests require project cookie; localhost (CLI) may omit it.
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the authenticated project (skip for localhost global access)
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

// ServeRAGReset handles POST /api/rag/reset — full rebuild: clears all RAG
// index data (chunks, FTS, vectors) and resets message indexed flags so the
// indexer will rebuild from scratch. Requires auth.
//
// No project-scoping: Unlike other RAG endpoints that isolate by project cookie,
// this reset intentionally operates globally because the RAG store (rag_chunks,
// rag_chunks_fts, rag_vec) is shared across all projects, and ResetAllIndexed
// must reset every message's indexed flag for consistency — a partial reset
// would leave orphaned vectors from other projects.
func ServeRAGReset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalStore == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
		return
	}

	// Prevent concurrent resets
	if ragResetting.Swap(true) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "RAGResetInProgress")
		return
	}
	defer ragResetting.Store(false)

	// Determine new embedding dimension (if embedder is available)
	newDim := 0
	if rag.GlobalEmbedder != nil {
		newDim = rag.GlobalEmbedder.Dim()
	}

	// Clear all RAG data (chunks, FTS, vec0) and reset embedding dimension
	if err := rag.GlobalStore.ResetForDimensionMismatch(newDim); err != nil {
		slog.Error("rag: full reset failed", slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "RAGResetFailed")
		return
	}

	// The store's dimension changed out of band; let the indexer re-sync on its
	// next health check instead of trusting its stale latch.
	rag.ResetIndexerDimensionSync()

	// Reset all messages' indexed flag so indexer will re-process them
	affected, err := service.ResetAllIndexed()
	if err != nil {
		slog.Error("rag: reset indexed flags failed", slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "RAGResetFailed")
		return
	}

	slog.Info("rag: full rebuild triggered", slog.Int64("messages_reset", affected))

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"messages_reset": affected,
	})
}

// ServeRAGRebuildFTS handles POST /api/rag/rebuild-fts — full-text index rebuild
// that is independent of the vector layer: it regenerates rag_chunks_fts from the
// existing chunk text without re-chunking, re-embedding, or resetting message
// indexed flags. Vector embeddings and chunk rows are left untouched.
//
// No project-scoping: the RAG store is shared across all projects, so the FTS
// index is rebuilt globally (same rationale as ServeRAGReset).
func ServeRAGRebuildFTS(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalStore == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
		return
	}

	// Prevent concurrent resets/rebuilds
	if ragResetting.Swap(true) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "RAGResetInProgress")
		return
	}
	defer ragResetting.Store(false)

	chunksRebuilt, err := rag.GlobalStore.RebuildFTS()
	if err != nil {
		slog.Error("rag: fts rebuild failed", slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "RAGResetFailed")
		return
	}

	slog.Info("rag: fts rebuild triggered", slog.Int64("chunks_rebuilt", chunksRebuilt))

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"chunks_rebuilt": chunksRebuilt,
	})
}

// ServeRAGResetVector handles POST /api/rag/reset-vector — vector-only rebuild:
// drops rag_vec and resets has_embedding flags, keeping chunk text and FTS intact.
// The indexer will re-embed existing chunks with the current model.
func ServeRAGResetVector(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalStore == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotAvailable")
		return
	}

	// Prevent concurrent resets
	if ragResetting.Swap(true) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "RAGResetInProgress")
		return
	}
	defer ragResetting.Store(false)

	// Determine new embedding dimension (if embedder is available)
	newDim := 0
	if rag.GlobalEmbedder != nil {
		newDim = rag.GlobalEmbedder.Dim()
	}

	// Clear vector data only (keep chunks and FTS intact)
	chunksReset, err := rag.GlobalStore.ResetVectorOnly(newDim)
	if err != nil {
		slog.Error("rag: vector reset failed", slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "RAGResetFailed")
		return
	}

	// The store's dimension changed out of band; let the indexer re-sync on its
	// next health check instead of trusting its stale latch.
	rag.ResetIndexerDimensionSync()

	slog.Info("rag: vector rebuild triggered", slog.Int64("chunks_reset", chunksReset))

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"chunks_reset": chunksReset,
	})
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
	if projectPath == "" && !middleware.IsLocalhost(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the authenticated project (skip for
	// localhost global access). Archived sessions are allowed.
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
	if projectPath == "" && !middleware.IsLocalhost(r) {
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
	// rejecting the request. Archive filter, time range, time sort and cursor
	// pagination apply here too. The frontend requests pages of searchLimit rows
	// and scrolls to load more, so there is no hard cap on the number shown.
	if req.Query == "" {
		cursor := normalizeCursorTime(req.Cursor)
		result, err := rag.RecentSessions(r.Context(), projectPath, searchLimit, req.Archived, req.SortOrder, fromTime, toTime, cursor, req.CursorID)
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
