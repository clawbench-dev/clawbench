package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// PushSessionEvent sends a DingTalk push notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath, toolName string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string
	shortID := shortSessionID(sessionID)

	switch status {
	case "completed":
		title = "会话已完成"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已完成\n**会话**: %s\n\n**项目**: %s\n\n%s%s",
			sessionTitle,
			projectPath,
			responsePreview,
			replyHint)
	case "cancelled":
		title = "会话已取消"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已取消\n**会话**: %s\n\n**项目**: %s\n\n%s%s",
			sessionTitle,
			projectPath,
			responsePreview,
			replyHint)
	case "permission_pending":
		title = "操作需批准"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 追加消息到队列", shortID)
		markdown = fmt.Sprintf("### 操作需批准\n**会话**: %s\n\n**项目**: %s\n\n**操作**: %s%s",
			sessionTitle,
			projectPath,
			toolName,
			replyHint)
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}

// PushTaskEvent sends a DingTalk push notification for a task event.
// Only processes started/completed/failed/cancelled statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushTaskEvent(taskID, status, taskName, responsePreview, projectPath string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string

	switch status {
	case "running":
		title = "定时任务已启动"
		markdown = fmt.Sprintf("### 定时任务已启动\n**任务**: %s\n\n**项目**: %s",
			taskName,
			projectPath)
	case "completed":
		title = "定时任务已完成"
		markdown = fmt.Sprintf("### 定时任务已完成\n**任务**: %s\n\n**项目**: %s\n\n%s",
			taskName,
			projectPath,
			responsePreview)
	case "failed":
		title = "定时任务失败"
		markdown = fmt.Sprintf("### 定时任务失败\n**任务**: %s\n\n**项目**: %s\n\n%s",
			taskName,
			projectPath,
			responsePreview)
	case "cancelled":
		title = "定时任务已取消"
		markdown = fmt.Sprintf("### 定时任务已取消\n**任务**: %s\n\n**项目**: %s\n\n%s",
			taskName,
			projectPath,
			responsePreview)
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

// shortSessionID returns the first 8 characters of a session ID for display.
// Session IDs are always ASCII hex (UUID format), so byte slicing is safe.
func shortSessionID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
