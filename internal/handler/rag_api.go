//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"
)

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
		ProjectPath      string `json:"project"`
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
	if req.Limit > 0 {
		defaultLimit = req.Limit
	}

	// Project isolation: use cookie-derived project path when set.
	// Empty projectPath (CLI global search) searches across all projects.
	params := rag.SearchParams{
		Query:            req.Query,
		Limit:            req.Limit,
		ProjectPath:      projectPath,
		Backend:          req.Backend,
		Role:             req.Role,
		SessionID:        req.SessionID,
		ExcludeSessionID: req.ExcludeSessionID,
		FromTime:         req.FromTime,
		ToTime:           req.ToTime,
	}

	searchPoolSize := model.ConfigInstance.RAG.SearchPoolSize
	result, err := rag.RAGSearch(r.Context(), rag.GlobalStore, rag.GlobalEmbedder, params, defaultLimit, searchPoolSize)
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

// ServeRAGStatus handles GET /api/rag/status — returns RAG availability status and indexing progress.
func ServeRAGStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	hasFTSData := rag.GlobalStore != nil && rag.GlobalStore.HasFTSData()
	embedderHealthy := rag.EmbedderHealthy()

	// Progress counters
	totalMessages, err := service.TotalMessageCount()
	if err != nil {
		slog.Warn("rag: failed to count total messages", slog.String("err", err.Error()))
	}
	indexedMessages, err := service.IndexedMessageCount()
	if err != nil {
		slog.Warn("rag: failed to count indexed messages", slog.String("err", err.Error()))
	}
	var totalChunks, embeddedChunks int
	if rag.GlobalStore != nil {
		totalChunks, err = rag.GlobalStore.ChunkCount()
		if err != nil {
			slog.Warn("rag: failed to count total chunks", slog.String("err", err.Error()))
		}
		embeddedChunks, err = rag.GlobalStore.EmbeddedChunkCount()
		if err != nil {
			slog.Warn("rag: failed to count embedded chunks", slog.String("err", err.Error()))
		}
	}
	hasVecData := embeddedChunks > 0

	mode := "none"
	if embedderHealthy && hasVecData {
		mode = "hybrid"
	} else if hasFTSData {
		mode = "fts"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"available":         hasFTSData || hasVecData,
		"mode":              mode,
		"has_fts_data":      hasFTSData,
		"has_vec_data":      hasVecData,
		"embedder_healthy":  embedderHealthy,
		"total_messages":    totalMessages,
		"indexed_messages":  indexedMessages,
		"total_chunks":      totalChunks,
		"embedded_chunks":   embeddedChunks,
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
	if req.Query == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return
	}

	searchLimit := model.ConfigInstance.RAG.SearchLimit
	if searchLimit <= 0 {
		searchLimit = 20
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
