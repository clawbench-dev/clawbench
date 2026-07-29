package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

var (
	// feishuMessageURL is the Feishu API for sending messages.
	// receive_id_type=open_id means receive_id is an open_id.
	feishuMessageURL = "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id"
)

// feishuMessageResponse is the Feishu send message API response.
type feishuMessageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// SendPostMessage sends a post (rich text) message to a Feishu user via bot single-chat.
// Uses the /open-apis/im/v1/messages API with msg_type="post".
// On 401 (token expired), it invalidates the cached token and retries once.
func (m *Manager) SendPostMessage(ctx context.Context, openID, title, content string) error {
	err := m.sendPostMessageOnce(ctx, openID, title, content)
	if err == nil {
		return nil
	}

	// If the error is due to a 401, invalidate token and retry once
	if isTokenError(err) {
		slog.Info("feishu: token expired, retrying with fresh token")
		m.invalidateToken()
		return m.sendPostMessageOnce(ctx, openID, title, content)
	}

	return err
}

// isTokenError checks if the error was caused by a 401 response.
func isTokenError(err error) bool {
	return err != nil && (err.Error() == "feishu: token expired (401)" ||
		bytes.Contains([]byte(err.Error()), []byte("401")))
}

// sendPostMessageOnce attempts a single send. Returns a 401 sentinel error
// if the response status is 401, so the caller can retry.
func (m *Manager) sendPostMessageOnce(ctx context.Context, openID, title, content string) error {
	token, err := m.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("feishu: get token: %w", err)
	}

	// Build post content in Feishu's rich text format.
	// Each content row is an array of content elements.
	// We use a single text element per row for simplicity.
	contentJSON, err := json.Marshal(map[string]any{
		"zh_cn": map[string]any{
			"title":   title,
			"content": [][]map[string]any{
				{{"tag": "text", "text": content}},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("feishu: marshal content: %w", err)
	}

	reqBody := map[string]any{
		"receive_id": openID,
		"msg_type":   "post",
		"content":    string(contentJSON),
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("feishu: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuMessageURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("feishu: send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: send fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("feishu: send read: %w", err)
	}

	// Handle token expiration — return sentinel for retry
	if resp.StatusCode == 401 {
		return fmt.Errorf("feishu: token expired (401)")
	}

	var result feishuMessageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("feishu: send parse: %w", err)
	}

	if result.Code != 0 {
		return fmt.Errorf("feishu: send error: %s (code %d)", result.Msg, result.Code)
	}

	slog.Debug("feishu: message sent", "open_id", openID)
	return nil
}
