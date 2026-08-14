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
			if text := n.Update.UserMessageChunk.Content.Text; text != nil && text.Text != "" {
				ai.AccumulateBlock(&blocks, ai.StreamEvent{Type: strContent, Content: text.Text})
			}
			continue
		}

		ch := make(chan ai.StreamEvent, 64)
		ai.MapACPSessionUpdateForTest(n.Update, ch)
		close(ch)
		for event := range ch {
			switch event.Type {
			case strContent, "thinking", "thinking_done", strToolUse, "tool_result", "warning", strError:
				ai.AccumulateBlock(&blocks, event)
			}
		}
	}
	flush()
	return messages
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
//nolint:gocognit // orchestration 顺序性高，拆分反而难读
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

	// 复用连接（全新连接会在此触发 LoadSession；已存活连接提前返回）
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
	if err := conn.SyncLoadSession(r.Context(), projectPath, acpSID); err != nil {
		if ai.IsACPResourceNotFound(err) {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ACPSessionNotFound")
			return
		}
		slog.Error("handler: SyncLoadSession failed", "session_id", req.SessionID, "acp_sid", acpSID, "error", err)
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

	// 加载本地已有消息（有序）与 external_message_id，用于增量去重。
	rows, err := service.ReadDB().Query(
		"SELECT external_message_id, role, content FROM chat_history WHERE session_id = ? ORDER BY id ASC",
		req.SessionID,
	)
	if err != nil {
		slog.Error("handler: failed to query existing chat_history", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	existingExtIDs := map[string]struct{}{}
	var localMessages []replayMessage
	for rows.Next() {
		var extID, role, content string
		if err := rows.Scan(&extID, &role, &content); err != nil {
			slog.Warn("handler: failed scanning chat_history row", "error", err)
			continue
		}
		if extID != "" {
			existingExtIDs[extID] = struct{}{}
		}
		localMessages = append(localMessages, replayMessage{role: role, content: content, extMsgID: extID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Error("handler: failed iterating existing chat_history", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 仅追加外部历史中本地尚未拥有的消息。按"本地历史是外部历史前缀"的续接方式匹配：
	// 即使消息没有稳定 external_message_id（如实时聊天的消息），也能正确去重。
	toPersist := computeSyncAdds(messages, localMessages, existingExtIDs)
	added := persistReplayMessages(req.SessionID, projectPath, agent.Backend, toPersist)

	slog.Info("handler: acp-sync completed",
		"session_id", req.SessionID, "agent", req.AgentID, "acp_sid", acpSID,
		"replayed", len(messages), "added", added)

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

// computeSyncAdds 计算应插入的增量消息。按"本地历史是外部历史的前缀"进行续接匹配：
// 从本地第一条消息起，逐条与回放对齐（role + 纯文本近似），本地匹配耗尽后，把回放
// 剩余的消息作为新增返回。即使本地消息没有稳定的 external_message_id（如实时聊天
// 生成的消息），也能避免被重复拉取。
func computeSyncAdds(replay []replayMessage, local []replayMessage, existingExtIDs map[string]struct{}) []replayMessage {
	k := 0
	for k < len(local) && k < len(replay) {
		if !sameMessage(local[k], replay[k]) {
			break
		}
		k++
	}
	var adds []replayMessage
	for _, m := range replay[k:] {
		// messageId 去重作为额外保险（前缀匹配错位时避免重复）
		if m.extMsgID != "" {
			if _, dup := existingExtIDs[m.extMsgID]; dup {
				continue
			}
		}
		adds = append(adds, m)
	}
	return adds
}

// sameMessage 判断两条消息是否代表同一条逻辑消息：角色相同，且纯文本相等或互为子串
// （外部回放可能在消息文本中夹带系统指令等额外上下文）。
func sameMessage(a, b replayMessage) bool {
	if a.role != b.role {
		return false
	}
	pa := strings.TrimSpace(service.ExtractPlainText(a.content))
	pb := strings.TrimSpace(service.ExtractPlainText(b.content))
	if pa == "" || pb == "" {
		return pa == pb
	}
	return pa == pb || strings.Contains(pa, pb) || strings.Contains(pb, pa)
}
