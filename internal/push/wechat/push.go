package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/push/common"
)

// PushSessionEvent sends a WeChat notification for a session event.
// Only processes completed/cancelled/permission_pending statuses.
// Returns true if the notification was sent to at least one subscriber.
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath, toolName, toolInput string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var body string
	shortID := common.ShortSessionID(sessionID)

	switch status {
	case "completed":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s\n\n---\n%s",
			common.T("PushSessionCompleted"),
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForWeChat(responsePreview),
			common.T("PushReplyHintSend", map[string]any{"ID": shortID}))
	case "cancelled":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s\n\n---\n%s",
			common.T("PushSessionCancelled"),
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			truncateForWeChat(responsePreview),
			common.T("PushReplyHintSend", map[string]any{"ID": shortID}))
	case "permission_pending":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s\n\n---\n%s",
			common.T("PushActionRequired"),
			common.T("PushLabelSession"), sessionTitle,
			common.T("PushLabelProject"), projectPath,
			formatPermissionDetail(toolName, toolInput),
			common.T("PushReplyHintQueue", map[string]any{"ID": shortID}))
	default:
		return false
	}

	return sendToAllSubscribers(body)
}

// formatPermissionDetail renders toolName and toolInput as plain text for a
// permission approval notification.
//
// Each backend owns its own copy (DingTalk emits sampleMarkdown, Feishu emits a
// card): WeChat has no Markdown at all, so the markers are omitted rather than
// carried through and stripped later.
func formatPermissionDetail(toolName, toolInput string) string {
	var detail string
	if toolName != "" {
		detail += fmt.Sprintf("%s: %s\n", common.T("PushLabelAction"), toolName)
	}
	if toolInput != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolInput), &parsed); err == nil {
			if command, _ := parsed["command"].(string); command != "" {
				detail += fmt.Sprintf("%s: %s\n", common.T("PushLabelCommand"), command)
			}
			filePath, _ := parsed["file_path"].(string)
			if filePath == "" {
				filePath, _ = parsed["path"].(string)
			}
			if filePath != "" {
				detail += fmt.Sprintf("%s: %s\n", common.T("PushLabelFile"), filePath)
			}
		}
	}
	return detail
}

// PushTaskEvent sends a WeChat notification for a task event.
func PushTaskEvent(taskID, status, taskName, responsePreview, projectPath string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var body string

	switch status {
	case "running":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s",
			common.T("PushTaskStarted"),
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath)
	case "completed":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s",
			common.T("PushTaskCompleted"),
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForWeChat(responsePreview))
	case "failed":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s",
			common.T("PushTaskFailed"),
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForWeChat(responsePreview))
	case "cancelled":
		body = fmt.Sprintf("%s\n%s: %s\n%s: %s\n\n%s",
			common.T("PushTaskCancelled"),
			common.T("PushLabelTask"), taskName,
			common.T("PushLabelProject"), projectPath,
			truncateForWeChat(responsePreview))
	default:
		return false
	}

	return sendToAllSubscribers(body)
}

// PushForgeEvent sends a WeChat notification for a GitHub/GitLab event.
// The wording is owned by the service layer; this function owns the transport
// and the push-mode gate.
func PushForgeEvent(title, content string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || content == "" {
		return false
	}
	return sendToAllSubscribers(joinTitleBody(title, content))
}

// PushUsageMeterEvent sends a WeChat notification for a usage-meter threshold
// crossing.
func PushUsageMeterEvent(title, content string) bool {
	if !IsStarted() || db == nil {
		return false
	}
	if title == "" || content == "" {
		return false
	}
	return sendToAllSubscribers(joinTitleBody(title, content))
}

// joinTitleBody composes a plain-text message from a service-provided title and
// body. WeChat has no card header to carry the title separately.
func joinTitleBody(title, content string) string {
	if title == "" {
		return stripMarkdownForWeChat(content)
	}
	return stripMarkdownForWeChat(title) + "\n\n" + stripMarkdownForWeChat(content)
}

// sendToAllSubscribers sends a notification to every subscribed user.
// Suppresses push when a WebSocket client is connected (the user is viewing the
// UI). Returns true if at least one subscriber received the notification.
func sendToAllSubscribers(body string) bool {
	if GetPushMode() != "wechat" {
		return false
	}

	// Suppress when a client is actively connected via WebSocket — consistent
	// with DingTalk/Feishu and with native notification behavior.
	if clientChecker != nil && clientChecker.HasConnectedClients() {
		slog.Debug("wechat: suppressing push, client is connected")
		return false
	}

	subscribers, err := db.GetSubscribers()
	if err != nil {
		slog.Warn("wechat: get subscribers failed", "error", err)
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
		// A subscriber without a context token is unreachable by protocol, not
		// a bug: the platform only mints tokens on inbound messages, so a row
		// from before this user's first message (or one cleared by logout)
		// simply cannot be pushed to.
		if sub.ContextToken == "" {
			slog.Debug("wechat: skipping subscriber", "reason", "no_context_token", "user_id", sub.UserID)
			continue
		}
		if err := mgr.SendText(ctx, sub.UserID, body, sub.ContextToken); err != nil {
			slog.Warn("wechat: send failed", "error", err, "user_id", sub.UserID)
		} else {
			sent++
		}
	}

	slog.Debug("wechat: sent notifications", "sent", sent, "total", len(subscribers))
	return sent > 0
}
