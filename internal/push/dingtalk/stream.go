package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/model"
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
//
// Every inbound message auto-subscribes the sender. Text is then routed by
// ClassifyIncoming: an explicit "@{shortID}" targets that session, "/ls" lists
// recent sessions, and anything else goes to the user's sticky session (the
// one they last addressed). A file/picture message carries no text and always
// targets the sticky session.
//
// The callback returns immediately. DingTalk expects a prompt ack and re-sends
// the frame when it does not get one; downloading a file (two round trips plus
// the transfer) would exceed that budget and duplicate the message. Media
// downloads therefore run in a goroutine and reply over the session webhook
// once done.
func (m *Manager) onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	slog.Info("dingtalk: received message",
		"sender_id", data.SenderId,
		"sender_nick", data.SenderNick,
		"conversation_id", data.ConversationId,
		"conversation_type", data.ConversationType,
		"msgtype", data.Msgtype,
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

	// Media messages carry no text to route on, so they always use the sticky
	// session. Downloading must not block the ack — run it in the background.
	if _, _, ok := extractMedia(data.Msgtype, data.Content); ok {
		go m.handleIncomingMedia(context.WithoutCancel(ctx), data, staffID)
		return []byte(""), nil
	}

	m.handleIncomingText(ctx, data, staffID)
	return []byte(""), nil
}

// handleIncomingText routes a text message. It is synchronous because the
// session-list and error replies are cheap (the send itself is fire-and-forget
// into the queue), so the ack is not delayed meaningfully.
func (m *Manager) handleIncomingText(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID string) {
	sticky := m.lastSessionID(staffID)
	route := common.ClassifyIncoming(data.Text.Content, sticky)

	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, data)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, data)
	case common.RouteToSession:
		m.deliverToSession(ctx, data, staffID, route)
	}
}

// handleIncomingMedia downloads the attached file/picture and sends it to the
// user's sticky session. Runs in its own goroutine (see onChatBotMessage).
func (m *Manager) handleIncomingMedia(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID string) {
	replier := chatbot.NewChatbotReplier()
	sticky := m.lastSessionID(staffID)

	route := common.ClassifyIncomingAttachment(sticky)
	if route.Kind == common.RouteNoTarget {
		m.replyNoTarget(ctx, data)
		return
	}
	if sessionMessenger == nil {
		slog.Warn("dingtalk: no session messenger for attachment")
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("会话服务不可用，文件未发送。"))
		return
	}

	// One lookup serves both the download target (ProjectPath) and the reply
	// label — resolving the route would repeat the same query.
	info, err := sessionMessenger.GetSessionInfo(sticky)
	if err != nil {
		slog.Warn("dingtalk: sticky session unavailable", "error", err, "staff_id", staffID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("最近会话不可用，请用 /ls 重新选择会话。"))
		return
	}
	sessionID := info.ID

	entry, err := m.downloadMedia(ctx, data.Msgtype, data.Content, info.ProjectPath)
	if err != nil {
		slog.Warn("dingtalk: media download failed", "error", err, "session_id", sessionID)
		msg := "文件下载失败，未发送。"
		if errors.Is(err, common.ErrAttachmentTooLarge) {
			msg = fmt.Sprintf("文件超过大小上限（%d MB），未发送。", common.AttachmentMaxBytes()/(1024*1024))
		}
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(msg))
		return
	}

	// An attachment-only message: no text, the file is the whole content.
	if err := sessionMessenger.SendMessageToSession(sessionID, "", []model.FileEntry{entry}); err != nil {
		slog.Warn("dingtalk: send attachment to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送文件失败: "+err.Error()))
		return
	}

	slog.Info("dingtalk: attachment sent to session",
		"session_id", sessionID, "path", entry.Path, "staff_id", staffID)
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte("文件已发送"),
		[]byte(fmt.Sprintf("### 文件已发送\n已发送到会话 **%s**，AI 正在处理",
			common.FormatSessionLabel(sessionID, info.Title))))
}

// deliverToSession resolves the route's target and sends the message, then
// records the target as the user's sticky session.
func (m *Manager) deliverToSession(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID string, route common.Route) {
	replier := chatbot.NewChatbotReplier()
	sticky := m.lastSessionID(staffID)

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	// SendMessageToSession routes through the unified enqueue path
	// (EnqueueAndMaybeStart): if the session is running the message is queued
	// and the drain loop picks it up; if not, the execution is started. The B2
	// self-heal inside handles the drain-loop exit race, so no separate
	// IsSessionRunning branching is needed here.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, nil); err != nil {
		slog.Warn("dingtalk: send message to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
		return
	}

	// Only a successful send moves the sticky target, so a failed one does not
	// silently redirect the user's next message.
	m.rememberSession(staffID, sessionID)

	slog.Info("dingtalk: message sent to session", "session_id", sessionID, "msg", route.Message)
	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte("消息已发送"), []byte(fmt.Sprintf("### 消息已发送\n已发送到会话 **%s**，AI 正在处理", sessionLabel)))
}

// lastSessionID reads the user's sticky target, treating any failure as "none
// recorded" so the caller falls back to the "/ls" hint instead of failing the
// whole message.
func (m *Manager) lastSessionID(staffID string) string {
	if db == nil {
		return ""
	}
	id, err := db.GetLastSessionID(staffID)
	if err != nil {
		slog.Warn("dingtalk: read last session id failed", "error", err, "staff_id", staffID)
		return ""
	}
	return id
}

// rememberSession records the sticky target. A failure is logged but not
// surfaced: the message was already sent, and the user simply gets the "/ls"
// hint on their next unprefixed message.
func (m *Manager) rememberSession(staffID, sessionID string) {
	if db == nil {
		return
	}
	if err := db.SetLastSessionID(staffID, sessionID); err != nil {
		slog.Warn("dingtalk: record last session id failed", "error", err, "staff_id", staffID)
	}
}

// replyNoTarget tells the user how to pick a session. It is the fallback when
// no sticky target exists yet (a first-time sender, or a message after the
// previous session was archived/deleted).
func (m *Manager) replyNoTarget(ctx context.Context, data *chatbot.BotCallbackDataModel) {
	replier := chatbot.NewChatbotReplier()
	_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(
		"尚未选择会话。发送 /ls 查看会话列表，然后用 @会话ID <消息> 选择。"))
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
