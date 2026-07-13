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
func (m *Manager) onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	slog.Info("dingtalk: received message",
		"sender_id", data.SenderId,
		"sender_nick", data.SenderNick,
		"conversation_id", data.ConversationId,
		"conversation_type", data.ConversationType,
		"text", data.Text.Content,
	)

	// Only handle single-chat (1=单聊, 2=群聊)
	if data.ConversationType != "1" {
		slog.Debug("dingtalk: ignoring non-single-chat message", "type", data.ConversationType)
		return []byte(""), nil
	}

	// Auto-subscribe: use SenderStaffId (real userId) not SenderId (encrypted LWCP format)
	// The robot single-chat API requires real userId for userIds parameter.
	staffID := data.SenderStaffId
	if staffID == "" {
		slog.Warn("dingtalk: senderStaffId is empty, falling back to senderId", "sender_id", data.SenderId)
		staffID = data.SenderId
	}
	if db != nil {
		if err := db.UpsertSubscriber(staffID, data.ConversationId, data.SenderNick, "stream"); err != nil {
			slog.Warn("dingtalk: auto-subscribe failed", "error", err, "staff_id", staffID)
		} else {
			slog.Info("dingtalk: auto-subscribed user", "user_id", staffID, "nick", data.SenderNick)
		}
	}

	// Reply to confirm subscription
	replier := chatbot.NewChatbotReplier()
	replyText := []byte("已订阅 ClawBench 通知。后续 AI 会话完成、定时任务完成、权限审批等事件将推送到此对话。")
	if err := replier.SimpleReplyText(ctx, data.SessionWebhook, replyText); err != nil {
		slog.Warn("dingtalk: reply failed", "error", err)
	}

	return []byte(""), nil
}
