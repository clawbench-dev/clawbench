package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	responsePreviewMaxLen = 200
)

// PushSessionEvent sends a DingTalk push notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string

	switch status {
	case "completed":
		title = "AI 会话已完成"
		markdown = fmt.Sprintf("### AI 会话已完成\n**会话**: %s\n**项目**: %s\n\n%s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath),
			truncatePreview(responsePreview))
	case "cancelled":
		title = "AI 会话已取消"
		markdown = fmt.Sprintf("### AI 会话已取消\n**会话**: %s\n**项目**: %s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath))
	case "permission_pending":
		title = "需要审批"
		markdown = fmt.Sprintf("### 需要审批\n**会话**: %s\n**项目**: %s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath))
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}

// PushTaskEvent sends a DingTalk push notification for a task event.
// Only processes completed/failed/cancelled statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushTaskEvent(taskID, status, taskName, responsePreview, projectPath string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string

	switch status {
	case "completed":
		title = "定时任务已完成"
		markdown = fmt.Sprintf("### 定时任务已完成\n**任务**: %s\n**项目**: %s\n\n%s",
			escapeMarkdown(taskName),
			escapeMarkdown(projectPath),
			truncatePreview(responsePreview))
	case "failed":
		title = "定时任务失败"
		markdown = fmt.Sprintf("### 定时任务失败\n**任务**: %s\n**项目**: %s",
			escapeMarkdown(taskName),
			escapeMarkdown(projectPath))
	case "cancelled":
		title = "定时任务已取消"
		markdown = fmt.Sprintf("### 定时任务已取消\n**任务**: %s\n**项目**: %s",
			escapeMarkdown(taskName),
			escapeMarkdown(projectPath))
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}

// sendToAllSubscribers sends a notification to all subscribed users directly.
// Fire-and-forget: success or failure is logged, no retry or DB persistence.
// Suppressed when at least one client (web/APP) is online watching the UI.
// Returns true if at least one subscriber received the notification.
func sendToAllSubscribers(title, markdown string) bool {
	if clientChecker != nil && clientChecker.HasConnectedClients() {
		slog.Debug("dingtalk: suppressed push, client is online", "title", title)
		return false
	}

	subscribers, err := db.GetSubscribers()
	if err != nil {
		slog.Warn("dingtalk: get subscribers failed", "error", err)
		return false
	}
	if len(subscribers) == 0 {
		return false
	}

	mgr := GetManager()
	if mgr == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sent := 0
	for _, sub := range subscribers {
		if err := mgr.SendMarkdownMessage(ctx, sub.UserID, title, markdown); err != nil {
			slog.Warn("dingtalk: send failed", "error", err, "user_id", sub.UserID)
		} else {
			sent++
		}
	}

	slog.Debug("dingtalk: sent notifications", "sent", sent, "total", len(subscribers), "title", title)
	return sent > 0
}

// escapeMarkdown escapes special Markdown characters in DingTalk messages.
func escapeMarkdown(s string) string {
	r := strings.NewReplacer(
		"*", "\\*",
		"#", "\\#",
		"_", "\\_",
		"`", "\\`",
		">", "\\>",
		"|", "\\|",
		"~", "\\~",
	)
	return r.Replace(s)
}

// truncatePreview limits response preview length for push messages (rune-safe).
func truncatePreview(preview string) string {
	if utf8.RuneCountInString(preview) > responsePreviewMaxLen {
		runes := []rune(preview)
		return string(runes[:responsePreviewMaxLen]) + "..."
	}
	return preview
}
