package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

const (
	strBlocks    = "blocks"
	strUser      = "user"
	strAssistant = "assistant"
	strContent   = "content"
	strSessionID = "sessionId"
	strError     = "error"
)

// getOrCreateConnForLoadFn is the function signature for obtaining an ACP
// connection for loading a session. Used to allow test overrides.
type getOrCreateConnForLoadFn func(ctx context.Context, agent *model.Agent, clawbenchSID, acpSessionID, cwd string) (*ai.ACPConn, error)

// getOrCreateConnForLoad is the function used by ServeACPLoadSession to obtain
// an ACP connection for loading a session. Defaults to the real implementation;
// can be overridden in tests.
var getOrCreateConnForLoad getOrCreateConnForLoadFn = defaultGetOrCreateConnForLoad

func defaultGetOrCreateConnForLoad(ctx context.Context, agent *model.Agent, clawbenchSID, acpSessionID, cwd string) (*ai.ACPConn, error) {
	return ai.GetACPConnManager().GetOrCreateConnForLoad(ctx, agent, clawbenchSID, acpSessionID, cwd)
}

// ServeSessionResume handles POST /api/ai/session/resume — restores a soft-deleted
// session and returns the session ID. Validates project ownership and session count limits.
func ServeSessionResume(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Check session exists and belongs to project
	var sessionProjectPath string
	var deleted int
	err := service.DBRead.QueryRowContext(
		r.Context(),
		"SELECT project_path, deleted FROM chat_sessions WHERE id = ?",
		req.SessionID,
	).Scan(&sessionProjectPath, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	// Project isolation
	if sessionProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// If soft-deleted, check session count limit before restoring
	if deleted == 1 {
		if model.SessionMaxCount > 0 {
			var count int
			err = service.DBRead.QueryRowContext(
				r.Context(),
				"SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND deleted = 0 AND session_type = 'chat'",
				sessionProjectPath,
			).Scan(&count)
			if err != nil {
				model.WriteError(w, model.Internal(err))
				return
			}
			// Restoring a soft-deleted session would increase active count by 1
			if count+1 > model.SessionMaxCount {
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{
					"Count": count,
					"Limit": model.SessionMaxCount,
				})
				return
			}
		}

		// Restore the session
		_, err = service.DB.ExecContext(
			r.Context(),
			"UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
			req.SessionID,
		)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to restore session %s: %w", req.SessionID, err)))
			return
		}
		slog.Info("session restored from soft-delete",
			slog.String("session", req.SessionID),
			slog.String("project", sessionProjectPath))
	} else {
		slog.Info("session resume requested (already active)",
			slog.String("session", req.SessionID),
			slog.String("project", sessionProjectPath))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": req.SessionID,
	})
}

// ServeACPLoadSession handles POST /api/ai/session/acp-load — creates a new ClawBench
// session by loading an existing ACP session via LoadSession. The agent replays the
// full conversation history which is collected and saved to chat_history.
//
//nolint:gocognit,gocyclo // ServeACPLoadSession orchestrates multi-step ACP session loading with replay collection and batch persistence; refactoring would obscure the sequential flow
func ServeACPLoadSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
		return
	}

	var req struct {
		AgentID      string `json:"agentId"`
		AcpSessionID string `json:"acpSessionId"`
		ProjectID    string `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AgentID == "" || req.AcpSessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	// Validate agent exists and supports LoadSession
	configMutex.RLock()
	agent, ok := model.Agents[req.AgentID]
	configMutex.RUnlock()

	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	if !agent.SupportsACP() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	reg := ai.GetAgentCapabilityRegistry()
	if !reg.GetLoadSession(req.AgentID) {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return
	}

	// Check if a ClawBench session already exists for this ACP session.
	// source_session_id = "acp:{acpSessionId}" tracks the ACP session origin.
	sourceID := "acp:" + req.AcpSessionID
	var existingID string
	var existingDeleted int
	err := service.DBRead.QueryRow( //nolint:noctx // r.Context() not easily propagated through ServeACPLoadSession
		"SELECT id, deleted FROM chat_sessions WHERE source_session_id = ? AND session_type = 'chat' ORDER BY deleted ASC, updated_at DESC LIMIT 1",
		sourceID,
	).Scan(&existingID, &existingDeleted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("handler: failed to check existing ACP session", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	if existingID != "" {
		// A session for this ACP session already exists.
		// Hard-delete the old session and its data so we can recreate
		// it fresh with the latest replay from the ACP agent.
		slog.Info("handler: hard-deleting existing session for ACP reload",
			"old_session", existingID,
			"acp_sid", req.AcpSessionID,
			"was_deleted", existingDeleted == 1)
		if errHardDel := service.HardDeleteSession(existingID); errHardDel != nil {
			slog.Error("handler: failed to hard-delete existing ACP session", "error", errHardDel)
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
	}

	// Create new ClawBench session
	sessionID, err := service.CreateSession(projectPath, agent.Backend, "", req.AgentID, "", "default", "chat")
	if err != nil {
		slog.Error("handler: failed to create session for acp-load", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Set SourceSessionID to track the ACP session origin
	if errSrc := service.UpdateSessionSourceID(sessionID, "acp:"+req.AcpSessionID); errSrc != nil {
		slog.Warn("handler: failed to update source_session_id", "session_id", sessionID, "error", errSrc)
	}

	// Set transport to acp-stdio for ACP-loaded sessions
	if errTransport := service.UpdateSessionTransport(sessionID, transportACP); errTransport != nil {
		slog.Warn("handler: failed to update transport for acp-load session", "session_id", sessionID, "error", errTransport)
	}

	// Load ACP session via connection manager
	conn, err := getOrCreateConnForLoad(r.Context(), agent, sessionID, req.AcpSessionID, projectPath)
	if err != nil {
		slog.Error("handler: LoadSession failed", "agent", req.AgentID, "acp_sid", req.AcpSessionID, "error", err)
		// Clean up the session we just created
		_ = service.DeleteSession(projectPath, agent.Backend, sessionID)
		// Clean up the dead connection from the pool
		ai.GetACPConnManager().CloseConn(sessionID)
		// Detect "Resource not found" from ACP agent — session no longer exists
		if ai.IsACPResourceNotFound(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Collect replayed messages from the buffer and parse through
	// mapACPSessionUpdate to produce properly structured content blocks
	// (same pipeline as live streaming), instead of storing raw ACP JSON.
	// Some ACP agents (e.g., OpenCode) send replay notifications AFTER the
	// LoadSession RPC response returns. Wait briefly for late notifications
	// before reading the buffer.
	client := conn.GetClient()
	type persistedMessage struct {
		role    string
		content string // JSON: {"blocks":[...]}
	}
	var messages []persistedMessage
	if client != nil {
		// Wait for late-arriving SessionUpdate notifications.
		// Some ACP agents replay history via notifications that arrive
		// after the LoadSession RPC response. A short delay ensures
		// these are captured before we read the buffer.
		time.Sleep(500 * time.Millisecond)
		buf := client.GetAndClearLoadSessionBuf()

		// Accumulate blocks across notifications, splitting on role boundaries.
		var blocks []model.ContentBlock
		var currentRole string // strUser or strAssistant

		flushBlocks := func() {
			if len(blocks) == 0 || currentRole == "" {
				return
			}
			blocks = ai.MergeConsecutiveThinkingBlocks(blocks)
			contentMap := map[string]any{strBlocks: blocks}
			if currentRole == strAssistant {
				contentMap["metadata"] = map[string]any{
					"transport": transportACP,
				}
			}
			contentJSON, _ := json.Marshal(contentMap)
			messages = append(messages, persistedMessage{
				role:    currentRole,
				content: string(contentJSON),
			})
			blocks = nil
		}

		for _, n := range buf {
			// Determine the role of this notification
			notifRole := strAssistant
			if n.Update.UserMessageChunk != nil {
				notifRole = strUser
			}

			// Flush accumulated blocks when role changes
			if notifRole != currentRole && currentRole != "" {
				flushBlocks()
			}
			currentRole = notifRole

			// UserMessageChunk is not handled by mapACPSessionUpdate —
			// extract text directly from the ACP notification.
			if n.Update.UserMessageChunk != nil {
				if text := n.Update.UserMessageChunk.Content.Text; text != nil && text.Text != "" {
					ai.AccumulateBlock(&blocks, ai.StreamEvent{Type: strContent, Content: text.Text})
				}
				continue
			}

			// Parse the SessionUpdate through the same pipeline used for
			// live streaming (mapACPSessionUpdate → StreamEvent → AccumulateBlock)
			ch := make(chan ai.StreamEvent, 64)
			ai.MapACPSessionUpdateForTest(n.Update, ch)
			close(ch)
			for event := range ch {
				// Skip non-content events (mode_update, config_update, etc.)
				switch event.Type {
				case strContent, "thinking", "thinking_done", "tool_use", "tool_result", "warning", strError:
					ai.AccumulateBlock(&blocks, event)
				}
			}
		}
		// Flush remaining blocks
		flushBlocks()
	}

	// Now that the buffer has been read, clear loadSessionActive so future
	// SessionUpdate notifications are routed normally (to SSE or dropped).
	conn.ClearLoadSessionActive()

	// Batch insert replay messages to chat_history
	for _, msg := range messages {
		_, err := service.DB.Exec( //nolint:noctx // r.Context() not easily propagated through ServeACPLoadSession
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, indexed) VALUES (?, ?, ?, ?, ?, 0, 0)",
			projectPath, agent.Backend, sessionID, msg.role, msg.content,
		)
		if err != nil {
			slog.Error("handler: failed to save LoadSession replay message", "error", err)
		}
	}

	// Set session title from first user message
	for _, msg := range messages {
		if msg.role == strUser {
			title := service.ExtractPlainText(msg.content)
			if title != "" {
				if runes := []rune(title); len(runes) > 50 {
					title = string(runes[:50]) + "..."
				}
				if err := service.UpdateSessionTitle(sessionID, title); err != nil {
					slog.Warn("handler: failed to set title for acp-load session", "session_id", sessionID, "error", err)
				}
			}
			break
		}
	}

	slog.Info("handler: acp-load completed",
		"session_id", sessionID,
		"agent", req.AgentID,
		"acp_sid", req.AcpSessionID,
		"messages", len(messages))

	writeJSON(w, http.StatusOK, map[string]any{
		strSessionID: sessionID,
	})
}
