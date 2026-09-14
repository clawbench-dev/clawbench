package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// DingTalkSessionInfo carries session metadata for the DingTalk session command feature.
type DingTalkSessionInfo struct {
	ID          string
	Title       string
	ProjectPath string
	Backend     string
	AgentID     string
	Model       string
}

// FeishuSessionInfo carries session metadata for the Feishu session command feature.
// It shares the same structure as DingTalkSessionInfo.
type FeishuSessionInfo = DingTalkSessionInfo

// FindSessionsByPrefix finds non-archived chat sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := dbRead.QueryContext(
		context.Background(),
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE LOWER(id) LIKE LOWER(?) AND archived = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT 10`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

// ListRecentSessions returns the most recently updated non-archived chat sessions.
func ListRecentSessions(limit int) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := dbRead.QueryContext(
		context.Background(),
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE archived = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

// FindRunningSessionsByPrefix finds currently-running sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindRunningSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	runningIDs := GetRunningSessionIDs()
	if len(runningIDs) == 0 {
		return nil, nil
	}

	lowerPrefix := strings.ToLower(prefix)
	var matchingIDs []string
	for _, id := range runningIDs {
		if len(id) >= len(lowerPrefix) && strings.ToLower(id[:len(lowerPrefix)]) == lowerPrefix {
			matchingIDs = append(matchingIDs, id)
		}
	}
	if len(matchingIDs) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	args := make([]any, len(matchingIDs))
	for i, id := range matchingIDs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args[i] = id
	}

	rows, err := dbRead.QueryContext(
		context.Background(),
		fmt.Sprintf(
			`SELECT id, title, project_path, backend, agent_id, model
			 FROM chat_sessions
			 WHERE id IN (%s) AND archived = 0`,
			sb.String(),
		),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows), nil
}

func scanDingTalkSessionInfos(rows *sql.Rows) []DingTalkSessionInfo {
	var results []DingTalkSessionInfo
	for rows.Next() {
		var info DingTalkSessionInfo
		if err := rows.Scan(&info.ID, &info.Title, &info.ProjectPath, &info.Backend, &info.AgentID, &info.Model); err != nil {
			slog.Warn("scanDingTalkSessionInfos: skipping row", "error", err)
			continue
		}
		results = append(results, info)
	}
	return results
}

// SendMessageToSessionFromDingTalk sends a message to a non-running session from DingTalk.
func SendMessageToSessionFromDingTalk(sessionID, message string) error {
	return sendMessageToSessionFromPush(sessionID, message)
}

// SendMessageToSessionFromFeishu sends a message to a non-running session from Feishu.
func SendMessageToSessionFromFeishu(sessionID, message string) error {
	return sendMessageToSessionFromPush(sessionID, message)
}

// sendMessageToSessionFromPush is the shared implementation for sending a message
// to a non-running session from any push backend (DingTalk, Feishu, etc.).
func sendMessageToSessionFromPush(sessionID, message string) error {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Persist the message + start execution or signal the running drain loop.
	// EnqueueAndMaybeStart handles the B2 drain-loop exit race internally, and
	// may inject the message into the running turn instead of queueing it.
	// msgID is the persisted DB id — used to emit a user_message event carrying
	// the real id (not 0) for cross-device sync.
	_, _, msgID, err := EnqueueAndMaybeStart(EnqueueStartConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     message,
	})
	if err != nil {
		return err
	}

	// Emit user_message for cross-device sync. MessageID is the persisted DB id.
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID: msgID,
			Content:   message,
		},
	})

	return nil
}

// LaunchConfig configures a session execution launched from non-HTTP contexts.
type LaunchConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string
	AgentID     string
	Message     string
	// QueueID is the queue_id of the queued user message this execution answers
	// (set when draining a queued message). It is recorded on the reply row so
	// the frontend can anchor the reply to its own question when multiple
	// queued messages interleave (DB id order ≠ conversational order).
	QueueID string
}

// LaunchSessionExecution starts the AI execution goroutine for a session.
// The caller must have already persisted the user message and called TrySetSessionRunning.
func LaunchSessionExecution(cfg LaunchConfig) {
	sessionID := cfg.SessionID
	// Reuse the context the runner created when it claimed the session, so
	// "running" and "cancellable" refer to the same execution. The caller has
	// just won TrySetSessionRunning, so the runner is present; if it vanished in
	// the meantime (a cancel landed) there is nothing to execute.
	ctx := runnerContext(sessionID)
	if ctx == nil {
		slog.Warn("launch: session runner gone before execution started",
			slog.String("session", sessionID))
		return
	}

	go func() {
		defer handleSessionPanic(cfg, sessionID)

		// Single cleanup: removes the runner and cancels its context.
		defer FinishSessionRun(sessionID)
		defer handleACPCleanup(sessionID, cfg.AgentID)

		markDoneAndSendFinal := func(event ai.StreamEvent) {
			SetSessionRunning(sessionID, false, true) // skip event — we emit directly
			emitDrainEvent(sessionID, event)
			// DingTalk/Feishu push — EmitSessionEvent is skipped above, so push
			// must be triggered here for normal completion.
			// Skip for cancelled: CancelSession already calls EmitSessionEvent("cancelled")
			// which handles push. Skip for error: no meaningful push content.
			if event.Type == eventTypeDone {
				// Only the first terminal state may push + broadcast "completed".
				// If a concurrent CancelSession already claimed the terminal guard
				// (broadcasting "cancelled"), EmitSessionPushNotification returns
				// false and we must NOT also broadcast "completed" — otherwise
				// clients see cancelled followed by a contradictory completed.
				if !EmitSessionPushNotification(sessionID, statusCompleted) {
					return
				}
				// Global WS broadcast so all clients (even ones that missed the
				// stream "done") clear the session's running flag.
				emitSessionEvent(sessionID, statusCompleted, false, false, true)
			}
		}

		result := executeStreamRunShared(ctx, cfg)
		RunDrainLoop(DrainConfig{
			SessionID:   sessionID,
			ProjectPath: cfg.ProjectPath,
			BackendName: cfg.BackendName,
			ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
				cfg.Message = msg.Content
				cfg.QueueID = msg.QueueID
				nextResult := executeStreamRunShared(ctx, cfg)
				return DrainResult{
					CancelReason: nextResult.cancelReason,
					Err:          nextResult.err,
					Empty:        nextResult.empty,
				}
			},
			MarkDoneAndSendFinal: markDoneAndSendFinal,
		}, DrainResult{
			CancelReason: result.cancelReason,
			Err:          result.err,
			Empty:        result.empty,
		})
	}()
}

// EnqueueStartConfig carries the parameters for EnqueueAndMaybeStart.
type EnqueueStartConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string
	AgentID     string
	Message     string
	Files       []model.FileEntry
	QueueID     string
	// ModelID / Transport are persisted to the session so the drain loop's
	// buildChatRequestFromQueue uses the user's choices, not agent defaults
	// (parity with the POST /api/ai/chat path).
	ModelID   string
	Transport string
}

// EnqueueAndMaybeStart is the unified enqueue entry point (POST /api/ai/queue).
// It persists the message to chat_history (queued=1), then:
//   - if the session is idle, claims it and starts an execution goroutine that
//     runs the message and then drains the rest of the queue (started=true);
//   - if the session is already running, marks its runner as having pending work
//     and returns (started=false) — that runner's loop will dequeue it.
//
// A message can no longer be stranded between those two outcomes: a runner only
// exits after re-checking for late work under the same lock this function uses
// to submit (see retireRunner). That atomic check replaced the previous
// "signal + 100ms delayed re-check" workaround.
//
// It returns started=true when a new execution goroutine was launched (the
// session was idle), plus the persisted DB message id (msgID, >0) so callers
// can emit a user_message event carrying the real id for cross-device sync.
func EnqueueAndMaybeStart(cfg EnqueueStartConfig) (started bool, injected bool, msgID int64, err error) {
	// Sending always queues while a turn is running — for every backend.
	//
	// Mid-turn injection is NOT attempted here on purpose. It used to be, which
	// meant a steer-capable backend silently swallowed the message into the
	// running turn: no queue bubble appeared, so the user got no feedback and no
	// way to act on it. Now the message is always queued (visible, cancelable)
	// and joining the current reply is an explicit action on its bubble
	// (POST /api/ai/queue/inject) — so the flow is uniform across backends and
	// only the action's LABEL differs.
	//
	// `injected` is kept in the signature for the HTTP response shape; it is now
	// always false on this path (see InjectQueuedMessage for the real one).
	_ = injected

	// Persist model/transport selection so the drain loop uses the user's
	// choices (parity with the POST /api/ai/chat handler).
	if cfg.ModelID != "" {
		if updateErr := UpdateSessionModel(cfg.SessionID, cfg.ModelID); updateErr != nil {
			slog.Warn("enqueue: failed to persist session model",
				slog.String("session", cfg.SessionID), slog.String("error", updateErr.Error()))
		}
	}
	if cfg.Transport != "" {
		if updateErr := UpdateSessionTransport(cfg.SessionID, cfg.Transport); updateErr != nil {
			slog.Warn("enqueue: failed to persist session transport",
				slog.String("session", cfg.SessionID), slog.String("error", updateErr.Error()))
		}
	}

	msgID, err = AddQueuedMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, cfg.Message, cfg.Files, cfg.QueueID, "")
	if err != nil {
		return false, false, 0, err
	}

	// Claim the session. created=true means we must start the execution; false
	// means a live runner exists and has been woken to pick this message up.
	// Either way the message cannot be stranded: a runner that is about to exit
	// re-checks for late work under the same lock (see retireRunner).
	if TrySetSessionRunning(cfg.SessionID) {
		// Session was idle — the message we just queued is the FIRST one and must
		// NOT be consumed twice. executeStreamRunShared runs cfg.Message directly,
		// so dequeue the row we just inserted (it would otherwise be picked up
		// again by the drain loop's DequeueQueuedMessage, executing it twice).
		//
		// Consume BY ID: a concurrent enqueue may have slipped an earlier row
		// into the queue between our insert and the TrySetSessionRunning claim,
		// and that earlier row belongs to the drain loop, not to this execution.
		consumeQueuedMessageByID(cfg.SessionID, msgID)
		// Start execution now; the loop inside will consume the REST of the
		// queue (any messages beyond the first).
		LaunchSessionExecution(LaunchConfig{
			SessionID:   cfg.SessionID,
			ProjectPath: cfg.ProjectPath,
			BackendName: cfg.BackendName,
			AgentID:     cfg.AgentID,
			Message:     cfg.Message,
			QueueID:     cfg.QueueID,
		})
		return true, false, msgID, nil
	}

	// A runner already exists and has been woken; its loop will dequeue this
	// message. No signal or delayed re-check is needed — the wake flag set by
	// TrySetSessionRunning is what the runner consults before exiting.
	return false, false, msgID, nil
}

// consumeQueuedMessageByID dequeues the specific queued message identified by
// msgID (the row just inserted by AddQueuedMessage). Called right before
// LaunchSessionExecution when the execution will run cfg.Message directly: the
// row is still queued=1, and without dequeuing it here the drain loop would
// pick it up a second time and execute it twice.
//
// Consuming by ID (instead of "first queued") is required because a concurrent
// enqueue can insert an earlier row between our AddQueuedMessage and the
// TrySetSessionRunning claim (R1). Dequeueing "the first" would claim that
// other message while we run our own — leaving it queued for a double
// execution, or (in the symmetric race) dropping it entirely.
func consumeQueuedMessageByID(sessionID string, msgID int64) {
	if msgID <= 0 {
		slog.Warn("enqueue: invalid msgID for consume", slog.String("session", sessionID))
		return
	}
	if _, ok, derr := DequeueQueuedMessageByID(sessionID, msgID); derr != nil {
		// Not fatal — the drain loop will retry the dequeue. Log and continue.
		slog.Warn("enqueue: failed to consume queued message",
			slog.String("session", sessionID), slog.Int64("msgID", msgID), slog.String("error", derr.Error()))
	} else if !ok {
		slog.Warn("enqueue: expected to consume queued message but row not queued",
			slog.String("session", sessionID), slog.Int64("msgID", msgID))
	}
}

// handleSessionPanic recovers from panics in the session goroutine.
func handleSessionPanic(cfg LaunchConfig, sessionID string) {
	if r := recover(); r != nil {
		slog.Error(
			"session goroutine panicked",
			slog.String("session", sessionID),
			slog.Any("panic", r),
			slog.String("stack", string(debug.Stack())),
		)
		// Retire the runner and cancel its context together, so the session is
		// not left running with nothing to cancel it.
		FinishSessionRun(sessionID)
		emitDrainEvent(sessionID, ai.StreamEvent{Type: eventTypeError, Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
		// Push cancelled notification — panic is a terminal state
		EmitSessionPushNotification(sessionID, statusCancelled)
		errMsg := "AI internal error, please retry"
		errContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{map[string]string{contentKeyType: blockTypeWarning, contentKeyText: errMsg, contentKeyReason: ai.ReasonPanic}}})
		_, _ = FinalizeStreamingMessage(cfg.ProjectPath, cfg.BackendName, sessionID, string(errContent))
	}
}

// handleACPCleanup marks the ACP connection as idle after session completion.
func handleACPCleanup(sessionID, agentID string) {
	effectiveTransport := transportCLI
	if t := GetSessionTransport(sessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[agentID]; ok && agent.Transport != "" {
		effectiveTransport = agent.Transport
	}
	if effectiveTransport == transportACPStdio {
		slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", agentID)
		ai.GetACPConnManager().MarkIdle(sessionID)
	}
}

// BuildChatRequest constructs an ai.ChatRequest from the given parameters.
// This is the service-layer equivalent of handler.buildChatRequest, without HTTP-specific i18n.
func BuildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string, hasAttachments bool) ai.ChatRequest {
	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}

	agentCfg := resolveAgentConfig(agentID, projectPath, modelOverride, thinkingEffortOverride, modeOverride)
	isACP := resolveIsACP(transportOverride, agentID)
	effectiveSessionID, resume, forkContext := resolveSessionState(sessionID, agentID, isACP)

	systemPrompt := agentCfg.systemPrompt
	if hasAttachments {
		systemPrompt = appendMediaPrompt(systemPrompt)
	}

	// HasConversationHistory drives the amnesia-prevention fallback in
	// acp_backend: true blocks silent fallback to NewSession when a session
	// may hold history. On count failure, conservatively assume history exists
	// rather than risk dropping the session context.
	hasHistory := true
	if count, err := GetChatMessageCount(sessionID); err == nil {
		hasHistory = count > 0
	} else {
		slog.Warn("BuildChatRequest: GetChatMessageCount failed, assuming conversation history", "session_id", sessionID, "err", err)
	}

	return ai.ChatRequest{
		Prompt:                 prompt,
		SessionID:              effectiveSessionID,
		WorkDir:                fileDir,
		SystemPrompt:           systemPrompt,
		Model:                  agentCfg.agentModel,
		Command:                agentCfg.agentCommand,
		AgentID:                agentID,
		ThinkingEffort:         agentCfg.effectiveThinkingEffort,
		Mode:                   agentCfg.effectiveMode,
		Resume:                 resume,
		HasAttachments:         hasAttachments,
		AssistantMessageCount:  GetAssistantMessageCount(sessionID),
		HasConversationHistory: hasHistory,
		ForkContext:            forkContext,
	}
}

// agentConfigResult holds the resolved agent configuration fields.
type agentConfigResult struct {
	systemPrompt            string
	agentModel              string
	agentCommand            string
	effectiveThinkingEffort string
	effectiveMode           string
}

// resolveAgentConfig resolves system prompt, model, command, thinking effort, and mode from agent config.
func resolveAgentConfig(agentID, projectPath, modelOverride, thinkingEffortOverride, modeOverride string) agentConfigResult {
	result := agentConfigResult{
		effectiveThinkingEffort: thinkingEffortOverride,
		effectiveMode:           modeOverride,
	}
	agent, ok := model.Agents[agentID]
	if !ok {
		return result
	}
	result.systemPrompt = agent.SystemPrompt
	if projectPath != "" {
		result.systemPrompt = strings.ReplaceAll(result.systemPrompt, "{{PROJECT_PATH}}", projectPath)
	}
	if modelOverride != "" {
		result.agentModel = modelOverride
	} else if defaultID := agent.DefaultModelID(); defaultID != "" {
		result.agentModel = defaultID
	}
	if agent.Command != "" {
		result.agentCommand = agent.Command
	}
	if result.effectiveThinkingEffort == "" && agent.EffectiveThinkingEffort() != "" {
		result.effectiveThinkingEffort = agent.EffectiveThinkingEffort()
	}
	if result.effectiveMode == "" && agent.EffectiveModeID() != "" {
		result.effectiveMode = agent.EffectiveModeID()
	}
	return result
}

// resolveIsACP determines whether the transport is ACP stdio.
func resolveIsACP(transportOverride, agentID string) bool {
	if transportOverride != "" {
		return transportOverride == transportACPStdio
	}
	if agent, ok := model.Agents[agentID]; ok {
		return agent.Transport == transportACPStdio
	}
	return false
}

// resolveSessionState resolves the effective session ID, resume flag, and fork context.
func resolveSessionState(sessionID string, _ string, isACP bool) (effectiveSessionID string, resume bool, forkContext string) {
	effectiveSessionID = sessionID
	resume = SessionHasAssistant(sessionID)

	var resolvedExtID string
	if resume {
		resolvedExtID = GetExternalSessionID(sessionID)
	}

	if resume && !isACP {
		if resolvedExtID != "" {
			effectiveSessionID = resolvedExtID
		} else {
			effectiveSessionID = ""
		}
	}

	if resume && resolvedExtID == "" {
		forkContext = BuildForkContext(sessionID)
		if forkContext != "" && isACP {
			resume = false
		}
	}

	return effectiveSessionID, resume, forkContext
}

// appendMediaPrompt appends the media prompt to the system prompt if non-empty.
func appendMediaPrompt(systemPrompt string) string {
	mediaPrompt := model.BuildMediaPrompt()
	if mediaPrompt == "" {
		return systemPrompt
	}
	if systemPrompt != "" {
		return systemPrompt + "\n\n" + mediaPrompt
	}
	return mediaPrompt
}

// BuildForkContext reads the chat history from DB and formats it as a text block
// that can be prepended to the user's prompt for fork sessions.
// Includes text blocks as-is and tool_use blocks as structured JSON wrapped in
// <tool_use> tags. Thinking blocks are excluded.
func BuildForkContext(sessionID string) string {
	messages, err := GetMessagesBySessionIDRaw(sessionID)
	if err != nil || len(messages) == 0 {
		return ""
	}

	// Batch-fetch tool call details for the session (input/output are stored
	// separately in chat_tool_calls, not in content JSON).
	toolCalls, err := GetToolCallsBySession(sessionID)
	if err != nil {
		toolCalls = nil // proceed without tool details; blocks get slim version
	}
	// Build lookup: toolID → ToolCallRecord for quick enrichment
	toolCallMap := make(map[string]*ToolCallRecord, len(toolCalls))
	for i := range toolCalls {
		toolCallMap[toolCalls[i].ToolID] = &toolCalls[i]
	}

	var sb strings.Builder
	for _, msg := range messages {
		if msg.Role != roleUser && msg.Role != roleAssistant {
			continue
		}
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
			continue
		}

		// Collect all non-skipped block outputs for this message
		msgParts := extractMessageParts(content.Blocks, toolCallMap)
		if len(msgParts) == 0 {
			continue
		}
		sb.WriteString(msg.Role)
		sb.WriteString(": ")
		for i, part := range msgParts {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(part)
		}
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// extractMessageParts collects non-skipped block outputs from content blocks.
func extractMessageParts(blocks []model.ContentBlock, toolCallMap map[string]*ToolCallRecord) []string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case contentKeyText:
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case eventTypeToolUse:
			tcJSON := FormatToolUseBlock(b, toolCallMap)
			if tcJSON != "" {
				parts = append(parts, tcJSON)
			}
			// thinking, warning, error: skipped
		}
	}
	return parts
}

// forkToolOutputMaxLen is the maximum number of runes kept from a tool_use
// output field when building fork context.  Long outputs (file reads, command
// results, etc.) are truncated to avoid blowing up the context window of the
// forked session.
const forkToolOutputMaxLen = 500

// truncateRunes returns s truncated to maxRunes with a "...(truncated)" suffix
// when the string exceeds the limit.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "...(truncated)"
}

// FormatToolUseBlock renders a tool_use ContentBlock as structured JSON wrapped
// in <tool_use> tags. Enriches the block with input/output from the detail table
// when available. Applies truncation to keep the output reasonable.
func FormatToolUseBlock(b model.ContentBlock, toolCallMap map[string]*ToolCallRecord) string {
	// Base fields from the slim content block
	obj := map[string]any{
		"name": b.Name,
		"id":   b.ID,
	}
	if b.Status != "" {
		obj["status"] = b.Status
	}
	if b.Done {
		obj["done"] = true
	}
	if b.DurationMs > 0 {
		obj["duration_ms"] = b.DurationMs
	}
	if b.Summary != "" {
		obj["summary"] = b.Summary
	}

	// Enrich with input/output from chat_tool_calls detail table
	tc, found := toolCallMap[b.ID]
	if found {
		inputStr := string(tc.Input)
		obj["input"] = inputStr
		obj["output"] = truncateRunes(tc.Output, forkToolOutputMaxLen)
	} else if b.Input != nil {
		// Fallback: use input from content block (interactive tools keep input inline)
		inputJSON, _ := json.Marshal(b.Input)
		obj["input"] = string(inputJSON)
		if b.Output != "" {
			obj["output"] = truncateRunes(b.Output, forkToolOutputMaxLen)
		}
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return "<tool_use>" + string(jsonBytes) + "</tool_use>"
}

type streamRunResultShared struct {
	cancelReason string
	err          string
	empty        bool
}

// executeStreamRunShared runs one AI backend execution.
// Uses the correct SessionExecutor API: NewSessionExecutor(ctx, RunConfig) -> RunWithChannel(eventCh) -> Finalize(result, eventCh)
func executeStreamRunShared(ctx context.Context, cfg LaunchConfig) streamRunResultShared {
	fileDir := resolveFileDir(cfg.ProjectPath)
	chatReq := BuildChatRequest(cfg.Message, cfg.SessionID, cfg.ProjectPath, cfg.BackendName, cfg.AgentID, "", "", "", "", fileDir, false)

	// The one AI-turn implementation — shared with the /api/ai/chat handler and
	// the scheduler so a fix can no longer land in only one copy.
	res := runTurn(TurnSpec{
		Ctx:             ctx,
		Mode:            ModeInteractive,
		ProjectPath:     cfg.ProjectPath,
		BackendName:     cfg.BackendName,
		SessionID:       cfg.SessionID,
		AgentID:         cfg.AgentID,
		ChatReq:         chatReq,
		FileDir:         fileDir,
		QueueID:         cfg.QueueID,
		DrainOnFinalize: true,
		LocalizeError:   serviceLocalizeError,
	})
	return streamRunResultShared{
		cancelReason: res.CancelReason,
		err:          res.Err,
		empty:        res.Empty,
	}
}
