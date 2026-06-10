//nolint:errcheck,gocyclo,gocognit,gosec,goconst,unparam // legacy file, nolint-only approach for diff stability
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
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/rag"
	"clawbench/internal/service"

	"github.com/google/uuid"
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
		setSessionID(w, sessionID)
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

		totalCount := service.GetChatMessageCount(sessionID)
		messages, err := service.GetChatHistoryPaged(projectPath, sessionBackend, sessionID, limit, beforeID)
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
		var modeState, thinkingEffortState, modelListState, planState any
		var commands []ai.AvailableCommandInfo
		if sessionID != "" && sessionTransport != "cli" {
			if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID); s.Mode != nil || s.Effort != nil || len(s.Commands) > 0 || s.ModelList != nil || s.Plan != nil {
				modeState = s.Mode
				thinkingEffortState = s.Effort
				commands = s.Commands
				modelListState = s.ModelList
				planState = s.Plan
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
			writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "transport": sessionTransport, "autoApprove": sessionAutoApprove, "total": totalCount, "modeState": modeState, "thinkingEffortState": thinkingEffortState, "commands": commands, "modelListState": modelListState, "planState": planState})
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

		// Persist user message to DB immediately
		service.AddChatMessage(projectPath, backendName, sessionID, "user", req.Message, allFiles, false, T(r, "FileMessage"))

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
			sendFinalEvent(streamCh, event)
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
			} else if agent, ok := model.Agents[effectiveAgentID]; ok && agent.Transport == "acp-stdio" {
				effectiveTransport = "acp-stdio"
			}
			if effectiveTransport == "acp-stdio" {
				slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", effectiveAgentID)
				ai.GetACPConnManager().MarkIdle(sessionID)
			}
		}()

		// Build the first chat request
		firstChatReq := buildChatRequest(prompt, sessionID, projectPath, backendName, effectiveAgentID, req.ModelID, req.ThinkingEffort, req.ModeID, req.Transport, fileDir)

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

			// Queue has next message — notify frontend that current message is done,
			// then send queue_consume + queue_update, persist, execute the next one
			slog.Info("draining queued message", slog.String("session", sessionID), slog.String("text", qMsg.Text))

			// Notify frontend: current streaming message is finalized (remove loading dots)
			sendEvent(ctx, streamCh, ai.StreamEvent{Type: "queue_done"})

			// Notify frontend: a queued message is about to execute
			sendEvent(ctx, streamCh, ai.StreamEvent{
				Type:       "queue_consume",
				QueueEvent: &ai.QueueEventData{Text: qMsg.Text, FilePaths: qMsg.FilePaths, Files: qMsg.Files},
			})

			// Persist user message to DB
			service.AddChatMessage(projectPath, backendName, sessionID, "user", qMsg.Text, qMsg.Files, false, T(r, "FileMessage"))

			// Send updated queue state
			remainingQueue := service.GetQueue(sessionID)
			sendEvent(ctx, streamCh, ai.StreamEvent{
				Type:       "queue_update",
				QueueEvent: &ai.QueueEventData{Queue: remainingQueue},
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
// It handles event accumulation, incremental DB persistence, resume_split,
// and finalizes the streaming message in the DB.
// It does NOT send a terminal SSE event — the caller decides what to send.
func executeStreamRun(
	ctx context.Context,
	r *http.Request,
	streamCh chan<- ai.StreamEvent,
	projectPath, sessionID, backendName, agentID string,
	chatReq ai.ChatRequest,
	fileDir string,
) streamRunResult {
	sessionTransport := service.GetSessionTransport(sessionID)
	backend, err := ai.NewBackendForAgentWithTransport(backendName, agentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", backendName), slog.String("err", err.Error()))
		errMsg := T(r, "BackendCreateFailed", map[string]any{"Error": err.Error()})
		if !sendEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg}) {
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

	eventCh, err := backend.ExecuteStream(ctx, chatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		errMsg := T(r, "StreamStartFailed", map[string]any{"Error": err.Error()})
		if !sendEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg}) {
			return streamRunResult{err: errMsg}
		}
		_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", errMsg, nil, false, "")
		return streamRunResult{err: errMsg}
	}

	// Record wall-clock start time for duration tracking
	wallStart := time.Now()

	// Create streaming placeholder message in DB
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", string(emptyContent), nil, true, "")

	var blocks []model.ContentBlock
	var responseMetadata *ai.Metadata
	var rawOutput string // collected from raw_output event for debugging

	// Incremental persistence: flush every 1s or every 5 events
	flushTicker := time.NewTicker(1 * time.Second)
	defer flushTicker.Stop()
	eventCount := 0

	serializeBlocks := func() string {
		serializedBlocks := blocks
		if serializedBlocks == nil {
			serializedBlocks = []model.ContentBlock{}
		}
		contentMap := map[string]any{"blocks": serializedBlocks}
		if responseMetadata != nil {
			contentMap["metadata"] = responseMetadata
		}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON)
	}

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Stream ended — finalize below
				return finalizeStreamRun(ctx, streamCh, projectPath, backendName, sessionID, agentID, chatReq, blocks, responseMetadata, rawOutput, eventCh, wallStart)
			}
			// Don't forward "done" here — finalize below
			if event.Type == "done" {
				return finalizeStreamRun(ctx, streamCh, projectPath, backendName, sessionID, agentID, chatReq, blocks, responseMetadata, rawOutput, eventCh, wallStart)
			}
			// Capture raw output for debugging (not forwarded to SSE)
			if event.Type == "raw_output" {
				if rawOutput != "" {
					rawOutput += "\n"
				}
				rawOutput += event.RawOutput
				continue
			}
			// Early capture of external session ID.
			// Persist immediately so that if the stream is cancelled before
			// step_finish/turn.completed, the ID is already saved for resumption.
			// All backends store their CLI-identifiable session ID in external_session_id.
			if event.Type == "session_capture" {
				if event.Content != "" {
					existingExtID := service.GetExternalSessionID(sessionID)
					if existingExtID == "" || existingExtID == sessionID {
						if err := service.UpdateExternalSessionID(sessionID, event.Content); err != nil {
							slog.Error(
								"failed to save external session ID (early capture)",
								slog.String("session", sessionID),
								slog.String("external_id", event.Content),
								slog.String("err", err.Error()),
							)
						} else {
							slog.Info("early-captured external session ID",
								slog.String("session", sessionID),
								slog.String("external_id", event.Content))
						}
					}
				}
				continue
			}
			// Forward to SSE channel
			if !sendEvent(ctx, streamCh, event) {
				return finalizeStreamRun(ctx, streamCh, projectPath, backendName, sessionID, agentID, chatReq, blocks, responseMetadata, rawOutput, eventCh, wallStart)
			}

			ai.AccumulateBlock(&blocks, event)

			// Handle resume_split: the AI adapter layer detected ExitPlanMode and
			// will auto-resume. Finalize current DB message and start a new one.
			if event.Type == "resume_split" {
				slog.Info("resume_split received, finalizing current message and starting new one",
					slog.String("session", sessionID))

				// Finalize current streaming message
				if msgID, err := service.FinalizeStreamingMessage(projectPath, backendName, sessionID, serializeBlocks()); err != nil {
					slog.Error("failed to finalize pre-resume message",
						slog.String("session", sessionID),
						slog.String("err", err.Error()))
				} else if msgID > 0 && responseMetadata != nil {
					_ = service.SaveMetadata(msgID, responseMetadata)
				}

				// Save raw output if captured so far
				if rawOutput != "" {
					if msgID := service.GetStreamingMessageID(sessionID); msgID > 0 {
						if err := service.SaveRawResponse(sessionID, backendName, msgID, rawOutput); err != nil {
							slog.Error("failed to save raw response",
								slog.String("session", sessionID),
								slog.String("err", err.Error()))
						}
					}
					rawOutput = ""
				}

				// Reset blocks and metadata for the resumed stream
				blocks = nil
				responseMetadata = nil
				eventCount = 0
				wallStart = time.Now() // Reset wall-clock start for the resumed segment

				// Create new streaming assistant placeholder
				emptyContent, _ = json.Marshal(map[string]any{"blocks": []any{}})
				if _, err := service.AddChatMessage(projectPath, backendName, sessionID, "assistant", string(emptyContent), nil, true, ""); err != nil {
					slog.Error("failed to create resume streaming message",
						slog.String("session", sessionID),
						slog.String("err", err.Error()))
					return streamRunResult{err: "failed to create resume streaming message"}
				}
				continue
			}

			if event.Type == "metadata" && event.Meta != nil {
				responseMetadata = event.Meta
				// Capture external session ID from metadata.
				// All backends store their CLI-identifiable session ID in external_session_id.
				// Only update if the current value is the default (ClawBench UUID) or empty,
				// preserving CLI-assigned IDs that were already captured via session_capture.
				if event.Meta.SessionID != "" {
					existingExtID := service.GetExternalSessionID(sessionID)
					if existingExtID == "" || existingExtID == sessionID {
						if err := service.UpdateExternalSessionID(sessionID, event.Meta.SessionID); err != nil {
							slog.Error(
								"failed to save external session ID",
								slog.String("session", sessionID),
								slog.String("external_id", event.Meta.SessionID),
								slog.String("err", err.Error()),
							)
						} else {
							slog.Info("captured external session ID from metadata",
								slog.String("session", sessionID),
								slog.String("external_id", event.Meta.SessionID))
						}
					} else {
						slog.Info("metadata session ID skipped (already captured)",
							slog.String("session", sessionID),
							slog.String("existing_external_id", existingExtID),
							slog.String("new_external_id", event.Meta.SessionID))
					}
				}
			}
			eventCount++
			if eventCount%5 == 0 {
				if err := service.UpdateStreamingMessage(projectPath, backendName, sessionID, serializeBlocks()); err != nil {
					slog.Error(
						"failed to update streaming message",
						slog.String("session", sessionID),
						slog.String("err", err.Error()),
					)
				}
			}
		case <-ctx.Done():
			// Context cancelled (user cancel or disconnect) — exit the event loop promptly.
			// Without this branch, the goroutine blocks until the next event or 1s ticker.
			slog.Info("executeStreamRun context cancelled, finalizing stream",
				slog.String("session", sessionID),
				slog.String("reason", ctx.Err().Error()))
			return finalizeStreamRun(ctx, streamCh, projectPath, backendName, sessionID, agentID, chatReq, blocks, responseMetadata, rawOutput, eventCh, wallStart)
		case <-flushTicker.C:
			if len(blocks) > 0 {
				if err := service.UpdateStreamingMessage(projectPath, backendName, sessionID, serializeBlocks()); err != nil {
					slog.Error(
						"failed to update streaming message",
						slog.String("session", sessionID),
						slog.String("err", err.Error()),
					)
				}
			}
		}
	}
}

// finalizeStreamRun handles the finalize phase of a stream run: ask-question detection,
// DB finalization, raw output saving, and determining the result.
// It does NOT send a terminal SSE event.
func finalizeStreamRun(
	ctx context.Context,
	streamCh chan<- ai.StreamEvent,
	projectPath, backendName, sessionID, agentID string,
	chatReq ai.ChatRequest,
	blocks []model.ContentBlock,
	responseMetadata *ai.Metadata,
	rawOutput string,
	eventCh <-chan ai.StreamEvent,
	wallStart time.Time,
) streamRunResult {
	// Detect <ask-question> in the fully accumulated text blocks and convert to tool_use blocks.
	// This enables all backends (not just Claude/Codebuddy) to produce interactive question cards.
	if stringsContainsAnyBlock(blocks, "<ask-question") {
		slog.Info(
			"detected ask-question tag(s) in accumulated text blocks",
			slog.String("session", sessionID),
		)
		blocks = convertAskQuestionBlocks(blocks)
	}

	// Remove tool_use blocks for tool names rejected by the CLI ("not found in agent cli").
	// This covers both AskUserQuestion (when XML tags are used instead) and hallucinated
	// tool names like "/commit" (model confuses slash commands with tools).
	blocks = removeRejectedToolBlocks(blocks)

	// Merge fragmented thinking blocks produced by ACP backends.
	// ACP agents interleave AgentThoughtChunk and ToolCall events, causing
	// many tiny thinking blocks separated by tool_use. Consolidate them.
	blocks = ai.MergeConsecutiveThinkingBlocks(blocks)

	// Compute wall-clock duration and inject into metadata
	wallMs := int(time.Since(wallStart).Milliseconds())
	if responseMetadata == nil {
		responseMetadata = &ai.Metadata{}
	}
	responseMetadata.WallMs = wallMs

	// Inject ACP mode and thinking effort into metadata (if available)
	if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID); s.Mode != nil || s.Effort != nil {
		if s.Mode != nil && s.Mode.CurrentModeID != "" {
			responseMetadata.Mode = s.Mode.CurrentModeID
			// Do NOT overwrite the user's mode selection in DB with the agent's
			// runtime mode switch. The agent may auto-switch modes (e.g. code→ask)
			// during execution, but that should not persist over the user's choice.
			// User-selected mode is already persisted at POST time (line ~422-423).
		}
		if s.Effort != nil && s.Effort.CurrentID != "" {
			responseMetadata.ThinkingEffort = s.Effort.CurrentID
		}
	}
	// Inject transport type based on session-level override or agent configuration
	effectiveTransport := "cli"
	if t := service.GetSessionTransport(sessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[agentID]; ok && agent.Transport == "acp-stdio" {
		effectiveTransport = "acp-stdio"
	}
	responseMetadata.Transport = effectiveTransport

	// Always store our own model selection (not the AI backend's reported model).
	// The backend may report a different model or none at all; we want consistency.
	if sessionModel := service.GetSessionModel(sessionID); sessionModel != "" {
		responseMetadata.Model = sessionModel
	}

	// Determine cancellation reason
	cancelReason := service.GetAndClearCancelReason(sessionID)

	// Ensure responseMetadata exists — even for cancelled/empty responses,
	// we want to persist whatever info we have (wallMs, mode, transport, etc.)
	if responseMetadata == nil {
		responseMetadata = &ai.Metadata{}
	}

	// Serialize blocks + metadata as JSON for database storage
	var content string
	if len(blocks) == 0 {
		// Auto-infer reason for empty response
		var errMsg string
		var reason string
		switch {
		case cancelReason == "user":
			errMsg, reason = "User cancelled", ai.ReasonUserCancel
		case ctx.Err() == context.Canceled:
			errMsg, reason = "AI response cancelled", ai.ReasonContextCancel
		case ctx.Err() == context.DeadlineExceeded:
			errMsg, reason = "AI response timed out (30 min)", ai.ReasonTimeout
		default:
			errMsg, reason = "AI returned no content", ai.ReasonEmpty
		}
		blocks = append(blocks, model.ContentBlock{Type: "warning", Text: errMsg, Reason: reason})
		contentMap := map[string]any{"blocks": blocks, "metadata": responseMetadata}
		if cancelReason == "user" || ctx.Err() == context.Canceled {
			contentMap["cancelled"] = true
		}
		blocksJSON, _ := json.Marshal(contentMap)
		content = string(blocksJSON)
	} else {
		contentMap := map[string]any{"blocks": blocks, "metadata": responseMetadata}
		// When there are blocks but the stream was interrupted, add a warning and mark cancelled
		if cancelReason == "user" {
			contentMap["cancelled"] = true
		} else if ctx.Err() == context.Canceled {
			contentMap["cancelled"] = true
		} else if ctx.Err() == context.DeadlineExceeded {
			blocks = append(blocks, model.ContentBlock{Type: "warning", Text: "AI response timed out (30 min)", Reason: ai.ReasonTimeout})
		}
		contentMap["blocks"] = blocks
		blocksJSON, _ := json.Marshal(contentMap)
		content = string(blocksJSON)
	}
	msgID, err := service.FinalizeStreamingMessage(projectPath, backendName, sessionID, content)
	if err != nil {
		slog.Error(
			"failed to finalize streaming message",
			slog.String("session", sessionID),
			slog.String("err", err.Error()),
		)
	}

	// Diagnostic: check if external_session_id was updated during this stream.
	// For codebuddy/claude/qoder, extID always equals sessionID (ClawBench UUID) — that's normal.
	// For opencode/codex/deepseek/pi, extID should differ (CLI-assigned ID).
	// If it still equals sessionID for those backends, the CLI ID was never captured,
	// which will cause context amnesia on the next resume attempt.
	if !chatReq.Resume {
		extID := service.GetExternalSessionID(sessionID)
		if extID == "" {
			slog.Warn("session: external_session_id is empty after stream",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("cancelled", cancelReason != "" || ctx.Err() != nil))
		}
	}
	// Save metadata to dedicated table for analytical queries
	if msgID > 0 && responseMetadata != nil {
		if saveErr := service.SaveMetadata(msgID, responseMetadata); saveErr != nil {
			slog.Warn("failed to save message metadata", slog.Int64("msg_id", msgID), slog.String("err", saveErr.Error()))
		}
	}

	// Drain any remaining events from channel
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				goto saveRaw
			}
			if event.Type == "raw_output" {
				if rawOutput != "" {
					rawOutput += "\n"
				}
				rawOutput += event.RawOutput
			}
		default:
			goto saveRaw
		}
	}

saveRaw:
	// Save raw AI backend output for debugging/analysis
	if rawOutput != "" {
		if msgID := service.GetStreamingMessageID(sessionID); msgID > 0 {
			if err := service.SaveRawResponse(sessionID, backendName, msgID, rawOutput); err != nil {
				slog.Error(
					"failed to save raw response",
					slog.String("session", sessionID),
					slog.String("err", err.Error()),
				)
			}
		}
	}

	// Build result — do NOT send terminal SSE event here
	result := streamRunResult{}

	if cancelReason == "user" {
		result.cancelReason = cancelReason
	} else if ctx.Err() == context.Canceled {
		result.cancelReason = "cancel"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.err = "AI response timed out (30 min)"
	} else if len(blocks) == 0 {
		result.empty = true
	}

	slog.Info(
		"ai stream run done",
		slog.String("session", sessionID),
		slog.Int("blocks", len(blocks)),
		slog.String("cancel_reason", cancelReason),
		slog.Int("wall_ms", wallMs),
	)

	// Send updated metadata (with wallMs) to SSE before the terminal event
	// so the frontend has duration info even for cancelled streams.
	sendEvent(ctx, streamCh, ai.StreamEvent{Type: "metadata", Meta: responseMetadata})

	return result
}

// buildChatRequest constructs an ai.ChatRequest from the given parameters.
// modelOverride, if non-empty, takes precedence over the agent's default model.
// thinkingEffortOverride, if non-empty, takes precedence over the agent's YAML default.
// modeOverride, if non-empty, takes precedence over the current ACP session mode.
func buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string) ai.ChatRequest {
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
	resume := service.SessionHasAssistant(sessionID)
	isACP := false
	if transportOverride != "" {
		isACP = transportOverride == "acp-stdio"
	} else if agent, ok := model.Agents[agentID]; ok && agent.Transport == "acp-stdio" {
		isACP = true
	}
	if resume && !isACP {
		extID := service.GetExternalSessionID(sessionID)
		if extID != "" {
			effectiveSessionID = extID
			slog.Info("session resume: resolved external_session_id",
				slog.String("session", sessionID),
				slog.String("external_session_id", extID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("ext_id_is_clawbench_uuid", extID == sessionID))
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
		AssistantMessageCount: service.GetAssistantMessageCount(sessionID),
	}
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
	return buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, sessionModel, "", "", sessionTransport, fileDir)
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

// stringsContainsAnyBlock checks if any text ContentBlock contains the given substring.
func stringsContainsAnyBlock(blocks []model.ContentBlock, substr string) bool {
	for _, b := range blocks {
		if b.Type == "text" && strings.Contains(b.Text, substr) {
			return true
		}
	}
	return false
}

// extractXMLCandidate checks if the content between <ask-question> tags contains
// valid XML with <item> child elements or valid JSON with "questions" array.
// Returns the raw content string if valid, or empty string otherwise.
func extractXMLCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// XML format: check for <item> element
	if strings.Contains(trimmed, "<item>") || strings.Contains(trimmed, "<item ") {
		// Basic validation: must have <question> and <option>
		if !strings.Contains(trimmed, "<question>") || !strings.Contains(trimmed, "<option>") {
			return ""
		}
		return trimmed
	}
	// JSON format: check for "questions" key
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"questions"`) {
		var data map[string]any
		if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
			return ""
		}
		questions, ok := data["questions"].([]any)
		if !ok || len(questions) == 0 {
			return ""
		}
		// Validate at least one question has question text and options
		for _, q := range questions {
			qm, ok := q.(map[string]any)
			if !ok {
				continue
			}
			if _, hasQ := qm["question"]; hasQ {
				if opts, ok := qm["options"].([]any); ok && len(opts) > 0 {
					return trimmed
				}
			}
		}
		return ""
	}
	return ""
}

// parseAskQuestionXML parses XML-format <ask-question> content into the
// map[string]any format expected by ContentBlock.Input for "AskUserQuestion" tool.
func parseAskQuestionXML(xmlContent string) map[string]any {
	// Use regex to extract item elements (lightweight XML parsing for Go)
	reItem := regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	reHeader := regexp.MustCompile(`(?s)<header>(.*?)</header>`)
	reMultiSelect := regexp.MustCompile(`(?s)<multi-select>(.*?)</multi-select>`)
	reQuestion := regexp.MustCompile(`(?s)<question>(.*?)</question>`)
	reOption := regexp.MustCompile(`(?s)<option>(.*?)</option>`)
	reLabel := regexp.MustCompile(`(?s)<label>(.*?)</label>`)
	reDesc := regexp.MustCompile(`(?s)<description>(.*?)</description>`)

	itemMatches := reItem.FindAllStringSubmatch(xmlContent, -1)
	if len(itemMatches) == 0 {
		return nil
	}

	var questions []map[string]any
	for _, itemMatch := range itemMatches {
		itemContent := itemMatch[1]

		headerMatch := reHeader.FindStringSubmatch(itemContent)
		header := ""
		if headerMatch != nil {
			header = strings.TrimSpace(headerMatch[1])
		}

		multiSelectMatch := reMultiSelect.FindStringSubmatch(itemContent)
		multiSelect := false
		if multiSelectMatch != nil {
			multiSelect = strings.TrimSpace(multiSelectMatch[1]) == "true"
		}

		questionMatch := reQuestion.FindStringSubmatch(itemContent)
		if questionMatch == nil {
			continue
		}
		question := strings.TrimSpace(questionMatch[1])

		optionMatches := reOption.FindAllStringSubmatch(itemContent, -1)
		var options []map[string]any
		for _, optMatch := range optionMatches {
			optContent := optMatch[1]
			labelMatch := reLabel.FindStringSubmatch(optContent)
			if labelMatch == nil {
				continue
			}
			opt := map[string]any{"label": strings.TrimSpace(labelMatch[1])}
			descMatch := reDesc.FindStringSubmatch(optContent)
			if descMatch != nil {
				opt["description"] = strings.TrimSpace(descMatch[1])
			}
			options = append(options, opt)
		}

		if len(options) == 0 {
			continue
		}

		questions = append(questions, map[string]any{
			"header":      header,
			"multiSelect": multiSelect,
			"question":    question,
			"options":     options,
		})
	}

	if len(questions) == 0 {
		return nil
	}

	return map[string]any{"questions": questions}
}

// parseAskQuestionJSON parses JSON-format <ask-question> content into the
// map[string]any format expected by ContentBlock.Input for "AskUserQuestion" tool.
// JSON format: { "questions": [{ "question", "header", "multiSelect", "options": [{ "label", "description" }] }] }
func parseAskQuestionJSON(jsonContent string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil {
		return nil
	}

	rawQuestions, ok := data["questions"].([]any)
	if !ok || len(rawQuestions) == 0 {
		return nil
	}

	var questions []map[string]any
	for _, rq := range rawQuestions {
		item, ok := rq.(map[string]any)
		if !ok {
			continue
		}

		question, _ := item["question"].(string)
		if question == "" {
			continue
		}

		header, _ := item["header"].(string)
		_, multiSelect := item["multiSelect"].(bool)

		rawOptions, ok := item["options"].([]any)
		if !ok || len(rawOptions) == 0 {
			continue
		}

		var options []map[string]any
		for _, ro := range rawOptions {
			opt, ok := ro.(map[string]any)
			if !ok {
				continue
			}
			label, _ := opt["label"].(string)
			if label == "" {
				continue
			}
			entry := map[string]any{"label": label}
			if desc, ok := opt["description"].(string); ok && desc != "" {
				entry["description"] = desc
			}
			options = append(options, entry)
		}

		if len(options) == 0 {
			continue
		}

		questions = append(questions, map[string]any{
			"header":      header,
			"multiSelect": multiSelect,
			"question":    question,
			"options":     options,
		})
	}

	if len(questions) == 0 {
		return nil
	}

	return map[string]any{"questions": questions}
}

// convertAskQuestionBlocks detects <ask-question> tags in text ContentBlocks,
// parses the XML content, and converts them into tool_use ContentBlocks with
// name="AskUserQuestion". Tags are stripped from text; if no text remains the
// block is replaced entirely, otherwise a new tool_use block is appended.
//
// Tolerates three closing-tag variants:
//  1. Standard </ask-question>
//  2. Non-standard closing tags (e.g. </user_query>, obfuscated tags)
//  3. No closing tag at all (tag runs to end-of-text)
//
// Returns the updated blocks slice.
func convertAskQuestionBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Pre-compiled regexes for the three matching strategies.
	reStandard := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]*?)</ask-question>`)
	reWrongClose := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]*?)</[^>]+>`)
	reUnclosed := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]+)$`)

	// findAskMatch tries three regex strategies (from strict to loose) to locate
	// a valid <ask-question> tag in text. It returns the XML content string and
	// the [start, end) byte positions of the full tag span in text (for removal).
	// Matches are tried from last to first because earlier occurrences may be prose
	// references rather than actual structured questions.
	// Returns ("", -1, -1) if no valid match is found.
	findAskMatch := func(text string) (string, int, int) {
		for _, re := range []*regexp.Regexp{reStandard, reWrongClose, reUnclosed} {
			matches := re.FindAllStringSubmatchIndex(text, -1)
			for j := len(matches) - 1; j >= 0; j-- {
				pair := matches[j]
				if candidate := extractXMLCandidate(text[pair[2]:pair[3]]); candidate != "" {
					return candidate, pair[0], pair[1]
				}
			}
		}
		return "", -1, -1
	}

	// First pass: collect all conversions needed
	type conversion struct {
		index     int
		input     map[string]any
		cleanText string
	}
	var conversions []conversion

	for i, block := range blocks {
		if block.Type != "text" || !strings.Contains(block.Text, "<ask-question") {
			continue
		}

		xmlContent, tagStart, tagEnd := findAskMatch(block.Text)
		if xmlContent == "" {
			continue
		}

		input := parseAskQuestionXML(xmlContent)
		if input == nil {
			// Try JSON format as fallback
			input = parseAskQuestionJSON(xmlContent)
		}
		if input == nil {
			slog.Error("failed to parse ask-question content (tried XML and JSON)")
			continue
		}

		questions, ok := input["questions"]
		if !ok {
			slog.Error("ask-question missing 'questions' field")
			continue
		}
		questionsArr, ok := questions.([]map[string]any)
		if !ok || len(questionsArr) == 0 {
			slog.Error("ask-question 'questions' must be a non-empty array")
			continue
		}

		// Strip the matched tag span from the text.
		cleanText := strings.TrimSpace(block.Text[:tagStart] + block.Text[tagEnd:])
		conversions = append(conversions, conversion{index: i, input: input, cleanText: cleanText})
	}

	// Apply conversions in reverse order so index shifts don't affect earlier entries
	for i := len(conversions) - 1; i >= 0; i-- {
		c := conversions[i]
		toolBlock := model.ContentBlock{
			Type:  "tool_use",
			Name:  "AskUserQuestion",
			ID:    "ask-" + uuid.New().String(),
			Input: c.input,
			Done:  true,
		}

		if c.cleanText == "" {
			blocks[c.index] = toolBlock
		} else {
			blocks[c.index].Text = c.cleanText
			insertAt := c.index + 1
			blocks = append(blocks[:insertAt], append([]model.ContentBlock{toolBlock}, blocks[insertAt:]...)...)
		}
	}

	blocks = removeRejectedToolBlocks(blocks)

	return blocks
}

// removeRejectedToolBlocks strips tool_use blocks that were rejected by the CLI
// (Status=="error" and output contains "not found in agent cli"). These occur when
// the AI model hallucinates tool names (e.g. "/commit" as a slash command, or
// "AskUserQuestion" when <ask-question> XML tags are also emitted). The rejected
// tool_use block and its matching warning are confusing noise for the user.
// Also removes warning blocks containing the "Tool <name> not found in agent cli" pattern.
func removeRejectedToolBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Collect names of rejected tools from failed tool_use blocks
	rejectedNames := make(map[string]bool)
	for _, block := range blocks {
		if block.Type == "tool_use" && block.Status == "error" && strings.Contains(block.Output, "not found in agent cli") {
			rejectedNames[block.Name] = true
		}
	}
	if len(rejectedNames) == 0 {
		return blocks
	}

	filtered := make([]model.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		// Remove failed tool_use blocks for rejected tool names
		if block.Type == "tool_use" && block.Status == "error" && rejectedNames[block.Name] {
			slog.Info(
				"removing rejected tool_use block from CLI",
				slog.String("name", block.Name),
				slog.String("id", block.ID),
				slog.String("output", block.Output),
			)
			continue
		}
		// Remove warning blocks that reference the rejected tool name with "not found"
		if block.Type == "warning" && strings.Contains(block.Text, "not found") {
			matched := false
			for name := range rejectedNames {
				if strings.Contains(block.Text, name) {
					matched = true
					break
				}
			}
			if matched {
				slog.Info(
					"removing rejected-tool warning block",
					slog.String("text", block.Text),
				)
				continue
			}
		}
		filtered = append(filtered, block)
	}
	return filtered
}

// sendEvent sends an event to the stream channel.
// Non-blocking: if the channel is full (no SSE client reading), the event is dropped.
// This is safe because content is persisted to DB independently.
func sendEvent(ctx context.Context, ch chan<- ai.StreamEvent, event ai.StreamEvent) bool {
	select {
	case ch <- event:
		return true
	case <-ctx.Done():
		return false
	default:
		// Channel full — drop the event, DB persistence ensures no data loss
		toolID := ""
		if event.Tool != nil {
			toolID = event.Tool.ID
		}
		slog.Warn(
			"SSE event dropped — channel full",
			slog.String("type", event.Type),
			slog.String("tool_id", toolID),
		)
		return true
	}
}

// sendFinalEvent sends a terminal event (done/cancelled/error) to the stream channel
// without checking context cancellation. This ensures the SSE client always receives
// the terminal event even after the CLI context has been cancelled (e.g. ExitPlanMode).
func sendFinalEvent(ch chan<- ai.StreamEvent, event ai.StreamEvent) {
	select {
	case ch <- event:
	default:
		slog.Warn(
			"SSE terminal event dropped — channel full",
			slog.String("type", event.Type),
		)
	}
}
