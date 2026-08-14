package handler

import (
	"encoding/json"
	"log/slog"

	"clawbench/internal/ai"
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
