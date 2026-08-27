package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// replayMessage 是一条从 LoadSession 回放重建的消息。
type replayMessage struct {
	role      string // strUser or strAssistant
	content   string // JSON: {"blocks":[...], "metadata":{...}}
	extMsgID  string // 外部 ACP messageId
	toolCalls []model.ContentBlock
}

// groupLoadSessionReplay 读取并清空 LoadSession 回放缓冲，按 role 边界分组
// 为消息，捕获每组首个外部 messageId。
//
//nolint:gocognit,gocyclo // 状态机按角色边界分组，顺序分支多但线性，拆分反而难读
func groupLoadSessionReplay(client *ai.ClawBenchACPClient) []replayMessage {
	var messages []replayMessage
	buf := client.GetAndClearLoadSessionBuf()

	var blocks []model.ContentBlock
	var currentRole string
	var currentMsgID string

	flush := func() {
		if len(blocks) == 0 || currentRole == "" {
			return
		}
		blocks = ai.MergeConsecutiveThinkingBlocks(blocks)
		var toolCalls []model.ContentBlock
		for _, b := range blocks {
			if b.Type == strToolUse && b.ID != "" {
				toolCalls = append(toolCalls, b)
			}
		}
		contentMap := map[string]any{strBlocks: blocks}
		if currentRole == strAssistant {
			contentMap["metadata"] = map[string]any{"transport": transportACP}
		}
		contentJSON, _ := json.Marshal(contentMap)
		messages = append(messages, replayMessage{
			role:      currentRole,
			content:   string(contentJSON),
			extMsgID:  currentMsgID,
			toolCalls: toolCalls,
		})
		blocks = nil
		currentMsgID = ""
	}

	for _, n := range buf {
		notifRole := strAssistant
		var notifMsgID string
		switch {
		case n.Update.UserMessageChunk != nil:
			notifRole = strUser
			if n.Update.UserMessageChunk.MessageId != nil {
				notifMsgID = *n.Update.UserMessageChunk.MessageId
			}
		case n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.MessageId != nil:
			notifMsgID = *n.Update.AgentMessageChunk.MessageId
		case n.Update.AgentThoughtChunk != nil && n.Update.AgentThoughtChunk.MessageId != nil:
			notifMsgID = *n.Update.AgentThoughtChunk.MessageId
		}

		if notifRole != currentRole && currentRole != "" {
			flush()
		}
		currentRole = notifRole
		if notifMsgID != "" && currentMsgID == "" {
			currentMsgID = notifMsgID
		}

		if n.Update.UserMessageChunk != nil {
			// Extract the user's text from the chunk. Most agents send a plain
			// text block; some (or historical data) wrap the text in a nested
			// JSON serialization (e.g. an ACP notification or content array).
			// ExtractPlainText unwraps every known shape, so a nested JSON
			// never leaks into storage as a literal JSON string.
			if text := n.Update.UserMessageChunk.Content.Text; text != nil {
				if cleaned := filterSystemPromptText(text.Text); cleaned != "" {
					if plain := service.ExtractPlainText(cleaned); plain != "" {
						ai.AccumulateBlock(&blocks, ai.StreamEvent{Type: strContent, Content: plain})
					}
				}
			}
			continue
		}

		ch := make(chan ai.StreamEvent, 64)
		ai.MapACPSessionUpdateForTest(n.Update, ch)
		close(ch)
		for event := range ch {
			switch event.Type {
			case strContent, "thinking", "thinking_done", strToolUse, "tool_result", "warning", strError:
				if event.Type == strContent {
					event.Content = filterSystemPromptText(event.Content)
				}
				ai.AccumulateBlock(&blocks, event)
			}
		}
	}
	flush()
	return messages
}

// filterSystemPromptText strips injected system-prompt/reminder content from a
// replayed text block. System prompts can be re-injected at ANY point in the
// history (not just the first message), so this runs on every text block.
// Returns "" when the block is purely system prompt (drop it).
func filterSystemPromptText(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	// Current CodeBuddy format: a standalone <system-reminder ...> block.
	if strings.HasPrefix(t, "<system-reminder") {
		return ""
	}
	// Legacy format: system instructions prepended to a user message as
	// "[System Instructions: ...]<whitespace><real user text>". Strip the block.
	if strings.HasPrefix(t, "[System Instructions") {
		if rest, ok := stripSystemInstructions(t); ok {
			return rest
		}
		return ""
	}
	return text
}

// stripSystemInstructions removes a leading "[System Instructions: ...]" block,
// finding the bracket that balances the opener (handling nested '['/']') and
// consuming the following whitespace, so the real user text after the block is
// kept. Returns (rest, true) when the block was found; (rest, false) when the
// block is unterminated and the whole message should be treated as system prompt.
func stripSystemInstructions(t string) (string, bool) {
	const prefix = "[System Instructions"
	// t is known to start with the prefix and be trimmed.
	depth := 1
	i := len(prefix)
	for i < len(t) {
		switch t[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				rest := strings.TrimSpace(t[i+1:])
				return rest, true
			}
		}
		i++
	}
	// Unterminated block: treat the whole message as system prompt.
	return "", false
}

// persistReplayMessages 批量插入回放消息及其 tool calls，并记录外部 messageId。
// 返回实际插入条数。
func persistReplayMessages(sessionID, projectPath, backend string, messages []replayMessage) int {
	inserted := 0
	for _, msg := range messages {
		res, err := service.WriteExec(
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, indexed, external_message_id) VALUES (?, ?, ?, ?, ?, 0, 0, ?)",
			projectPath, backend, sessionID, msg.role, msg.content, msg.extMsgID,
		)
		if err != nil {
			slog.Error("handler: failed to save LoadSession replay message", "error", err)
			continue
		}
		msgID, _ := res.LastInsertId()
		for i := range msg.toolCalls {
			tc := &msg.toolCalls[i]
			inputJSON, _ := json.Marshal(tc.Input)
			if err := service.UpsertToolCall(msgID, sessionID, tc.ID, tc.Name, inputJSON, tc.Output, tc.Status, tc.Summary, tc.Done, tc.DurationMs); err != nil {
				slog.Warn("handler: failed to persist LoadSession replay tool call",
					"session_id", sessionID, "tool_id", tc.ID, "error", err)
			}
		}
		inserted++
	}
	return inserted
}

// ServeACPSyncSession handles POST /api/ai/session/acp-sync — 复用当前会话的
// ACP 连接强制 LoadSession 回放，按 external messageId 增量合并外部新增消息到
// 当前会话，已存在消息保持不变。返回新增条数。
//
//nolint:gocyclo // orchestration 顺序性高，拆分反而难读
func ServeACPSyncSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
		return
	}

	var req struct {
		AgentID   string `json:"agentId"`
		SessionID string `json:"sessionId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AgentID == "" || req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

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
	spec := model.FindSpecByBackend(agent.Backend)
	if spec == nil || !spec.ACPLoadSession {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return
	}

	// 校验会话归属当前项目，并读取 external_session_id
	var sessProject, extID string
	err := service.ReadDB().QueryRowContext(
		r.Context(),
		"SELECT project_path, external_session_id FROM chat_sessions WHERE id = ?",
		req.SessionID,
	).Scan(&sessProject, &extID)
	if errors.Is(err, sql.ErrNoRows) {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}
	if sessProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// 解析 ACP 会话 ID：优先 external_session_id，其次活动连接 acpSID
	acpSID := extID
	if acpSID == "" {
		if conn := ai.GetACPConnManager().GetConn(req.SessionID); conn != nil {
			acpSID = conn.AcpSID()
		}
	}
	if acpSID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoAcpSession")
		return
	}

	// 强制用全新进程做同步：复用的已存活进程在 LoadSession 时返回内存中的旧会话
	// 状态（CodeBuddy 等），看不到外部新增消息。先关闭现有连接，让 getOrCreateConnForLoad
	// 重新 spawn 一个进程，其 LoadSession 会重新读取磁盘上的最新会话历史。
	ai.GetACPConnManager().CloseConn(req.SessionID)

	conn, err := getOrCreateConnForLoad(r.Context(), agent, req.SessionID, acpSID, projectPath)
	if err != nil {
		if ai.IsACPResourceNotFound(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 强制回放（已存活连接在此触发 LoadSession）
	if syncErr := conn.SyncLoadSession(r.Context(), projectPath, acpSID); syncErr != nil {
		if ai.IsACPResourceNotFound(syncErr) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		slog.Error("handler: SyncLoadSession failed", "session_id", req.SessionID, "acp_sid", acpSID, "error", syncErr)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 等待回放缓冲停止增长（回放完成），而不是固定 500ms —— 固定延时对长对话/慢
	// agent 不可靠，可能读到不完整的历史而漏掉新增消息。
	client := conn.GetClient()
	waitForReplaySettled(client)

	conn.ClearLoadSessionActive()

	var messages []replayMessage
	if client != nil {
		messages = groupLoadSessionReplay(client)
	}

	// 空回放守卫：外部会话没加载到任何消息时不覆盖，避免误删原会话。
	var oldCount int
	_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", req.SessionID).Scan(&oldCount)
	if len(messages) == 0 {
		slog.Info("handler: acp-sync skipped (empty replay)", "session_id", req.SessionID, "acp_sid", acpSID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": 0, "skipped": true})
		return
	}

	// 覆盖式同步：把当前会话的历史整体替换为加载到的完整外部历史。删除+插入在
	// 同一事务内，任何失败回滚，原会话保持完好。
	svcMsgs := make([]service.ReplayMessage, 0, len(messages))
	for _, m := range messages {
		svcMsgs = append(svcMsgs, service.ReplayMessage{Role: m.role, Content: m.content, ExtMsgID: m.extMsgID, ToolCalls: m.toolCalls})
	}
	replaced, err := service.ReplaceSessionHistory(req.SessionID, projectPath, agent.Backend, svcMsgs)
	if err != nil {
		slog.Error("handler: ReplaceSessionHistory failed", "session_id", req.SessionID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 新增条数 = 覆盖后消息数 - 覆盖前消息数（钳到非负，供前端 toast）。
	added := replaced - oldCount
	if added < 0 {
		added = 0
	}

	slog.Info("handler: acp-sync completed",
		"session_id", req.SessionID, "agent", req.AgentID, "acp_sid", acpSID,
		"replayed", len(messages), "old", oldCount, "replaced", replaced, "added", added)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"added": added,
	})
}

// waitForReplaySettled 阻塞直到 LoadSession 回放缓冲在安静窗口内不再增长，或达到
// 最大等待时间。比固定延时更可靠：长对话/慢 agent 需要更久才能把全部回放通知写入
// 缓冲，固定延时可能读到不完整历史而漏掉新增消息。
func waitForReplaySettled(client *ai.ClawBenchACPClient) {
	if client == nil {
		return
	}
	const quietWindow = 800 * time.Millisecond
	const maxWait = 15 * time.Second
	deadline := time.Now().Add(maxWait)
	quietStart := time.Now()
	lastLen := client.GetLoadSessionBufLen()
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		cur := client.GetLoadSessionBufLen()
		if cur != lastLen {
			lastLen = cur
			quietStart = time.Now()
			continue
		}
		if time.Since(quietStart) >= quietWindow {
			return
		}
	}
}
