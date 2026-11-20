package share

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/plugin/live"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// 夹具本体在 internal/handlertest（各功能插件共用）。这里保留同名薄包装，
// 是为了让测试函数体不必改写——它们从 handler 包搬过来时就是这个形状。
// testEnv 是分享测试的夹具。比公共夹具多一个 DB()——分享的降级用例要 DROP
// 表来模拟旧数据目录。
type testEnv struct {
	ProjectDir string
	WatchDir   string
}

func setupTestEnv(t *testing.T) (*testEnv, func()) {
	t.Helper()
	env, teardown := handlertest.Setup(t)

	// 分享的"按类型开关"默认是开的（见 model.ApplyDefaults），但测试里
	// FeaturePolicy 是零值，创建链接会被拒成 409。
	// 分享自己的表：链接、会话分享、会议纪要（会议链接会落到这里）。
	for _, ddl := range []string{
		service.FileSharesDDL,
		service.SessionSharesDDL,
		service.LiveNotesDDL,
		service.NotificationsDDL,
		chatSessionsDDL,
	} {
		_, err := store.WriteExec(ddl)
		require.NoError(t, err)
	}

	origPolicy := model.FeaturePolicy
	model.FeaturePolicy.ShareAllowFiles = true
	model.FeaturePolicy.ShareAllowSessions = true
	model.FeaturePolicy.ShareAllowMeetings = true
	model.FeaturePolicy.ShareAllowDownload = true
	t.Cleanup(func() { model.FeaturePolicy = origPolicy })

	return &testEnv{ProjectDir: env.ProjectDir, WatchDir: env.WatchDir}, teardown
}

func newRequest(t *testing.T, method, path string, body any) *http.Request {
	return handlertest.NewRequest(t, method, path, body)
}

func callHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	return handlertest.Call(handler, req)
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	handlertest.AssertStatus(t, w, expected)
}

func withProjectCookie(req *http.Request, projectPath string) *http.Request {
	return handlertest.WithProjectCookie(req, projectPath)
}

func createTestFile(t *testing.T, projectDir, relPath, content string) {
	handlertest.CreateFile(t, projectDir, relPath, content)
}

func assertOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	handlertest.AssertStatus(t, w, http.StatusOK)
}

// resetLiveRegistry installs a fresh room table for a test.
func resetLiveRegistry(t *testing.T) {
	t.Helper()
	prev := live.LiveRegistry
	live.LiveRegistry = liveshare.NewRegistry()
	t.Cleanup(func() { live.LiveRegistry = prev })
}

// projPath normalizes a test project path the same way the server does.
func projPath(p string) string { return model.NormalizeProjectPath(p) }

// setupLiveNotesDB 造一个带会议纪要表的内存库（分享会议链接的用例要用）。
func setupLiveNotesDB(t *testing.T) *sql.DB {
	t.Helper()
	env, teardown := setupTestEnv(t)
	_ = env
	for _, ddl := range []string{service.LiveNotesDDL, service.NotificationsDDL} {
		_, err := store.WriteExec(ddl)
		require.NoError(t, err)
	}
	t.Cleanup(teardown)
	return store.UnsafeDBForTest()
}

// chatSessionsDDL 是会话分享 meta 联查的最小表。
const chatSessionsDDL = `
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
			description TEXT,
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
	` + service.NotificationsDDL + `
`
