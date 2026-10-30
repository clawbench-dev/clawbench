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

	// Extract the text the user composed and the media the message carried,
	// then route on THAT TEXT. A "post" (rich text) message keeps its text in
	// the content JSON and its images as img elements — routing on
	// extractTextContent alone would send the text and silently drop the images.
	text, bodyText, media := parseInbound(msgType, msgContent)

	slog.Info(
		"feishu: received message",
		"open_id", openID,
		"chat_id", chatID,
		"chat_type", chatType,
		"msgtype", msgType,
		"text", text,
		"media_count", len(media),
	)

	// Always auto-subscribe regardless of command outcomes
	if db != nil {
		if err := db.UpsertSubscriber(openID, chatID, senderName, "stream"); err != nil {
			slog.Warn("feishu: auto-subscribe failed", "error", err, "open_id", openID)
		} else {
			slog.Info("feishu: auto-subscribed user", "user_id", openID)
		}
	}

	// A message we could neither read text from nor download anything from
	// (audio, media, unknown types) must not become an empty send.
	if strings.TrimSpace(text) == "" && len(media) == 0 {
		m.replyUnsupported(ctx, openID, msgType)
		return nil
	}

	// Anything with media needs a download, which must not block the ack —
	// Feishu re-sends an event it does not get acknowledged promptly, and a
	// download is an API round trip plus the transfer.
	if len(media) > 0 {
		go m.handleIncomingWithMedia(context.WithoutCancel(ctx), openID, text, bodyText, media, messageID)
		return nil
	}

	m.handleIncomingText(ctx, openID, text, bodyText)
	return nil
}

// mediaRef is one attachment to download: the resource key, the resource type
// the download API expects ("file" or "image"), and the filename to save it as.
type mediaRef struct {
	key      string
	resType  string
	filename string
}

// parseInbound extracts the user's text and the attachments to download.
//
// It returns two text variants: text is what the user composed (a post's title
// plus body, matching extractTextContent) and is what gets SENT; bodyText omits
// the title and is what ROUTING matches against. SessionCmdRe is anchored with
// ^ and has no (?m) flag, so a title prefix would push a leading "@{shortID}"
// off the start of the string and the target would never resolve.
func parseInbound(msgType, content string) (text, bodyText string, media []mediaRef) {
	// file/image: the attachment IS the whole message.
	if key, resType, filename, ok := extractMedia(msgType, content); ok {
		return "", "", []mediaRef{{key: key, resType: resType, filename: filename}}
	}
	if msgType == msgTypePost {
		full, body, keys, ok := parsePost(content)
		if !ok {
			return "", "", nil
		}
		refs := make([]mediaRef, 0, len(keys))
		for _, k := range keys {
			refs = append(refs, mediaRef{key: k, resType: msgTypeImage, filename: "image.png"})
		}
		return full, body, refs
	}
	t := extractTextContent(content, msgType)
	return t, t, nil
}

// handleIncomingText routes a text message.
//
// bodyText is the routing variant (a post's body without its title); it differs
// from text only for a titled post, where the title would otherwise prefix a
// leading "@{shortID}" and defeat the ^-anchored match. An explicit target's
// message keeps the full text so the title is still delivered.
func (m *Manager) handleIncomingText(ctx context.Context, openID, text, bodyText string) {
	sticky := m.lastSessionID(openID)
	route := common.ClassifyIncoming(bodyText, sticky)
	if route.Kind == common.RouteToSession && route.ShortID == "" {
		route.Message = text
	}

	switch route.Kind {
	case common.RouteListSessions:
		m.handleSessionList(ctx, openID)
	case common.RouteShowLastReply:
		m.handleShowLastReply(ctx, openID, route.Arg)
	case common.RouteStopSession:
		m.handleStopSession(ctx, openID, route.Arg)
	case common.RouteNewSession:
		m.handleNewSession(ctx, openID, route.Arg)
	case common.RouteNoTarget:
		m.replyNoTarget(ctx, openID)
	case common.RouteToSession:
		m.deliverToSession(ctx, openID, route)
	}
}

// handleIncomingWithMedia downloads the message's attachments and sends them,
// together with any accompanying text, to the resolved target session. Runs in
// its own goroutine (see onMessageReceive).
//
// The route is resolved from the parsed text first so an "@{shortID}" works for
// a media message exactly as it does for a text one; the resolved session also
// supplies ProjectPath, which is where the downloads land.
func (m *Manager) handleIncomingWithMedia(ctx context.Context, openID, text, bodyText string, media []mediaRef, messageID string) {
	sticky := m.lastSessionID(openID)

	// Route on the title-less text so a leading "@{shortID}" still matches.
	route := common.ClassifyIncoming(bodyText, sticky)
	// A slash command is not addressed to a session, and Route.Message is empty
	// for one — falling through would post an empty message carrying the
	// attachments and silently drop the command word. Refuse instead of guessing
	// which half the user wanted.
	//
	// This must come BEFORE the "/ls" branch: /ls is a command too, and answering
	// it while discarding the attachment loses the user's file with no warning.
	if route.Kind.IsCommand() {
		slog.Info("feishu: command cannot carry attachments",
			"open_id", openID, "command", route.Kind.CommandWord(), "media", len(media))
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"),
			common.CommandWithAttachmentText(route.Kind.CommandWord()))
		return
	}
	if route.Kind == common.RouteNoTarget {
		m.replyNoTarget(ctx, openID)
		return
	}
	// The message that reaches the session keeps the title, so fall back to the
	// full text when routing found no target of its own.
	if route.ShortID == "" {
		route.Message = text
	}

	if sessionMessenger == nil {
		slog.Warn("feishu: no session messenger for attachment")
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionServiceUnavailable"))
		return
	}

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("feishu: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	// ProjectPath decides where the downloads land, so it must come from the
	// resolved session — not the sticky one, which may differ when the user
	// named a target explicitly.
	info, err := sessionMessenger.GetSessionInfo(sessionID)
	if err != nil {
		slog.Warn("feishu: session unavailable", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionUnavailable"))
		return
	}

	entries, out := m.downloadAll(ctx, media, messageID, info.ProjectPath)
	if len(entries) == 0 {
		slog.Warn("feishu: all media downloads failed",
			"session_id", sessionID, "requested", len(media), "failed", out.failed)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), m.downloadFailureText(out))
		return
	}

	// One send carrying the text and every successfully downloaded file, so the
	// AI sees the question and its attachments as a single turn.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, entries); err != nil {
		slog.Warn("feishu: send attachment to session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSendFileFailed")+err.Error())
		return
	}

	// Only a successful send moves the sticky target (mirrors deliverToSession).
	m.rememberSession(openID, sessionID)

	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	slog.Info("feishu: attachment sent to session",
		"session_id", sessionID, "count", len(entries), "paths", paths, "open_id", openID)
	_ = m.SendPostMessage(ctx, openID, common.T("PushFileSent"),
		m.attachmentSentText(sessionID, sessionTitle, len(entries), out))
}

// downloadOutcome summarizes a batch download for the user-facing reply.
type downloadOutcome struct {
	sent      int  // files that downloaded and were handed to the session
	failed    int  // files that could not be downloaded
	truncated bool // the request exceeded the per-message count cap
	tooLarge  bool // at least one failure was the size limit
}

// downloadAll downloads every reference into projectPath.
//
// A partial failure is not fatal: the caller still sends what did download, so
// one bad image cannot discard a message that also carried a valid one.
func (m *Manager) downloadAll(ctx context.Context, media []mediaRef, messageID, projectPath string) (entries []model.FileEntry, out downloadOutcome) {
	maxFiles := common.AttachmentMaxFiles()
	if len(media) > maxFiles {
		slog.Warn("feishu: truncating attachments to the configured cap",
			"requested", len(media), "max", maxFiles)
		media = media[:maxFiles]
		out.truncated = true
	}

	for _, ref := range media {
		entry, err := m.downloadResource(ctx, ref.key, ref.resType, ref.filename, messageID, projectPath)
		if err != nil {
			slog.Warn("feishu: media download failed", "error", err, "res_type", ref.resType)
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

// downloadFailureText explains why nothing could be sent. The size-limit case
// names the configured cap, which is the actionable part for the user.
func (m *Manager) downloadFailureText(out downloadOutcome) string {
	if out.tooLarge {
		return common.T("PushFileTooLarge", map[string]any{"MaxMB": common.AttachmentMaxBytes() / (1024 * 1024)})
	}
	if out.failed > 1 {
		return common.T("PushFilesDownloadFailed", map[string]any{common.TemplateCount: out.failed})
	}
	return common.T("PushFileDownloadFailed")
}

// attachmentSentText builds the success reply, noting any partial failure or
// truncation so the user knows the message was not delivered in full.
func (m *Manager) attachmentSentText(sessionID, sessionTitle string, sent int, out downloadOutcome) string {
	var sb strings.Builder
	sb.WriteString(common.T("PushSentToSessionPlain", map[string]any{common.TemplateLabel: common.FormatSessionLabel(sessionID, sessionTitle)}))
	if sent > 1 {
		sb.WriteString(common.T("PushFilesCount", map[string]any{common.TemplateCount: sent}))
	}
	if out.failed > 0 {
		fmt.Fprintf(&sb, "\n\n%s", common.T("PushPartialDownloadFailed", map[string]any{common.TemplateCount: out.failed}))
	}
	if out.truncated {
		fmt.Fprintf(&sb, "\n\n%s", common.T("PushAttachmentsTruncated", map[string]any{
			"Max": common.AttachmentMaxFiles(), "Sent": sent,
		}))
	}
	return sb.String()
}

// replyUnsupported tells the user the message type is not handled, instead of
// posting an empty message to the session (which is what used to happen).
func (m *Manager) replyUnsupported(ctx context.Context, openID, msgType string) {
	slog.Info("feishu: unsupported message type", "msgtype", msgType)
	_ = m.SendPostMessage(ctx, openID, common.T("PushUnsupportedTitle"),
		common.T("PushUnsupportedMsgType", map[string]any{"Type": msgType}))
}

// deliverToSession resolves the route's target and sends the message, then
// records the target as the user's sticky session.
func (m *Manager) deliverToSession(ctx context.Context, openID string, route common.Route) {
	sticky := m.lastSessionID(openID)

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("feishu: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	sessionLabel := common.FormatSessionLabel(sessionID, sessionTitle)

	// SendMessageToSession routes through the unified enqueue path
	// (EnqueueAndMaybeStart): running sessions get the message queued for the
	// drain loop, non-running sessions start execution. The B2 self-heal inside
	// handles the drain-loop exit race, so no IsSessionRunning branching here.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, nil); err != nil {
		slog.Warn("feishu: send message to session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSendMessageFailed")+err.Error())
		return
	}

	// Only a successful send moves the sticky target.
	m.rememberSession(openID, sessionID)

	slog.Info("feishu: message sent to session", "session_id", sessionID, "msg", route.Message)
	_ = m.SendPostMessage(ctx, openID, common.T("PushMessageSent"),
		common.T("PushSentToSessionPlain", map[string]any{common.TemplateLabel: sessionLabel}))
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

// handleShowLastReply resends the target session's last finalized AI reply.
//
// The target is the command's argument or the sticky session; when neither
// exists the user gets the same "/ls" hint as any other untargeted message. A
// session with no finished reply gets an explicit hint rather than an empty
// message.
func (m *Manager) handleShowLastReply(ctx context.Context, openID, arg string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionServiceUnavailable"))
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(openID))
	if err != nil {
		slog.Warn("feishu: show target resolve failed", "error", err, "arg", arg)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	label := common.FormatSessionLabel(sessionID, title)
	// Sample the running state BEFORE the read so the note and the text describe
	// the same moment: a turn that finishes in between would otherwise be
	// labeled "still running" while the text already includes it.
	running := sessionMessenger.IsSessionRunning(sessionID)
	text, ok, err := sessionMessenger.LastAssistantReply(sessionID)
	if err != nil {
		slog.Warn("feishu: read last reply failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionUnavailable"))
		return
	}

	body := common.T("PushShowSessionLabel", map[string]any{common.TemplateLabel: label}) + "\n\n"
	if ok && running {
		// The streaming row is deliberately excluded, so the text below belongs
		// to the PREVIOUS turn. Without this note the user reads it as the answer
		// to the question they just asked.
		body += common.T("PushShowRunning") + "\n\n"
	}
	if !ok {
		body += common.T("PushShowEmpty")
	} else {
		body += truncateForFeishu(text)
	}
	_ = m.SendPostMessage(ctx, openID, common.T("PushShowTitle"), body)
}

// handleStopSession stops the target session and drops its queue.
//
// The reply distinguishes "was running" from "already idle": a silent success
// on an idle session looks identical to a failed cancel, and the cleared count
// is the only evidence the queue was touched.
func (m *Manager) handleStopSession(ctx context.Context, openID, arg string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionServiceUnavailable"))
		return
	}

	sessionID, title, err := common.ResolveCommandTarget(sessionMessenger, arg, m.lastSessionID(openID))
	if err != nil {
		slog.Warn("feishu: stop target resolve failed", "error", err, "arg", arg)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	wasRunning, cleared, err := sessionMessenger.StopAndClearSession(sessionID)
	if err != nil {
		slog.Warn("feishu: stop session failed", "error", err, "session_id", sessionID)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	key := "PushStoppedIdle"
	if wasRunning {
		key = "PushStoppedRunning"
	}
	body := common.T(key, map[string]any{
		common.TemplateLabel: common.FormatSessionLabel(sessionID, title),
		common.TemplateCount: cleared,
	})
	_ = m.SendPostMessage(ctx, openID, common.T("PushStoppedTitle"), body)
}

// handleNewSession renders the project list for a bare "/new", or creates an
// empty session in the selected project and makes it the sticky target.
func (m *Manager) handleNewSession(ctx context.Context, openID, arg string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushSessionServiceUnavailable"))
		return
	}

	projects, err := sessionMessenger.ListProjects()
	if err != nil {
		slog.Warn("feishu: list projects failed", "error", err)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), common.T("PushNewListFailed"))
		return
	}

	if strings.TrimSpace(arg) == "" {
		_ = m.SendPostMessage(ctx, openID, common.T("PushNewTitle"), common.RenderProjectList(projects, feishuPreviewMaxRunes))
		return
	}

	path, ref, err := common.SelectProject(projects, arg)
	if err != nil {
		slog.Warn("feishu: project selector failed", "error", err, "arg", arg)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}

	sessionID, _, err := sessionMessenger.CreateSessionInProject(path)
	if err != nil {
		slog.Warn("feishu: create session failed", "error", err, "path", path)
		_ = m.SendPostMessage(ctx, openID, common.T("PushErrorTitle"), err.Error())
		return
	}
	// Only a successful create moves the sticky target, mirroring deliverToSession.
	m.rememberSession(openID, sessionID)

	body := common.T("PushNewCreated", map[string]any{
		common.TemplateProject: path,
		common.TemplateID:      common.ShortSessionID(sessionID),
	})
	if !ref.Exists {
		body += "\n\n" + common.T("PushNewProjectMissingDir")
	}
	_ = m.SendPostMessage(ctx, openID, common.T("PushNewTitle"), body)
}

// replyNoTarget tells the user how to pick a session.
func (m *Manager) replyNoTarget(ctx context.Context, openID string) {
	_ = m.SendPostMessage(ctx, openID, common.T("PushSelectSessionTitle"),
		common.T("PushNoSessionSelected"))
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
// An "img" element carries image_key; a "text" element carries text.
type postElement struct {
	Tag      string `json:"tag"`
	Text     string `json:"text"`
	ImageKey string `json:"image_key"`
}

// parsePost extracts what is needed from a Feishu "post" (rich text) message:
// the full text the user composed (title + body, matching extractTextContent),
// the body-only text used for command matching, and the embedded image keys.
//
// bodyText exists because SessionCmdRe is anchored with ^ and carries no (?m)
// flag: a non-empty title would prefix the body, so an "@{shortID}" at the
// start of the body could never match. Routing therefore tries bodyText, and
// falls back to the full text for the sticky/"/ls" cases so a title is still
// sent when the user did not name a session.
//
// ok is false when content is not a post object (malformed or unexpected).
func parsePost(content string) (text, bodyText string, imageKeys []string, ok bool) {
	if content == "" {
		return "", "", nil, false
	}

	var postMsg struct {
		ZhCn struct {
			Title   string          `json:"title"`
			Content [][]postElement `json:"content"`
		} `json:"zh_cn"`
	}
	if err := json.Unmarshal([]byte(content), &postMsg); err != nil {
		slog.Warn("feishu: post content parse failed", "error", err)
		return "", "", nil, false
	}

	var body strings.Builder
	seen := make(map[string]struct{})
	for _, row := range postMsg.ZhCn.Content {
		// Build the row first so a row that contributes no text (an image-only
		// row) does not leave a stray blank line behind.
		var rowText strings.Builder
		for _, elem := range row {
			switch elem.Tag {
			case "text":
				rowText.WriteString(elem.Text)
			case "img":
				if elem.ImageKey == "" {
					continue
				}
				if _, dup := seen[elem.ImageKey]; dup {
					continue
				}
				seen[elem.ImageKey] = struct{}{}
				imageKeys = append(imageKeys, elem.ImageKey)
			}
		}
		if rowText.Len() == 0 {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString(rowText.String())
	}

	bodyText = body.String()
	if postMsg.ZhCn.Title != "" {
		return postMsg.ZhCn.Title + "\n" + bodyText, bodyText, imageKeys, true
	}
	return bodyText, bodyText, imageKeys, true
}

// handleSessionList lists recent sessions so the user can pick one to send a message to.
func (m *Manager) handleSessionList(ctx context.Context, openID string) {
	if sessionMessenger == nil {
		_ = m.SendPostMessage(ctx, openID, common.T("PushSubscribedTitle"), common.T("PushSubscribedNoAvailableFeishu"))
		return
	}

	sessions, err := sessionMessenger.ListRecentSessions(10)
	if err != nil {
		slog.Warn("feishu: list sessions failed", "error", err)
		_ = m.SendPostMessage(ctx, openID, common.T("PushSubscribedTitle"), common.T("PushSubscribedListFailedFeishu"))
		return
	}

	if len(sessions) == 0 {
		_ = m.SendPostMessage(ctx, openID, common.T("PushSubscribedTitle"), common.T("PushSubscribedNoSessionsFeishu"))
		return
	}

	var sb strings.Builder
	// Feishu keeps the bold span on the ID alone (DingTalk bolds the whole
	// placeholder); the wording is otherwise identical. Restored per-platform so
	// this refactor does not change the rendered card.
	sb.WriteString(common.T("PushSessionListIntroFeishu") + "\n\n")

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
		fmt.Fprintf(&sb, "**%s**\n", g.project)
		for _, s := range g.items {
			id := common.ShortSessionID(s.ID)
			title := s.Title
			if title == "" {
				title = common.T("PushNoTitle")
			}
			running := ""
			if sessionMessenger.IsSessionRunning(s.ID) {
				running = " *"
			}
			fmt.Fprintf(&sb, "- **@%s** %s%s\n", id, title, running)
		}
		sb.WriteString("\n")
	}

	_ = m.SendPostMessage(ctx, openID, common.T("PushSessionListTitle"), sb.String())
}

// ptrStr safely dereferences a *string, returning "" for nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
