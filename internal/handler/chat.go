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
		messages, totalCount, err := service.GetChatHistoryPaged(projectPath, sessionBackend, sessionID, limit, beforeID)
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
		// without waiting for SSE events (which may have already been consumed).
		// Fallback: for brand-new sessions with no pool session mapping yet,
		// look up from AgentCapabilityRegistry so mode chips appear on first load.
		// For CLI sessions, synthesize a read-only mode from the backend name
		// so the mode chip is visible but non-switchable.
		var modeState, thinkingEffortState, modelListState, planState, usageState any
		var commands []ai.AvailableCommandInfo
		if sessionID != "" && sessionTransport != "cli" {
			if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID); s.Mode != nil || s.Effort != nil || len(s.Commands) > 0 || s.ModelList != nil || s.Plan != nil || s.Usage != nil {
				modeState = s.Mode
				thinkingEffortState = s.Effort
				commands = s.Commands
				modelListState = s.ModelList
				planState = s.Plan
				usageState = s.Usage
			} else if sessionAgentID != "" {
				// No session-level mapping yet (new session, never sent a message).
				// Fall back to agent-level registry so mode/thinking/command chips
				// appear immediately without requiring the first message.
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
		}

		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState, "usageState": usageState})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState, "usageState": usageState})
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
		Message        string   `json:"message"`
		FilePaths      []string `json:"filePaths"`
		Files          []string `json:"files"`
		AgentID        string   `json:"agentId"`
		ModelID        string   `json:"modelId"`
		ThinkingEffort string   `json:"thinkingEffort"`
		ModeID         string   `json:"modeId"`
		Transport      string   `json:"transport"`
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

	// Validate file paths are within project and collect absolute paths
	fileAbsPaths := make([]string, 0, len(req.Files))
	for _, fPath := range req.Files {
		fAbsPath, ok := validateAndResolvePath(w, r, basePath, fPath)
		if !ok {
			return
		}
		if _, err := os.Stat(fAbsPath); err != nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFound", map[string]any{"Path": fPath})
			return
		}
		fileAbsPaths = append(fileAbsPaths, fAbsPath)
	}

	prompt := req.Message
	if len(validatedFilePaths) > 0 {
		prompt = fmt.Sprintf("[Current file: %s]\n%s", strings.Join(validatedFilePaths, ", "), req.Message)
	}
	if len(validatedDirPaths) > 0 {
		prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(validatedDirPaths, ", "), prompt)
	}
	if len(fileAbsPaths) > 0 {
		prompt = fmt.Sprintf("[User uploaded %d file(s): %s]\n%s", len(fileAbsPaths), strings.Join(fileAbsPaths, ", "), prompt)
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

	// allFiles already includes filePaths (frontend merges them before sending)
	allFiles := req.Files

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
		// Session already running — enqueue the message
		qMsg := model.QueuedMessage{
			Text:      req.Message,
			FilePaths: allFilePaths,
			Files:     allFiles,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		queueState := service.EnqueueMessage(sessionID, qMsg)

		// Notify the running goroutine via SSE
		service.SendSessionEvent(sessionID, ai.StreamEvent{
			Type:       "queue_update",
			QueueEvent: &ai.QueueEventData{Queue: queueState},
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"running": true,
			"queued":  true,
			"queue":   queueState,
		})
		return
	}

	if _, err := service.AddChatMessage(projectPath, backendName, sessionID, "user", req.Message, allFiles, false, T(r, "FileMessage")); err != nil {
		service.SetSessionRunning(sessionID, false)
		model.WriteError(w, model.Internal(fmt.Errorf("failed to save message")))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"started": true, "sessionId": sessionID})

	// Register stream channel BEFORE starting goroutine to avoid race with SSE connection
	streamCh := service.RegisterSessionStream(sessionID)

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
				service.SetSessionRunning(sessionID, false)
				service.UnregisterSessionCancel(sessionID)
				cancel()
				// Try to send error event to SSE stream
				service.SendSessionEvent(sessionID, ai.StreamEvent{Type: "error", Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
				service.UnregisterSessionStream(sessionID)
				// Persist error to database
				errMsg := "AI internal error, please retry"
				errContent, _ := json.Marshal(map[string]any{"blocks": []any{map[string]string{"type": "error", "text": errMsg, "reason": ai.ReasonPanic}}})
				service.FinalizeStreamingMessage(projectPath, backendName, sessionID, string(errContent))
			}
		}()
		slog.Info("ai goroutine started", slog.String("project", projectPath))
		defer service.SetSessionRunning(sessionID, false)
		defer service.UnregisterSessionStream(sessionID)
		defer cancel()
		defer service.UnregisterSessionCancel(sessionID)
		// Mark session as not-running BEFORE sending terminal SSE event.
		// Without this, a race exists: the "done" event reaches the client,
		// which calls loadHistory(), but the deferred SetSessionRunning(false)
		// hasn't run yet, so the API returns running=true and the frontend
		// reconnects SSE in a loop — leaving the stop button stuck.
		// By setting running=false first, loadHistory() always sees the
		// correct terminal state.
		markDoneAndSendFinal := func(event ai.StreamEvent) {
			service.SetSessionRunning(sessionID, false, true) // skip event — we send SSE directly
			ai.SendFinalStreamEvent(streamCh, event)
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
			} else if agent, ok := model.Agents[effectiveAgentID]; ok && agent.SupportsACP() {
				effectiveTransport = "acp-stdio"
			}
			if effectiveTransport == "acp-stdio" {
				slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", effectiveAgentID)
				ai.GetACPConnManager().MarkIdle(sessionID)
			}
		}()

		// Build the first chat request
		firstChatReq := buildChatRequest(prompt, sessionID, projectPath, backendName, effectiveAgentID, req.ModelID, req.ThinkingEffort, req.ModeID, req.Transport, fileDir, hasAttachments)

		// Execute first message
		result := executeStreamRun(ctx, r, streamCh, projectPath, sessionID, backendName, effectiveAgentID, firstChatReq, fileDir)

		// Drain loop: keep executing queued messages after normal completion
		for {
			if result.cancelReason == "user" {
				service.ClearQueue(sessionID)
				markDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
				return
			}
			if result.err != "" {
				markDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: result.err})
				return
			}
			if result.empty {
				markDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: "AI returned no content", Reason: ai.ReasonEmpty})
				return
			}
			if result.cancelReason != "" {
				// Other cancel reasons
				markDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
				return
			}

			// Normal completion — check queue for next message
			qMsg, ok := service.DequeueMessage(sessionID)
			if !ok {
				// Brief re-check for enqueue-during-exit race
				time.Sleep(50 * time.Millisecond)
				qMsg, ok = service.DequeueMessage(sessionID)
			}
			if !ok {
				// Queue empty — truly done
				markDoneAndSendFinal(ai.StreamEvent{Type: "done"})
				return
			}

			// Queue has next message — drain it atomically
			slog.Info("draining queued message", slog.String("session", sessionID), slog.String("text", qMsg.Text))

			// Persist user message to DB
			drainMsgID, err := service.AddChatMessage(projectPath, backendName, sessionID, "user", qMsg.Text, qMsg.Files, false, T(r, "FileMessage"))
			if err != nil {
				slog.Error("failed to persist drain message", slog.String("session", sessionID), slog.String("error", err.Error()))
				// Continue with drainMsgID=0 — frontend will use synthetic drain ID
			}

			// Send single atomic queue_drain event (replaces old queue_done + queue_consume + queue_update)
			remainingQueue := service.GetQueue(sessionID)
			ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{
				Type: "queue_drain",
				QueueEvent: &ai.QueueEventData{
					Text:      qMsg.Text,
					MessageID: drainMsgID,
					FilePaths: qMsg.FilePaths,
					Files:     qMsg.Files,
					Queue:     remainingQueue,
				},
			})

			// Build chat request from queued message and execute
			nextChatReq := buildChatRequestFromQueue(qMsg, sessionID, projectPath, backendName, effectiveAgentID, fileDir)
			result = executeStreamRun(ctx, r, streamCh, projectPath, sessionID, backendName, effectiveAgentID, nextChatReq, fileDir)
			// Loop continues
		}
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
// It does NOT send a terminal SSE event — the caller decides what to send.
func executeStreamRun(
	ctx context.Context,
	r *http.Request,
	streamCh chan<- ai.StreamEvent,
	projectPath, sessionID, backendName, agentID string,
	chatReq ai.ChatRequest,
	fileDir string,
) streamRunResult {
	runStart := time.Now()
	sessionTransport := service.GetSessionTransport(sessionID)
	slog.Info("acp perf: executeStreamRun.start", "session_id", sessionID, "backend", backendName, "agent_id", agentID, "transport", sessionTransport, "resume", chatReq.Resume)

	backend, err := ai.NewBackendForAgentWithTransport(backendName, agentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", backendName), slog.String("err", err.Error()))
		errMsg := T(r, "BackendCreateFailed", map[string]any{"Error": err.Error()})
		if !ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg}) {
			return streamRunResult{err: errMsg}
		}
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
		if !ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg}) {
			return streamRunResult{err: errMsg}
		}
		_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", errMsg, nil, false, "")
		return streamRunResult{err: errMsg}
	}

	// Create streaming placeholder message in DB
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	streamingMsgID, _ := service.AddChatMessage(projectPath, backendName, sessionID, "assistant", string(emptyContent), nil, true, "")

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
		StreamCh:           streamCh,
		LocalizeError: func(err error, key string, args map[string]any) string {
			return T(r, key, args)
		},
	}
	executor := service.NewSessionExecutor(ctx, cfg)
	runResult := executor.RunWithChannel(eventCh)

	// Finalize: persist to DB, drain channel, save metadata/raw
	runResult = executor.Finalize(runResult, eventCh)

	// Send updated metadata (with wallMs) to SSE before the terminal event
	ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "metadata", Meta: runResult.Metadata})

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
	} else if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
		isACP = true
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
	// Guard against re-injection on subsequent messages:
	// After the first AI response, session_capture persists external_session_id,
	// so resolvedExtID != "" and this branch is skipped. If capture fails
	// (unlikely), the condition would re-trigger, but that's acceptable
	// because the AI still needs context.
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
		Prompt:                prompt,
		SessionID:             effectiveSessionID,
		WorkDir:               fileDir,
		SystemPrompt:          systemPrompt,
		Model:                 agentModel,
		Command:               agentCommand,
		AgentID:               agentID,
		ThinkingEffort:        effectiveThinkingEffort,
		Mode:                  effectiveMode,
		Resume:                resume,
		HasAttachments:        hasAttachments,
		AssistantMessageCount: service.GetAssistantMessageCount(sessionID),
		ForkContext:           forkContext,
	}
}

// buildForkContext reads the chat history from DB and formats it as a text block
// that can be prepended to the user's prompt. This gives the AI context from the
// parent session when the forked session sends its first message.
//
// Limits: each message is truncated to 2000 bytes; total context is capped at
// 10000 bytes to avoid token explosion. Assistant message content in JSON block
// format is converted to plain text via ExtractPlainText before inclusion.
func buildForkContext(sessionID string) string {
	msgs, err := service.GetMessagesBySessionID(sessionID)
	if err != nil || len(msgs) == 0 {
		return ""
	}

	const maxPerMsg = 2000
	const maxTotal = 10000

	var sb strings.Builder
	sb.WriteString("[Below is the conversation history from before this session. Continue based on this context.]\n\n")

	total := 0
	for _, m := range msgs {
		role := "User"
		if m.Role == "assistant" {
			role = "Assistant"
		}
		// Convert JSON block format to readable plain text.
		// For assistant messages this strips tool_use/tool_result blocks
		// and keeps only the text content.
		content := service.ExtractPlainText(m.Content)
		if content == "" {
			continue // skip messages with no readable text (e.g. pure tool calls)
		}
		if len(content) > maxPerMsg {
			content = content[:maxPerMsg] + "...(truncated)"
		}
		line := fmt.Sprintf("%s: %s\n\n", role, content)
		if total+len(line) > maxTotal {
			sb.WriteString("...(history too long, remaining messages omitted)\n\n")
			break
		}
		sb.WriteString(line)
		total += len(line)
	}

	sb.WriteString("[End of conversation history. Now answer the user's new question.]\n\n")
	return sb.String()
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
		prompt = fmt.Sprintf("[User uploaded %d file(s): %s]\n%s", len(qMsg.Files), strings.Join(qMsg.Files, ", "), prompt)
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
