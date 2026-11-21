package forge

import (
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Start / Stop 是插件的生命周期钩子：Start 拉起轮询器并把三个 sink 接上，
// Stop 停掉它。两个都必须在「没有绑定任何仓库」的空库上安全（生产上功能
// 默认是开的，但没人绑仓库）。
func TestStartStopHooks(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	plugins.Reset()
	t.Cleanup(plugins.Reset)
	Register()

	p := plugins.Lookup("forge")
	require.NotNil(t, p)
	require.NotNil(t, p.Start)
	require.NotNil(t, p.Stop)

	p.Start(model.Config{})
	p.Stop()
}

// 未登录访问 forge 接口必须 403；已登录但没绑定仓库时，列表接口报 404
// （而不是空列表）——前端靠这个区分"没绑"和"绑了但没数据"。
func TestServeForgeItems_RequiresProjectAndBinding(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	w := callHandler(ServeForgeItems, newRequest(t, http.MethodGet, "/api/forge/items", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/forge/items", nil), env.ProjectDir)
	w = callHandler(ServeForgeItems, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), errNoRepositoryBound)
}

// ServeForgeItem 只接受 GET；用错方法必须 405，而不是走到"未绑定"的 404。
func TestServeForgeItem_RejectsNonGet(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	w := callHandler(ServeForgeItem, newRequest(t, http.MethodPost, "/api/forge/item", map[string]any{}))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
