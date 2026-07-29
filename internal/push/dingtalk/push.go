package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"clawbench/internal/push/common"
)

// dingtalkPreviewMaxRunes is the maximum number of runes for the response
// preview in DingTalk Markdown messages. DingTalk sampleMarkdown supports
// ~20000 bytes; 4000 runes (~12000 bytes for CJK) leaves room for the
// message template and reply hint.
const dingtalkPreviewMaxRunes = 4000

// truncateForDingTalk truncates text to dingtalkPreviewMaxRunes with
// ellipsis if needed. Truncates by rune boundary to avoid corrupted characters.
func truncateForDingTalk(text string) string {
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= dingtalkPreviewMaxRunes {
		return text
	}
	return string([]rune(text)[:dingtalkPreviewMaxRunes]) + "…"
}

// PushSessionEvent sends a DingTalk push notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath, toolName, toolInput string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string
	shortID := common.ShortSessionID(sessionID)

	switch status {
	case "completed":
		title = "会话已完成"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已完成\n**会话**: %s\n\n**项目**: %s\n\n%s%s",
			sessionTitle,
			projectPath,
			truncateForDingTalk(responsePreview),
			replyHint)
	case "cancelled":
		title = "会话已取消"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已取消\n**会话**: %s\n\n**项目**: %s\n\n%s%s",
			sessionTitle,
			projectPath,
			truncateForDingTalk(responsePreview),
			replyHint)
	case "permission_pending":
		title = "操作需批准"
		replyHint := fmt.Sprintf("\n\n---\n发送 `@%s <消息>` 追加消息到队列", shortID)
		detail := formatPermissionDetail(toolName, toolInput)
		markdown = fmt.Sprintf("### 操作需批准\n**会话**: %s\n\n**项目**: %s\n\n%s%s",
			sessionTitle,
			projectPath,
			detail,
			replyHint)
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}

// formatPermissionDetail formats toolName and toolInput into DingTalk Markdown
// for permission approval notifications. Parses toolInput JSON to extract
// command and file_path, matching the frontend PermissionApproval card logic.
func formatPermissionDetail(toolName, toolInput string) string {
	var detail string
	if toolName != "" {
		detail += fmt.Sprintf("**操作**: %s\n\n", toolName)
	}
	if toolInput != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolInput), &parsed); err == nil {
			if command, _ := parsed["command"].(string); command != "" {
				detail += fmt.Sprintf("**命令**: `%s`\n\n", command)
			}
			filePath, _ := parsed["file_path"].(string)
			if filePath == "" {
				filePath, _ = parsed["path"].(string)
			}
			if filePath != "" {
				detail += fmt.Sprintf("**文件**: `%s`\n\n", filePath)
			}
		}
	}
	return detail
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
			truncateForDingTalk(responsePreview))
	case "failed":
		title = "定时任务失败"
		markdown = fmt.Sprintf("### 定时任务失败\n**任务**: %s\n\n**项目**: %s\n\n%s",
			taskName,
			projectPath,
			truncateForDingTalk(responsePreview))
	case "cancelled":
		title = "定时任务已取消"
		markdown = fmt.Sprintf("### 定时任务已取消\n**任务**: %s\n\n**项目**: %s\n\n%s",
			taskName,
			projectPath,
			truncateForDingTalk(responsePreview))
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}

// sendToAllSubscribers sends a notification to all subscribed users directly.
// Fire-and-forget: success or failure is logged, no retry or DB persistence.
// Suppresses push when a WebSocket client is connected (user is viewing the UI).
// Returns true if at least one subscriber received the notification.
func sendToAllSubscribers(title, markdown string) bool {
	// Only push in dingtalk mode
	mode := GetPushMode()
	if mode != "dingtalk" {
		return false
	}

	// Suppress DingTalk push when a client is actively connected via WebSocket.
	// Consistent with native notification logic: if the user is viewing the UI,
	// no push notification is needed. HasConnectedClients() returns true when
	// at least one browser/WebView WS is active (i.e., user is in foreground).
	//
	// NOTE: On desktop browsers, WS connections persist when the tab is backgrounded,
	// so DingTalk notifications may be over-suppressed. This is acceptable because:
	// 1. On Android (primary DingTalk use case), WS disconnects on background,
	//    so foreground detection is accurate.
	// 2. On desktop, native browser notifications are the primary channel
	//    (push_mode=native), so DingTalk over-suppression is a non-issue.
	if clientChecker != nil && clientChecker.HasConnectedClients() {
		slog.Debug("dingtalk: suppressing push, client is connected", "title", title)
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
