package store

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// storeTestSchema is the subset of the production schema the store package's
// own queries touch. It is duplicated here rather than imported from service
// because Go coverage is per-package: the same SQL exercised only from
// service_test leaves this package at 0% diff coverage. Only the columns the
// queries actually read/write are declared.
const storeTestSchema = `
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	archived INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(backend, id)
);
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	agent_id TEXT DEFAULT '',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
CREATE TABLE IF NOT EXISTS session_shares (
	token TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	backend TEXT NOT NULL DEFAULT '',
	message_count INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_tool_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
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
CREATE TABLE IF NOT EXISTS chat_thinking (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	think_id TEXT NOT NULL,
	seq INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(think_id, message_id, seq)
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
	message_id INTEGER PRIMARY KEY,
	summary    TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	trigger_type TEXT NOT NULL DEFAULT 'auto',
	status TEXT NOT NULL DEFAULT 'completed',
	read_at DATETIME,
	summary TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
`

// setupStoreTestDB installs a fresh in-memory database carrying the store
// schema as the package-global handle and restores the previous handles on
// cleanup. The projects registry is created from ProjectsDDL so the project
// lookups the queries depend on resolve.
func setupStoreTestDB(t *testing.T) {
	t.Helper()
	raw, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// :memory: is per-connection, so pin to one connection: a second pooled
	// connection would see an empty database.
	raw.SetMaxOpenConns(1)

	_, err = raw.Exec(storeTestSchema)
	require.NoError(t, err)
	_, err = raw.Exec(ProjectsDDL)
	require.NoError(t, err)

	restore := SetDBForTest(raw, raw)
	t.Cleanup(func() {
		restore()
		_ = raw.Close()
	})
}
