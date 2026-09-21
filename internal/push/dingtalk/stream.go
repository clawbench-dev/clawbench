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

	// Parse the inbound message into the text the user composed and the media
	// it carried, then route on THAT TEXT. A richText message (text + images)
	// has an empty data.Text.Content, so routing on it would lose both the
	// "@{shortID}" target and the images.
	text, media := parseInbound(data.Msgtype, data)

	// A message we could neither read text from nor download anything from
	// (audio, video, unknown types) must not become an empty send. Tell the
	// user instead of silently posting a blank message.
	if strings.TrimSpace(text) == "" && len(media) == 0 {
		m.replyUnsupported(ctx, data)
		return []byte(""), nil
	}

	// Anything with media needs a download, which must not block the ack —
	// DingTalk re-sends a frame it does not get acknowledged promptly, and a
	// download is two round trips plus the transfer.
	if len(media) > 0 {
		go m.handleIncomingWithMedia(context.WithoutCancel(ctx), data, staffID, text, media)
		return []byte(""), nil
	}

	m.handleIncomingText(ctx, data, staffID, text)
	return []byte(""), nil
}

// mediaRef is one attachment to download: the downloadCode plus the filename to
// save it under. The name travels with the code because only the parser knows
// what the source message type implies (a picture has no name of its own).
type mediaRef struct {
	code     string
	filename string
}

// parseInbound extracts the user's text and the attachments to download from an
// inbound callback. It is the single place that knows how each DingTalk message
// type is shaped, so routing always sees real text.
//
// The text body normally lives in data.Text.Content; a richText message is the
// exception, carrying it inside data.Content instead. Both are handled here so
// the caller never has to know which.
func parseInbound(msgType string, data *chatbot.BotCallbackDataModel) (text string, media []mediaRef) {
	// file/picture: the attachment IS the whole message.
	if code, filename, ok := extractMedia(msgType, data.Content); ok {
		return "", []mediaRef{{code: code, filename: filename}}
	}
	if msgType == msgTypeRichText {
		rtText, codes, ok := parseRichText(data.Content)
		if !ok {
			return "", nil
		}
		refs := make([]mediaRef, 0, len(codes))
		for _, c := range codes {
			// A richText picture element carries no name; use the same derived
			// default the picture message type uses.
			refs = append(refs, mediaRef{code: c, filename: "image.png"})
		}
		return rtText, refs
	}
	// Text, and any type we do not model: the body is here when there is one,
	// and empty for types that carry no text (audio/video/unknown), which the
	// caller's guard turns into an "unsupported" hint.
	return data.Text.Content, nil
}

// handleIncomingText routes a text message. It is synchronous because the
// session-list and error replies are cheap (the send itself is fire-and-forget
// into the queue), so the ack is not delayed meaningfully.
func (m *Manager) handleIncomingText(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, text string) {
	sticky := m.lastSessionID(staffID)
	route := common.ClassifyIncoming(text, sticky)

	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, data)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, data)
	case common.RouteToSession:
		m.deliverToSession(ctx, data, staffID, route)
	}
}

// handleIncomingWithMedia downloads the message's attachments and sends them,
// together with any accompanying text, to the resolved target session. Runs in
// its own goroutine (see onChatBotMessage).
//
// The route is resolved from the parsed text first so an "@{shortID}" works for
// a media message exactly as it does for a text one; the resolved session also
// supplies ProjectPath, which is where the downloads land.
func (m *Manager) handleIncomingWithMedia(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, text string, media []mediaRef) {
	replier := chatbot.NewChatbotReplier()
	sticky := m.lastSessionID(staffID)

	// ClassifyIncoming also handles "/ls" and the no-target hint, so a media
	// message that is only "@{shortID}" or "/ls" behaves like its text twin.
	route := common.ClassifyIncoming(text, sticky)
	if route.Kind == common.RouteListSessions {
		m.handleSessionList(ctx, data)
		return
	}
	if route.Kind == common.RouteNoTarget {
		m.replyNoTarget(ctx, data)
		return
	}

	if sessionMessenger == nil {
		slog.Warn("dingtalk: no session messenger for attachment")
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("会话服务不可用，文件未发送。"))
		return
	}

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	// ProjectPath decides where the downloads land, so it must come from the
	// resolved session — not the sticky one, which may differ when the user
	// named a target explicitly.
	info, err := sessionMessenger.GetSessionInfo(sessionID)
	if err != nil {
		slog.Warn("dingtalk: session unavailable", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("会话不可用，文件未发送。"))
		return
	}

	entries, dl := m.downloadAll(ctx, media, info.ProjectPath)
	if len(entries) == 0 {
		// Nothing downloadable: report why rather than sending an empty message.
		slog.Warn("dingtalk: all media downloads failed",
			"session_id", sessionID, "requested", len(media), "failed", dl.failed)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(m.downloadFailureText(dl)))
		return
	}

	// One send carrying the text and every successfully downloaded file, so the
	// AI sees the question and its attachments as a single turn.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, entries); err != nil {
		slog.Warn("dingtalk: send attachment to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送文件失败: "+err.Error()))
		return
	}

	// Only a successful send moves the sticky target (mirrors deliverToSession),
	// so a failed one does not silently redirect the user's next message.
	m.rememberSession(staffID, sessionID)

	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	slog.Info("dingtalk: attachment sent to session",
		"session_id", sessionID, "count", len(entries), "paths", paths, "staff_id", staffID)

	_ = replier.SimpleReplyMarkdown(ctx, data.SessionWebhook,
		[]byte("文件已发送"),
		[]byte(m.attachmentSentMarkdown(sessionID, sessionTitle, len(entries), dl)))
}

// downloadOutcome summarizes a batch download for the user-facing reply.
type downloadOutcome struct {
	sent      int  // files that downloaded and were handed to the session
	failed    int  // files that could not be downloaded
	truncated bool // the request exceeded the per-message count cap
	tooLarge  bool // at least one failure was the size limit
}

// downloadAll downloads every code into projectPath.
//
// A partial failure is not fatal: the caller still sends what did download, so
// one bad image cannot discard a message that also carried a valid one.
func (m *Manager) downloadAll(ctx context.Context, media []mediaRef, projectPath string) (entries []model.FileEntry, out downloadOutcome) {
	maxFiles := common.AttachmentMaxFiles()
	if len(media) > maxFiles {
		slog.Warn("dingtalk: truncating attachments to the configured cap",
			"requested", len(media), "max", maxFiles)
		media = media[:maxFiles]
		out.truncated = true
	}

	for _, ref := range media {
		entry, err := m.downloadRef(ctx, ref, projectPath)
		if err != nil {
			slog.Warn("dingtalk: media download failed", "error", err, "filename", ref.filename)
			out.failed++
			if errors.Is(err, common.ErrAttachmentTooLarge) {
				out.tooLarge = true
			}
			continue
		}
		entries = append(entries, entry)
	}
	out.sent = len(entries)
	return entries, out
}

// downloadRef resolves one media reference and saves it under its filename.
func (m *Manager) downloadRef(ctx context.Context, ref mediaRef, projectPath string) (model.FileEntry, error) {
	url, err := m.resolveDownloadURL(ctx, ref.code)
	if err != nil {
		return model.FileEntry{}, err
	}
	entry, err := common.DownloadAttachment(m.httpClient, url, projectPath, ref.filename)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("dingtalk: %w", err)
	}
	return entry, nil
}

// downloadFailureText explains why nothing could be sent. The size-limit case
// names the configured cap, which is the actionable part for the user.
func (m *Manager) downloadFailureText(out downloadOutcome) string {
	if out.tooLarge {
		return fmt.Sprintf("文件超过大小上限（%d MB），未发送。", common.AttachmentMaxBytes()/(1024*1024))
	}
	if out.failed > 1 {
		return fmt.Sprintf("文件下载失败，未发送（%d 个）。", out.failed)
	}
	return "文件下载失败，未发送。"
}

// attachmentSentMarkdown builds the success reply, noting any partial failure
// or truncation so the user knows the message was not delivered in full.
func (m *Manager) attachmentSentMarkdown(sessionID, sessionTitle string, sent int, out downloadOutcome) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "### 文件已发送\n已发送到会话 **%s**，AI 正在处理",
		common.FormatSessionLabel(sessionID, sessionTitle))
	if sent > 1 {
		fmt.Fprintf(&sb, "（共 %d 个文件）", sent)
	}
	if out.failed > 0 {
		fmt.Fprintf(&sb, "\n\n⚠️ 有 %d 个文件下载失败，未发送。", out.failed)
	}
	if out.truncated {
		fmt.Fprintf(&sb, "\n\n⚠️ 附件数量超过上限（%d），仅发送前 %d 个。",
			common.AttachmentMaxFiles(), sent)
	}
	return sb.String()
}

// replyUnsupported tells the user the message type is not handled, instead of
// posting an empty message to the session (which is what used to happen).
func (m *Manager) replyUnsupported(ctx context.Context, data *chatbot.BotCallbackDataModel) {
	slog.Info("dingtalk: unsupported message type", "msgtype", data.Msgtype)
	replier := chatbot.NewChatbotReplier()
	_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(
		fmt.Sprintf("暂不支持该消息类型（%s）。请发送文字、图片或文件。", data.Msgtype)))
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
