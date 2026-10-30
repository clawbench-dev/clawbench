package wechat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
)

// downloadOutcome summarizes a batch download for the user-facing reply.
type downloadOutcome struct {
	sent      int  // files that downloaded and were handed to the session
	failed    int  // files that could not be downloaded
	truncated bool // the request exceeded the per-message count cap
	tooLarge  bool // at least one failure was the size limit
}

// handleIncomingWithMedia downloads the message's attachments and sends them,
// together with any accompanying text, to the resolved target session.
//
// Runs in its own goroutine (see onInbound). The manager context is used as-is,
// not detached: Stop must abort an in-flight download.
func (m *Manager) handleIncomingWithMedia(ctx context.Context, userID, token, text string, media []mediaRef) {
	sticky := m.lastSessionID(userID)
	route := common.ClassifyIncoming(text, sticky)

	// A command never routes to a session (Route.Message is empty), so sending
	// the attachments on would deliver them with no text — and silently drop the
	// command word. Tell the user to split the request instead.
	//
	// This must come BEFORE the "/ls" branch: /ls is a command too, and answering
	// it while discarding the attachment loses the user's file with no warning.
	if route.Kind.IsCommand() {
		slog.Info("wechat: command cannot carry attachments",
			"user_id", userID, "command", route.Kind.CommandWord(), "media", len(media))
		_ = m.SendText(ctx, userID, common.CommandWithAttachmentText(route.Kind.CommandWord()), token)
		return
	}
	if route.Kind == common.RouteNoTarget {
		m.replyNoTarget(ctx, userID, token)
		return
	}

	if sessionMessenger == nil {
		slog.Warn("wechat: no session messenger for attachment")
		_ = m.SendText(ctx, userID, common.T("PushSessionServiceUnavailable"), token)
		return
	}

	sessionID, sessionTitle, err := common.ResolveTarget(sessionMessenger, route, sticky)
	if err != nil {
		slog.Warn("wechat: session command resolve failed", "error", err, "short_id", route.ShortID)
		_ = m.SendText(ctx, userID, err.Error(), token)
		return
	}

	// ProjectPath decides where the downloads land, so it must come from the
	// resolved session — not the sticky one, which may differ when the user
	// named a target explicitly.
	info, err := sessionMessenger.GetSessionInfo(sessionID)
	if err != nil {
		slog.Warn("wechat: session unavailable", "error", err, "session_id", sessionID)
		_ = m.SendText(ctx, userID, common.T("PushSessionUnavailable"), token)
		return
	}

	entries, out := m.downloadAll(ctx, media, info.ProjectPath)
	if len(entries) == 0 {
		slog.Warn("wechat: all media downloads failed",
			"session_id", sessionID, "requested", len(media), "failed", out.failed)
		_ = m.SendText(ctx, userID, m.downloadFailureText(out), token)
		return
	}

	// One send carrying the text and every successfully downloaded file, so the
	// AI sees the question and its attachments as a single turn.
	if err := sessionMessenger.SendMessageToSession(sessionID, route.Message, entries); err != nil {
		slog.Warn("wechat: send attachment to session failed", "error", err, "session_id", sessionID)
		_ = m.SendText(ctx, userID, common.T("PushSendFileFailed")+err.Error(), token)
		return
	}

	// Only a successful send moves the sticky target (mirrors deliverToSession).
	m.rememberSession(userID, sessionID)

	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	slog.Info("wechat: attachment sent to session",
		"session_id", sessionID, "count", len(entries), "paths", paths, "user_id", userID)
	_ = m.SendText(ctx, userID,
		m.attachmentSentText(sessionID, sessionTitle, len(entries), out), token)
}

// downloadAll downloads every reference into projectPath.
//
// A partial failure is not fatal: the caller still sends what did download, so
// one bad image cannot discard a message that also carried a valid one.
func (m *Manager) downloadAll(ctx context.Context, media []mediaRef, projectPath string) (entries []model.FileEntry, out downloadOutcome) {
	maxFiles := common.AttachmentMaxFiles()
	if len(media) > maxFiles {
		slog.Warn("wechat: truncating attachments to the configured cap",
			"requested", len(media), "max", maxFiles)
		media = media[:maxFiles]
		out.truncated = true
	}

	maxBytes := common.AttachmentMaxBytes()
	for _, ref := range media {
		// Reject on the declared size before buffering. DownloadMedia returns
		// the whole payload as []byte, so the disk cap alone does not bound
		// memory — this pre-check is the only thing standing between a huge
		// attachment and a huge allocation.
		if ref.declaredSize > maxBytes {
			slog.Warn("wechat: attachment rejected on declared size",
				"kind", ref.kind, "declared", ref.declaredSize, "max", maxBytes)
			out.failed++
			out.tooLarge = true
			continue
		}

		entry, err := m.downloadOne(ctx, ref, projectPath, maxBytes)
		if err != nil {
			slog.Warn("wechat: media download failed", "error", err, "kind", ref.kind)
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

// downloadOne fetches one attachment and writes it into the project's uploads
// directory.
func (m *Manager) downloadOne(ctx context.Context, ref mediaRef, projectPath string, maxBytes int64) (model.FileEntry, error) {
	m.startMu.Lock()
	client := m.client
	m.startMu.Unlock()
	if client == nil {
		return model.FileEntry{}, ErrNotStarted
	}

	data, err := client.DownloadMedia(ctx, mediaOf(ref.item))
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("download media: %w", err)
	}
	// The declared size was only a hint; enforce the real one too.
	if int64(len(data)) > maxBytes {
		return model.FileEntry{}, common.ErrAttachmentTooLarge
	}

	return common.SaveAttachment(projectPath, ref.filename, bytes.NewReader(data), maxBytes)
}

// mediaOf returns the CDN descriptor of a media item.
func mediaOf(item ilink.MessageItem) *ilink.CDNMedia {
	switch item.Type {
	case ilink.ItemImage:
		if item.ImageItem != nil {
			return item.ImageItem.Media
		}
	case ilink.ItemFile:
		if item.FileItem != nil {
			return item.FileItem.Media
		}
	case ilink.ItemVideo:
		if item.VideoItem != nil {
			return item.VideoItem.Media
		}
	}
	return nil
}

// downloadFailureText explains why nothing could be sent. The size-limit case
// names the configured cap, which is the actionable part for the user.
func (m *Manager) downloadFailureText(out downloadOutcome) string {
	if out.tooLarge {
		return common.T("PushFileTooLarge", map[string]any{
			"MaxMB": common.AttachmentMaxBytes() / (1024 * 1024),
		})
	}
	if out.failed > 1 {
		return common.T("PushFilesDownloadFailed", map[string]any{"Count": out.failed})
	}
	return common.T("PushFileDownloadFailed")
}

// attachmentSentText builds the success reply, noting any partial failure or
// truncation so the user knows the message was not delivered in full.
func (m *Manager) attachmentSentText(sessionID, sessionTitle string, sent int, out downloadOutcome) string {
	var sb strings.Builder
	sb.WriteString(common.T("PushSentToSessionPlain",
		map[string]any{"Label": common.FormatSessionLabel(sessionID, sessionTitle)}))
	if sent > 1 {
		sb.WriteString(common.T("PushFilesCount", map[string]any{"Count": sent}))
	}
	if out.failed > 0 {
		fmt.Fprintf(&sb, "\n\n%s",
			common.T("PushPartialDownloadFailed", map[string]any{"Count": out.failed}))
	}
	if out.truncated {
		fmt.Fprintf(&sb, "\n\n%s", common.T("PushAttachmentsTruncated", map[string]any{
			"Max": common.AttachmentMaxFiles(), "Sent": sent,
		}))
	}
	return sb.String()
}
