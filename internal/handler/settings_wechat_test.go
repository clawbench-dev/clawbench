package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeConfig_Patch_PushModeWechatAccepted(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"push_mode":"wechat"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "wechat", model.ConfigInstance.PushMode)
	// The other channels must be switched off by the same write.
	assert.False(t, model.ConfigInstance.DingTalk.Enabled)
	assert.False(t, model.ConfigInstance.Feishu.Enabled)
}

func TestServeConfig_Patch_PushModeRejectsUnknown(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"push_mode":"telegram"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServeConfig_Patch_WeChatTokenRejectedByWhitelist pins the write-only
// design: the token is produced by the QR login endpoint, never typed into the
// generic config PATCH. Accepting it here would create a second, unvalidated
// write path for a credential the server owns.
func TestServeConfig_Patch_WeChatTokenRejectedByWhitelist(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	before := model.ConfigInstance.WeChat.BotToken

	body := `{"wechat":{"bot_token":"injected"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code,
		"wechat.bot_token must not be patchable")
	assert.Equal(t, before, model.ConfigInstance.WeChat.BotToken,
		"a rejected patch must not mutate the config")
}

// TestServeConfig_Get_WeChatReportsHasTokenOnly documents the read shape.
func TestServeConfig_Get_WeChatReportsHasTokenOnly(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.PushMode = "wechat"
	model.ConfigInstance.WeChat = model.WeChatConfig{BotToken: "t", BotID: "b", UserID: "u"}

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	wechat, ok := resp["wechat"].(map[string]any)
	require.True(t, ok, "the response must carry a wechat block")
	assert.Equal(t, true, wechat["has_token"])
	assert.Equal(t, "b", wechat["bot_id"])
	assert.Equal(t, "u", wechat["user_id"])
	assert.NotContains(t, wechat, "bot_token")
}
