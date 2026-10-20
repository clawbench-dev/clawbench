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
		title = common.T("PushSessionCompleted")
		replyHint := "\n\n---\n" + common.T("PushReplyHintSend", map[string]any{"ID": shortID})
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s%s",
			title,
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForDingTalk(responsePreview),
			replyHint)
	case "cancelled":
		title = common.T("PushSessionCancelled")
		replyHint := "\n\n---\n" + common.T("PushReplyHintSend", map[string]any{"ID": shortID})
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s%s",
			title,
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForDingTalk(responsePreview),
			replyHint)
	case "permission_pending":
		title = common.T("PushActionRequired")
		replyHint := "\n\n---\n" + common.T("PushReplyHintQueue", map[string]any{"ID": shortID})
		detail := formatPermissionDetail(toolName, toolInput)
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s%s",
			title,
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
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
		title = common.T("PushTaskStarted")
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s",
			title,
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath)
	case "completed":
		title = common.T("PushTaskCompleted")
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s",
			title,
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForDingTalk(responsePreview))
	case "failed":
		title = common.T("PushTaskFailed")
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s",
			title,
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForDingTalk(responsePreview))
	case "cancelled":
		title = common.T("PushTaskCancelled")
		markdown = fmt.Sprintf("### %s\n**%s**: %s\n\n**%s**: %s\n\n%s",
			title,
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
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

// PushForgeEvent sends a DingTalk notification for a GitHub/GitLab event.
// title/body are provided by the service layer, which owns the event wording;
// this function only owns the transport and the push-mode gate.
func PushForgeEvent(title, markdown string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || markdown == "" {
		return false
	}
	return sendToAllSubscribers(title, markdown)
}

// PushUsageMeterEvent sends a DingTalk notification for a usage-meter threshold
// crossing. Like PushForgeEvent, the wording is owned by the service layer;
// this function only owns the transport and the push-mode gate.
func PushUsageMeterEvent(title, markdown string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || markdown == "" {
		return false
	}
	return sendToAllSubscribers(title, markdown)
}
