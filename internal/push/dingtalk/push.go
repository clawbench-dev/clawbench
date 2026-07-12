package dingtalk

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"clawbench/internal/model"
)

const (
	responsePreviewMaxLen = 200
)

// PushSessionEvent sends a DingTalk push notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath string) {
	if !IsStarted() || db == nil {
		return
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
		return
	}

	enqueueForAllSubscribers(title, markdown)
}

// PushTaskEvent sends a DingTalk push notification for a task event.
// Only processes completed/failed/cancelled statuses.
func PushTaskEvent(taskID, status, taskName, responsePreview, projectPath string) {
	if !IsStarted() || db == nil {
		return
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
		return
	}

	enqueueForAllSubscribers(title, markdown)
}

// enqueueForAllSubscribers creates outbox entries for all subscribed users.
func enqueueForAllSubscribers(title, markdown string) {
	subscribers, err := db.GetSubscribers()
	if err != nil {
		slog.Warn("dingtalk: get subscribers failed", "error", err)
		return
	}
	if len(subscribers) == 0 {
		return
	}

	msgParam := map[string]string{
		"title": title,
		"text":  markdown,
	}
	msgParamJSON, err := json.Marshal(msgParam)
	if err != nil {
		slog.Warn("dingtalk: marshal msg_param failed", "error", err)
		return
	}

	maxRetries := model.ConfigInstance.DingTalk.MaxRetries
	for _, sub := range subscribers {
		if err := db.EnqueueMessage(sub.UserID, msgKeyMarkdown, string(msgParamJSON), maxRetries); err != nil {
			slog.Warn("dingtalk: enqueue failed", "error", err, "user_id", sub.UserID)
		}
	}

	slog.Debug("dingtalk: enqueued notifications", "count", len(subscribers), "title", title)
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
