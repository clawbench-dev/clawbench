package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

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

	if !TrySetSessionRunning(sessionID) {
		// Session already running — enqueue the message (not yet persisted to DB)
		EnqueueMessage(sessionID, model.QueuedMessage{
			Text:      message,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		// Emit user_message for cross-device sync (messageId=0 because not yet persisted)
		ws.EmitToSession(sessionID, ai.StreamEvent{
			Type: "user_message",
			UserMessage: &ai.UserMessageData{
				MessageID: 0,
				Content:   message,
			},
		})
		return nil
	}

	// Session not running — persist user message and launch execution
	msgID, err := AddChatMessage(info.ProjectPath, info.Backend, sessionID, roleUser, message, nil, false, info.Title)
	if err != nil {
		SetSessionRunning(sessionID, false) // rollback running state
		return fmt.Errorf("persist message: %w", err)
	}
	// Emit user_message for cross-device sync
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID: msgID,
			Content:   message,
		},
	})

	LaunchSessionExecution(LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     message,
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
}

// LaunchSessionExecution starts the AI execution goroutine for a session.
// The caller must have already persisted the user message and called TrySetSessionRunning.
func LaunchSessionExecution(cfg LaunchConfig) {
	sessionID := cfg.SessionID
	ctx, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel(sessionID, cancel)

	go func() {
		defer handleSessionPanic(cfg, sessionID, cancel)

		defer SetSessionRunning(sessionID, false)
		defer cancel()
		defer UnregisterSessionCancel(sessionID)
		defer handleACPCleanup(sessionID, cfg.AgentID)

		markDoneAndSendFinal := func(event ai.StreamEvent) {
			SetSessionRunning(sessionID, false, true) // skip event — we emit directly
			emitDrainEvent(sessionID, event)
		}

		result := executeStreamRunShared(ctx, cfg)
		RunDrainLoop(DrainConfig{
			SessionID:   sessionID,
			ProjectPath: cfg.ProjectPath,
			BackendName: cfg.BackendName,
			PersistUser: func(text string, files []model.FileEntry) (int64, error) {
				msgID, err := AddChatMessage(cfg.ProjectPath, cfg.BackendName, sessionID, roleUser, text, files, false, "")
				if err != nil {
					slog.Error("failed to persist drain message", slog.String("session", sessionID), slog.String("error", err.Error()))
				}
				return msgID, err
			},
			ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
				cfg.Message = qMsg.Text
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

// handleSessionPanic recovers from panics in the session goroutine.
func handleSessionPanic(cfg LaunchConfig, sessionID string, cancel context.CancelFunc) {
	if r := recover(); r != nil {
		slog.Error(
			"session goroutine panicked",
			slog.String("session", sessionID),
			slog.Any("panic", r),
			slog.String("stack", string(debug.Stack())),
		)
		SetSessionRunning(sessionID, false, true)
		UnregisterSessionCancel(sessionID)
		cancel()
		emitDrainEvent(sessionID, ai.StreamEvent{Type: eventTypeError, Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
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

	return ai.ChatRequest{
		Prompt:                prompt,
		SessionID:             effectiveSessionID,
		WorkDir:               fileDir,
		SystemPrompt:          systemPrompt,
		Model:                 agentCfg.agentModel,
		Command:               agentCfg.agentCommand,
		AgentID:               agentID,
		ThinkingEffort:        agentCfg.effectiveThinkingEffort,
		Mode:                  agentCfg.effectiveMode,
		Resume:                resume,
		HasAttachments:        hasAttachments,
		AssistantMessageCount: GetAssistantMessageCount(sessionID),
		ForkContext:           forkContext,
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
	messages, err := GetMessagesBySessionID(sessionID)
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
		obj["summary"] = truncateString(b.Summary, 500)
	}

	// Enrich with input/output from chat_tool_calls detail table
	tc, found := toolCallMap[b.ID]
	if found {
		inputStr := string(tc.Input)
		obj["input"] = truncateString(inputStr, 500)
		obj["output"] = truncateString(tc.Output, 1000)
	} else if b.Input != nil {
		// Fallback: use input from content block (interactive tools keep input inline)
		inputJSON, _ := json.Marshal(b.Input)
		obj["input"] = truncateString(string(inputJSON), 500)
		if b.Output != "" {
			obj["output"] = truncateString(b.Output, 1000)
		}
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return "<tool_use>" + string(jsonBytes) + "</tool_use>"
}

// truncateString truncates s to maxLen bytes, appending "...(truncated)" if exceeded.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

type streamRunResultShared struct {
	cancelReason string
	err          string
	empty        bool
}

// executeStreamRunShared runs one AI backend execution.
// Uses the correct SessionExecutor API: NewSessionExecutor(ctx, RunConfig) -> RunWithChannel(eventCh) -> Finalize(result, eventCh)
func executeStreamRunShared(ctx context.Context, cfg LaunchConfig) streamRunResultShared {
	sessionTransport := GetSessionTransport(cfg.SessionID)

	backend, err := ai.NewBackendForAgentWithTransport(cfg.BackendName, cfg.AgentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", cfg.BackendName), slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("create backend: %v", err)
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{Type: eventTypeError, Error: errMsg})
		errContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{map[string]string{contentKeyType: blockTypeWarning, contentKeyText: errMsg, contentKeyReason: ai.ReasonBackendExit}}})
		if _, saveErr := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, roleAssistant, string(errContent), nil, false, ""); saveErr != nil {
			slog.Error("failed to save error message", slog.String("err", saveErr.Error()))
		}
		return streamRunResultShared{err: errMsg}
	}

	if sessionTransport == transportACPStdio {
		if _, ok := backend.(*ai.ACPBackend); !ok {
			_ = UpdateSessionTransport(cfg.SessionID, "")
		}
	}

	// Resolve fileDir to absolute path, matching handler/chat.go logic.
	// Without this, ACP ResumeSession/NewSession receives cwd="" and fails.
	fileDir := cfg.ProjectPath
	if absDir, absErr := filepath.Abs(cfg.ProjectPath); absErr == nil {
		fileDir = absDir
	}

	chatReq := BuildChatRequest(cfg.Message, cfg.SessionID, cfg.ProjectPath, cfg.BackendName, cfg.AgentID, "", "", "", "", fileDir, false)

	eventCh, err := backend.ExecuteStream(ctx, chatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("start stream: %v", err)
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{Type: eventTypeError, Error: errMsg})
		errContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{map[string]string{contentKeyType: blockTypeWarning, contentKeyText: errMsg, contentKeyReason: ai.ReasonBackendExit}}})
		if _, saveErr := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, roleAssistant, string(errContent), nil, false, ""); saveErr != nil {
			slog.Error("failed to save error message", slog.String("err", saveErr.Error()))
		}
		return streamRunResultShared{err: errMsg}
	}

	emptyContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{}})
	streamingMsgID, err := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, roleAssistant, string(emptyContent), nil, true, "")
	if err != nil {
		slog.Error("failed to create streaming message", slog.String("session", cfg.SessionID), slog.String("err", err.Error()))
	}

	execCfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        cfg.ProjectPath,
		BackendName:        cfg.BackendName,
		SessionID:          cfg.SessionID,
		AgentID:            cfg.AgentID,
		ChatRequest:        chatReq,
		StreamingMessageID: streamingMsgID,
		LocalizeError:      nil,
	}
	executor := NewSessionExecutor(ctx, execCfg)
	runResult := executor.RunWithChannel(eventCh)
	runResult = executor.Finalize(runResult, eventCh)

	emitDrainEvent(cfg.SessionID, ai.StreamEvent{Type: contentKeyMetadata, Meta: runResult.Metadata})

	result := streamRunResultShared{}
	if runResult.CancelReason == cancelReasonUser {
		result.cancelReason = runResult.CancelReason
	} else if ctx.Err() == context.Canceled {
		result.cancelReason = "cancel"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.err = "AI response timed out (30 min)"
	} else if runResult.Empty {
		result.empty = true
	}

	return result
}
