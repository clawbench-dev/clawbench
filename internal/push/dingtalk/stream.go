package dingtalk

import (
	"context"
	"log/slog"

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

	// Not a session command — reply with help (includes @{8hex} syntax)
	replier := chatbot.NewChatbotReplier()
	replyText := []byte("已订阅 ClawBench 通知。发送 @{会话ID前8位} <消息> 向会话发送消息。")
	if err := replier.SimpleReplyText(ctx, data.SessionWebhook, replyText); err != nil {
		slog.Warn("dingtalk: reply failed", "error", err)
	}

	return []byte(""), nil
}

// handleSessionCommand processes a "@{shortID} message" command from DingTalk.
func (m *Manager) handleSessionCommand(ctx context.Context, data *chatbot.BotCallbackDataModel, shortID, msg string) {
	replier := chatbot.NewChatbotReplier()

	sessionID, err := resolveShortSessionID(shortID)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", shortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	if sessionMessenger.IsSessionRunning(sessionID) {
		if err := sessionMessenger.EnqueueMessage(sessionID, msg); err != nil {
			slog.Warn("dingtalk: enqueue message failed", "error", err, "session_id", sessionID)
			_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息入队失败: "+err.Error()))
			return
		}
		slog.Info("dingtalk: message enqueued to running session", "session_id", sessionID, "msg", msg)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息已发送到运行中的会话"))
		return
	}

	if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
		slog.Warn("dingtalk: send message to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
		return
	}
	slog.Info("dingtalk: message sent to session", "session_id", sessionID, "msg", msg)
	_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息已发送到会话，AI 正在处理"))
}
