package push

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
)

type JPushConfig = model.JPushConfig

type JPushClient struct {
	mu           sync.Mutex
	enabled      bool
	appKey       string
	masterSecret string
	httpClient   *http.Client
	baseURL      string // overridable for testing
}

func NewJPushClient(cfg JPushConfig) *JPushClient {
	return &JPushClient{
		enabled:      cfg.Enabled,
		appKey:       cfg.AppKey,
		masterSecret: cfg.MasterSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		baseURL:      "https://api.jpush.cn",
	}
}

// SetBaseURL overrides the JPush API base URL. For testing only.
func (c *JPushClient) SetBaseURL(url string) {
	c.baseURL = url
}

// Reconfigure updates the JPush client configuration at runtime.
// Goroutine-safe: concurrent SendNotification calls are protected by mutex.
func (c *JPushClient) Reconfigure(cfg JPushConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = cfg.Enabled
	c.appKey = cfg.AppKey
	c.masterSecret = cfg.MasterSecret
}

func (c *JPushClient) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled && c.appKey != "" && c.masterSecret != ""
}

// AppKey returns the configured JPush AppKey (may be empty if push is not configured).
func (c *JPushClient) AppKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appKey
}

func (c *JPushClient) SendNotification(registrationID, title, alert string, extras map[string]string) error {
	c.mu.Lock()
	enabled := c.enabled && c.appKey != "" && c.masterSecret != ""
	appKey := c.appKey
	masterSecret := c.masterSecret
	c.mu.Unlock()

	if !enabled {
		return nil
	}
	if registrationID == "" {
		return fmt.Errorf("jpush: empty registration ID")
	}

	payload := map[string]any{
		"platform": "android",
		"audience": map[string]any{
			"registration_id": []string{registrationID},
		},
		"notification": map[string]any{
			"android": map[string]any{
				"alert":  alert,
				"title":  title,
				"extras": extras,
			},
		},
		"options": map[string]any{
			"time_to_live": 86400,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jpush: marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v3/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("jpush: create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(appKey + ":" + masterSecret))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	slog.Debug("jpush: sending notification", "reg_id", registrationID, "title", title, "alert", alert, "extras", extras)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jpush: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		slog.Error("jpush: push failed", "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("jpush: server returned %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Debug("jpush: notification sent", "reg_id", registrationID, "title", title)
	return nil
}
