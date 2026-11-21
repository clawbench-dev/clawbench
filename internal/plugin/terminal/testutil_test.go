package terminal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// 夹具本体在 internal/handlertest（各功能插件共用）。terminal 比它多要两张表：
// 命令历史与按键配置，都在终端接口的读写路径上。
type testEnv = handlertest.Env

const terminalExtraDDL = `
CREATE TABLE IF NOT EXISTS terminal_quick_commands (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	label TEXT NOT NULL,
	command TEXT NOT NULL,
	hidden INTEGER NOT NULL DEFAULT 0,
	auto_execute INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	project_id INTEGER DEFAULT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_quick_send (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	label TEXT NOT NULL,
	command TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	project_id INTEGER DEFAULT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS terminal_key_config (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	key_id TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(type, key_id)
);`

func setupTestEnv(t *testing.T) (*testEnv, func()) {
	t.Helper()
	env, teardown := handlertest.Setup(t)

	_, err := store.WriteExec(service.TerminalHistoryDDL)
	require.NoError(t, err)
	_, err = store.WriteExec(terminalExtraDDL)
	require.NoError(t, err)

	return env, teardown
}

func newRequest(t *testing.T, method, path string, body any) *http.Request {
	return handlertest.NewRequest(t, method, path, body)
}

func callHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	return handlertest.Call(handler, req)
}

func decodeRespJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

func assertOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	handlertest.AssertStatus(t, w, http.StatusOK)
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	handlertest.AssertStatus(t, w, expected)
}

func withProjectCookie(req *http.Request, projectPath string) *http.Request {
	return handlertest.WithProjectCookie(req, projectPath)
}
