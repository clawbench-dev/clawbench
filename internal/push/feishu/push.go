package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"clawbench/internal/push/common"
)

// feishuPreviewMaxRunes is the maximum number of runes for the response
// preview in Feishu post messages. Feishu post supports ~40000 bytes;
// 4000 runes (~12000 bytes for CJK) leaves room for the message template.
const feishuPreviewMaxRunes = 4000

// truncateForFeishu truncates text to feishuPreviewMaxRunes with
// ellipsis if needed.
func truncateForFeishu(text string) string {
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= feishuPreviewMaxRunes {
		return text
	}
	return string([]rune(text)[:feishuPreviewMaxRunes]) + "…"
}

// PushSessionEvent sends a Feishu push notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath, toolName, toolInput string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, content string
	shortID := common.ShortSessionID(sessionID)

	switch status {
	case "completed":
		title = common.T("PushSessionCompleted")
		replyHint := "\n\n---\n" + common.T("PushReplyHintSend", map[string]any{"ID": shortID})
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s%s",
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForFeishu(responsePreview),
			replyHint)
	case "cancelled":
		title = common.T("PushSessionCancelled")
		replyHint := "\n\n---\n" + common.T("PushReplyHintSend", map[string]any{"ID": shortID})
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s%s",
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForFeishu(responsePreview),
			replyHint)
	case "permission_pending":
		title = common.T("PushActionRequired")
		replyHint := "\n\n---\n" + common.T("PushReplyHintQueue", map[string]any{"ID": shortID})
		detail := formatPermissionDetail(toolName, toolInput)
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s%s",
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			detail,
			replyHint)
	default:
		return false
	}

	return sendToAllSubscribers(title, content)
}

// formatPermissionDetail formats toolName and toolInput for permission approval notifications.
func formatPermissionDetail(toolName, toolInput string) string {
	var detail string
	if toolName != "" {
		detail += fmt.Sprintf("**%s**: %s\n\n", common.T("PushLabelAction"), toolName)
	}
	if toolInput != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolInput), &parsed); err == nil {
			if command, _ := parsed["command"].(string); command != "" {
				detail += fmt.Sprintf("**%s**: `%s`\n\n", common.T("PushLabelCommand"), command)
			}
			filePath, _ := parsed["file_path"].(string)
			if filePath == "" {
				filePath, _ = parsed["path"].(string)
			}
			if filePath != "" {
				detail += fmt.Sprintf("**%s**: `%s`\n\n", common.T("PushLabelFile"), filePath)
			}
		}
	}
	return detail
}

// PushTaskEvent sends a Feishu push notification for a task event.
func PushTaskEvent(taskID, status, taskName, responsePreview, projectPath string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, content string

	switch status {
	case "running":
		title = common.T("PushTaskStarted")
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s",
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath)
	case "completed":
		title = common.T("PushTaskCompleted")
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s",
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForFeishu(responsePreview))
	case "failed":
		title = common.T("PushTaskFailed")
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s",
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForFeishu(responsePreview))
	case "cancelled":
		title = common.T("PushTaskCancelled")
		content = fmt.Sprintf("**%s**: %s\n**%s**: %s\n\n%s",
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForFeishu(responsePreview))
	default:
		return false
	}

	return sendToAllSubscribers(title, content)
}

// sendToAllSubscribers sends a notification to all subscribed users directly.
// Suppresses push when a WebSocket client is connected (user is viewing the UI).
// Returns true if at least one subscriber received the notification.
func sendToAllSubscribers(title, content string) bool {
	// Only push in feishu mode
	mode := GetPushMode()
	if mode != "feishu" {
		return false
	}

	// Suppress Feishu push when a client is actively connected via WebSocket.
	if clientChecker != nil && clientChecker.HasConnectedClients() {
		slog.Debug("feishu: suppressing push, client is connected", "title", title)
		return false
	}

	subscribers, err := db.GetSubscribers()
	if err != nil {
		slog.Warn("feishu: get subscribers failed", "error", err)
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
		if err := mgr.SendPostMessage(ctx, sub.UserID, title, content); err != nil {
			slog.Warn("feishu: send failed", "error", err, "user_id", sub.UserID)
		} else {
			sent++
		}
	}

	slog.Debug("feishu: sent notifications", "sent", sent, "total", len(subscribers), "title", title)
	return sent > 0
}

// PushForgeEvent sends a Feishu notification for a GitHub/GitLab event.
// title/body are provided by the service layer, which owns the event wording;
// this function only owns the transport and the push-mode gate.
func PushForgeEvent(title, content string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || content == "" {
		return false
	}
	return sendToAllSubscribers(title, content)
}

// PushUsageMeterEvent sends a Feishu notification for a usage-meter threshold
// crossing. Like PushForgeEvent, the wording is owned by the service layer;
// this function only owns the transport and the push-mode gate.
func PushUsageMeterEvent(title, content string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || content == "" {
		return false
	}
	return sendToAllSubscribers(title, content)
}
