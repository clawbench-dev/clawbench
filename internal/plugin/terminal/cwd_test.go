package terminal

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// resolveTerminalCwd 的两条拒绝分支：绝对路径不在允许的根下、相对路径越界。
// 漏掉它们意味着终端能开在项目外的任意目录。
func TestResolveTerminalCwd_RejectsPathsOutsideRoots(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	// 绝对路径、但不在 model.RootPaths 里。用平台自己的绝对路径："/etc" 在
	// Windows 上不是绝对路径（没有盘符），会被当成相对路径拼到项目下。
	outside := t.TempDir()
	w := httptest.NewRecorder()
	_, ok := resolveTerminalCwd(w, httptest.NewRequest(http.MethodGet, "/api/terminal/status?cwd="+url.QueryEscape(outside), http.NoBody), env.ProjectDir)
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 相对路径越界
	w = httptest.NewRecorder()
	_, ok = resolveTerminalCwd(w, httptest.NewRequest(http.MethodGet, "/api/terminal/status?cwd=../../etc", http.NoBody), env.ProjectDir)
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 空值 = 项目根
	w = httptest.NewRecorder()
	got, ok := resolveTerminalCwd(w, httptest.NewRequest(http.MethodGet, "/api/terminal/status", http.NoBody), env.ProjectDir)
	assert.True(t, ok)
	assert.Equal(t, env.ProjectDir, got)
}

// PUT /api/terminal/quick-commands/reorder with no ids is a no-op 200: the
// client sends the whole list, and an empty one means "nothing to reorder".
func TestServeQuickCommands_ReorderEmptyIsANoop(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/reorder", map[string]any{"ids": []int64{}})
	w := callHandler(ServeQuickCommands, req)
	assertOK(t, w)
	assert.Contains(t, w.Body.String(), `"success":true`)
}

// 终端没启用时 WebSocket 入口必须是 503（而不是去升级连接）：前端据此显示
// "终端不可用"。
func TestTerminalWebSocket_UnavailableWhenManagerMissing(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	orig := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(orig) })
	SetTerminalManager(nil)

	w := callHandler(TerminalWebSocket, newRequest(t, http.MethodGet, "/api/terminal/ws", nil))
	assertStatus(t, w, http.StatusServiceUnavailable)
}
