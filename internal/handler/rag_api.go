//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"
)

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

	writeJSON(w, http.StatusOK, map[string]any{
		"available":         hasFTSData || hasVecData,
		"mode":              mode,
		"has_fts_data":      hasFTSData,
		"has_vec_data":      hasVecData,
		"embedder_healthy":  embedderHealthy,
		"total_messages":    totalMessages,
		"indexed_messages":  indexedMessages,
		"embedded_messages": embeddedMessages,
	})
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
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	searchLimit := model.ConfigInstance.RAG.SearchLimit
	if searchLimit <= 0 {
		searchLimit = 100
	}

	// Empty query → "browse all" mode: list the project's sessions newest-first
	// instead of rejecting the request.
	if req.Query == "" {
		result, err := rag.RecentSessions(r.Context(), projectPath, searchLimit)
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
		FromTime:         req.FromTime,
		ToTime:           req.ToTime,
		PreferMode:       req.PreferMode,
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
