package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

var (
	// dingtalkTokenURL is the DingTalk API for getting an access token.
	//nolint:gosec // G101: this is a public API endpoint URL, not a credential
	dingtalkTokenURL = "https://oapi.dingtalk.com/gettoken"
	// tokenRefreshBuffer is how long before expiration we refresh.
	tokenRefreshBuffer = 5 * time.Minute
)

// tokenResponse is the DingTalk gettoken API response.
type tokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
}

// getAccessToken returns a valid DingTalk access token, refreshing if needed.
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

	url := fmt.Sprintf("%s?appkey=%s&appsecret=%s", dingtalkTokenURL, m.cfg.AppKey, m.cfg.AppSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("dingtalk: token request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("dingtalk: token fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("dingtalk: token read: %w", err)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("dingtalk: token parse: %w", err)
	}

	if tokenResp.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk: token error: %s (code %d)", tokenResp.ErrMsg, tokenResp.ErrCode)
	}

	m.cachedToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		m.cachedExp = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		m.cachedExp = time.Now().Add(2 * time.Hour) // default 2h
	}

	slog.Debug("dingtalk: token refreshed", "expires_in", tokenResp.ExpiresIn)
	return m.cachedToken, nil
}

// invalidateToken clears the cached token (e.g., on 401 errors).
func (m *Manager) invalidateToken() {
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	m.cachedToken = ""
	m.cachedExp = time.Time{}
}

// InvalidateToken clears the global token cache. Kept for backward compatibility.
//
// Deprecated: use m.invalidateToken() instead.
func InvalidateToken() {
	mgr := GetManager()
	if mgr != nil {
		mgr.invalidateToken()
	}
}
