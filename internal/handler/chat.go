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
		var sessionTitle, sessionAgentID, sessionModelID, sessionThinkingEffort string
		var sessionInfoBackend string
		if cachedSessionInfo != nil {
			sessionTitle = cachedSessionInfo.Title
			sessionInfoBackend = cachedSessionInfo.Backend
			sessionAgentID = cachedSessionInfo.AgentID
			sessionModelID = cachedSessionInfo.Model
			sessionThinkingEffort = cachedSessionInfo.ThinkingEffort
		}
		if sessionInfoBackend != "" {
			sessionBackend = sessionInfoBackend
		}
		running := service.IsSessionRunning(sessionID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "thinkingEffort": sessionThinkingEffort, "total": totalCount})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "running": running, "sessionId": sessionID, "sessionTitle": sessionTitle, "backend": sessionBackend, "agentId": sessionAgentID, "modelId": sessionModelID, "thinkingEffort": sessionThinkingEffort, "total": totalCount})
		return
	}

	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Get backend from session, not from global state
	sessionID := getSessionID(r)
	if sessionID == "" {
		// No session yet — auto-create one (same logic as GET)
		// Check session count limit before auto-creating (0 = unlimited)
		if model.SessionMaxCount > 0 {
			if count, cerr := service.GetSessionCount(projectPath); cerr == nil && count >= model.SessionMaxCount {
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
				return
			}
		}
		agentID2 := model.GetDefaultAgentID()
		sessionBackend2, _, _, _, ok := resolveAgentConfig(agentID2)
		if !ok {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
			return
		}
		var err error
		// Don't pre-fill agent default model — leave empty so frontend
		// falls back to global localStorage preference (cross-project).
		sessionID, err = service.CreateSession(projectPath, sessionBackend2, T(r, "NewSession"), agentID2, "", "default", "chat")
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to create session")))
			return
		}
		setSessionID(w, sessionID)
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

	// Persist thinking effort selection for this session so subsequent loads
	// restore the user's choice instead of the agent default (auto/empty).
	if req.ThinkingEffort != "" {
		service.UpdateSessionThinkingEffort(sessionID, req.ThinkingEffort)
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

		// Use independent context with cancel to prevent goroutine leaks
		// and support user-initiated cancellation (no timeout - let AI run indefinitely)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		service.RegisterSessionCancel(sessionID, cancel)
		defer service.UnregisterSessionCancel(sessionID)

		// Build the first chat request
		firstChatReq := buildChatRequest(prompt, sessionID, projectPath, backendName, effectiveAgentID, req.ModelID, req.ThinkingEffort, fileDir)

		// Execute first message
		result := executeStreamRun(ctx, r, streamCh, projectPath, sessionID, backendName, effectiveAgentID, firstChatReq, fileDir)

		// Drain loop: keep executing queued messages after normal completion
		for {
			if result.cancelReason == "user" {
				service.ClearQueue(sessionID)
				sendFinalEvent(streamCh, ai.StreamEvent{Type: "cancelled"})
				return
			}
			if result.err != "" {
				sendFinalEvent(streamCh, ai.StreamEvent{Type: "error", Error: result.err})
				return
			}
			if result.empty {
				sendFinalEvent(streamCh, ai.StreamEvent{Type: "error", Error: "AI returned no content", Reason: ai.ReasonEmpty})
				return
			}
			if result.cancelReason != "" {
				// Other cancel reasons
				sendFinalEvent(streamCh, ai.StreamEvent{Type: "cancelled"})
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
				sendFinalEvent(streamCh, ai.StreamEvent{Type: "done"})
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
	backend, err := ai.NewBackend(backendName)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", backendName), slog.String("err", err.Error()))
		errMsg := T(r, "BackendCreateFailed", map[string]any{"Error": err.Error()})
		if !sendEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg}) {
			return streamRunResult{err: errMsg}
		}
		_, _ = service.AddChatMessage(projectPath, backendName, sessionID, "assistant", errMsg, nil, false, "")
		return streamRunResult{err: errMsg}
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
		contentMap := map[string]any{"blocks": blocks}
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
				rawOutput = event.RawOutput
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
				if err := service.FinalizeStreamingMessage(projectPath, backendName, sessionID, serializeBlocks()); err != nil {
					slog.Error("failed to finalize pre-resume message",
						slog.String("session", sessionID),
						slog.String("err", err.Error()))
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
						}
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

	// Compute wall-clock duration and inject into metadata
	wallMs := int(time.Since(wallStart).Milliseconds())
	if responseMetadata == nil {
		responseMetadata = &ai.Metadata{}
	}
	responseMetadata.WallMs = wallMs

	// Determine cancellation reason
	cancelReason := service.GetAndClearCancelReason(sessionID)

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
		contentMap := map[string]any{"blocks": blocks}
		if cancelReason == "user" || ctx.Err() == context.Canceled {
			contentMap["cancelled"] = true
		}
		blocksJSON, _ := json.Marshal(contentMap)
		content = string(blocksJSON)
	} else {
		contentMap := map[string]any{"blocks": blocks}
		if responseMetadata != nil {
			contentMap["metadata"] = responseMetadata
		}
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
	if err := service.FinalizeStreamingMessage(projectPath, backendName, sessionID, content); err != nil {
		slog.Error(
			"failed to finalize streaming message",
			slog.String("session", sessionID),
			slog.String("err", err.Error()),
		)
	}

	// Drain any remaining events from channel
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				goto saveRaw
			}
			if event.Type == "raw_output" && rawOutput == "" {
				rawOutput = event.RawOutput
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
func buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, fileDir string) ai.ChatRequest {
	systemPrompt := ""
	agentModel := ""
	agentCommand := ""
	effectiveThinkingEffort := thinkingEffortOverride // Frontend selection takes priority

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
	effectiveSessionID := sessionID
	resume := service.SessionHasAssistant(sessionID)
	if resume {
		extID := service.GetExternalSessionID(sessionID)
		if extID != "" {
			effectiveSessionID = extID
		} else {
			// No external session ID available — the CLI cannot resume a session
			// it has never seen. Start a fresh session instead.
			effectiveSessionID = ""
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
	return buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, sessionModel, service.GetSessionThinkingEffort(sessionID), fileDir)
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
// XML with <item> child elements. Returns the raw XML content string if valid,
// or empty string otherwise.
func extractXMLCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// Must contain <item> element (XML format)
	if !strings.Contains(trimmed, "<item>") && !strings.Contains(trimmed, "<item ") {
		return ""
	}
	// Basic validation: must have <question> and <option>
	if !strings.Contains(trimmed, "<question>") || !strings.Contains(trimmed, "<option>") {
		return ""
	}
	return trimmed
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
			slog.Error("failed to parse ask-question XML")
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
