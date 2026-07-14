package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
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

// FindSessionsByPrefix finds non-deleted chat sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := dbRead.Query(
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE LOWER(id) LIKE LOWER(?) AND deleted = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT 10`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows)
}

// ListRecentSessions returns the most recently updated non-deleted chat sessions.
func ListRecentSessions(limit int) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := dbRead.Query(
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE deleted = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows)
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

	rows, err := dbRead.Query(
		fmt.Sprintf(
			`SELECT id, title, project_path, backend, agent_id, model
			 FROM chat_sessions
			 WHERE id IN (%s) AND deleted = 0`,
			sb.String(),
		),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows)
}

func scanDingTalkSessionInfos(rows *sql.Rows) ([]DingTalkSessionInfo, error) {
	var results []DingTalkSessionInfo
	for rows.Next() {
		var info DingTalkSessionInfo
		if err := rows.Scan(&info.ID, &info.Title, &info.ProjectPath, &info.Backend, &info.AgentID, &info.Model); err != nil {
			slog.Warn("scanDingTalkSessionInfos: skipping row", "error", err)
			continue
		}
		results = append(results, info)
	}
	return results, nil
}

// SendMessageToSessionFromDingTalk sends a message to a non-running session from DingTalk.
func SendMessageToSessionFromDingTalk(sessionID, message string) error {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if _, err := AddChatMessage(info.ProjectPath, info.Backend, sessionID, "user", message, nil, false, info.Title); err != nil {
		return fmt.Errorf("persist message: %w", err)
	}

	if !TrySetSessionRunning(sessionID) {
		EnqueueMessage(sessionID, model.QueuedMessage{
			Text:      message,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		return nil
	}

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
	streamCh := RegisterSessionStream(sessionID)
	ctx, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel(sessionID, cancel)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("session goroutine panicked",
					slog.String("session", sessionID),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				SetSessionRunning(sessionID, false, true)
				UnregisterSessionCancel(sessionID)
				cancel()
				SendSessionEvent(sessionID, ai.StreamEvent{Type: "error", Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
				UnregisterSessionStream(sessionID)
				errMsg := "AI internal error, please retry"
				errContent, _ := json.Marshal(map[string]any{"blocks": []any{map[string]string{"type": "error", "text": errMsg, "reason": ai.ReasonPanic}}})
				FinalizeStreamingMessage(cfg.ProjectPath, cfg.BackendName, sessionID, string(errContent))
			}
		}()

		defer SetSessionRunning(sessionID, false)
		defer UnregisterSessionStream(sessionID)
		defer cancel()
		defer UnregisterSessionCancel(sessionID)

		defer func() {
			effectiveTransport := "cli"
			if t := GetSessionTransport(sessionID); t != "" {
				effectiveTransport = t
			} else if agent, ok := model.Agents[cfg.AgentID]; ok && agent.Transport != "" {
				effectiveTransport = agent.Transport
			}
			if effectiveTransport == "acp-stdio" {
				slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", cfg.AgentID)
				ai.GetACPConnManager().MarkIdle(sessionID)
			}
		}()

		markDoneAndSendFinal := func(event ai.StreamEvent) {
			SetSessionRunning(sessionID, false, true)
			ai.SendFinalStreamEvent(streamCh, event)
		}

		result := executeStreamRunShared(ctx, streamCh, cfg)

		for {
			if result.cancelReason == "user" {
				ClearQueue(sessionID)
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
				markDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
				return
			}

			qMsg, ok := DequeueMessage(sessionID)
			if !ok {
				time.Sleep(50 * time.Millisecond)
				qMsg, ok = DequeueMessage(sessionID)
			}
			if !ok {
				markDoneAndSendFinal(ai.StreamEvent{Type: "done"})
				return
			}

			slog.Info("draining queued message", slog.String("session", sessionID), slog.String("text", qMsg.Text))

			drainMsgID, err := AddChatMessage(cfg.ProjectPath, cfg.BackendName, sessionID, "user", qMsg.Text, qMsg.Files, false, "")
			if err != nil {
				slog.Error("failed to persist drain message", slog.String("session", sessionID), slog.String("error", err.Error()))
			}

			remainingQueue := GetQueue(sessionID)
			ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{
				Type: "queue_drain",
				QueueEvent: &ai.QueueEventData{
					SessionID: sessionID,
					Text:      qMsg.Text,
					MessageID: drainMsgID,
					FilePaths: qMsg.FilePaths,
					Files:     qMsg.Files,
					Queue:     remainingQueue,
				},
			})

			cfg.Message = qMsg.Text
			result = executeStreamRunShared(ctx, streamCh, cfg)
		}
	}()
}

// BuildChatRequest constructs an ai.ChatRequest from the given parameters.
// This is the service-layer equivalent of handler.buildChatRequest, without HTTP-specific i18n.
func BuildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string, hasAttachments bool) ai.ChatRequest {
	systemPrompt := ""
	agentModel := ""
	agentCommand := ""
	effectiveThinkingEffort := thinkingEffortOverride
	effectiveMode := modeOverride

	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}
	if agent, ok := model.Agents[agentID]; ok {
		systemPrompt = agent.SystemPrompt
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
		if effectiveThinkingEffort == "" && agent.EffectiveThinkingEffort() != "" {
			effectiveThinkingEffort = agent.EffectiveThinkingEffort()
		}
		if effectiveMode == "" && agent.EffectiveModeID() != "" {
			effectiveMode = agent.EffectiveModeID()
		}
	}

	effectiveSessionID := sessionID
	resume := SessionHasAssistant(sessionID)

	isACP := false
	if transportOverride != "" {
		isACP = transportOverride == "acp-stdio"
	} else if agent, ok := model.Agents[agentID]; ok {
		isACP = agent.Transport == "acp-stdio"
	}

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

	var forkContext string
	if resume && resolvedExtID == "" {
		forkContext = BuildForkContext(sessionID)
		if forkContext != "" && isACP {
			resume = false
		}
	}

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
		AssistantMessageCount: GetAssistantMessageCount(sessionID),
		ForkContext:           forkContext,
	}
}

// BuildForkContext reads the chat history from DB and formats it as a text block
// that can be prepended to the user's prompt for fork sessions.
func BuildForkContext(sessionID string) string {
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil || len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
			continue
		}
		for _, b := range content.Blocks {
			if b.Type == "text" && b.Text != "" {
				sb.WriteString(msg.Role)
				sb.WriteString(": ")
				sb.WriteString(b.Text)
				sb.WriteString("\n\n")
			}
		}
	}
	return sb.String()
}

type streamRunResultShared struct {
	cancelReason string
	err          string
	empty        bool
}

// executeStreamRunShared runs one AI backend execution.
// Uses the correct SessionExecutor API: NewSessionExecutor(ctx, RunConfig) -> RunWithChannel(eventCh) -> Finalize(result, eventCh)
func executeStreamRunShared(ctx context.Context, streamCh chan ai.StreamEvent, cfg LaunchConfig) streamRunResultShared {
	sessionTransport := GetSessionTransport(cfg.SessionID)

	backend, err := ai.NewBackendForAgentWithTransport(cfg.BackendName, cfg.AgentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", cfg.BackendName), slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("create backend: %v", err)
		ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg})
		AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", errMsg, nil, false, "")
		return streamRunResultShared{err: errMsg}
	}

	if sessionTransport == "acp-stdio" {
		if _, ok := backend.(*ai.ACPBackend); !ok {
			_ = UpdateSessionTransport(cfg.SessionID, "")
		}
	}

	chatReq := BuildChatRequest(cfg.Message, cfg.SessionID, cfg.ProjectPath, cfg.BackendName, cfg.AgentID, "", "", "", "", "", false)

	eventCh, err := backend.ExecuteStream(ctx, chatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("start stream: %v", err)
		ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg})
		AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", errMsg, nil, false, "")
		return streamRunResultShared{err: errMsg}
	}

	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	streamingMsgID, _ := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", string(emptyContent), nil, true, "")

	execCfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        cfg.ProjectPath,
		BackendName:        cfg.BackendName,
		SessionID:          cfg.SessionID,
		AgentID:            cfg.AgentID,
		ChatRequest:        chatReq,
		StreamingMessageID: streamingMsgID,
		StreamCh:           streamCh,
		LocalizeError:      nil,
	}
	executor := NewSessionExecutor(ctx, execCfg)
	runResult := executor.RunWithChannel(eventCh)
	runResult = executor.Finalize(runResult, eventCh)

	ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "metadata", Meta: runResult.Metadata})

	result := streamRunResultShared{}
	if runResult.CancelReason == "user" {
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
