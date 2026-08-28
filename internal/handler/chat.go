//nolint:errcheck,gocyclo,gocognit,gosec,goconst // legacy file, nolint-only approach for diff stability
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/rag"
	"clawbench/internal/service"
	"clawbench/internal/ws"
)

const maxChatBodySize = 10 << 20 // 10MB

// ServeAISession handles DELETE for Claude CLI internal session files.
func ServeAISession(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodDelete) {
		return
	}

	// Get Claude session directory using cross-platform path mangling
	sessionDir := platform.ClaudeProjectDir(projectPath)

	// Delete all .jsonl session files
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		// Session dir doesn't exist — nothing to delete, treat as success
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": 0})
		return
	}

	deleted := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			if err := os.Remove(filepath.Join(sessionDir, entry.Name())); err == nil {
				deleted++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": deleted})
}

// AIChat handles GET (status/history) and POST (send message) for AI chat.
func AIChat(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	// GET: return full chat history + running status
	if r.Method == http.MethodGet {
		// Check if a specific session is requested
		requestedSessionID := r.URL.Query().Get("session_id")

		var sessionID string
		var sessionBackend string
		var cachedSessionInfo *service.SessionInfo // reused to avoid extra DB queries

		if requestedSessionID != "" {
			// Use the requested session — single query to get backend + project_path + metadata
			sessionID = requestedSessionID
			cachedSessionInfo = service.GetSessionFullInfo(sessionID)
			if cachedSessionInfo == nil {
				writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
				return
			}
			sessionBackend = cachedSessionInfo.Backend
			// Verify the session belongs to the requesting project (ISS-180)
			if cachedSessionInfo.ProjectPath != projectPath {
				writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
				return
			}
		} else {
			// No specific session requested — use lightweight query to find the most recent session
			latestID, latestBackend, err := service.GetLatestSessionID(projectPath)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					model.WriteError(w, model.Internal(fmt.Errorf("failed to find latest session")))
					return
				}
				// No sessions exist, create a new one with default agent.
				// Don't pre-fill agent default model — leave empty so frontend
				// falls back to global localStorage preference (cross-project).
				agentID := model.GetDefaultAgentID()
				sessionBackend2, _, _, _, ok := resolveAgentConfig(agentID)
				if !ok {
					writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
					return
				}
				sessionID, err = service.CreateSession(projectPath, sessionBackend2, T(r, "NewSession"), agentID, "", "default", "chat")
				if err != nil {
					model.WriteError(w, model.Internal(fmt.Errorf("failed to create session")))
					return
				}
			} else {
				sessionID = latestID
				sessionBackend = latestBackend
			}
		}

		// Always update cookie with current session ID
		setSessionID(w, r, sessionID)
		// Mark session as read
		service.UpdateLastRead(sessionID)

		// Parse pagination params
		// Supports both before_id (preferred, integer cursor) and before (legacy, timestamp cursor).
		// before_id takes priority when both are provided.
		limit := 0
		beforeID := 0
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		if bid := r.URL.Query().Get("before_id"); bid != "" {
			if id, err := strconv.Atoi(bid); err == nil && id > 0 {
				beforeID = id
			}
		}
		// Legacy: accept "before" (timestamp) for backward compatibility with older clients.
		// When before_id is absent and before is present, fall back to timestamp-based lookup.
		if beforeID == 0 {
			if bt := r.URL.Query().Get("before"); bt != "" {
				if id, err := service.GetMessageIDBeforeTime(projectPath, sessionBackend, sessionID, bt); err == nil && id > 0 {
					beforeID = id
				}
			}
		}

		// If limit not specified, use config default
		if limit == 0 {
			limit = model.ChatInitialMessages
		}
		// Cap limit to prevent abuse
		if limit > 100 {
			limit = 100
		}

		totalCount := 0
		queuedCount := 0
		messages, totalCount, queuedCount, err := service.GetChatHistoryPaged(projectPath, sessionBackend, sessionID, limit, beforeID)
		// Use cached session info from earlier lookup, or fetch if not yet available
		// (e.g. when session was found via GetLatestSessionID or newly created).
		// This avoids an extra DB query for the common case of switching to an existing session.
		if cachedSessionInfo == nil {
			cachedSessionInfo = service.GetSessionFullInfo(sessionID)
		}
		var sessionTitle, sessionAgentID, sessionModelID, sessionTransport string
		var sessionAutoApprove bool
		var sessionInfoBackend string
		if cachedSessionInfo != nil {
			sessionTitle = cachedSessionInfo.Title
			sessionInfoBackend = cachedSessionInfo.Backend
			sessionAgentID = cachedSessionInfo.AgentID
			sessionModelID = cachedSessionInfo.Model
			sessionTransport = cachedSessionInfo.Transport
			sessionAutoApprove = cachedSessionInfo.AutoApprove
		}
		if sessionInfoBackend != "" {
			sessionBackend = sessionInfoBackend
		}
		running := service.IsSessionRunning(sessionID)

		// Look up cached ACP mode/thinking/model list state for this session.
		// This allows the frontend to populate mode chips immediately
		// without waiting for WS events (which may have already been consumed).
		// Fallback: for brand-new sessions with no pool session mapping yet,
		// look up from AgentCapabilityRegistry so mode chips appear on first load.
		// For CLI sessions, synthesize a read-only mode from the backend name
		// so the mode chip is visible but non-switchable.
		var modeState, thinkingEffortState, modelListState, planState, usageState any
		var commands []ai.AvailableCommandInfo
		var replayPending bool
		if sessionID != "" && sessionTransport != "cli" {
			if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID); s.Mode != nil || s.Effort != nil || len(s.Commands) > 0 || s.ModelList != nil || s.Plan != nil || s.Usage != nil || s.ReplayPending {
				modeState = s.Mode
				thinkingEffortState = s.Effort
				commands = s.Commands
				modelListState = s.ModelList
				planState = s.Plan
				usageState = s.Usage
				replayPending = s.ReplayPending
			} else if sessionAgentID != "" {
				// No session-level mapping yet (new session, never sent a message).
				// Fall back to agent-level registry so mode/thinking/command chips
				// appear immediately without requiring the first message.
				// Note: usageState is NOT restored from agent-level registry — it is
				// per-session and using the agent-level cache would show another
				// session's context usage. It is resolved from DB fallback below.
				reg := ai.GetAgentCapabilityRegistry()
				agentCap := reg.Get(sessionAgentID)
				if agentCap != nil && agentCap.HasData() {
					if ms := reg.GetModeState(sessionAgentID, ""); ms != nil {
						modeState = ms
					}
					if es := reg.GetThinkingEffortState(sessionAgentID, ""); es != nil {
						thinkingEffortState = es
					}
					if cmds := reg.GetCommands(sessionAgentID); len(cmds) > 0 {
						commands = cmds
					}
					if ml := reg.GetModelListState(sessionAgentID, ""); ml != nil {
						modelListState = ml
					}
				}
			}

			// DB fallback: if any state is still nil after in-memory lookups,
			// try to restore from persisted context_state (survives server restart).
			if modeState == nil || thinkingEffortState == nil || usageState == nil {
				if ctxState := service.GetContextState(sessionID); ctxState != nil {
					if modeState == nil && ctxState.Mode != nil {
						modeState = ctxState.Mode
					}
					if thinkingEffortState == nil && ctxState.ThinkingEffort != nil {
						thinkingEffortState = ctxState.ThinkingEffort
					}
					if usageState == nil && ctxState.Usage != nil {
						usageState = ctxState.Usage
					}
				}
			}
		}

		// queuedCount tells the frontend how many messages are still waiting for
		// the drain loop, so it can compute hasMore without counting them as
		// loaded history. The queued messages themselves are returned in the
		// messages array (they are real chat_history rows with queued=1).

		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "queuedCount": queuedCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState, "usageState": usageState, "replayPending": replayPending})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "queuedCount": queuedCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState, "usageState": usageState, "replayPending": replayPending})
		return
	}

	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Get backend from session, not from global state
	sessionID := getSessionID(r)
	if sessionID == "" {
		// No session ID in query param or cookie — this should not happen
		// during normal operation. The frontend always tracks currentSessionId
		// and sends it explicitly. Auto-creating a new session here is dangerous
		// because it creates a "ghost" session that the frontend doesn't know about,
		// causing the user to appear to lose their conversation.
		// Return an error so the frontend can recover explicitly.
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	backendName := service.GetSessionBackend(sessionID)
	if backendName == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionBackendNotFound")
		return
	}

	// Verify the session belongs to the requesting project (ISS-180)
	// For POST, sessionID is always from a DB-backed session (auto-created above or from cookie),
	// so an empty sessionProject means the session doesn't exist — will fail at backendName check.
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Decode request body BEFORE the running check so we can enqueue when busy
	var req struct {
		Message        string            `json:"message"`
		QueueID        string            `json:"queueId"`
		FilePaths      []string          `json:"filePaths"`
		Files          []model.FileEntry `json:"files"`
		AgentID        string            `json:"agentId"`
		ModelID        string            `json:"modelId"`
		ThinkingEffort string            `json:"thinkingEffort"`
		ModeID         string            `json:"modeId"`
		Transport      string            `json:"transport"`
		ClientID       string            `json:"clientId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	// Allow empty message if files are provided
	if req.Message == "" && len(req.Files) == 0 && len(req.FilePaths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageOrFilesRequired")
		return
	}

	// Validate file paths
	allFilePaths := req.FilePaths

	basePath, _ := filepath.Abs(projectPath)
	// Always use project root as workDir for CLI backends. Using filepath.Dir(attachment)
	// breaks --resume because Claude/Codebuddy CLI looks up session files by cwd — a different
	// cwd means it can't find the existing session, producing "No conversation found" errors.
	fileDir := basePath

	// Validate all attached file paths are within project
	validatedFilePaths := make([]string, 0, len(allFilePaths))
	validatedDirPaths := make([]string, 0, len(allFilePaths))
	for _, fp := range allFilePaths {
		fAbsPath, ok := validateAndResolvePath(w, r, basePath, fp)
		if !ok {
			return
		}
		info, err := os.Stat(fAbsPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFound", map[string]any{"Path": fp})
			return
		}
		if info.IsDir() {
			validatedDirPaths = append(validatedDirPaths, fAbsPath)
		} else {
			validatedFilePaths = append(validatedFilePaths, fAbsPath)
		}
	}

	// Validate file entries are within project and determine isDir via os.Stat
	validatedFileEntries := make([]model.FileEntry, 0, len(req.Files))
	for _, fEntry := range req.Files {
		fAbsPath, ok := validateAndResolvePath(w, r, basePath, fEntry.Path)
		if !ok {
			return
		}
		info, err := os.Stat(fAbsPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFound", map[string]any{"Path": fEntry.Path})
			return
		}
		validatedFileEntries = append(validatedFileEntries, model.FileEntry{
			Path:      fAbsPath,
			IsDir:     info.IsDir(),
			StartLine: fEntry.StartLine,
			EndLine:   fEntry.EndLine,
		})
	}

	// Derive file/dir paths from validatedFileEntries for prompt prefixing,
	// excluding entries already covered by filePaths (cross-deduplication).
	filePathsSet := make(map[string]struct{}, len(validatedFilePaths)+len(validatedDirPaths))
	for _, p := range append(validatedFilePaths, validatedDirPaths...) {
		filePathsSet[p] = struct{}{}
	}

	fileEntryFileLabels := make([]string, 0) // "path" or "path:startLine-endLine"
	fileEntryDirPaths := make([]string, 0)
	for _, f := range validatedFileEntries {
		if _, exists := filePathsSet[f.Path]; exists {
			continue // already covered by filePaths
		}
		if f.IsDir {
			fileEntryDirPaths = append(fileEntryDirPaths, f.Path)
		} else {
			fileEntryFileLabels = append(fileEntryFileLabels, fileEntryLabel(f))
		}
	}

	prompt := req.Message
	// Slash commands (e.g. /reload-plugins, /compact) must be sent as-is to ACP
	// agents — they detect commands by the leading "/" prefix. Prepending file
	// paths or system instructions would break command detection.
	isSlashCmd := ai.IsACPSlashCommand(req.Message)
	if !isSlashCmd {
		if len(validatedFilePaths) > 0 {
			prompt = fmt.Sprintf("[Current file: %s]\n%s", strings.Join(validatedFilePaths, ", "), req.Message)
		}
		if len(validatedDirPaths) > 0 {
			prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(validatedDirPaths, ", "), prompt)
		}
		if len(fileEntryFileLabels) > 0 {
			prompt = fmt.Sprintf("[User uploaded %d file(s): %s]\n%s", len(fileEntryFileLabels), strings.Join(fileEntryFileLabels, ", "), prompt)
		}
		if len(fileEntryDirPaths) > 0 {
			prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(fileEntryDirPaths, ", "), prompt)
		}
	}

	// @ command injection: detect on raw req.Message, prepend template to prompt.
	// Must happen after file path prefixes are added so the AI sees both
	// the injected context and file context, but detection is on raw req.Message
	// (since file prefixes would break the @ prefix check).
	if strings.HasPrefix(req.Message, "@chatsearch ") {
		// RAG availability check — GlobalStore is nil when RAG index is not ready
		if rag.GlobalStore == nil {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotReady")
			return
		}
		// Empty query rejection
		query := strings.TrimPrefix(req.Message, "@chatsearch ")
		if strings.TrimSpace(query) == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
			return
		}
		atInjected := processAtCommand(req.Message, projectPath, sessionID)
		prompt = atInjected + "\n\n" + prompt
	} else if strings.HasPrefix(req.Message, "@task ") {
		atInjected := processAtCommand(req.Message, projectPath, sessionID)
		prompt = atInjected + "\n\n" + prompt
	}

	// allFiles uses validated entries (with resolved absolute paths and isDir from os.Stat)
	allFiles := validatedFileEntries

	// Determine if the user message carries file attachments for conditional prompt injection
	hasAttachments := len(req.FilePaths) > 0 || len(req.Files) > 0

	// Resolve agent config early (needed for both enqueue and execution paths)
	effectiveAgentID := req.AgentID
	if effectiveAgentID == "" {
		effectiveAgentID = model.GetDefaultAgentID()
	}

	// Persist user's model selection to session so that subsequent GET requests
	// return the correct modelId. This ensures the frontend can restore the
	// user's choice after stream completion instead of resetting to agent default.
	if req.ModelID != "" {
		service.UpdateSessionModel(sessionID, req.ModelID)
	}

	// Persist transport selection for this session so subsequent loads
	// restore the user's choice instead of the agent default.
	// Per-session cleanup: when switching THIS session to CLI, close only
	// this session's ACP connection (not all sessions for the agent).
	if req.Transport != "" {
		service.UpdateSessionTransport(sessionID, req.Transport)
		if req.Transport == "cli" {
			ai.GetACPConnManager().CloseConn(sessionID)
		}
	}

	// Sync auto-approve mode from DB to ACPConn on prompt
	if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
		conn.SetAutoApprove(service.GetSessionAutoApprove(sessionID))
	}

	// Prevent concurrent sessions for the same session ID
	if !service.TrySetSessionRunning(sessionID) {
		// Session already running — enqueue the message to DB (queued=1).
		// The running drain loop picks it up via DequeueQueuedMessage.
		msgID, err := service.AddQueuedMessage(projectPath, backendName, sessionID, req.Message, allFiles, req.QueueID, T(r, "FileMessage"))
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "EnqueueFailed")
			return
		}
		service.SignalDrain(sessionID)

		// Emit user_message to other session subscribers for cross-device sync.
		// SenderClientID allows the sending device to skip its own echo. MessageID
		// carries the persisted DB id so the receiving device can anchor its bubble
		// to the authoritative backend id (same as the non-queue path).
		ws.EmitToSession(sessionID, ai.StreamEvent{
			Type: "user_message",
			UserMessage: &ai.UserMessageData{
				MessageID:      msgID,
				Content:        req.Message,
				Files:          allFiles,
				SenderClientID: req.ClientID,
				QueueID:        req.QueueID,
			},
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"running": true,
			"queued":  true,
		})
		return
	}

	msgID, err := service.AddChatMessage(projectPath, backendName, sessionID, "user", req.Message, allFiles, false, T(r, "FileMessage"))
	if err != nil {
		service.SetSessionRunning(sessionID, false, true) // skipEvent: session never actually started
		model.WriteError(w, model.Internal(fmt.Errorf("failed to save message")))
		return
	}
	// Emit user_message to other session subscribers for cross-device sync.
	// SenderClientID allows the sending device to skip its own echo; QueueID is
	// the frontend-generated id so the sender can adopt this message's DB id
	// from MessageID (its optimistic bubble carries the same id).
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID:      msgID,
			Content:        req.Message,
			Files:          allFiles,
			SenderClientID: req.ClientID,
			QueueID:        req.QueueID,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{"started": true, "sessionId": sessionID, "msgId": msgID})

	// Create context and cancel AFTER TrySetSessionRunning succeeded, but BEFORE
	// starting the goroutine. Registering the cancel function here (not inside the
	// goroutine) prevents a race where CancelSession finds no cancel func but
	// activeSessions is true, which would leave the session permanently stuck.
	ctx, cancel := context.WithCancel(context.Background())
	service.RegisterSessionCancel(sessionID, cancel)

	slog.Info("about to start ai goroutine", slog.String("project", projectPath))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error(
					"AI goroutine panicked",
					slog.String("session", sessionID),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				service.SetSessionRunning(sessionID, false, true) // skipEvent: error event already emitted below
				service.UnregisterSessionCancel(sessionID)
				cancel()
				// Emit error event to WS clients
				ws.EmitToSession(sessionID, ai.StreamEvent{Type: "error", Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
				// Push cancelled notification — panic is a terminal state
				service.EmitSessionPushNotification(sessionID, "cancelled")
				// Persist error to database
				errMsg := "AI internal error, please retry"
				errContent, _ := json.Marshal(map[string]any{"blocks": []any{map[string]string{"type": "error", "text": errMsg, "reason": ai.ReasonPanic}}})
				service.FinalizeStreamingMessage(projectPath, backendName, sessionID, string(errContent))
			}
		}()
		slog.Info("ai goroutine started", slog.String("project", projectPath))
		defer service.SetSessionRunning(sessionID, false, true) // skipEvent: markDoneAndSendFinal already emitted the terminal event
		defer cancel()
		defer service.UnregisterSessionCancel(sessionID)
		// Mark session as not-running BEFORE sending terminal WS event.
		// Without this, a race exists: the "done" event reaches the client,
		// which calls loadHistory(), but the deferred SetSessionRunning(false)
		// hasn't run yet, so the API returns running=true and the frontend
		// reconnects WS in a loop — leaving the stop button stuck.
		// By setting running=false first, loadHistory() always sees the
		// correct terminal state.
		markDoneAndSendFinal := func(event ai.StreamEvent) {
			service.SetSessionRunning(sessionID, false, true) // skip event — we emit directly
			// Emit terminal event to WS clients via StreamHub
			ws.EmitToSession(sessionID, event)
			// DingTalk/Feishu push — EmitSessionEvent is skipped above, so push
			// must be triggered here for normal completion.
			// Skip for cancelled: CancelSession already calls EmitSessionEvent("cancelled")
			// which handles push. Skip for error: no meaningful push content.
			if event.Type == "done" {
				service.EmitSessionPushNotification(sessionID, "completed")
				// Broadcast the terminal status to ALL clients (not just the
				// session's StreamHub subscribers). Clients that missed the
				// stream-level "done" (WS blip, another device, scheduled run)
				// rely on this to clear the session's running flag.
				service.EmitSessionEventWSOnly(sessionID, "completed", false)
			}
		}
		// Mark ACP connection as idle when the session goroutine exits.
		// Previously this used CloseConn, which caused a race: the goroutine
		// sets session-running=false, then a new request starts and reuses the
		// connection, but the deferred CloseConn still fires and kills the
		// process mid-prompt. MarkIdle is safe because idleSweep will close
		// the connection after the idle timeout only if no new request has
		// claimed it.
		defer func() {
			effectiveTransport := "cli"
			if t := service.GetSessionTransport(sessionID); t != "" {
				effectiveTransport = t
			} else if agent, ok := model.Agents[effectiveAgentID]; ok && agent.Transport != "" {
				effectiveTransport = agent.Transport
			}
			if effectiveTransport == "acp-stdio" {
				slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", effectiveAgentID)
				ai.GetACPConnManager().MarkIdle(sessionID)
			}
		}()

		// Build the first chat request
		firstChatReq := buildChatRequest(prompt, sessionID, projectPath, backendName, effectiveAgentID, req.ModelID, req.ThinkingEffort, req.ModeID, req.Transport, fileDir, hasAttachments)

		// Execute first message
		result := executeStreamRun(ctx, r, projectPath, sessionID, backendName, effectiveAgentID, firstChatReq, fileDir, req.QueueID)

		// Drain loop: keep executing queued messages after normal completion
		service.RunDrainLoop(service.DrainConfig{
			SessionID:   sessionID,
			ProjectPath: projectPath,
			BackendName: backendName,
			ExecuteRunWithMessage: func(msg model.ChatMessage) service.DrainResult {
				qMsg := model.QueuedMessage{
					QueueID:   msg.QueueID,
					Text:      msg.Content,
					Files:     msg.Files,
					CreatedAt: msg.CreatedAt.Format(time.RFC3339),
				}
				nextChatReq := buildChatRequestFromQueue(qMsg, sessionID, projectPath, backendName, effectiveAgentID, fileDir)
				nextResult := executeStreamRun(ctx, r, projectPath, sessionID, backendName, effectiveAgentID, nextChatReq, fileDir, msg.QueueID)
				return service.DrainResult{
					CancelReason: nextResult.cancelReason,
					Err:          nextResult.err,
					Empty:        nextResult.empty,
				}
			},
			MarkDoneAndSendFinal: markDoneAndSendFinal,
		}, service.DrainResult{
			CancelReason: result.cancelReason,
			Err:          result.err,
			Empty:        result.empty,
		})
	}()
}

// streamRunResult captures the outcome of a single AI stream execution.
type streamRunResult struct {
	cancelReason string // "", "user"
	err          string // error message if execution failed
	empty        bool   // true if AI returned no content
}

// executeStreamRun runs one AI backend execution from start to finish.
// It creates a backend, starts the stream, then delegates the event loop
// to SessionExecutor.RunWithChannel() and database finalization to
// SessionExecutor.Finalize().
// It does NOT send a terminal WS event — the caller decides what to send.
func executeStreamRun(
	ctx context.Context,
	r *http.Request,
	projectPath, sessionID, backendName, agentID string,
	chatReq ai.ChatRequest,
	fileDir string,
	queueID string,
) streamRunResult {
	runStart := time.Now()
	sessionTransport := service.GetSessionTransport(sessionID)
	slog.Info("acp perf: executeStreamRun.start", "session_id", sessionID, "backend", backendName, "agent_id", agentID, "transport", sessionTransport, "resume", chatReq.Resume)

	backend, err := ai.NewBackendForAgentWithTransport(backendName, agentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", backendName), slog.String("err", err.Error()))
		errMsg := T(r, "BackendCreateFailed", map[string]any{"Error": err.Error()})
		ws.EmitToSession(sessionID, ai.StreamEvent{Type: "error", Error: errMsg})
		_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", errMsg, nil, false, "")
		return streamRunResult{err: errMsg}
	}

	// If session transport was acp-stdio but agent fell back to CLI, clear the
	// stale transport override so subsequent messages don't keep warning.
	if sessionTransport == "acp-stdio" {
		if _, ok := backend.(*ai.ACPBackend); !ok {
			_ = service.UpdateSessionTransport(sessionID, "")
		}
	}

	slog.Info("acp perf: executeStreamRun.ExecuteStream_start", "session_id", sessionID, "transport", sessionTransport, "after_backend_create", time.Since(runStart))
	eventCh, err := backend.ExecuteStream(ctx, chatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		errMsg := T(r, "StreamStartFailed", map[string]any{"Error": err.Error()})
		ws.EmitToSession(sessionID, ai.StreamEvent{Type: "error", Error: errMsg})
		_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", errMsg, nil, false, "")
		return streamRunResult{err: errMsg}
	}

	// Create streaming placeholder message in DB. When this run answers a queued
	// message, record its queue_id so the frontend can anchor the reply to its
	// own question (anchorRepliesToQuestions) instead of falling back to raw DB
	// id order (user2,user3,reply2,reply3).
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	streamingMsgID, _ := service.AddChatMessage(projectPath, backendName, sessionID, "assistant", string(emptyContent), nil, true, "", queueID)
	slog.Info("chat: created streaming assistant placeholder",
		slog.String("session", sessionID),
		slog.Int64("streamingMsgID", streamingMsgID),
		slog.String("queueID", queueID))

	// Delegate event loop to SessionExecutor
	cfg := service.RunConfig{
		Mode:               service.ModeInteractive,
		ProjectPath:        projectPath,
		BackendName:        backendName,
		SessionID:          sessionID,
		AgentID:            agentID,
		ChatRequest:        chatReq,
		FileDir:            fileDir,
		StreamingMessageID: streamingMsgID,
		LocalizeError: func(err error, key string, args map[string]any) string {
			return T(r, key, args)
		},
	}
	executor := service.NewSessionExecutor(ctx, cfg)
	runResult := executor.RunWithChannel(eventCh)

	// Finalize: persist to DB, drain channel, save metadata/raw
	runResult = executor.Finalize(runResult, eventCh)

	// Send updated metadata (with wallMs) to WS clients before the terminal event
	ws.EmitToSession(sessionID, ai.StreamEvent{Type: "metadata", Meta: runResult.Metadata})

	// Convert RunResult to streamRunResult
	result := streamRunResult{}
	if runResult.CancelReason == "user" {
		result.cancelReason = runResult.CancelReason
	} else if ctx.Err() == context.Canceled {
		result.cancelReason = "cancel"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.err = "AI response timed out (30 min)"
	} else if runResult.Empty {
		result.empty = true
	}

	slog.Info(
		"ai stream run done",
		slog.String("session", sessionID),
		slog.Int("blocks", len(runResult.Blocks)),
		slog.String("cancel_reason", runResult.CancelReason),
		slog.Int("wall_ms", runResult.WallMs),
	)

	return result
}

// buildChatRequest constructs an ai.ChatRequest from the given parameters.
// modelOverride, if non-empty, takes precedence over the agent's default model.
// thinkingEffortOverride, if non-empty, takes precedence over the agent's YAML default.
// modeOverride, if non-empty, takes precedence over the current ACP session mode.
func buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string, hasAttachments bool) ai.ChatRequest {
	systemPrompt := ""
	agentModel := ""
	agentCommand := ""
	effectiveThinkingEffort := thinkingEffortOverride // Frontend selection takes priority
	effectiveMode := modeOverride                     // Frontend selection takes priority

	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}
	if agent, ok := model.Agents[agentID]; ok {
		systemPrompt = agent.SystemPrompt
		// Replace {{PROJECT_PATH}} per-request with the actual project path from cookie
		if projectPath != "" {
			systemPrompt = strings.ReplaceAll(systemPrompt, "{{PROJECT_PATH}}", projectPath)
		}
		if modelOverride != "" {
			agentModel = modelOverride
		} else if defaultID := agent.DefaultModelID(); defaultID != "" {
			agentModel = defaultID
		}
		if agent.Command != "" {
			agentCommand = agent.Command
		}
		// Fall back to agent's effective thinking effort when frontend didn't specify
		if effectiveThinkingEffort == "" && agent.EffectiveThinkingEffort() != "" {
			effectiveThinkingEffort = agent.EffectiveThinkingEffort()
		}
		// Fall back to agent's preferred mode when frontend didn't specify
		if effectiveMode == "" && agent.EffectiveModeID() != "" {
			effectiveMode = agent.EffectiveModeID()
		}
	}

	// Resolve effective session ID for CLI.
	// All backends now store their CLI-identifiable session ID in external_session_id:
	//   - codebuddy/claude/qoder: ClawBench UUID (same as session id)
	//   - opencode/codex/deepseek/pi: CLI-assigned ID (captured from stream events)
	// When resuming, we always use external_session_id so the CLI can find its session context.
	//
	// EXCEPTION: ACP-backed agents manage their own session mapping internally
	// via ACPConnectionPool (clawbench UUID → ACP session ID). For ACP agents,
	// always use the ClawBench UUID as the session ID — the pool handles the rest.
	effectiveSessionID := sessionID
	resumeStart := time.Now()
	resume := service.SessionHasAssistant(sessionID)
	slog.Info("acp perf: buildChatRequest.SessionHasAssistant", "session_id", sessionID, "resume", resume, "elapsed", time.Since(resumeStart))
	isACP := false
	if transportOverride != "" {
		isACP = transportOverride == "acp-stdio"
	} else if agent, ok := model.Agents[agentID]; ok {
		isACP = agent.Transport == "acp-stdio"
	}
	// Resolve external_session_id for fork detection (used by both CLI resume and fork context injection).
	var resolvedExtID string
	if resume {
		extStart := time.Now()
		resolvedExtID = service.GetExternalSessionID(sessionID)
		slog.Info("acp perf: buildChatRequest.GetExternalSessionID", "session_id", sessionID, "ext_id", resolvedExtID, "elapsed", time.Since(extStart))
	}

	if resume && !isACP {
		if resolvedExtID != "" {
			effectiveSessionID = resolvedExtID
			slog.Info("session resume: resolved external_session_id",
				slog.String("session", sessionID),
				slog.String("external_session_id", resolvedExtID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("ext_id_is_clawbench_uuid", resolvedExtID == sessionID))
		} else if !service.SessionHasRealAssistantContent(sessionID) {
			// The first assistant message is an empty cancel/warning placeholder
			// (no text/tool_use/thinking blocks). This means the AI never responded
			// with real content — the stream was interrupted before the CLI
			// established a session. Resume with any ID would fail because the AI
			// never saw this session. Clear effectiveSessionID and set resume=false
			// so the backend starts a completely fresh session (no --resume, proper
			// system prompt injection).
			effectiveSessionID = ""
			resume = false
			slog.Info("session resume: external_session_id is empty and no real AI content (first message interrupted), starting fresh",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID))
		} else {
			// No external session ID available — the CLI cannot resume a session
			// it has never seen. Clear effectiveSessionID so the backend does not
			// pass an invalid ID to --resume. This results in a fresh CLI session
			// (context amnesia). Log a warning for diagnosis.
			effectiveSessionID = ""
			slog.Warn("session resume: external_session_id is empty, CLI will start a new session (context amnesia)",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID))
		}
	} else if !resume {
		slog.Info("session: new conversation (no resume)",
			slog.String("session", sessionID),
			slog.String("backend", backendName),
			slog.String("agent", agentID))
	}

	// Detect fork session first message: resume=true (has copied assistant messages)
	// but no external_session_id (AI side has no context). This happens after
	// ForkSession which copies messages in DB but doesn't inherit the AI-side session.
	// Inject formatted history so the AI can continue with context.
	//
	// Note: This branch only fires when resume is still true after the above
	// checks. If the first message was interrupted (resume set to false above),
	// this fork detection is skipped — correct, because a phantom-cancel session
	// is not a fork.
	//
	// Guard against re-injection on subsequent messages:
	// After the first AI response, session_capture persists external_session_id,
	// so resolvedExtID != "" and the above resume branch uses it directly,
	// bypassing this fork detection.
	var forkContext string
	if resume && resolvedExtID == "" {
		forkContext = buildForkContext(sessionID)
		if forkContext != "" {
			slog.Info("fork session: injecting context history",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("is_acp", isACP))

			// For ACP sessions: external_session_id is empty, so the ACP pool
			// has no existing connection for this session. Setting Resume=false
			// ensures the ACP backend calls NewSession (not ResumeSession with
			// an invalid ID). The fork context in the prompt provides the
			// necessary history, so a new session is the correct approach.
			if isACP {
				resume = false
			}
		}
	}

	// Inject media handling rules only when the user message carries attachments.
	// These rules are omitted for text-only messages to save tokens.
	if hasAttachments {
		mediaPrompt := model.BuildMediaPrompt()
		if mediaPrompt != "" {
			if systemPrompt != "" {
				systemPrompt += "\n\n" + mediaPrompt
			} else {
				systemPrompt = mediaPrompt
			}
		}
	}

	return ai.ChatRequest{
		Prompt:                 prompt,
		SessionID:              effectiveSessionID,
		WorkDir:                fileDir,
		SystemPrompt:           systemPrompt,
		Model:                  agentModel,
		Command:                agentCommand,
		AgentID:                agentID,
		ThinkingEffort:         effectiveThinkingEffort,
		Mode:                   effectiveMode,
		Resume:                 resume,
		HasAttachments:         hasAttachments,
		AssistantMessageCount:  service.GetAssistantMessageCount(sessionID),
		HasConversationHistory: service.GetChatMessageCount(sessionID) > 0,
		ForkContext:            forkContext,
	}
}

// buildForkContext reads the chat history from DB and formats it as a text block
// that can be prepended to the user's prompt. This gives the AI context from the
// parent session when the forked session sends its first message.
//
// Tool output fields are truncated to 500 runes (see
// service.forkToolOutputMaxLen) to avoid token explosion.
func buildForkContext(sessionID string) string {
	msgs, err := service.GetMessagesBySessionID(sessionID)
	if err != nil || len(msgs) == 0 {
		return ""
	}

	// Batch-fetch tool call details for the session
	toolCalls, _ := service.GetToolCallsBySession(sessionID)
	toolCallMap := make(map[string]*service.ToolCallRecord, len(toolCalls))
	for i := range toolCalls {
		toolCallMap[toolCalls[i].ToolID] = &toolCalls[i]
	}

	var sb strings.Builder
	sb.WriteString("[Below is the conversation history from before this session. Continue based on this context.]\n\n")

	for _, m := range msgs {
		role := "User"
		if m.Role == "assistant" {
			role = "Assistant"
		}

		if m.Role != "user" && m.Role != "assistant" {
			continue
		}

		var wrapper struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if !strings.HasPrefix(m.Content, `{"blocks":`) || json.Unmarshal([]byte(m.Content), &wrapper) != nil {
			// Non-block content: treat as plain text. Use the unified extractor
			// so nested JSON serializations (bare content arrays, ACP notification
			// wrappers from sync replay) never leak raw JSON into the model.
			content := service.ExtractPlainText(m.Content)
			if content == "" {
				continue
			}
			fmt.Fprintf(&sb, "%s: %s\n\n", role, content)
			continue
		}

		// Render blocks: text as-is, tool_use as structured JSON, thinking skipped
		var msgParts []string
		for _, b := range wrapper.Blocks {
			switch b.Type {
			case "text":
				if b.Text != "" {
					msgParts = append(msgParts, b.Text)
				}
			case strToolUse:
				tcJSON := service.FormatToolUseBlock(b, toolCallMap)
				if tcJSON != "" {
					msgParts = append(msgParts, tcJSON)
				}
				// thinking, warning, error: skipped
			}
		}
		if len(msgParts) == 0 {
			continue
		}

		content := strings.Join(msgParts, "\n\n")
		fmt.Fprintf(&sb, "%s: %s\n\n", role, content)
	}

	sb.WriteString("[End of conversation history. Now answer the user's new question.]\n\n")
	return sb.String()
}

// fileEntryLabel returns a prompt label for a FileEntry, appending line info
// when present: "path", "path:10", or "path:10-20".
func fileEntryLabel(f model.FileEntry) string {
	if f.StartLine > 0 && f.EndLine > 0 && f.StartLine != f.EndLine {
		return fmt.Sprintf("%s:%d-%d", f.Path, f.StartLine, f.EndLine)
	}
	if f.StartLine > 0 {
		return fmt.Sprintf("%s:%d", f.Path, f.StartLine)
	}
	return f.Path
}

// buildChatRequestFromQueue constructs an ai.ChatRequest from a queued message.
func buildChatRequestFromQueue(qMsg model.QueuedMessage, sessionID, projectPath, backendName, agentID, fileDir string) ai.ChatRequest {
	prompt := qMsg.Text
	if len(qMsg.FilePaths) > 0 {
		basePath, _ := filepath.Abs(projectPath)
		var filePaths, dirPaths []string
		for _, fp := range qMsg.FilePaths {
			absPath, ok := model.ValidatePath(basePath, fp)
			if !ok {
				filePaths = append(filePaths, fp)
				continue
			}
			info, err := os.Stat(absPath)
			if err != nil {
				filePaths = append(filePaths, fp)
				continue
			}
			if info.IsDir() {
				dirPaths = append(dirPaths, absPath)
			} else {
				filePaths = append(filePaths, absPath)
			}
		}
		if len(filePaths) > 0 {
			prompt = fmt.Sprintf("[Current file: %s]\n%s", strings.Join(filePaths, ", "), qMsg.Text)
		}
		if len(dirPaths) > 0 {
			prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(dirPaths, ", "), prompt)
		}
	}
	if len(qMsg.Files) > 0 {
		// Build line-number-aware labels, excluding files already in filePaths
		filePathsLookup := make(map[string]struct{}, len(qMsg.FilePaths))
		for _, p := range qMsg.FilePaths {
			filePathsLookup[p] = struct{}{}
		}
		var fileLabels, dirPaths []string
		for _, f := range qMsg.Files {
			if _, exists := filePathsLookup[f.Path]; exists {
				continue
			}
			if f.IsDir {
				dirPaths = append(dirPaths, f.Path)
			} else {
				fileLabels = append(fileLabels, fileEntryLabel(f))
			}
		}
		if len(fileLabels) > 0 {
			prompt = fmt.Sprintf("[User uploaded %d file(s): %s]\n%s", len(fileLabels), strings.Join(fileLabels, ", "), prompt)
		}
		if len(dirPaths) > 0 {
			prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(dirPaths, ", "), prompt)
		}
	}

	// @ command injection for queued messages (same logic as primary message path)
	if atInjected := processAtCommand(qMsg.Text, projectPath, sessionID); atInjected != qMsg.Text {
		prompt = atInjected + "\n\n" + prompt
	}

	// Use session-persisted model (if user explicitly chose one) as modelOverride
	// so queued messages respect the user's model choice, not just the agent default.
	sessionModel := service.GetSessionModel(sessionID)
	sessionTransport := service.GetSessionTransport(sessionID)
	hasAttachments := len(qMsg.FilePaths) > 0 || len(qMsg.Files) > 0
	return buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, sessionModel, "", "", sessionTransport, fileDir, hasAttachments)
}

// CancelChat handles POST to cancel an ongoing AI stream for a session.
func CancelChat(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = getSessionID(r)
	}
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	if !service.CancelSession(sessionID) {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotRunning")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
