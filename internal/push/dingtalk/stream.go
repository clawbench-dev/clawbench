package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/push/common"

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
	if shortID, msg, ok := common.ParseSessionCommand(data.Text.Content); ok {
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

	sessionID, sessionTitle, err := common.ResolveShortSessionID(sessionMessenger, shortID)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", shortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	// SendMessageToSession routes through the unified enqueue path
	// (EnqueueAndMaybeStart): if the session is running the message is queued
	// and the drain loop picks it up; if not, the execution is started. The B2
	// self-heal inside handles the drain-loop exit race, so no separate
	// IsSessionRunning branching is needed here.
	if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
		slog.Warn("dingtalk: send message to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
		return
	}
	slog.Info("dingtalk: message sent to session", "session_id", sessionID, "msg", msg)
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte("消息已发送"), []byte(fmt.Sprintf("### 消息已发送\n已发送到会话 **%s**，AI 正在处理", sessionLabel)))
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
		fmt.Fprintf(&sb, "**%s**\n", g.project)
		for _, s := range g.items {
			id := common.ShortSessionID(s.ID)
			title := s.Title
			if title == "" {
				title = "（无标题）"
			}
			running := ""
			if sessionMessenger.IsSessionRunning(s.ID) {
				running = " 🟢"
			}
			fmt.Fprintf(&sb, "- **@%s** %s%s\n", id, title, running)
		}
		sb.WriteString("\n")
	}
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook, []byte("会话列表"), []byte(sb.String()))
}
