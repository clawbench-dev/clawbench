// Package handlertest 是功能插件测试的公共夹具。
//
// 功能搬进 internal/plugin/<id> 之后，它们的测试不能再引用 handler 包里
// testutil_test.go 的符号——测试文件不导出，跨包不可见。这里按插件测试的
// 实际需要重建一份最小版本：临时项目目录、内存 SQLite（projects 表）、
// 请求构造与断言。
//
// 与 handler 的 setupTestEnv 的区别：那个还准备了 npm 探测、chat 表等与
// 插件无关的夹具。插件测试需要额外表时自己执行对应的 DDL（见各插件包的
// setupXxxEnv）。
//
// 本包只被 _test.go 引用，本身不含可测逻辑，因此列入覆盖率豁免。
package handlertest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/plugins"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // registers the "sqlite" driver used by sql.Open above
)

// Env is the state a test needs after setup.
type Env struct {
	ProjectDir string
	WatchDir   string
}

// Setup creates a temp project directory and an in-memory DB with the projects
// table, and restores every global it touches on teardown.
func Setup(t *testing.T) (*Env, func()) {
	t.Helper()

	watchDir := CanonPath(t.TempDir())
	projectDir := filepath.Join(watchDir, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	origRootPaths := model.RootPaths
	origToken := model.SessionToken
	origCookieToken := model.CookieToken

	model.SessionToken = ""
	model.CookieToken = ""
	model.RootPaths = []string{watchDir}

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, _ = db.ExecContext(context.Background(), "PRAGMA busy_timeout=5000")
	_, err = db.ExecContext(context.Background(), store.ProjectsDDL)
	require.NoError(t, err)
	restoreDB := store.SetDBForTest(db, db)

	teardown := func() {
		restoreDB()
		_ = db.Close()
		model.RootPaths = origRootPaths
		model.SessionToken = origToken
		model.CookieToken = origCookieToken
	}
	return &Env{ProjectDir: projectDir, WatchDir: watchDir}, teardown
}

// CanonPath resolves symlinks so paths match what production compares
// (macOS /var → /private/var).
func CanonPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// NewRequest builds a request with an optional JSON body.
func NewRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// Call runs a handler and returns the recorder.
func Call(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// AssertStatus checks the status code and includes the body on failure.
func AssertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Errorf("expected status %d, got %d; body: %s", expected, w.Code, w.Body.String())
	}
}

// WithProjectCookie adds the clawbench_project cookie.
func WithProjectCookie(req *http.Request, projectPath string) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(projectPath),
	})
	return req
}

// CreateFile writes a file under projectDir, creating parent directories.
func CreateFile(t *testing.T, projectDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(projectDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}

// FullSchema is the post-migration production schema (base DDL + every
// ALTER TABLE ADD COLUMN), copied from internal/service/database.go.
//
// It exists so a feature's tests do not each re-declare a partial schema: a
// fixture that drifts from production (a column that only exists here) makes a
// test pass on a shape the real database does not have.
//
// It is still a copy — when the production DDL changes, update this. The
// alternative (calling service.InitDB) opens a file-backed DB and reopens the
// store handles, which the in-memory fixture cannot mix with.
//
// Feature tables that have their own DDL constant (notifications, live notes)
// are added by the package that needs them.
const FullSchema = `

		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			injected INTEGER NOT NULL DEFAULT 0,
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS queued_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL DEFAULT '',
			queue_id TEXT NOT NULL,
			content TEXT NOT NULL,
			files TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_queued_session ON queued_messages(session_id, id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_queued_identity ON queued_messages(session_id, queue_id);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_name TEXT NOT NULL DEFAULT '',
			agent_avatar TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			agent_takeover_from TEXT NOT NULL DEFAULT '',
			model TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			context_state TEXT DEFAULT '',
			source_session_id TEXT DEFAULT NULL,
			transport TEXT DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			title_renamed INTEGER NOT NULL DEFAULT 0,
			title_source TEXT NOT NULL DEFAULT '',
			compacted INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0,
			removed INTEGER NOT NULL DEFAULT 0,
			pinned INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_read_at DATETIME,
			awaiting_user_at DATETIME,
			UNIQUE(backend, id)
		);
		CREATE TABLE IF NOT EXISTS recent_projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_default INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id)
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			cron_expr TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			script TEXT NOT NULL DEFAULT '',
			script_timeout INTEGER NOT NULL DEFAULT 0,
			session_id TEXT,
			trigger_mode TEXT NOT NULL DEFAULT 'cron',
			event_types TEXT NOT NULL DEFAULT '',
			mail_account_id INTEGER NOT NULL DEFAULT 0,
			mail_folder TEXT NOT NULL DEFAULT '',
			mail_from TEXT NOT NULL DEFAULT '',
			mail_subject TEXT NOT NULL DEFAULT '',
			mail_body TEXT NOT NULL DEFAULT '',
			mail_has_attachment INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
			max_runs INTEGER DEFAULT 0,
			last_run_at DATETIME,
			next_run_at DATETIME,
			run_count INTEGER DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			trigger_type TEXT NOT NULL DEFAULT 'auto',
			status TEXT NOT NULL DEFAULT 'completed',
			read_at DATETIME,
			summary TEXT,
			event_url TEXT NOT NULL DEFAULT '',
			event_summary TEXT NOT NULL DEFAULT '',
			script_exit_code INTEGER,
			script_outcome TEXT NOT NULL DEFAULT '',
			script_stdout TEXT NOT NULL DEFAULT '',
			script_stderr TEXT NOT NULL DEFAULT '',
			script_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_id, backend, session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_id, backend);
		CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
		CREATE TABLE IF NOT EXISTS chat_recommendations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			message_id INTEGER NOT NULL DEFAULT 0,
			recommendation TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
		CREATE TABLE IF NOT EXISTS tts_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id   INTEGER NOT NULL,
			tts_summary  TEXT NOT NULL,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(message_id)
		);
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
		);
		CREATE TABLE IF NOT EXISTS chat_metadata (
			message_id INTEGER PRIMARY KEY,
			mode TEXT DEFAULT '',
			thinking_effort TEXT DEFAULT '',
			transport TEXT DEFAULT '',
			model TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			duration_ms INTEGER DEFAULT 0,
			wall_ms INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			stop_reason TEXT DEFAULT '',
			is_error INTEGER DEFAULT 0,
			error_message TEXT DEFAULT '',
			cached_read_tokens INTEGER DEFAULT 0,
			cached_write_tokens INTEGER DEFAULT 0,
			thought_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			cache_creation_tokens INTEGER DEFAULT 0,
			cache_hit_tokens INTEGER DEFAULT 0,
			cache_miss_tokens INTEGER DEFAULT 0,
			credit REAL DEFAULT 0,
			usage_by_category TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			request_id TEXT DEFAULT '',
			trace_id TEXT DEFAULT '',
			agent_message_id TEXT DEFAULT '',
			message_request_id TEXT DEFAULT '',
			request_model_name TEXT DEFAULT '',
			response_model_id TEXT DEFAULT '',
			finish_reason TEXT DEFAULT '',
			outcome TEXT DEFAULT '',
			agent_phase TEXT DEFAULT '',
			project_id INTEGER DEFAULT '',
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			cost_session_total REAL DEFAULT 0,
			cost_cumulative INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_project_created ON chat_metadata(project_id, created_at);
		CREATE TABLE IF NOT EXISTS chat_tool_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			tool_id TEXT NOT NULL,
			name TEXT NOT NULL,
			input TEXT NOT NULL DEFAULT '{}',
			output TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			done INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tool_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON chat_tool_calls(message_id);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON chat_tool_calls(session_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			think_id TEXT NOT NULL,
			seq INTEGER NOT NULL DEFAULT 0,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id, seq)
		);
		CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
		CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS session_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'project',
			project_id INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(name, project_id)
		);
		CREATE TABLE IF NOT EXISTS session_tag_links (
			session_id TEXT NOT NULL,
			tag_id INTEGER NOT NULL REFERENCES session_tags(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, tag_id)
		);
		CREATE TABLE IF NOT EXISTS file_shares (
			token TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			root TEXT NOT NULL DEFAULT '',
			project_id INTEGER NOT NULL DEFAULT 0,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS project_forges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			platform TEXT NOT NULL,
			host TEXT NOT NULL,
			scheme TEXT NOT NULL DEFAULT '',
			owner TEXT NOT NULL,
			repo TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'auto',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS btw_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			anchor_message_id INTEGER NOT NULL DEFAULT 0,
			question TEXT NOT NULL,
			answer TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS session_shares (
			token TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			message_ids TEXT NOT NULL DEFAULT '[]',
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		-- DeleteProjectData cleans this table by group_id; without it here the
		-- handler's project delete would fail with "no such table".
		CREATE TABLE IF NOT EXISTS group_pending_bcc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id TEXT NOT NULL,
			target_name TEXT NOT NULL,
			content TEXT NOT NULL,
			sender_role TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS message_clusters_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			representative TEXT NOT NULL,
			variants TEXT NOT NULL,
			total_count INTEGER NOT NULL,
			representative_count INTEGER NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS message_clusters_meta (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			mode TEXT NOT NULL DEFAULT '',
			progress TEXT NOT NULL DEFAULT 'idle',
			phase TEXT NOT NULL DEFAULT '',
			msg_count INTEGER NOT NULL DEFAULT 0,
			cluster_count INTEGER NOT NULL DEFAULT 0,
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			error_msg TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
`

// MountRecorder captures what a plugin's MountRoutes registers.
//
// 用它写挂载测试只为了两件事：让 MountRoutes 真的被执行（否则 routes.go 的
// 覆盖率永远是 0，而它恰恰是「删掉一个目录就等于删掉一个功能」的关键文件），
// 以及断言关键路由的鉴权归属。
type MountRecorder struct {
	Auth   []string
	Public []string
	Raw    []string
}

// Mount returns a plugins.Mount that records into the receiver.
func (r *MountRecorder) Mount() plugins.Mount {
	return plugins.Mount{
		Auth:   func(pattern string, _ http.HandlerFunc) { r.Auth = append(r.Auth, pattern) },
		Public: func(pattern string, _ http.HandlerFunc) { r.Public = append(r.Public, pattern) },
		Raw:    func(pattern string, _ http.HandlerFunc) { r.Raw = append(r.Raw, pattern) },
	}
}

// All returns every recorded pattern, in registration order.
func (r *MountRecorder) All() []string {
	out := make([]string, 0, len(r.Auth)+len(r.Public)+len(r.Raw))
	out = append(out, r.Auth...)
	out = append(out, r.Public...)
	out = append(out, r.Raw...)
	return out
}
