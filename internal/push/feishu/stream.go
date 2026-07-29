package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/push/common"

	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// startWebSocket establishes the Feishu WebSocket connection for receiving events.
// This is non-blocking — the SDK manages reconnection internally.
func (m *Manager) startWebSocket(ctx context.Context) error {
	dispatcher := larkdispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(m.onMessageReceive)

	m.wsClient = larkws.NewClient(m.cfg.AppID, m.cfg.AppSecret,
		larkws.WithEventHandler(dispatcher),
		larkws.WithOnReady(func() {
			slog.Info("feishu: websocket ready, receiving events")
		}),
		larkws.WithOnError(func(err error) {
			slog.Warn("feishu: websocket error", "error", err)
		}),
		larkws.WithOnDisconnected(func() {
			slog.Warn("feishu: websocket disconnected")
		}),
		larkws.WithOnReconnected(func() {
			slog.Info("feishu: websocket reconnected")
		}),
	)

	go func() {
		if err := m.wsClient.Start(ctx); err != nil {
			slog.Warn("feishu: websocket start error", "error", err)
		}
	}()

	slog.Info("feishu: websocket connecting")
	return nil
}

// onMessageReceive handles incoming messages from Feishu users.
// When a user sends a message to the bot, we auto-subscribe them.
// If the message matches the "@{shortID} message" format, it is
// forwarded to the corresponding session.
func (m *Manager) onMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event.Event == nil {
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	if msg == nil || sender == nil {
		return nil
	}

	// Only handle p2p (single) chat
	chatType := ptrStr(msg.ChatType)
	if chatType != "p2p" {
		slog.Debug("feishu: ignoring non-p2p message", "chat_type", chatType)
		return nil
	}

	// Get sender open_id
	openID := ""
	if sender.SenderId != nil {
		openID = ptrStr(sender.SenderId.OpenId)
	}
	if openID == "" {
		slog.Warn("feishu: sender open_id is empty")
		return nil
	}

	// Get sender type as name fallback
	senderName := ptrStr(sender.SenderType)

	// Get chat_id
	chatID := ptrStr(msg.ChatId)

	// Get message content
	msgContent := ptrStr(msg.Content)

	// Extract text content from the message JSON
	text := extractTextContent(msgContent, ptrStr(msg.MessageType))

	slog.Info("feishu: received message",
		"open_id", openID,
		"chat_id", chatID,
		"chat_type", chatType,
		"text", text,
	)

	// Always auto-subscribe regardless of command outcomes
	if db != nil {
		if err := db.UpsertSubscriber(openID, chatID, senderName, "stream"); err != nil {
			slog.Warn("feishu: auto-subscribe failed", "error", err, "open_id", openID)
		} else {
			slog.Info("feishu: auto-subscribed user", "user_id", openID)
		}
	}

	// Try to parse as session command: "@{8hex} message"
	if shortID, msgText, ok := common.ParseSessionCommand(text); ok {
		m.handleSessionCommand(ctx, openID, shortID, msgText)
		return nil
	}

	// No @ prefix — list recent sessions for the user to pick from
	m.handleSessionList(ctx, openID)
	return nil
}

// extractTextContent extracts plain text from a Feishu message content JSON.
// Handles "text" messages (plain text) and "post" messages (rich text).
func extractTextContent(content, msgType string) string {
	if content == "" {
		return ""
	}

	// Handle text messages
	if msgType == "text" {
		var textMsg struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textMsg); err == nil {
			return textMsg.Text
		}
	}

	// Handle post (rich text) messages — extract all text elements
	if msgType == "post" {
		var postMsg struct {
			ZhCn struct {
				Title   string           `json:"title"`
				Content [][]postElement  `json:"content"`
			} `json:"zh_cn"`
		}
		if err := json.Unmarshal([]byte(content), &postMsg); err == nil {
			var sb strings.Builder
			if postMsg.ZhCn.Title != "" {
				sb.WriteString(postMsg.ZhCn.Title)
				sb.WriteString("\n")
			}
			for i, row := range postMsg.ZhCn.Content {
				if i > 0 {
					sb.WriteString("\n")
				}
				for _, elem := range row {
					if elem.Tag == "text" && elem.Text != "" {
						sb.WriteString(elem.Text)
					}
				}
			}
			return sb.String()
		}
	}

	// For other message types, return empty — we only handle text/post commands
	return ""
}

// postElement represents a content element in a Feishu post message.
type postElement struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

// handleSessionCommand processes a "@{shortID} message" command from Feishu.
func (m *Manager) handleSessionCommand(ctx context.Context, openID, shortID, msg string) {
	sessionID, sessionTitle, err := common.ResolveShortSessionID(sessionMessenger, shortID)
	if err != nil {
		slog.Warn("feishu: session command resolve failed", "error", err, "short_id", shortID)
		_ = m.SendPostMessage(ctx, openID, "错误", err.Error())
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	if sessionMessenger.IsSessionRunning(sessionID) {
		if err := sessionMessenger.EnqueueMessage(sessionID, msg); err != nil {
			slog.Warn("feishu: enqueue message failed", "error", err, "session_id", sessionID)
			_ = m.SendPostMessage(ctx, openID, "错误", "消息入队失败: "+err.Error())
			return
		}
		// Verify the session still has a consumer
		if !sessionMessenger.IsSessionRunning(sessionID) {
			slog.Info("feishu: session ended after enqueue, falling back to send", "session_id", sessionID)
			sessionMessenger.ClearQueue(sessionID)
			if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
				slog.Warn("feishu: fallback send failed", "error", err, "session_id", sessionID)
				_ = m.SendPostMessage(ctx, openID, "错误", "发送消息失败: "+err.Error())
				return
			}
			_ = m.SendPostMessage(ctx, openID, "消息已发送",
				fmt.Sprintf("消息已发送到会话 %s，AI 正在处理", sessionLabel))
			return
		}
		slog.Info("feishu: message enqueued to running session", "session_id", sessionID, "msg", msg)
		_ = m.SendPostMessage(ctx, openID, "消息已发送",
			fmt.Sprintf("消息已发送到运行中的会话 %s", sessionLabel))
		return
	}

	if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
		slog.Warn("feishu: send message to session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, "错误", "发送消息失败: "+err.Error())
		return
	}
	slog.Info("feishu: message sent to session", "session_id", sessionID, "msg", msg)
	_ = m.SendPostMessage(ctx, openID, "消息已发送",
		fmt.Sprintf("消息已发送到会话 %s，AI 正在处理", sessionLabel))
}

// handleSessionList lists recent sessions so the user can pick one to send a message to.
func (m *Manager) handleSessionList(ctx context.Context, openID string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, "已订阅", "已订阅 ClawBench 通知。暂无可用会话。")
		return
	}

	sessions, err := sessionMessenger.ListRecentSessions(10)
	if err != nil {
		slog.Warn("feishu: list sessions failed", "error", err)
		_ = m.SendPostMessage(ctx, openID, "已订阅", "已订阅 ClawBench 通知。获取会话列表失败。")
		return
	}

	if len(sessions) == 0 {
		_ = m.SendPostMessage(ctx, openID, "已订阅", "已订阅 ClawBench 通知。暂无会话。")
		return
	}

	var sb strings.Builder
	sb.WriteString("会话列表\n发送 @会话ID <消息> 向会话发送消息：\n\n")

	// Group sessions by project path
	type group struct {
		project string
		items   []common.SessionInfo
	}
	var groups []group
	groupIdx := map[string]int{}
	for _, s := range sessions {
		project := s.ProjectPath
		if project == "" {
			project = "（无项目）"
		}
		if idx, ok := groupIdx[project]; ok {
			groups[idx].items = append(groups[idx].items, s)
		} else {
			groupIdx[project] = len(groups)
			groups = append(groups, group{project: project, items: []common.SessionInfo{s}})
		}
	}

	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("%s\n", g.project))
		for _, s := range g.items {
			id := common.ShortSessionID(s.ID)
			title := s.Title
			if title == "" {
				title = "（无标题）"
			}
			running := ""
			if sessionMessenger.IsSessionRunning(s.ID) {
				running = " *"
			}
			sb.WriteString(fmt.Sprintf("- @%s %s%s\n", id, title, running))
		}
		sb.WriteString("\n")
	}

	_ = m.SendPostMessage(ctx, openID, "会话列表", sb.String())
}

// ptrStr safely dereferences a *string, returning "" for nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
