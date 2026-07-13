//nolint:noctx // HTTP client context handled internally
package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	// dingtalkRobotSendURL is the DingTalk API for sending robot single-chat messages.
	// POST /v1.0/robot/oToMessages/batchSend
	// Uses msgKey + msgParam (JSON string) format, not the work notification "msg" format.
	dingtalkRobotSendURL = "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"

	// msgKeyMarkdown is the message key for Markdown messages.
	msgKeyMarkdown = "sampleMarkdown"
)

// robotSendResult is the DingTalk robot send API response.
type robotSendResult struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestid,omitempty"`
}

// SendMarkdownMessage sends a Markdown message to a DingTalk user via robot single-chat.
// It uses the /v1.0/robot/oToMessages/batchSend API.
// userID must be a real staffId, NOT the encrypted $:LWCP_v1:$ format.
func (m *Manager) SendMarkdownMessage(ctx context.Context, userID, title, markdown string) error {
	token, err := m.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("dingtalk: get token: %w", err)
	}

	// Build the request body for robot single-chat API.
	// msgParam must be a JSON string (not a nested object).
	msgParam, err := json.Marshal(map[string]string{
		"title": title,
		"text":  markdown,
	})
	if err != nil {
		return fmt.Errorf("dingtalk: marshal msg_param: %w", err)
	}
	reqBody := map[string]any{
		"robotCode": m.cfg.AppKey,
		"userIds":   []string{userID},
		"msgKey":    msgKeyMarkdown,
		"msgParam":  string(msgParam),
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dingtalkRobotSendURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("dingtalk: send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: send fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("dingtalk: send read: %w", err)
	}

	// Handle token expiration
	if resp.StatusCode == 401 {
		InvalidateToken()
		return fmt.Errorf("dingtalk: token expired (401), invalidated for retry")
	}

	var result robotSendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("dingtalk: send parse: %w", err)
	}

	if result.Code != "" && result.Code != "0" {
		// Token invalid — invalidate for retry
		if result.Code == "InvalidAuthentication" {
			InvalidateToken()
		}
		return fmt.Errorf("dingtalk: send error: %s (code %s)", result.Message, result.Code)
	}

	slog.Debug("dingtalk: message sent", "user_id", userID)
	return nil
}
