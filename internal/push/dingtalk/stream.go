package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// startStream establishes the DingTalk Stream long-polling connection.
// This is non-blocking — the SDK manages reconnection internally.
func (m *Manager) startStream(ctx context.Context) error {
	cred := client.NewAppCredentialConfig(m.cfg.AppKey, m.cfg.AppSecret)
	m.streamCli = client.NewStreamClient(client.WithAppCredential(cred))

	// Register chatbot message handler for auto-subscribe
	m.streamCli.RegisterChatBotCallbackRouter(m.onChatBotMessage)

	if err := m.streamCli.Start(ctx); err != nil {
		return err
	}

	slog.Info("dingtalk: stream connected")
	return nil
}

// onChatBotMessage handles incoming messages from DingTalk users.
// When a user sends a message to the bot, we auto-subscribe them.
// If the message matches the "@{shortID} message" format, it is
// forwarded to the corresponding session.
func (m *Manager) onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	slog.Info("dingtalk: received message",
		"sender_id", data.SenderId,
		"sender_nick", data.SenderNick,
		"conversation_id", data.ConversationId,
		"conversation_type", data.ConversationType,
		"text", data.Text.Content,
	)

	if data.ConversationType != "1" {
		slog.Debug("dingtalk: ignoring non-single-chat message", "type", data.ConversationType)
		return []byte(""), nil
	}

	staffID := data.SenderStaffId
	if staffID == "" {
		slog.Warn("dingtalk: senderStaffId is empty, falling back to senderId", "sender_id", data.SenderId)
		staffID = data.SenderId
	}

	// Always auto-subscribe regardless of command outcomes
	if db != nil {
		if err := db.UpsertSubscriber(staffID, data.ConversationId, data.SenderNick, "stream"); err != nil {
			slog.Warn("dingtalk: auto-subscribe failed", "error", err, "staff_id", staffID)
		} else {
			slog.Info("dingtalk: auto-subscribed user", "user_id", staffID, "nick", data.SenderNick)
		}
	}

	// Try to parse as session command: "@{8hex} message"
	if shortID, msg, ok := parseSessionCommand(data.Text.Content); ok {
		m.handleSessionCommand(ctx, data, shortID, msg)
		return []byte(""), nil
	}

	// No @ prefix — list recent sessions for the user to pick from
	m.handleSessionList(ctx, data)
	return []byte(""), nil
}

// handleSessionCommand processes a "@{shortID} message" command from DingTalk.
func (m *Manager) handleSessionCommand(ctx context.Context, data *chatbot.BotCallbackDataModel, shortID, msg string) {
	replier := chatbot.NewChatbotReplier()

	sessionID, sessionTitle, err := resolveShortSessionID(shortID)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", shortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	sessionLabel := formatSessionLabel(sessionID, sessionTitle)

	if sessionMessenger.IsSessionRunning(sessionID) {
		if err := sessionMessenger.EnqueueMessage(sessionID, msg); err != nil {
			slog.Warn("dingtalk: enqueue message failed", "error", err, "session_id", sessionID)
			_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息入队失败: "+err.Error()))
			return
		}
		// Verify the session still has a consumer. If it ended between IsSessionRunning
		// and EnqueueMessage, the queued message would be orphaned — clear it and
		// resend via the resume path to avoid duplicate delivery.
		if !sessionMessenger.IsSessionRunning(sessionID) {
			slog.Info("dingtalk: session ended after enqueue, falling back to send", "session_id", sessionID)
			sessionMessenger.ClearQueue(sessionID)
			if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
				slog.Warn("dingtalk: fallback send failed", "error", err, "session_id", sessionID)
				_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
				return
			}
			_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
				[]byte("消息已发送"), []byte(fmt.Sprintf("### 消息已发送\n已发送到会话 **%s**，AI 正在处理", escapeMarkdown(sessionLabel))))
			return
		}
		slog.Info("dingtalk: message enqueued to running session", "session_id", sessionID, "msg", msg)
		_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
			[]byte("消息已发送"), []byte(fmt.Sprintf("### 消息已发送\n已发送到运行中的会话 **%s**", escapeMarkdown(sessionLabel))))
		return
	}

	if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
		slog.Warn("dingtalk: send message to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
		return
	}
	slog.Info("dingtalk: message sent to session", "session_id", sessionID, "msg", msg)
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte("消息已发送"), []byte(fmt.Sprintf("### 消息已发送\n已发送到会话 **%s**，AI 正在处理", escapeMarkdown(sessionLabel))))
}

// handleSessionList lists recent sessions so the user can pick one to send a message to.
func (m *Manager) handleSessionList(ctx context.Context, data *chatbot.BotCallbackDataModel) {
	replier := chatbot.NewChatbotReplier()

	if sessionMessenger == nil {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("已订阅 ClawBench 通知。暂无可用会话。"))
		return
	}

	sessions, err := sessionMessenger.ListRecentSessions(10)
	if err != nil {
		slog.Warn("dingtalk: list sessions failed", "error", err)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("已订阅 ClawBench 通知。获取会话列表失败。"))
		return
	}

	if len(sessions) == 0 {
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("已订阅 ClawBench 通知。暂无会话。"))
		return
	}

	var sb strings.Builder
	sb.WriteString("### 会话列表\n发送 **@会话ID <消息>** 向会话发送消息：\n\n")
	for _, s := range sessions {
		id := shortSessionID(s.ID)
		title := s.Title
		if title == "" {
			title = "（无标题）"
		}
		running := ""
		if sessionMessenger.IsSessionRunning(s.ID) {
			running = " 🟢"
		}
		sb.WriteString(fmt.Sprintf("- **@%s** %s%s\n", id, escapeMarkdown(title), running))
	}
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook, []byte("会话列表"), []byte(sb.String()))
}

// formatSessionLabel returns a human-readable label for a session.
func formatSessionLabel(sessionID, sessionTitle string) string {
	if sessionTitle != "" {
		return sessionTitle
	}
	return "会话 " + shortSessionID(sessionID)
}
