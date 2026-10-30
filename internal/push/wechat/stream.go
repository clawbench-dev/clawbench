package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
)

// onInbound handles one message from the long-poll stream.
//
// It runs synchronously inside the poll loop, so it returns as soon as the
// routing decision is made: a media download is several network round trips and
// would stall every message queued behind it. Downloads therefore run in a
// goroutine (see handleIncomingWithMedia).
//
// Replies carry the context token from THIS message, not a value looked up per
// reply: it is the token the platform expects to see echoed back, and it costs
// no extra query.
func (m *Manager) onInbound(ctx context.Context, msg ilink.WeixinMessage) {
	// Only 1:1 chats from a human. Group messages carry GroupID, and bot
	// echoes carry MsgTypeBot — replying to our own echo would loop.
	//
	// Group support is not merely unverified: the outbound message struct has no
	// group addressing field at all (only ToUserID), so a group reply cannot be
	// expressed with this SDK.
	if msg.MessageType != ilink.MsgTypeUser || msg.GroupID != "" {
		slog.Debug("wechat: ignoring non-1:1 message",
			"message_type", msg.MessageType, "group_id", msg.GroupID)
		return
	}

	userID := msg.FromUserID
	if userID == "" {
		slog.Warn("wechat: inbound message without from_user_id")
		return
	}
	token := msg.ContextToken

	text, media := parseInbound(msg)

	slog.Info("wechat: received message",
		"user_id", userID,
		"text", text,
		"media_count", len(media),
		"has_context_token", token != "")

	// Always auto-subscribe, regardless of what the message turns out to be:
	// the subscription is what makes outbound push possible at all.
	if db != nil {
		if err := db.UpsertSubscriber(userID, msg.SessionID, "", "stream"); err != nil {
			slog.Warn("wechat: auto-subscribe failed", "error", err, "user_id", userID)
		}
		// Persist the reply token. This is the whole reason the column exists:
		// the SDK's cache is process-local, so without it every restart would
		// disable push until the user messaged again.
		if token != "" {
			if err := db.SetContextToken(userID, token); err != nil {
				slog.Warn("wechat: persist context token failed", "error", err, "user_id", userID)
			}
		}
	}

	// Nothing readable and nothing downloadable: say so instead of posting an
	// empty message into a session.
	if strings.TrimSpace(text) == "" && len(media) == 0 {
		m.replyUnsupported(ctx, userID, describeUnsupported(msg), token)
		return
	}

	if len(media) > 0 {
		go m.handleIncomingWithMedia(ctx, userID, token, text, media)
		return
	}

	m.handleIncomingText(ctx, userID, token, text)
}

// mediaRef is one attachment to download.
type mediaRef struct {
	kind     string // "image" | "file" | "video"
	filename string
	item     ilink.MessageItem
	// declaredSize is the size the platform claims, used to reject an oversized
	// attachment before buffering it. 0 means unknown.
	declaredSize int64
}

// parseInbound extracts the user's text and the attachments from a message.
//
// WeChat can mix a text item with media items in one message, so unlike a
// Feishu "file" message the text is not necessarily empty. There is no title
// field, so the "route on the title-less body but send the full text" split
// that Feishu needs collapses into a single string.
//
// A quoted message (RefMsg) contributes a prefix. ilink.ExtractText prepends it,
// but that would push a leading "@{shortID}" off the start of the string and
// defeat the ^-anchored command match — so the quote is appended at the END
// instead, where it cannot break routing.
func parseInbound(msg ilink.WeixinMessage) (text string, media []mediaRef) {
	var parts []string
	var quoted []string

	for i := range msg.ItemList {
		item := &msg.ItemList[i]
		if piece := textOfItem(item); piece != "" {
			parts = append(parts, piece)
		}
		if q := quoteText(item.RefMsg); q != "" {
			quoted = append(quoted, q)
		}
		if ref, ok := mediaOfItem(item); ok {
			media = append(media, ref)
		}
	}

	text = strings.Join(parts, "\n")
	if len(quoted) > 0 {
		text = strings.TrimSpace(text + "\n\n" + strings.Join(quoted, "\n"))
	}
	return text, media
}

// textOfItem returns the user-composed text an item carries, or "".
//
// Voice prefers the platform's speech-to-text. A raw .silk payload is useless to
// every AI backend, and decoding it would require an injected SILK decoder
// (breaking the zero-dependency property) for something the model cannot consume
// anyway.
func textOfItem(item *ilink.MessageItem) string {
	switch item.Type {
	case ilink.ItemText:
		if item.TextItem != nil {
			return item.TextItem.Text
		}
	case ilink.ItemVoice:
		if item.VoiceItem != nil {
			return item.VoiceItem.Text
		}
	}
	return ""
}

// mediaOfItem converts a downloadable item into a mediaRef, reporting false for
// text/voice items and for malformed media without a CDN descriptor.
func mediaOfItem(item *ilink.MessageItem) (mediaRef, bool) {
	switch item.Type {
	case ilink.ItemImage:
		if item.ImageItem != nil && item.ImageItem.Media != nil {
			return mediaRef{
				kind:         "image",
				filename:     "image.png",
				item:         *item,
				declaredSize: imageSize(item.ImageItem),
			}, true
		}
	case ilink.ItemFile:
		if item.FileItem != nil && item.FileItem.Media != nil {
			name := item.FileItem.FileName
			if name == "" {
				name = "attachment"
			}
			return mediaRef{
				kind:         "file",
				filename:     name,
				item:         *item,
				declaredSize: parseLen(item.FileItem.Len),
			}, true
		}
	case ilink.ItemVideo:
		if item.VideoItem != nil && item.VideoItem.Media != nil {
			return mediaRef{
				kind:         "video",
				filename:     "video.mp4",
				item:         *item,
				declaredSize: item.VideoItem.VideoSize,
			}, true
		}
	}
	return mediaRef{}, false
}

// quoteText renders a quoted message as a trailing block.
//
// The bracketed prefix is localized rather than written inline: this text is
// shown to the user, so it must follow the configured language like every other
// bot reply (the push backends run without an HTTP request, hence common.T).
func quoteText(ref *ilink.RefMessage) string {
	if ref == nil {
		return ""
	}
	var body string
	if ref.MessageItem != nil {
		if ref.MessageItem.TextItem != nil {
			body = ref.MessageItem.TextItem.Text
		} else if ilink.IsMediaItem(ref.MessageItem) {
			body = common.T("PushQuoteMedia")
		}
	}
	label := common.T("PushQuoteLabel")
	switch {
	case ref.Title != "" && body != "":
		return fmt.Sprintf("[%s: %s | %s]", label, ref.Title, body)
	case body != "":
		return fmt.Sprintf("[%s: %s]", label, body)
	case ref.Title != "":
		return fmt.Sprintf("[%s: %s]", label, ref.Title)
	default:
		return ""
	}
}

// imageSize returns the largest declared dimension of an image item, used for
// the pre-download size check.
func imageSize(img *ilink.ImageItem) int64 {
	size := img.HDSize
	if img.MidSize > size {
		size = img.MidSize
	}
	if img.ThumbSize > size {
		size = img.ThumbSize
	}
	return size
}

// parseLen converts the string-typed FileItem.Len into a byte count, returning
// 0 when it is absent or unparseable.
func parseLen(s string) int64 {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0
	}
	return n
}

// describeUnsupported names the message type for the user-facing reply.
func describeUnsupported(msg ilink.WeixinMessage) string {
	for _, item := range msg.ItemList {
		switch item.Type {
		case ilink.ItemVoice:
			return "voice"
		case ilink.ItemImage:
			return "image"
		case ilink.ItemFile:
			return "file"
		case ilink.ItemVideo:
			return "video"
		}
	}
	return "unknown"
}

// handleIncomingText routes a text message.
func (m *Manager) handleIncomingText(ctx context.Context, userID, token, text string) {
	sticky := m.lastSessionID(userID)
	route := common.ClassifyIncoming(text, sticky)

	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, userID, token)
	case common.RouteShowLastReply:
		m.handleShowLastReply(ctx, userID, token, route.Arg)
	case common.RouteStopSession:
		m.handleStopSession(ctx, userID, token, route.Arg)
	case common.RouteNewSession:
		m.handleNewSession(ctx, userID, token, route.Arg)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, userID, token)
	case common.RouteToSession:
		m.deliverToSession(ctx, userID, token, route)
	}
}

// handleShowLastReply resends the target session's last finalized AI reply.
//
// The target is the command's argument or the sticky session. A session with no
// finished reply gets an explicit hint rather than an empty message, and the
// text is the whole final answer — /show exists because the push preview is
// truncated.
//
// WeChat is a plain-text channel: the template strings carry Markdown emphasis
// (`**上一条**`), so the composed body goes through stripMarkdownForWeChat. The
// answer itself is capped by truncateForWeChat, which is the channel's
// single-message ceiling (~2000 runes), not an arbitrary preview budget.
func (m *Manager) handleShowLastReply(ctx context.Context, userID, token, arg string) {
	if sessionMessenger == nil {
		_ = m.SendText(ctx, userID, common.T("PushSessionServiceUnavailable"), token)
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(userID))
	if err != nil {
		slog.Warn("wechat: show target resolve failed", "error", err, "arg", arg)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	label := common.FormatSessionLabel(sessionID, title)
	// Sample the running state BEFORE the read so the note and the text describe
	// the same moment (mirrors the DingTalk/Feishu handlers).
	running := sessionMessenger.IsSessionRunning(sessionID)
	text, ok, err := sessionMessenger.LastAssistantReply(sessionID)
	if err != nil {
		slog.Warn("wechat: read last reply failed", "error", err, "session_id", sessionID)
		_ = m.SendText(ctx, userID, common.T("PushSessionUnavailable"), token)
		return
	}

	// Strip each segment on its own: the answer already went through
	// truncateForWeChat (which strips once, protecting fenced code). Stripping the
	// composed body again would run the emphasis rules over code that is already
	// plain text, turning "def __init__" into "def init".
	var sb strings.Builder
	sb.WriteString(stripMarkdownKeeping(
		common.T("PushShowSessionLabel", map[string]any{common.TemplateLabel: label}), label) + "\n\n")
	if ok && running {
		// The streaming row is deliberately excluded, so the text below belongs
		// to the PREVIOUS turn; without this note the user reads it as the answer
		// to the question they just asked.
		sb.WriteString(stripMarkdownForWeChat(common.T("PushShowRunning")) + "\n\n")
	}
	if !ok {
		sb.WriteString(stripMarkdownForWeChat(common.T("PushShowEmpty")))
	} else {
		sb.WriteString(truncateForWeChat(text))
	}
	_ = m.SendText(ctx, userID, sb.String(), token)
}

// handleStopSession stops the target session and drops its queue.
//
// The reply distinguishes "was running" from "already idle": a silent success on
// an idle session looks identical to a failed cancel, and the cleared count is
// the only evidence the queue was touched.
func (m *Manager) handleStopSession(ctx context.Context, userID, token, arg string) {
	if sessionMessenger == nil {
		_ = m.SendText(ctx, userID, common.T("PushSessionServiceUnavailable"), token)
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(userID))
	if err != nil {
		slog.Warn("wechat: stop target resolve failed", "error", err, "arg", arg)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	wasRunning, cleared, err := sessionMessenger.StopAndClearSession(sessionID)
	if err != nil {
		slog.Warn("wechat: stop session failed", "error", err, "session_id", sessionID)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	key := "PushStoppedIdle"
	if wasRunning {
		key = "PushStoppedRunning"
	}
	label := common.FormatSessionLabel(sessionID, title)
	body := common.T(key, map[string]any{
		common.TemplateLabel: label,
		common.TemplateCount: cleared,
	})
	_ = m.SendText(ctx, userID, stripMarkdownKeeping(body, label), token)
}

// handleNewSession renders the project list for a bare "/new", or creates an
// empty session in the selected project and makes it the sticky target.
func (m *Manager) handleNewSession(ctx context.Context, userID, token, arg string) {
	if sessionMessenger == nil {
		_ = m.SendText(ctx, userID, common.T("PushSessionServiceUnavailable"), token)
		return
	}

	projects, err := sessionMessenger.ListProjects()
	if err != nil {
		slog.Warn("wechat: list projects failed", "error", err)
		_ = m.SendText(ctx, userID, common.T("PushNewListFailed"), token)
		return
	}

	if strings.TrimSpace(arg) == "" {
		// The budget is applied BEFORE stripping Markdown, which only shortens the
		// text, so the stripped result stays under the ceiling. The paths are data:
		// stripping would eat the underscores in "/srv/my_project", and the user is
		// told to type a path back.
		paths := make([]string, 0, len(projects))
		for _, r := range projects {
			paths = append(paths, r.Path)
		}
		_ = m.SendText(ctx, userID,
			stripMarkdownKeeping(common.RenderProjectList(projects, wechatPreviewMaxRunes), paths...), token)
		return
	}

	path, ref, err := common.SelectProject(projects, arg)
	if err != nil {
		slog.Warn("wechat: project selector failed", "error", err, "arg", arg)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	sessionID, _, err := sessionMessenger.CreateSessionInProject(path)
	if err != nil {
		slog.Warn("wechat: create session failed", "error", err, "path", path)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}
	// Only a successful create moves the sticky target, mirroring deliverToSession.
	m.rememberSession(userID, sessionID)

	body := common.T("PushNewCreated", map[string]any{
		common.TemplateProject: path,
		common.TemplateID:      common.ShortSessionID(sessionID),
	})
	if !ref.Exists {
		body += "\n\n" + common.T("PushNewProjectMissingDir")
	}
	_ = m.SendText(ctx, userID, stripMarkdownKeeping(body, path), token)
}

// deliverToSession resolves the route's target, sends the message, then records
// the target as the user's sticky session.
func (m *Manager) deliverToSession(ctx context.Context, userID, token string, route common.Route) {
	sticky := m.lastSessionID(userID)

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("wechat: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	// SendMessageToSession routes through the unified enqueue path: running
	// sessions get the message queued for the drain loop, idle sessions start an
	// execution. Its B2 self-heal covers the drain-loop exit race.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, nil); err != nil {
		slog.Warn("wechat: send message to session failed", "error", err, "session_id", sessionID)
		_ = m.SendText(ctx, userID, common.T("PushSendMessageFailed")+err.Error(), token)
		return
	}

	// Only a successful send moves the sticky target.
	m.rememberSession(userID, sessionID)

	slog.Info("wechat: message sent to session", "session_id", sessionID, "msg", route.Message)
	_ = m.SendText(ctx, userID,
		common.T("PushSentToSessionPlain", map[string]any{"Label": sessionLabel}), token)
}

// handleSessionList replies with the recent sessions grouped by project.
func (m *Manager) handleSessionList(ctx context.Context, userID, token string) {
	if sessionMessenger == nil {
		_ = m.SendText(ctx, userID, common.T("PushSubscribedNoAvailableWechat"), token)
		return
	}

	sessions, err := sessionMessenger.ListRecentSessions(10)
	if err != nil {
		slog.Warn("wechat: list sessions failed", "error", err)
		_ = m.SendText(ctx, userID, common.T("PushSubscribedListFailedWechat"), token)
		return
	}

	if len(sessions) == 0 {
		_ = m.SendText(ctx, userID, common.T("PushSubscribedNoSessionsWechat"), token)
		return
	}

	var sb strings.Builder
	sb.WriteString(common.T("PushSessionListIntroWechat") + "\n\n")

	type group struct {
		project string
		items   []common.SessionInfo
	}
	var groups []group
	groupIdx := map[string]int{}
	for _, s := range sessions {
		project := s.ProjectPath
		if project == "" {
			project = common.T("PushNoProject")
		}
		if idx, ok := groupIdx[project]; ok {
			groups[idx].items = append(groups[idx].items, s)
		} else {
			groupIdx[project] = len(groups)
			groups = append(groups, group{project: project, items: []common.SessionInfo{s}})
		}
	}

	for _, g := range groups {
		fmt.Fprintf(&sb, "%s\n", g.project)
		for _, s := range g.items {
			title := s.Title
			if title == "" {
				title = common.T("PushNoTitle")
			}
			running := ""
			if sessionMessenger.IsSessionRunning(s.ID) {
				running = " *"
			}
			fmt.Fprintf(&sb, "- @%s %s%s\n", common.ShortSessionID(s.ID), title, running)
		}
		sb.WriteString("\n")
	}

	_ = m.SendText(ctx, userID, sb.String(), token)
}

// replyNoTarget tells the user how to pick a session.
func (m *Manager) replyNoTarget(ctx context.Context, userID, token string) {
	_ = m.SendText(ctx, userID, common.T("PushNoSessionSelected"), token)
}

// replyUnsupported tells the user the message type is not handled, instead of
// posting an empty message to the session.
func (m *Manager) replyUnsupported(ctx context.Context, userID, kind, token string) {
	slog.Info("wechat: unsupported message type", "kind", kind)
	_ = m.SendText(ctx, userID,
		common.T("PushUnsupportedMsgType", map[string]any{"Type": kind}), token)
}

// lastSessionID reads the user's sticky target, treating any failure as "none
// recorded" so the caller falls back to the "/ls" hint.
func (m *Manager) lastSessionID(userID string) string {
	if db == nil {
		return ""
	}
	id, err := db.GetLastSessionID(userID)
	if err != nil {
		slog.Warn("wechat: read last session id failed", "error", err, "user_id", userID)
		return ""
	}
	return id
}

// rememberSession records the sticky target. A failure is logged but not
// surfaced: the message was already sent.
func (m *Manager) rememberSession(userID, sessionID string) {
	if db == nil {
		return
	}
	if err := db.SetLastSessionID(userID, sessionID); err != nil {
		slog.Warn("wechat: record last session id failed", "error", err, "user_id", userID)
	}
}
