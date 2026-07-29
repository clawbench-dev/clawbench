package feishu

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

var (
	// feishuTokenURL is the Feishu API for getting a tenant access token.
	//nolint:gosec // G101: this is a public API endpoint URL, not a credential
	feishuTokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"

	// tokenRefreshBuffer is how long before expiration we refresh.
	tokenRefreshBuffer = 5 * time.Minute
)

// feishuTokenResponse is the Feishu tenant_access_token API response.
type feishuTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // seconds
}

// getAccessToken returns a valid Feishu tenant access token, refreshing if needed.
func (m *Manager) getAccessToken(ctx context.Context) (string, error) {
	m.tokenMu.RLock()
	if m.cachedToken != "" && time.Now().Add(tokenRefreshBuffer).Before(m.cachedExp) {
		token := m.cachedToken
		m.tokenMu.RUnlock()
		return token, nil
	}
	m.tokenMu.RUnlock()

	// Need to refresh
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()

	// Double-check after acquiring write lock
	if m.cachedToken != "" && time.Now().Add(tokenRefreshBuffer).Before(m.cachedExp) {
		return m.cachedToken, nil
	}

	reqBody := map[string]string{
		"app_id":     m.cfg.AppID,
		"app_secret": m.cfg.AppSecret,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("feishu: token marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuTokenURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("feishu: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("feishu: token fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("feishu: token read: %w", err)
	}

	var tokenResp feishuTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("feishu: token parse: %w", err)
	}

	if tokenResp.Code != 0 {
		return "", fmt.Errorf("feishu: token error: %s (code %d)", tokenResp.Msg, tokenResp.Code)
	}

	m.cachedToken = tokenResp.TenantAccessToken
	if tokenResp.Expire > 0 {
		m.cachedExp = time.Now().Add(time.Duration(tokenResp.Expire) * time.Second)
	} else {
		m.cachedExp = time.Now().Add(2 * time.Hour) // default 2h
	}

	slog.Debug("feishu: token refreshed", "expire", tokenResp.Expire)
	return m.cachedToken, nil
}

// invalidateToken clears the cached token (e.g., on 401 errors).
func (m *Manager) invalidateToken() {
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	m.cachedToken = ""
	m.cachedExp = time.Time{}
}

// InvalidateToken clears the global token cache. Kept for backward compatibility
// with Manager.Stop() which calls it before the per-instance refactor.
// Deprecated: use m.invalidateToken() instead.
func InvalidateToken() {
	mgr := GetManager()
	if mgr != nil {
		mgr.invalidateToken()
	}
}
