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
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ws"
)

const maxChatBodySize = 10 << 20 // 10MB

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

		// Note: loading message history must NOT mark the session as read.
		// Automatic reloads (WS reconnect refresh, completion-event refresh,
		// pagination) hit this GET and would otherwise clear the unread badge
		// before the user actually views the session — the Android floating
		// window then never shows an unread state for a background-finished
		// session. "Mark as read" is an explicit user action and goes through
		// POST /api/ai/chat/read (MarkChatRead).

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
	// Support sending to an external project's session: the frontend passes ?project_path=
	// (the session's owning project), which overrides the cookie project for ownership
	// verification. Only an exact match with the session's own project is accepted, so
	// this cannot be used to bypass access control.
	if qp := r.URL.Query().Get("project_path"); qp != "" {
		if sp := service.GetSessionProjectPath(sessionID); sp != "" && sp == qp {
			// The session belongs to the requested project. Switch the working
			// project to the session's owner so every subsequent write (user
			// message insert, queued insert, the AI goroutine's streaming
			// placeholder and drain-loop messages) persists under the session's
			// project_path — NOT the current cookie project. Without this,
			// replying from the completion popover while the user has switched
			// to another project orphans the reply under the cookie project,
			// making it invisible when the user returns to the session.
			projectPath = qp
		} else {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
	} else if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
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

	// Validate file entries are within project and determine isDir via os.Stat.
	// URL entries carry an external address, not a local path: they are passed
	// through untouched and never resolved against the filesystem.
	validatedFileEntries := make([]model.FileEntry, 0, len(req.Files))
	for _, fEntry := range req.Files {
		if fEntry.IsURL() {
			entry, ok := validatedURLEntry(w, r, fEntry)
			if !ok {
				return
			}
			validatedFileEntries = append(validatedFileEntries, entry)
			continue
		}
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
		// URL entries are not filesystem paths and must not be prefixed onto
		// the prompt as if they were local files.
		if f.IsURL() {
			continue
		}
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
	// ClawBench's own "/cb-*" commands share the "/" prefix but are NOT agent
	// commands: they are injected locally below and must keep file prefixes.
	isSlashCmd := ai.IsACPSlashCommand(req.Message) && !IsClawbenchCommand(req.Message)
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

	// ClawBench built-in command injection: detect on raw req.Message, prepend
	// template to prompt. Must happen after file path prefixes are added so the
	// AI sees both the injected context and file context, but detection is on
	// raw req.Message (since file prefixes would break the "/cb-" prefix check).
	// matchClawbenchCommand also matches the bare command (no trailing space),
	// which is what the frontend sends after trimming a menu selection.
	//
	// Every built-in command shares the same shape — optional precondition,
	// then render-and-prepend — so new commands only need an entry in
	// clawbenchCommandPrecheck and processClawbenchCommand, not another branch
	// here.
	if IsClawbenchCommand(req.Message) {
		if !clawbenchCommandPrecheck(w, r, req.Message) {
			return
		}
		injected, err := processClawbenchCommand(req.Message, projectPath, sessionID)
		if err != nil {
			slog.Error("failed to render clawbench command injection", slog.String("err", err.Error()))
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		prompt = injected + "\n\n" + prompt
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
	runCtx, claimed := service.TryClaimSessionRun(sessionID)
	if !claimed {
		// Session already running — enqueue the message to DB (queued=1).
		// The running drain loop picks it up via DequeueQueuedMessage.
		//
		// Deliberately NOT auto-injecting into the running turn here: joining
		// the current reply is an explicit choice the user makes on the queued
		// bubble (POST /api/ai/queue/inject). Sending always queues, so the
		// behavior is identical for every backend and the message is visible
		// (and actionable) in the queue instead of silently disappearing into
		// the reply being written.
		//
		// The row is inserted here, AFTER the claim above. That order matters:
		// the claim marks the live runner as having pending work, and a runner
		// about to exit consumes that mark on its next retire check. Inserting
		// afterwards would leave the mark consumed with the row not yet visible,
		// and the runner would exit before it could dequeue — stranding the
		// message until the reaper's next pass. (The queue path inserts first
		// for the same reason.) A row that is already visible when the runner
		// re-checks cannot be missed.
		msgID, err := service.AddQueuedMessage(projectPath, backendName, sessionID, req.Message, allFiles, req.QueueID, T(r, "FileMessage"))
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "EnqueueFailed")
			return
		}
		// Re-mark the runner as having pending work. The claim above already set
		// this, but a retire pass may have consumed the mark between the claim
		// and the insert above; re-marking now that the row exists makes the
		// guarantee unconditional rather than depending on timing.
		service.WakeSessionRunner(sessionID)

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
				Queued:         true, // enqueued: waiting for the drain loop, not yet started
			},
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"running": true,
			"queued":  true,
		})
		return
	}

	msgID, err := service.AddChatMessage(projectPath, backendName, sessionID, "user", req.Message, allFiles, false, T(r, "FileMessage"), req.QueueID)
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

	// The context came from the claim, so it is both the one the runner owns and
	// the one CancelSession will cancel. It is captured at claim time on purpose:
	// a lookup here could find nothing if a cancel landed in between, and by now
	// the user message has already been persisted with queued=0 — the reaper only
	// scans queued rows, so a message skipped here would be lost outright.
	ctx := runCtx

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
				// Retire the runner and cancel its context in one step, so the
				// session cannot be left running with nothing to cancel it.
				service.FinishSessionRun(sessionID)
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
		// Single cleanup: removes the runner and cancels its context. Replaces the
		// previous three separate defers (clear flag / cancel / unregister) whose
		// execution order was load-bearing and left a window where the session was
		// still "running" with no cancel func registered.
		defer service.FinishSessionRun(sessionID)
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
				// Only the first terminal state may push + broadcast "completed".
				// If a concurrent CancelSession already claimed the terminal guard
				// (broadcasting "cancelled"), EmitSessionPushNotification returns
				// false and we must NOT also broadcast "completed" — otherwise
				// clients see cancelled followed by a contradictory completed
				// (ISS-247 reverse race).
				if !service.EmitSessionPushNotification(sessionID, "completed") {
					return
				}
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
				nextChatReq, buildErr := buildChatRequestFromQueue(qMsg, sessionID, projectPath, backendName, effectiveAgentID, fileDir)
				if buildErr != nil {
					return service.DrainResult{Err: buildErr.Error()}
				}
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
	// Delegate the whole turn to the service layer's single implementation,
	// shared with the queue/push path and the scheduler. runTurn owns the
	// per-turn context, the turn-cancel registration, backend creation, the
	// streaming placeholder, stream_start, the event loop and Finalize — so a
	// fix can no longer land in only one of the copies.
	res := service.RunTurn(service.TurnSpec{
		Ctx:             ctx,
		Mode:            service.ModeInteractive,
		ProjectPath:     projectPath,
		BackendName:     backendName,
		SessionID:       sessionID,
		AgentID:         agentID,
		ChatReq:         chatReq,
		FileDir:         fileDir,
		QueueID:         queueID,
		DrainOnFinalize: true,
		LocalizeError: func(err error, key string, args map[string]any) string {
			return T(r, key, args)
		},
	})

	return streamRunResult{
		cancelReason: res.CancelReason,
		err:          res.Err,
		empty:        res.Empty,
	}
}

// buildChatRequest delegates to the service layer's single implementation.
//
// Kept as a thin wrapper (rather than updating the ~30 call sites and tests that
// use it) so the unification lands as one behavioural change: the handler no
// longer has its own copy to drift from the queue/push path.
func buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string, hasAttachments bool) ai.ChatRequest {
	return service.BuildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir, hasAttachments)
}

// buildForkContext delegates to the service layer's single implementation.
func buildForkContext(sessionID string) string {
	return service.BuildForkContext(sessionID)
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
// It returns an error only when a ClawBench built-in command's endpoint
// reference cannot be rendered from the embedded spec.
func buildChatRequestFromQueue(qMsg model.QueuedMessage, sessionID, projectPath, backendName, agentID, fileDir string) (ai.ChatRequest, error) {
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

	// ClawBench built-in command injection for queued messages (same logic as
	// the primary message path). A render failure means the embedded spec is
	// unusable — surface it rather than sending the AI an incomplete contract.
	injected, err := processClawbenchCommand(qMsg.Text, projectPath, sessionID)
	if err != nil {
		slog.Error("failed to render clawbench command injection", slog.String("err", err.Error()))
		return ai.ChatRequest{}, err
	}
	if injected != qMsg.Text {
		prompt = injected + "\n\n" + prompt
	}

	// No explicit model/transport overrides: the shared builder reads the
	// session's persisted choices itself, so a queued message honours the same
	// model and transport a direct send would. Passing them here as well would
	// duplicate that lookup and invite the two paths to drift again.
	hasAttachments := len(qMsg.FilePaths) > 0 || len(qMsg.Files) > 0
	return buildChatRequest(prompt, sessionID, projectPath, backendName, agentID, "", "", "", "", fileDir, hasAttachments), nil
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

// MarkChatRead handles POST to mark a session as read (updates last_read_at
// and broadcasts status="read"). Used by the completion popover's "mark as
// read" button when the user replies without typing anything.
func MarkChatRead(w http.ResponseWriter, r *http.Request) {
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

	// Verify the session belongs to the requesting project. Support marking an
	// external project's session read: ?project_path= (the session's owning
	// project) overrides the cookie project; only an exact match is accepted.
	if qp := r.URL.Query().Get("project_path"); qp != "" {
		if sp := service.GetSessionProjectPath(sessionID); sp == "" || sp != qp {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
	} else if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	service.UpdateLastRead(sessionID)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// isSafeExternalURL reports whether an external URL attachment may be persisted
// and later rendered as a clickable link.
//
// Only http(s) is allowed. The value is stored and re-rendered as an anchor
// href on every subsequent load, so a javascript:/data: entry would become an
// executable link long after the request that created it. Rejecting it at the
// boundary keeps every renderer safe without each one having to remember a
// guard.
func isSafeExternalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

// validatedURLEntry normalizes one URL attachment, writing the error response
// and returning ok=false when it is unusable.
//
// Shared by every endpoint that accepts file entries (chat and queue): both
// persist the entry and both later render it as a link, so the scheme check and
// the label handling must not drift apart between them.
func validatedURLEntry(w http.ResponseWriter, r *http.Request, fEntry model.FileEntry) (model.FileEntry, bool) {
	// Path carries the human-readable label (e.g. "owner/repo#123") and must be
	// preserved: it is the chip text shown after a reload. Only the filesystem
	// resolution is skipped for URL entries.
	//
	// The scheme is restricted to http(s) here, at the boundary, because this
	// value is persisted and later re-rendered as an anchor href. A
	// javascript:/data: entry stored once would otherwise become an executable
	// link on every subsequent load, and client-side guards are not enough (a
	// different renderer may forget to apply one).
	u := strings.TrimSpace(fEntry.URL)
	if !isSafeExternalURL(u) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidURLAttachment")
		return model.FileEntry{}, false
	}
	return model.FileEntry{Path: fEntry.Path, Kind: "url", URL: u}, true
}
