package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// startWebSocket establishes the Feishu WebSocket connection for receiving events.
// This is non-blocking — the SDK manages reconnection internally.
func (m *Manager) startWebSocket(ctx context.Context) {
	dispatcher := larkdispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(m.onMessageReceive)

	m.wsClient = larkws.NewClient(
		m.cfg.AppID, m.cfg.AppSecret,
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
}

// onMessageReceive handles incoming messages from Feishu users.
//
// Every inbound message auto-subscribes the sender. Text is then routed by
// ClassifyIncoming: an explicit "@{shortID}" targets that session, "/ls" lists
// recent sessions, and anything else goes to the user's sticky session (the
// one they last addressed). A file/image message carries no text and always
// targets the sticky session.
//
// The handler returns as soon as the routing decision is made. Feishu expects a
// prompt ack and retries the event when it does not get one; downloading a file
// would exceed that budget and duplicate the message. Media downloads therefore
// run in a goroutine and reply via the messages API once done.
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

	msgType := ptrStr(msg.MessageType)
	msgContent := ptrStr(msg.Content)
	messageID := ptrStr(msg.MessageId)

	// Extract text content from the message JSON
	text := extractTextContent(msgContent, msgType)

	slog.Info(
		"feishu: received message",
		"open_id", openID,
		"chat_id", chatID,
		"chat_type", chatType,
		"msgtype", msgType,
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

	// Media messages carry no text to route on, so they always use the sticky
	// session. Downloading must not block the ack — run it in the background.
	if _, _, _, ok := extractMedia(msgType, msgContent); ok {
		go m.handleIncomingMedia(context.WithoutCancel(ctx), openID, msgType, msgContent, messageID)
		return nil
	}

	m.handleIncomingText(ctx, openID, text)
	return nil
}

// handleIncomingText routes a text message.
func (m *Manager) handleIncomingText(ctx context.Context, openID, text string) {
	sticky := m.lastSessionID(openID)
	route := common.ClassifyIncoming(text, sticky)

	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, openID)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, openID)
	case common.RouteToSession:
		m.deliverToSession(ctx, openID, route)
	}
}

// handleIncomingMedia downloads the attached file/image and sends it to the
// user's sticky session. Runs in its own goroutine (see onMessageReceive).
func (m *Manager) handleIncomingMedia(ctx context.Context, openID, msgType, content, messageID string) {
	sticky := m.lastSessionID(openID)

	route := common.ClassifyIncomingAttachment(sticky)
	if route.Kind == common.RouteNoTarget {
		m.replyNoTarget(ctx, openID)
		return
	}
	if sessionMessenger == nil {
		slog.Warn("feishu: no session messenger for attachment")
		_ = m.SendPostMessage(ctx, openID, "错误", "会话服务不可用，文件未发送。")
		return
	}

	// One lookup serves both the download target (ProjectPath) and the reply
	// label — resolving the route would repeat the same query.
	info, err := sessionMessenger.GetSessionInfo(sticky)
	if err != nil {
		slog.Warn("feishu: sticky session unavailable", "error", err, "open_id", openID)
		_ = m.SendPostMessage(ctx, openID, "错误", "最近会话不可用，请用 /ls 重新选择会话。")
		return
	}
	sessionID := info.ID

	entry, err := m.downloadMedia(ctx, msgType, content, messageID, info.ProjectPath)
	if err != nil {
		slog.Warn("feishu: media download failed", "error", err, "session_id", sessionID)
		msg := "文件下载失败，未发送。"
		if errors.Is(err, common.ErrAttachmentTooLarge) {
			msg = fmt.Sprintf("文件超过大小上限（%d MB），未发送。", common.AttachmentMaxBytes()/(1024*1024))
		}
		_ = m.SendPostMessage(ctx, openID, "错误", msg)
		return
	}

	// An attachment-only message: no text, the file is the whole content.
	if err := sessionMessenger.SendMessageToSession(sessionID, "", []model.FileEntry{entry}); err != nil {
		slog.Warn("feishu: send attachment to session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, "错误", "发送文件失败: "+err.Error())
		return
	}

	slog.Info("feishu: attachment sent to session",
		"session_id", sessionID, "path", entry.Path, "open_id", openID)
	_ = m.SendPostMessage(ctx, openID, "文件已发送",
		fmt.Sprintf("已发送到会话 %s，AI 正在处理", common.FormatSessionLabel(sessionID, info.Title)))
}

// deliverToSession resolves the route's target and sends the message, then
// records the target as the user's sticky session.
func (m *Manager) deliverToSession(ctx context.Context, openID string, route common.Route) {
	sticky := m.lastSessionID(openID)

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("feishu: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = m.SendPostMessage(ctx, openID, "错误", err.Error())
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	// SendMessageToSession routes through the unified enqueue path
	// (EnqueueAndMaybeStart): running sessions get the message queued for the
	// drain loop, non-running sessions start execution. The B2 self-heal inside
	// handles the drain-loop exit race, so no IsSessionRunning branching here.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, nil); err != nil {
		slog.Warn("feishu: send message to session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, "错误", "发送消息失败: "+err.Error())
		return
	}

	// Only a successful send moves the sticky target.
	m.rememberSession(openID, sessionID)

	slog.Info("feishu: message sent to session", "session_id", sessionID, "msg", route.Message)
	_ = m.SendPostMessage(ctx, openID, "消息已发送",
		fmt.Sprintf("已发送到会话 %s，AI 正在处理", sessionLabel))
}

// lastSessionID reads the user's sticky target, treating any failure as "none
// recorded" so the caller falls back to the "/ls" hint.
func (m *Manager) lastSessionID(openID string) string {
	if db == nil {
		return ""
	}
	id, err := db.GetLastSessionID(openID)
	if err != nil {
		slog.Warn("feishu: read last session id failed", "error", err, "open_id", openID)
		return ""
	}
	return id
}

// rememberSession records the sticky target. A failure is logged but not
// surfaced: the message was already sent.
func (m *Manager) rememberSession(openID, sessionID string) {
	if db == nil {
		return
	}
	if err := db.SetLastSessionID(openID, sessionID); err != nil {
		slog.Warn("feishu: record last session id failed", "error", err, "open_id", openID)
	}
}

// replyNoTarget tells the user how to pick a session.
func (m *Manager) replyNoTarget(ctx context.Context, openID string) {
	_ = m.SendPostMessage(ctx, openID, "请选择会话",
		"尚未选择会话。发送 /ls 查看会话列表，然后用 @会话ID <消息> 选择。")
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
				Title   string          `json:"title"`
				Content [][]postElement `json:"content"`
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

// handleSessionList lists recent sessions so the user can pick one to send a message to.
func (m *Manager) handleSessionList(ctx context.Context, openID string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, "已订阅", "ClawBench 通知已订阅。暂无可用会话。")
		return
	}

	sessions, err := sessionMessenger.ListRecentSessions(10)
	if err != nil {
		slog.Warn("feishu: list sessions failed", "error", err)
		_ = m.SendPostMessage(ctx, openID, "已订阅", "ClawBench 通知已订阅。获取会话列表失败。")
		return
	}

	if len(sessions) == 0 {
		_ = m.SendPostMessage(ctx, openID, "已订阅", "ClawBench 通知已订阅。暂无会话。")
		return
	}

	var sb strings.Builder
	sb.WriteString("发送 **@会话ID** <消息> 向会话发送消息：\n\n")

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
				running = " *"
			}
			fmt.Fprintf(&sb, "- **@%s** %s%s\n", id, title, running)
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
