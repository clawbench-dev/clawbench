//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"context"
	"encoding/json"
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
			// Numbering is unified across agents per project — count all chat
			// sessions regardless of backend so switching agents does not
			// reset the counter (e.g. two claude sessions then a codebuddy
			// session should be "New Session 3", not "New Session 1").
			existingSessions, err := service.GetSessions(projectPath, "")
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

// ArchiveSession handles DELETE for archiving a single session.
func ArchiveSession(w http.ResponseWriter, r *http.Request) {
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

	// Cancel the running session before archiving to kill the CLI process.
	// This ensures no orphan CLI processes remain after archive.
	if service.IsSessionRunning(sessionID) {
		slog.Info("cancelling running session before archive", "session_id", sessionID)
		service.CancelSession(sessionID)
	}

	// Close the ACP connection for this session before archive
	// (GetSessionAgentID queries WHERE archived=0, so we must read it first)
	// Run in a goroutine because CloseConn calls cmd.Wait() which can
	// block indefinitely if the agent subprocess doesn't exit cleanly,
	// preventing the HTTP response from being sent.
	agentID := service.GetSessionAgentID(sessionID)
	if agentID != "" {
		if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
			slog.Info("acp: closing connection for archived session", "session_id", sessionID, "agent_id", agentID)
			go ai.GetACPConnManager().CloseConn(sessionID)
		}
	}

	// Empty sessions have nothing worth preserving for RAG — hard-delete instead.
	// Use GetFinalizedMessageCount to exclude streaming placeholder rows,
	// so a session with only a streaming row (e.g. interrupted mid-generation)
	// is still considered empty.
	msgCount := service.GetFinalizedMessageCount(sessionID)
	if msgCount == 0 {
		slog.Info("archiving empty session → hard-delete", "session_id", sessionID)

		// Delete RAG chunks (best-effort, no-op if RAG not initialized)
		if chunksDeleted, err := service.PurgeRAGChunksBySessionIDs([]string{sessionID}); err != nil {
			slog.Warn("failed to delete RAG chunks for empty archived session", "session_id", sessionID, "err", err)
		} else if chunksDeleted > 0 {
			slog.Info("deleted RAG chunks for empty archived session", "session_id", sessionID, "chunks", chunksDeleted)
		}

		if err := service.HardDeleteSession(sessionID); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to destroy empty session")))
			return
		}

		sessionCount, _ := service.GetSessionCount(projectPath)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": true, "sessionCount": sessionCount})
		return
	}

	if err := service.ArchiveSession(projectPath, backend, sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to archive session")))
		return
	}

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": false, "sessionCount": sessionCount})
}

// DestroySession handles DELETE for physically removing a session and all its data.
// Unlike ArchiveSession, this irreversibly removes the session
// from the database — chat_history, tool_calls, raw_responses, task_executions, and
// the session record itself.
func DestroySession(w http.ResponseWriter, r *http.Request) {
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

	// Cancel the running session before destroying to kill the CLI process.
	if service.IsSessionRunning(sessionID) {
		slog.Info("cancelling running session before destroy", "session_id", sessionID)
		service.CancelSession(sessionID)
	}

	// Close the ACP connection for this session before destroying
	agentID := service.GetSessionAgentID(sessionID)
	if agentID != "" {
		if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
			slog.Info("acp: closing connection for destroyed session", "session_id", sessionID, "agent_id", agentID)
			go ai.GetACPConnManager().CloseConn(sessionID)
		}
	}

	// Delete RAG chunks for this session before hard-deleting session data.
	// Best-effort — if RAG is not initialized, this is a no-op.
	if chunksDeleted, err := service.PurgeRAGChunksBySessionIDs([]string{sessionID}); err != nil {
		slog.Warn("failed to delete RAG chunks for destroyed session", "session_id", sessionID, "err", err)
	} else if chunksDeleted > 0 {
		slog.Info("deleted RAG chunks for destroyed session", "session_id", sessionID, "chunks", chunksDeleted)
	}

	if err := service.HardDeleteSession(sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to destroy session")))
		return
	}

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": true, "sessionCount": sessionCount})
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
		// Persist mode change to DB context_state so it survives restarts
		persistContextStateModeChange(sessionID, req.ModeID)
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
		// Persist thinking effort change to DB context_state so it survives restarts
		persistContextStateThinkingEffortChange(sessionID, req.ThinkingEffort)
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

// persistContextStateModeChange updates the mode currentModeId in DB context_state
// using atomic json_set() so it doesn't erase previously saved thinking/usage data.
func persistContextStateModeChange(sessionID, modeID string) {
	// Only update currentModeId; availableModes list stays as-is from ACP events.
	patch := &service.ModeStatePersist{CurrentModeID: modeID}
	patchJSON, _ := json.Marshal(patch)
	service.PatchContextStateMerge(sessionID, map[string]string{"mode": string(patchJSON)})
}

// persistContextStateThinkingEffortChange updates the thinkingEffort currentId in DB context_state
// using atomic json_set() so it doesn't erase previously saved mode/usage data.
func persistContextStateThinkingEffortChange(sessionID, effortID string) {
	patch := &service.ThinkingEffortPersist{CurrentID: effortID}
	patchJSON, _ := json.Marshal(patch)
	service.PatchContextStateMerge(sessionID, map[string]string{"thinkingEffort": string(patchJSON)})
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
		SessionID       string `json:"sessionId"`
		BeforeMessageID int64  `json:"beforeMessageId"`
		AgentID         string `json:"agentId"`
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

	// Resolve agent override: if the user selected a different agent, validate it exists
	overrideAgentID := req.AgentID
	if overrideAgentID != "" {
		if _, _, _, _, ok := resolveAgentConfig(overrideAgentID); !ok {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentID")
			return
		}
	}

	title := buildForkTitle(r, sourceID)

	newSessionID, err := service.ForkSession(sourceID, projectPath, title, req.BeforeMessageID, overrideAgentID)
	if err != nil {
		writeForkError(w, r, err)
		return
	}

	setSessionID(w, r, newSessionID)
	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": newSessionID, "sessionCount": sessionCount})
}

// buildForkTitle builds the title for a forked session.
// It uses the original session title with a fork emoji prefix.
func buildForkTitle(r *http.Request, sourceID string) string {
	sourceTitle, _ := service.GetSessionTitle(sourceID)
	if sourceTitle == "" {
		sourceTitle = T(r, "Session")
	}
	return T(r, "ForkPrefix") + sourceTitle
}

// writeForkError writes the appropriate error response for a ForkSession error.
func writeForkError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("handler: failed to fork session", "error", err)
	errMsg := err.Error()
	if strings.Contains(errMsg, "session limit") {
		writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
	} else if strings.Contains(errMsg, "not found in session") || strings.Contains(errMsg, "must be a user or assistant message") || strings.Contains(errMsg, "streaming message") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidForkPoint")
	} else if strings.Contains(errMsg, "not found") {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
	} else {
		model.WriteError(w, model.Internal(err))
	}
}
