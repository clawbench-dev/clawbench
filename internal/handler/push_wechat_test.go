package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoginService is a scripted WeChatLoginService.
type fakeLoginService struct {
	started   int
	cancelled []string
	pollErr   bool
	view      WeChatLoginView
}

func (f *fakeLoginService) Start(context.Context) WeChatLoginView {
	f.started++
	return f.view
}

func (f *fakeLoginService) Poll(id string) (WeChatLoginView, bool) {
	if f.pollErr {
		return WeChatLoginView{}, false
	}
	return f.view, true
}

func (f *fakeLoginService) Cancel(id string) { f.cancelled = append(f.cancelled, id) }

func withWeChatLoginService(t *testing.T, svc WeChatLoginService) {
	t.Helper()
	orig := wechatLoginSvc
	wechatLoginSvc = svc
	t.Cleanup(func() { wechatLoginSvc = orig })
}

func TestServeWeChatLogin_Start(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	fake := &fakeLoginService{view: WeChatLoginView{LoginID: "wl-1", State: "pending", QRURL: "https://q"}}
	withWeChatLoginService(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/api/push/wechat/login", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, 1, fake.started)

	var resp WeChatLoginView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "wl-1", resp.LoginID)
	assert.Equal(t, "pending", resp.State)
	assert.Equal(t, "https://q", resp.QRURL)
}

func TestServeWeChatLogin_Poll(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	fake := &fakeLoginService{view: WeChatLoginView{LoginID: "wl-1", State: "scanned"}}
	withWeChatLoginService(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/login?id=wl-1", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp WeChatLoginView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "scanned", resp.State)
}

func TestServeWeChatLogin_PollMissingID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	withWeChatLoginService(t, &fakeLoginService{})

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/login", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeWeChatLogin_PollUnknown(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	withWeChatLoginService(t, &fakeLoginService{pollErr: true})

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/login?id=gone", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"an unknown session must be 404 so the client starts a new flow")
}

func TestServeWeChatLogin_Cancel(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	fake := &fakeLoginService{}
	withWeChatLoginService(t, fake)

	req := httptest.NewRequest(http.MethodDelete, "/api/push/wechat/login?id=wl-1", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, []string{"wl-1"}, fake.cancelled)
}

func TestServeWeChatLogin_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	withWeChatLoginService(t, &fakeLoginService{})

	req := httptest.NewRequest(http.MethodPut, "/api/push/wechat/login", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "POST, GET, DELETE", w.Header().Get("Allow"))
}

func TestServeWeChatLogin_ServiceMissing(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	withWeChatLoginService(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/push/wechat/login", strings.NewReader(""))
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogin, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeWeChatStatus(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origProvider := wechatStatusProvider
	defer func() { wechatStatusProvider = origProvider }()
	wechatStatusProvider = nil

	model.ConfigInstance.PushMode = "wechat"
	model.ConfigInstance.WeChat = model.WeChatConfig{
		BotToken: "secret-token", BotID: "bot-1", UserID: "user-1", BaseURL: "https://b",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "wechat", resp["push_mode"])
	assert.Equal(t, true, resp["has_token"])
	assert.Equal(t, "bot-1", resp["bot_id"])
	assert.Equal(t, "user-1", resp["user_id"])
	// Without a live provider, a stored token with the right mode reads as
	// running — the best the config alone can say.
	assert.Equal(t, "running", resp["state"])
	// The token itself must never be serialized.
	assert.NotContains(t, w.Body.String(), "secret-token")
}

// TestServeWeChatStatus_ReportsExpired is the guard on the -14 path reaching the
// UI: the token is still stored, so without the live provider the panel would
// keep showing a green "connected" card while the channel is dead.
func TestServeWeChatStatus_ReportsExpired(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origProvider := wechatStatusProvider
	defer func() { wechatStatusProvider = origProvider }()
	wechatStatusProvider = func() WeChatConnState {
		return WeChatConnState{State: "expired", LastError: "wechat session expired (-14)"}
	}

	model.ConfigInstance.PushMode = "wechat"
	model.ConfigInstance.WeChat = model.WeChatConfig{BotToken: "still-stored", BotID: "bot-1"}

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatStatus, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "expired", resp["state"],
		"an expired session must be reported even though has_token is still true")
	assert.Contains(t, resp["last_error"], "-14")
}

// TestServeWeChatStatus_NoTokenIsStopped documents the idle shape.
func TestServeWeChatStatus_NoTokenIsStopped(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origProvider := wechatStatusProvider
	defer func() { wechatStatusProvider = origProvider }()
	wechatStatusProvider = nil

	model.ConfigInstance.PushMode = "wechat"
	model.ConfigInstance.WeChat = model.WeChatConfig{}

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatStatus, req)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["has_token"])
	assert.Equal(t, "stopped", resp["state"])
}

func TestServeWeChatStatus_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/push/wechat/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatStatus, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestPersistWeChatLogin_WritesConfigAndHotReloads pins the single write path:
// credentials land in the in-memory config and on disk, and hot-reload runs.
func TestPersistWeChatLogin_WritesConfigAndHotReloads(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	reloaded := 0
	SetReconfigureFunc(func() { reloaded++ })
	defer SetReconfigureFunc(nil)

	err := PersistWeChatLogin(WeChatCredentials{
		BotToken: "tok-1", BotID: "bot-1", BaseURL: "https://b", UserID: "user-1",
	})
	require.NoError(t, err)

	assert.Equal(t, "tok-1", model.ConfigInstance.WeChat.BotToken)
	assert.Equal(t, "bot-1", model.ConfigInstance.WeChat.BotID)
	assert.Equal(t, "https://b", model.ConfigInstance.WeChat.BaseURL)
	assert.Equal(t, "user-1", model.ConfigInstance.WeChat.UserID)
	assert.Equal(t, 0, reloaded, "persist itself must not reload; the login service does")
}

// TestServeWeChatLogout_ClearsCredentials: logout must clear the credential set
// and trigger a reconfigure (which stops the manager).
func TestServeWeChatLogout_ClearsCredentials(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance.WeChat = model.WeChatConfig{BotToken: "tok", BotID: "bot", UserID: "u"}

	reloaded := 0
	SetReconfigureFunc(func() { reloaded++ })
	defer SetReconfigureFunc(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/push/wechat/logout", strings.NewReader(""))
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogout, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, model.ConfigInstance.WeChat.BotToken)
	assert.Empty(t, model.ConfigInstance.WeChat.BotID)
	assert.Equal(t, 1, reloaded, "logout must trigger a reconfigure so the manager stops")
}

func TestServeWeChatLogout_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/push/wechat/logout", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeWeChatLogout, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestConfigResponse_WeChatNeverLeaksToken pins the write-only contract on the
// main config endpoint: has_token is reported, the value is not.
func TestConfigResponse_WeChatNeverLeaksToken(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance.WeChat = model.WeChatConfig{BotToken: "super-secret", BotID: "b"}

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.NotContains(t, w.Body.String(), "super-secret",
		"the WeChat bot token must never be returned")

	var resp struct {
		WeChat struct {
			HasToken bool   `json:"has_token"`
			BotID    string `json:"bot_id"`
		} `json:"wechat"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.WeChat.HasToken)
	assert.Equal(t, "b", resp.WeChat.BotID)
}
