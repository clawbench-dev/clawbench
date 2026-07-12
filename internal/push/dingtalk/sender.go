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
	// dingtalkSendMsgURL is the DingTalk API for sending single-chat messages
	// via enterprise internal application robot.
	// POST /topapi/message/corpconversation/asyncsend_v2
	dingtalkSendMsgURL = "https://oapi.dingtalk.com/topapi/message/corpconversation/asyncsend_v2"

	// msgKeyMarkdown is the message key for Markdown single-chat messages.
	msgKeyMarkdown = "sampleMarkdown"
)

// sendResult is the DingTalk send message API response.
type sendResult struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	TaskID  int64  `json:"task_id,omitempty"`
}

// SendMarkdownMessage sends a Markdown message to a DingTalk user via single-chat.
// It uses the corpconversation asyncsend_v2 API.
func (m *Manager) SendMarkdownMessage(ctx context.Context, userID, title, markdown string) error {
	token, err := m.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("dingtalk: get token: %w", err)
	}

	// Build the request body for asyncsend_v2
	// agent_id is the numeric application agent ID from DingTalk developer console.
	reqBody := map[string]any{
		"agent_id":    m.cfg.AgentID,
		"userid_list": userID,
		"msg_key":     msgKeyMarkdown,
		"msg_param": map[string]any{
			"title": title,
			"text":  markdown,
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s?access_token=%s", dingtalkSendMsgURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("dingtalk: send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

	var result sendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("dingtalk: send parse: %w", err)
	}

	if result.ErrCode != 0 {
		// Token invalid — invalidate for retry
		if result.ErrCode == 40014 || result.ErrCode == 42001 {
			InvalidateToken()
		}
		return fmt.Errorf("dingtalk: send error: %s (code %d)", result.ErrMsg, result.ErrCode)
	}

	slog.Debug("dingtalk: message sent", "user_id", userID, "task_id", result.TaskID)
	return nil
}
