//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessions handles GET (list) and POST (create) for chat sessions.
func ServeSessions(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // multi-method session handler
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Parse optional pagination parameters
		limit := 0
		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				limit = v
			}
		}
		cursor := r.URL.Query().Get("cursor")
		cursorID := r.URL.Query().Get("cursor_id")
		// Normalize cursor timestamp: frontend sends ISO 8601 (2026-05-16T15:25:50Z)
		// but SQLite stores as "2026-05-16 15:25:50". Convert T→space and strip Z/+00:00.
		if cursor != "" {
			cursor = strings.ReplaceAll(cursor, "T", " ")
			cursor = strings.TrimSuffix(cursor, "Z")
			cursor = strings.TrimSuffix(cursor, "+00:00")
		}

		var sessions []model.ChatSession
		var hasMore bool
		var err error

		if limit > 0 {
			sessions, hasMore, err = service.GetSessionsPaged(projectPath, "", limit, cursor, cursorID)
		} else {
			sessions, err = service.GetSessions(projectPath, "")
			hasMore = false
		}
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load sessions")))
			return
		}
		// Batch-check running state: single mutex acquisition instead of N
		runningIDs := service.GetRunningSessionIDs()
		runningSet := make(map[string]bool, len(runningIDs))
		for _, id := range runningIDs {
			runningSet[id] = true
		}
		// Batch-check pending approval state from ACP connection pool
		pendingApprovalSet := ai.GetACPConnManager().GetPendingApprovalSessionIDs()
		for i := range sessions {
			sessions[i].Running = runningSet[sessions[i].ID]
			sessions[i].PendingApproval = pendingApprovalSet[sessions[i].ID]
		}
		totalCount, _ := service.GetSessionCount(projectPath)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"sessions":   sessions,
			"hasMore":    hasMore,
			"totalCount": totalCount,
		})

	case http.MethodPost:
		// Check session count limit before creating (0 = unlimited)
		if model.SessionMaxCount > 0 {
			if count, cerr := service.GetSessionCount(projectPath); cerr == nil && count >= model.SessionMaxCount {
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
				return
			}
		}

		var req struct {
			Title   string `json:"title"`
			Backend string `json:"backend"`
			AgentID string `json:"agentId"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
		if !decodeJSON(w, r, &req) {
			return
		}
		backend := req.Backend
		agentID := req.AgentID
		resolvedAgentID := agentID
		agentSource := "default"
		backend2, _, _, _, ok := resolveAgentConfig(agentID)
		if !ok {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
			return
		}
		if backend2 != "" {
			backend = backend2
		}
		// Don't pre-fill agent default model into session — leave model empty so
		// the frontend falls back to the global localStorage preference, making the
		// user's model choice persist across projects. The model will be persisted
		// to the session only when the user explicitly sends a message with a modelId.
		agentModel := ""
		if resolvedAgentID == "" {
			resolvedAgentID = model.GetDefaultAgentID()
		}
		// If user explicitly specified an agent, mark source as "user"
		if agentID != "" {
			agentSource = "user"
		}
		if backend == "" {
			backend = "codebuddy"
		}
		title := req.Title
		if title == "" {
			existingSessions, err := service.GetSessions(projectPath, backend)
			if err == nil {
				title = T(r, "NewSessionN", map[string]any{"N": len(existingSessions) + 1})
			} else {
				title = T(r, "NewSession")
			}
		}
		sessionID, err := service.CreateSession(projectPath, backend, title, resolvedAgentID, agentModel, agentSource, "chat")
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to create session")))
			return
		}
		setSessionID(w, r, sessionID)
		// Return session count for UI indicator
		sessionCount, _ := service.GetSessionCount(projectPath)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": sessionID, "backend": backend, "agentId": resolvedAgentID, "sessionCount": sessionCount, "title": title})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// DeleteSession handles DELETE for a single session.
func DeleteSession(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodDelete) {
		return
	}

	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}

	backend := r.URL.Query().Get("backend")
	if backend == "" {
		backend = "codebuddy"
	}

	// Cancel the running session before deleting to kill the CLI process.
	// This ensures no orphan CLI processes remain after soft-delete.
	if service.IsSessionRunning(sessionID) {
		slog.Info("cancelling running session before delete", "session_id", sessionID)
		service.CancelSession(sessionID)
	}

	// Close the ACP connection for this session before soft-delete
	// (GetSessionAgentID queries WHERE deleted=0, so we must read it first)
	// Run in a goroutine because CloseConn calls cmd.Wait() which can
	// block indefinitely if the agent subprocess doesn't exit cleanly,
	// preventing the HTTP response from being sent.
	agentID := service.GetSessionAgentID(sessionID)
	if agentID != "" {
		if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
			slog.Info("acp: closing connection for deleted session", "session_id", sessionID, "agent_id", agentID)
			go ai.GetACPConnManager().CloseConn(sessionID)
		}
	}

	if err := service.DeleteSession(projectPath, backend, sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to delete session")))
		return
	}

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionCount": sessionCount})
}

// getSessionID retrieves session ID from query param or cookie.
func getSessionID(r *http.Request) string {
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		return sessionID
	}
	cookie, err := r.Cookie(model.ScopedCookieName("chat_session_id"))
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ServeAISessionUpdate handles PATCH /api/ai/session — immediately persists
// session-scoped settings (mode, thinkingEffort, model, transport) so they
// survive page reload even without sending a chat message.
func ServeAISessionUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPatch) {
		return
	}
	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}
	var req struct {
		ModeID         string `json:"modeId"`
		ThinkingEffort string `json:"thinkingEffort"`
		ModelID        string `json:"modelId"`
		Transport      string `json:"transport"`
		AutoApprove    *bool  `json:"autoApprove"` // pointer: distinguish "not sent" from false
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ModeID != "" {
		// Forward mode change to ACP agent so it updates its runtime state.
		// Run asynchronously — the RPC can block for up to 30s if the agent
		// is slow (e.g., Claude bridge adapter starting its CLI subprocess).
		// Blocking the HTTP handler would tie up a browser HTTP/1.1 connection
		// and prevent other requests (like session list) from being served.
		if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				conn.SetSessionConfigOption(ctx, "mode", req.ModeID)
			}()
		}
	}
	if req.ThinkingEffort != "" {
		// Forward thinking effort change to ACP agent — same async pattern as mode.
		if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				conn.SetSessionConfigOption(ctx, "thinkingEffort", req.ThinkingEffort)
			}()
		}
	}
	if req.ModelID != "" {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionModel(sessionID, req.ModelID)
	}
	if req.Transport != "" {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionTransport(sessionID, req.Transport)
		if req.Transport == "cli" {
			ai.GetACPConnManager().CloseConn(sessionID)
		}
	}
	if req.AutoApprove != nil {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionAutoApprove(sessionID, *req.AutoApprove)
		// Sync to ACPConn runtime state
		if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
			conn.SetAutoApprove(*req.AutoApprove)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// setSessionID sets session ID in cookie.
// HttpOnly: true prevents JavaScript access, mitigating XSS-based session hijack (ISS-123).
func setSessionID(w http.ResponseWriter, r *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     model.ScopedCookieName("chat_session_id"),
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// ServeForkSession handles POST /api/ai/session/fork — creates a new chat session
// by copying all messages from the current session (without external_session_id).
func ServeForkSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if !decodeJSON(w, r, &req) {
		return
	}
	// Use body sessionId if provided, otherwise fall back to query/cookie
	sourceID := req.SessionID
	if sourceID == "" {
		sourceID = sessionID
	}
	if sourceID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Build title: localized fork prefix + source session title
	sourceTitle, _ := service.GetSessionTitle(sourceID)
	if sourceTitle == "" {
		sourceTitle = T(r, "Session")
	}
	title := T(r, "ForkPrefix") + sourceTitle

	newSessionID, err := service.ForkSession(sourceID, projectPath, title)
	if err != nil {
		slog.Error("handler: failed to fork session", "source_session", sourceID, "error", err)
		if strings.Contains(err.Error(), "session limit") {
			writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
		} else if strings.Contains(err.Error(), "not found") {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		} else {
			model.WriteError(w, model.Internal(err))
		}
		return
	}

	setSessionID(w, r, newSessionID)
	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": newSessionID, "sessionCount": sessionCount})
}
