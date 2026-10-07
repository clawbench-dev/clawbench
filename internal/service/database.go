//nolint:noctx,govet,goconst,rowserrcheck // db global singleton, context not applicable; shadowed err is standard Go pattern; JSON/SQL field names are domain strings; legacy store.ReadDB().Query pattern
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	_ "modernc.org/sqlite" // register SQLite driver
)

// schemaMigrationsDDL 是「已应用迁移」的台账。数据转换类迁移无法用列探针
// 判断是否已完成（它们的守卫是 LIKE 扫描 + NOT EXISTS，而某些行按设计永远
// 无法转换），所以改为按名字记账：跑完一次就不再重扫。
//
// 加列 / 建索引类迁移不在这里记账——它们已由 pragma_table_info / sqlite_master
// 探针天然只跑一次，且探针比台账更能反映真实 schema（例如用户手工 DROP 过列）。
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// 数据转换类迁移的台账名。名字里带日期是为了可读；一旦发布就不得再改，
// 改了等于让所有已升级的库重跑一次全表扫描。
const (
	migMetadataFromContent  = "2026-09-29_migrate_metadata_from_content"
	migTaskExecSummaries    = "2026-09-29_migrate_task_execution_summaries"
	migToolCallsFromContent = "2026-09-29_migrate_tool_calls_from_content"
	migThinkingFromContent  = "2026-09-29_migrate_thinking_from_content"
)

// dataMigrationNames 是上表的名字集合，供测试遍历。
var dataMigrationNames = []string{
	migMetadataFromContent,
	migTaskExecSummaries,
	migToolCallsFromContent,
	migThinkingFromContent,
}

// ensureSchemaMigrationsTable 幂等建台账表。必须在任何 runOnce 之前调用。
func ensureSchemaMigrationsTable() error {
	_, err := store.WriteExec(schemaMigrationsDDL)
	return err
}

// isMigrationApplied 报告某个具名迁移是否已成功记账。
func isMigrationApplied(name string) bool {
	var n int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name,
	).Scan(&n); err != nil {
		// 读失败按「未应用」处理：宁可重跑一次（幂等迁移无副作用），
		// 也不要因为一次读错误永久跳过迁移。
		slog.Warn("schema_migrations: probe failed", "name", name, "err", err)
		return false
	}
	return n > 0
}

// markMigrationApplied 记账。INSERT OR IGNORE 使并发/重复调用安全。
func markMigrationApplied(name string) {
	if _, err := store.WriteExec(
		"INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)", name,
	); err != nil {
		slog.Warn("schema_migrations: mark failed", "name", name, "err", err)
	}
}

// runOnce 只在 name 未记账时执行 fn。fn 返回 true 表示「本轮已完成」——包括
// 「没有需要转换的行」和「扫描循环正常跑完但留下了按设计无法转换的行」两种
// 情况。返回 false 表示硬失败（查询/事务错误），此时不记账，下次启动重试。
func runOnce(name string, fn func() bool) {
	if isMigrationApplied(name) {
		return
	}
	if fn() {
		markMigrationApplied(name)
		return
	}
	slog.Warn("migration did not complete; will retry on next start", "name", name)
}

// ResetSchemaMigrationsForTest 清空台账，让测试能重新走迁移路径。
func ResetSchemaMigrationsForTest() {
	_, _ = store.WriteExec("DELETE FROM schema_migrations")
}

// InitDB initializes the SQLite database with latest schema.
// When runFromServer is true (server startup), orphaned streaming messages
// from previous crashes are cleaned up. When false (CLI subcommand), cleanup
// is skipped because the server process may still be actively streaming.
func InitDB(runFromServer ...bool) error { //nolint:gocognit,gocyclo // multi-table schema migration
	dbDir := model.DataDir
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	dbPath := filepath.Join(dbDir, "ClawBench.db")
	// Open (or re-open) both connection pools and apply the PRAGMAs. Open closes
	// any previously opened handles first, so InitDB may be called more than once
	// (migration rerun tests, restart flows) without leaking the old pool. The
	// read pool is opened up front so every reader below can use store.ReadDB()
	// from the first statement.
	if err := store.Open(dbPath); err != nil {
		return err
	}
	var err error

	// 迁移台账：数据转换类迁移按名字记账，只跑一次。
	if err := ensureSchemaMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}

	// Pre-migration: add columns that must exist before createTables runs
	// (because createTables creates indexes referencing these columns).
	// Only apply when the table already exists (upgrading from an older schema).
	// chat_history.indexed — added for RAG indexing progress tracking
	var chatHistoryExists int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_history'").Scan(&chatHistoryExists)
	if chatHistoryExists > 0 {
		var hasIndexed int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='indexed'").Scan(&hasIndexed)
		if hasIndexed == 0 {
			if _, err := store.WriteExec("ALTER TABLE chat_history ADD COLUMN indexed INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add indexed column: %w", err)
			}
		}

		// chat_history.external_message_id — external ACP messageId for incremental ACP sync dedup
		var hasExternalMsgID int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='external_message_id'").Scan(&hasExternalMsgID)
		if hasExternalMsgID == 0 {
			if _, err := store.WriteExec("ALTER TABLE chat_history ADD COLUMN external_message_id TEXT DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add external_message_id column: %w", err)
			}
		}

		// chat_history.completed_at — when the assistant reply finished streaming
		// (streaming=1 -> 0). NULL for user messages and for rows finalized
		// before this column existed.
		//
		// The unread check compares a reply's timestamp against
		// chat_sessions.last_read_at. Using created_at is wrong for a reply:
		// created_at is stamped when the turn STARTS (the streaming placeholder
		// row is inserted up front), while the reply only becomes visible to the
		// user minutes later when it is finalized. Reading the session while its
		// turn is still running therefore pushes last_read_at past created_at,
		// and the finished reply can never be unread again — the completion
		// popup fires but no badge ever appears. completed_at fixes the
		// comparison by timestamping the moment the reply actually landed.
		//
		// No backfill: rows predating the column keep completed_at NULL and the
		// unread queries fall back to created_at via COALESCE, which is exactly
		// the old behavior for them.
		var hasCompletedAt int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='completed_at'").Scan(&hasCompletedAt)
		if hasCompletedAt == 0 {
			if _, err := store.WriteExec("ALTER TABLE chat_history ADD COLUMN completed_at DATETIME"); err != nil {
				return fmt.Errorf("failed to add completed_at column: %w", err)
			}
		}
	}

	// Pre-migration: rename chat_sessions.deleted to archived.
	// Session "delete" is actually an archive; the column name
	// should reflect that. Must run before createTables because its indexes
	// reference the archived column. SQLite RENAME COLUMN also rewrites any
	// index definitions referencing the old column name.
	var hasSessionDeleted int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='deleted'").Scan(&hasSessionDeleted)
	if hasSessionDeleted > 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions RENAME COLUMN deleted TO archived"); err != nil {
			return fmt.Errorf("failed to rename chat_sessions.deleted to archived: %w", err)
		}
		slog.Info("renamed chat_sessions.deleted column to archived")
	}

	// Pre-migration: add project_meta.forge_bind_opt_out before createTables runs.
	// The CREATE TABLE below is a no-op on an existing database, so the column
	// would never appear there. On a fresh database the table does not exist yet
	// and the CREATE TABLE (which now includes the column) covers it — hence the
	// existence guard rather than an unconditional ALTER.
	var projectMetaExists int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project_meta'").Scan(&projectMetaExists)
	if projectMetaExists > 0 {
		var hasCol int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('project_meta') WHERE name='forge_bind_opt_out'").Scan(&hasCol)
		if hasCol == 0 {
			if _, err := store.WriteExec("ALTER TABLE project_meta ADD COLUMN forge_bind_opt_out INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add project_meta.forge_bind_opt_out column: %w", err)
			}
		}
	}

	// Pre-migration: make sure file_shares carries project_id before createTables
	// indexes it. A database can hold file_shares WITHOUT any project-scoped
	// chat table (an install that only ever used file sharing), in which case the
	// projects migration above does not run and CREATE TABLE IF NOT EXISTS leaves
	// the legacy table untouched — so its index would reference a missing column
	// and abort the whole Exec.
	var fileSharesExists int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='file_shares'").Scan(&fileSharesExists)
	if fileSharesExists > 0 {
		var hasShareProject int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('file_shares') WHERE name='project_id'").Scan(&hasShareProject)
		if hasShareProject == 0 {
			if _, err := store.WriteExec("ALTER TABLE file_shares ADD COLUMN project_id INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add file_shares.project_id column: %w", err)
			}
		}
	}

	// Pre-migration: replace every project *path* column with a project *id*,
	// backed by the new projects registry.
	//
	// This MUST run before createTables: createTables is a no-op for existing
	// tables (CREATE TABLE IF NOT EXISTS) but its CREATE INDEX statements still
	// run and now name project_id, so on an old database they would reference a
	// column that does not exist and abort the whole Exec. Same hazard the
	// chat_history.indexed and chat_metadata blocks above handle.
	//
	// Detected by ANY project-scoped table still carrying project_path: a fresh
	// install creates them with project_id already, so it skips this entirely.
	// Checking every candidate rather than just chat_history matters because a
	// database can hold a subset of the tables (a test fixture, or an install
	// that only ever used file sharing), and createTables would then abort on
	// the missing project_id column of whichever table does exist.
	var hasLegacyProjectPath int
	for _, tbl := range legacyProjectPathTables {
		var n int
		_ = store.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='project_path'", tbl).Scan(&n)
		if n > 0 {
			hasLegacyProjectPath = 1
			break
		}
	}
	if hasLegacyProjectPath > 0 {
		if err := migrateProjectsToIDs(); err != nil {
			return fmt.Errorf("failed to migrate project paths to ids: %w", err)
		}
	}

	// Pre-migration: add the chat_metadata ledger attribution columns before
	// createTables runs. On an existing database the CREATE TABLE below is a
	// no-op, so the new index on (project_id, created_at) would reference a
	// column that does not exist yet and abort the whole multi-statement Exec,
	// breaking startup. Mirrors the chat_history.indexed handling above.
	var chatMetadataExists int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_metadata'").Scan(&chatMetadataExists)
	if chatMetadataExists > 0 {
		for _, col := range []struct{ name, ddl string }{
			{"project_id", "ALTER TABLE chat_metadata ADD COLUMN project_id INTEGER DEFAULT 0"},
			{"backend", "ALTER TABLE chat_metadata ADD COLUMN backend TEXT DEFAULT ''"},
			{"agent_id", "ALTER TABLE chat_metadata ADD COLUMN agent_id TEXT DEFAULT ''"},
			{"clawbench_session_id", "ALTER TABLE chat_metadata ADD COLUMN clawbench_session_id TEXT DEFAULT ''"},
		} {
			var hasCol int
			_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_metadata') WHERE name=?", col.name).Scan(&hasCol)
			if hasCol == 0 {
				if _, err := store.WriteExec(col.ddl); err != nil {
					return fmt.Errorf("failed to add chat_metadata.%s column: %w", col.name, err)
				}
			}
		}
	}

	// Pre-migration: group-chat columns. On an existing database the CREATE
	// TABLE below is a no-op, so the new idx_sessions_group index would
	// reference a column that does not exist yet and abort the whole
	// multi-statement Exec, breaking startup. Add the columns first.
	// (chatHistoryExists is declared in the earlier pre-migration block above.)
	if chatHistoryExists > 0 {
		var hasAgentID int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='agent_id'").Scan(&hasAgentID)
		if hasAgentID == 0 {
			if _, err := store.WriteExec("ALTER TABLE chat_history ADD COLUMN agent_id TEXT DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add chat_history.agent_id: %w", err)
			}
		}
	}
	var chatSessionsExists int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_sessions'").Scan(&chatSessionsExists)
	if chatSessionsExists > 0 {
		var hasGroupID int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='group_id'").Scan(&hasGroupID)
		if hasGroupID == 0 {
			if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN group_id TEXT DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add chat_sessions.group_id: %w", err)
			}
		}
	}

	// Create tables with latest schema
	_, err = store.WriteExec(`
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			-- role='system' carries group-chat system events (member joins and
			-- leaves). It is NOT an AI reply: the unread subquery counts only
			-- role='assistant', so system events never register as unread.
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			model TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(backend, id)
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_group ON chat_sessions(group_id, session_type);
		-- Private notes (密送) whose target was NOT named in the round that
		-- carried them. A note is delivered with its target's next turn, so a
		-- host can "hand everyone a word, then call on one player first"
		-- (the Who-Is-The-Spy setup). Rows are DELETED on successful delivery;
		-- a failed/cancelled turn leaves them for the next attempt (decision
		-- #69 semantics). target_name is the trimmed display name; the human
		-- user is addressed by the reserved name 'User'.
		CREATE TABLE IF NOT EXISTS group_pending_bcc (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id      TEXT NOT NULL,
			target_name   TEXT NOT NULL,
			content       TEXT NOT NULL,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_group_pending_bcc ON group_pending_bcc(group_id, target_name, id);
		CREATE TABLE IF NOT EXISTS recent_projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_default INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id)
		);
		-- The project registry. Every project-scoped table stores project_id
		-- (an integer) instead of the project path, so renaming or moving a
		-- project directory is a single UPDATE here rather than a rewrite of
		-- every table. path is canonical (see store.NormalizeProjectPath), and
		-- UNIQUE(path) is what collapses two spellings of one directory.
		--
		-- forge_bind_opt_out is folded in from the former project_meta table,
		-- which had exactly one live column; its next_session_number was dead.
		--
		-- No foreign key is declared from the other tables: project_id = 0 is a
		-- reserved sentinel (global tags, unattributable file shares) that has
		-- no row here. See store.ProjectsDDL in projects.go.
		CREATE TABLE IF NOT EXISTS projects (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			path               TEXT NOT NULL,
			forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
			created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(path)
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
			session_id TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			repeat_mode TEXT DEFAULT 'unlimited',
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
			status TEXT NOT NULL DEFAULT 'running',
			event_url TEXT NOT NULL DEFAULT '',
			event_summary TEXT NOT NULL DEFAULT '',
			script_exit_code INTEGER,
			script_stdout TEXT NOT NULL DEFAULT '',
			script_stderr TEXT NOT NULL DEFAULT '',
			script_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Create indexes for efficient queries
		CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_id, backend, session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_id, backend);
		CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_type ON chat_sessions(session_type, project_id, archived);

		-- Covering index for session-based queries (GetChatMessageCount, GetAssistantMessageCount,
		-- unread subquery, GetChatHistoryPaged) — avoids full table scan through large content rows.
		CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at);
		-- Index for task listing by project
		CREATE INDEX IF NOT EXISTS idx_tasks_project ON scheduled_tasks(project_id, created_at DESC);
		-- Covering index for unread count subquery in GetSessions/GetSessionsPaged:
		-- WHERE project_id = ? AND role = 'assistant' AND streaming = 0 AND created_at > ?
		-- Without this, the unread subquery can only use the project_id prefix of idx_history_session,
		-- requiring a full scan of all messages in the project to filter by role and streaming.
		CREATE INDEX IF NOT EXISTS idx_history_unread ON chat_history(project_id, role, streaming, created_at);
		-- Covering index for the PER-SESSION unread count subquery (GetSessions,
		-- GetSessionsPaged, GetOverviewSessions). Those queries ask "how many unread
		-- replies does THIS session have", so session_id must lead.
		--
		-- session_id must come first, and that is the whole point of this index.
		-- idx_history_unread leads with project_id, and the moment the subquery
		-- mentions project_id (which it must — a message row's project_id is
		-- not guaranteed to equal its session's, see the ISS-420 note below) the
		-- planner prefers idx_history_unread and rescans the whole project for
		-- EVERY listed session. That turned a 0.0ms seek into a 186ms scan.
		--
		-- The column list stops at project_id deliberately. Adding completed_at
		-- or created_at (to make it fully covering) was measured to be no faster
		-- (0.01ms either way — the seek already narrows to a handful of rows), and
		-- it BREAKS the completed_at migration: SQLite refuses
		-- "ALTER TABLE ... DROP COLUMN completed_at" while any index references
		-- that column, and TestSchema_CompletedAtMigration simulates exactly that
		-- pre-completed_at database. Keep this list to columns that migrations
		-- never drop.
		--
		-- NOTE: the equality on h.project_id = s.project_id is kept even though
		-- it is redundant for rows written by current code. Historic rows can
		-- disagree (messages persisted under the cookie's project rather than the
		-- session's), and dropping the predicate would count those as unread here
		-- while UpdateLastRead anchors on a different set — the two sides must
		-- agree or the badge never clears.
		CREATE INDEX IF NOT EXISTS idx_history_sess_unread ON chat_history(session_id, role, streaming, project_id);
		-- Covering index for RAG indexing progress queries:
		-- TotalMessageCount (WHERE streaming = 0) and IndexedMessageCount (WHERE indexed = 1 AND streaming = 0)
		CREATE INDEX IF NOT EXISTS idx_history_indexing ON chat_history(streaming, indexed);

		-- Queued (pending) user messages, held OUTSIDE chat_history until the
		-- drain loop picks them up.
		--
		-- A queued message is materialized into chat_history only when it is
		-- dequeued (or injected mid-turn). That is what makes DB id order equal
		-- conversational order: the user row gets its id immediately before the
		-- assistant reply it produces, so no reply-anchoring token is needed.
		-- Previously a queued message was a chat_history row with queued=1,
		-- persisted at ENQUEUE time (before its reply existed), which forced a
		-- queue_id anchor through the whole stack.
		--
		-- UNIQUE(session_id, queue_id) makes the client-generated queue id a real
		-- identity key: the frontend addresses a queued message by it (cancel /
		-- inject) and the backend echoes it back on user_message.
		CREATE TABLE IF NOT EXISTS queued_messages (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL,
			project_id   INTEGER NOT NULL,
			backend      TEXT NOT NULL DEFAULT '',
			queue_id     TEXT NOT NULL,
			content      TEXT NOT NULL,
			files        TEXT,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_queued_session ON queued_messages(session_id, id);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_queued_identity ON queued_messages(session_id, queue_id);

		-- Tool call detail storage (input/output split from chat_history.content for performance)
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

		-- Thinking block detail storage (text split from chat_history.content for performance)
		-- seq: chunk sequence for incremental streaming persistence — the streaming
		-- flush appends delta segments (seq 0,1,2,...) instead of rewriting the full
		-- text each 500ms window. Readers concatenate rows ordered by seq. Finalize
		-- deletes all rows and rewrites a single seq=0 row with the complete text.
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
		-- Covering index for session list ORDER BY + cursor pagination:
		-- WHERE session_type = 'chat' AND project_id = ? AND archived = 0 ORDER BY created_at DESC, id DESC
		-- Without this, idx_sessions_type covers WHERE but requires a filesort for ORDER BY.
		CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_id, archived, created_at DESC, id DESC);

		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);

		CREATE TABLE IF NOT EXISTS forwarded_ports (
			local_port INTEGER PRIMARY KEY,
			port INTEGER NOT NULL,
			host TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			protocol TEXT NOT NULL DEFAULT 'http',
			direction TEXT NOT NULL DEFAULT 'forward',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

		-- One auto_execute command per project scope: COALESCE(NULL->0) lets a
		-- single global command and one per project coexist. (0 is the reserved
		-- global-scope sentinel; see store.GlobalScopeProjectID.)
		CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_commands_auto_execute
			ON terminal_quick_commands(COALESCE(project_id, 0), auto_execute)
			WHERE auto_execute = 1;

		CREATE TABLE IF NOT EXISTS terminal_key_config (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			key_id TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(type, key_id)
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

		-- Usage ledger. Deliberately has NO foreign key to chat_history: a row
		-- records tokens/cost that were actually consumed and must survive the
		-- session/message being deleted (hard-delete, rewind, ACP replay
		-- replace) so usage statistics never under-count. project_id/backend/
		-- agent_id are denormalized at write time so the stats query needs no
		-- join back to the (possibly deleted) session; agent_name is still
		-- resolved live via LEFT JOIN agents.
		--
		-- session_id holds the EXTERNAL ACP session id (may be empty for CLI
		-- agents); clawbench_session_id holds the ClawBench chat_sessions.id.
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
			project_id INTEGER DEFAULT 0,
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_project_created ON chat_metadata(project_id, created_at);

		-- Pending events for offline push notifications (added 2026-07)
		CREATE TABLE IF NOT EXISTS pending_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_event_id ON pending_events(event_id);
		CREATE INDEX IF NOT EXISTS idx_pending_expires ON pending_events(expires_at);
		CREATE INDEX IF NOT EXISTS idx_pending_created ON pending_events(created_at);

		-- DingTalk subscriber management (added 2026-07)
		CREATE TABLE IF NOT EXISTS dingtalk_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			conversation_id TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_dingtalk_user ON dingtalk_subscribers(user_id);

		-- Feishu subscriber management (added 2026-07)
		CREATE TABLE IF NOT EXISTS feishu_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			chat_id         TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_feishu_user ON feishu_subscribers(user_id);

		-- Cluster cache: stores precomputed cluster results for quick-send suggestions
		CREATE TABLE IF NOT EXISTS message_clusters_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			representative TEXT NOT NULL,
			variants TEXT NOT NULL,
			total_count INTEGER NOT NULL,
			representative_count INTEGER NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Cluster meta: single-row (id=1) tracking computation state
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

		-- Conversation recommendations (推荐回复), persisted so recommendations
		-- generated while the client was offline can be shown later.
		CREATE TABLE IF NOT EXISTS chat_recommendations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			message_id INTEGER NOT NULL DEFAULT 0,
			recommendation TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chat_rec_session ON chat_recommendations(session_id, id);

		-- Public file share links (capability tokens). A row maps an opaque,
		-- unguessable token to an absolute file path. Public endpoints accept the
		-- token WITHOUT auth; removing the row revokes the link immediately.
		-- root confines the link to a directory (resolved at creation time,
		-- while the request is still authenticated); see FileSharesDDL.
		--
		-- project_id scopes the management list to the project the file was
		-- shared FROM (so the "shared files" drawer matches the project-scoped
		-- "shared conversations" drawer). It is 0 when the share predates the
		-- column and could not be attributed. root stays a frozen PATH, not an
		-- id: it is the security boundary the unauthenticated read endpoints
		-- enforce, and resolving it through a mutable projects row would
		-- silently retarget a live link when a project is renamed.
		CREATE TABLE IF NOT EXISTS file_shares (
			token TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			root TEXT NOT NULL DEFAULT '',
			project_id INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_file_shares_path ON file_shares(path);
		CREATE INDEX IF NOT EXISTS idx_file_shares_project ON file_shares(project_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Forge repo bindings (GitHub / GitLab). Defined in project_forges.go as a
	// constant so tests share one source of truth for the schema.
	if _, err := store.WriteExec(ProjectForgesDDL); err != nil {
		return fmt.Errorf("failed to create project_forges table: %w", err)
	}
	// Public conversation-share links (capability tokens). Defined in
	// session_shares.go as a constant so tests share one source of truth.
	if _, err := store.WriteExec(SessionSharesDDL); err != nil {
		return fmt.Errorf("failed to create session_shares table: %w", err)
	}
	// "/btw" side questions. Defined in btw.go as a constant so tests share one
	// source of truth for the schema.
	if _, err := store.WriteExec(BtwQuestionsDDL); err != nil {
		return fmt.Errorf("failed to create btw_questions table: %w", err)
	}
	// project_forges.scheme: the API scheme the binding's host is reached with.
	//
	// Existing rows backfill to '' (unknown) rather than 'https'. The distinction
	// is load-bearing: '' means "the remote did not say", which lets the resolver
	// fall back to the instance hint and then https, whereas writing 'https'
	// would freeze a guess into the row and silently override a later http hint.
	// No existing binding can have known its scheme — the column did not exist,
	// and every remote parsed before this change dropped it.
	{
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('project_forges') WHERE name='scheme'").Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec("ALTER TABLE project_forges ADD COLUMN scheme TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add project_forges.scheme column: %w", err)
			}
		}
	}
	// Forge sync state: snapshot rows, watermark, derived events, and the CI
	// run ledger.
	for _, ddl := range []string{ForgeItemsDDL, ForgeSyncStateDDL, ForgeEventDDL, ForgePipelineRunsDDL} {
		if _, err := store.WriteExec(ddl); err != nil {
			return fmt.Errorf("failed to create forge sync tables: %w", err)
		}
	}
	// forge_sync_state split the single `watermark` column into one cursor per
	// item type. The split is what stops the PR half of a sync (which cannot be
	// time-filtered on GitHub and therefore walks the whole history every pass)
	// from pushing the shared cursor past issues the issue half had not fetched
	// yet — those issues were then permanently skipped.
	//
	// This runs AFTER the DDL above, and that ordering is load-bearing: on a
	// fresh database the CREATE TABLE already declares both new columns, so both
	// guards below find the column and skip. On an existing database the CREATE
	// TABLE IF NOT EXISTS is a no-op (the old table, with the old column, is
	// kept), so the guards see the old shape and migrate it. Running this before
	// the DDL would make the guards misread an empty pragma_table_info on a
	// fresh database as "column missing" and try to rename a nonexistent table.
	//
	// `watermark` is not part of the primary key, so RENAME COLUMN is safe here
	// (the repo already relies on this for chat_sessions.deleted → archived).
	{
		var hasOld, hasNew int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forge_sync_state') WHERE name='watermark'").Scan(&hasOld)
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forge_sync_state') WHERE name='issue_watermark'").Scan(&hasNew)
		if hasOld > 0 && hasNew == 0 {
			if _, err := store.WriteExec("ALTER TABLE forge_sync_state RENAME COLUMN watermark TO issue_watermark"); err != nil {
				return fmt.Errorf("failed to rename forge_sync_state.watermark: %w", err)
			}
		}
	}
	{
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forge_sync_state') WHERE name='pr_watermark'").Scan(&exists)
		if exists == 0 {
			// Deliberately NOT backfilled from the old shared watermark. A NULL
			// pr_watermark means "no PR baseline yet", so the first PR pass
			// baselines silently instead of dispatching an event for every
			// already-merged historical PR. Copying the old value here would
			// open a window over the repository's whole PR history.
			if _, err := store.WriteExec("ALTER TABLE forge_sync_state ADD COLUMN pr_watermark DATETIME"); err != nil {
				return fmt.Errorf("failed to add forge_sync_state.pr_watermark column: %w", err)
			}
		}
	}
	// forge_items.comments_baselined: distinguishes "comments fetched, none
	// exist" from "comments never fetched". Without it, a first pass that
	// skipped comments leaves a zero baseline and the next pass replays every
	// historical comment as new. Existing rows are backfilled to 1 only when
	// they already have a comment id, so genuinely unbaselined items stay
	// unbaselined and are silently absorbed on their next comment pass.
	{
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forge_items') WHERE name='comments_baselined'").Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec("ALTER TABLE forge_items ADD COLUMN comments_baselined INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add forge_items.comments_baselined column: %w", err)
			}
			if _, err := store.WriteExec("UPDATE forge_items SET comments_baselined = 1 WHERE last_comment_id > 0"); err != nil {
				return fmt.Errorf("failed to backfill forge_items.comments_baselined: %w", err)
			}
		}
	}
	// forge_events.item_key: identifies the item an event is about, so the unread
	// badge can count distinct ITEMS rather than raw events (and so a row can be
	// marked read on its own). It cannot be derived from (item_type, number)
	// because pipeline events carry Number 0 for every run.
	//
	// The backfill reconstructs the key from the columns that are already there.
	// A pipeline row's run id only exists inside its dedupe_key ("...|run:<id>"),
	// so it is parsed out; if that parse fails the row is marked read rather than
	// given a junk key, since a wrong key would create a phantom unread item that
	// the user could never clear by opening anything.
	{
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forge_events') WHERE name='item_key'").Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec("ALTER TABLE forge_events ADD COLUMN item_key TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add forge_events.item_key column: %w", err)
			}
			if _, err := store.WriteExec(
				"UPDATE forge_events SET item_key = item_type || '/' || number WHERE item_type != 'pipeline'",
			); err != nil {
				return fmt.Errorf("failed to backfill forge_events.item_key: %w", err)
			}
			if _, err := store.WriteExec(
				`UPDATE forge_events SET item_key = 'pipeline/run:' || substr(dedupe_key, instr(dedupe_key, 'run:') + 4)
				 WHERE item_type = 'pipeline' AND instr(dedupe_key, 'run:') > 0`,
			); err != nil {
				return fmt.Errorf("failed to backfill forge_events.item_key (pipeline): %w", err)
			}
			// Rows whose key could not be reconstructed are retired instead of
			// left with an empty key (they would be invisible to the badge but
			// permanently unread).
			if _, err := store.WriteExec(
				"UPDATE forge_events SET read_at = COALESCE(read_at, CURRENT_TIMESTAMP) WHERE item_key = ''",
			); err != nil {
				return fmt.Errorf("failed to retire keyless forge_events rows: %w", err)
			}
		}
		// The index is created AFTER the column exists (on a fresh database the
		// column is already in the CREATE TABLE, so this is a no-op there).
		if _, err := store.WriteExec(ForgeEventItemIndexDDL); err != nil {
			return fmt.Errorf("failed to create forge_events item index: %w", err)
		}
	}

	// Create agent store tables.
	// Defined in agent_store.go as AgentDDL constant.
	if _, err := store.WriteExec(AgentDDL); err != nil {
		return fmt.Errorf("failed to create agent tables: %w", err)
	}

	// Schema migrations: add columns that may not exist in older databases.
	// NOTE: Migration reads go through store.ReadDB(); the read pool is opened by
	// store.Open before any migration runs, so reads here see the write pool's
	// committed rows under WAL.
	// Forge event-triggered tasks: trigger_mode selects cron vs event, and
	// event_types scopes which forge events fire the task. The watched
	// repository is always the task project's binding, so no repo column exists.
	for _, col := range []struct{ name, ddl string }{
		{"trigger_mode", "ALTER TABLE scheduled_tasks ADD COLUMN trigger_mode TEXT NOT NULL DEFAULT 'cron'"},
		{"event_types", "ALTER TABLE scheduled_tasks ADD COLUMN event_types TEXT NOT NULL DEFAULT ''"},
		{"script", "ALTER TABLE scheduled_tasks ADD COLUMN script TEXT NOT NULL DEFAULT ''"},
		{"script_timeout", "ALTER TABLE scheduled_tasks ADD COLUMN script_timeout INTEGER NOT NULL DEFAULT 0"},
	} {
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('scheduled_tasks') WHERE name=?", col.name).Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec(col.ddl); err != nil {
				return fmt.Errorf("failed to add scheduled_tasks.%s column: %w", col.name, err)
			}
		}
	}

	var hasReadAt int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('task_executions') WHERE name='read_at'").Scan(&hasReadAt)
	if hasReadAt == 0 {
		if _, err := store.WriteExec("ALTER TABLE task_executions ADD COLUMN read_at DATETIME"); err != nil {
			return fmt.Errorf("failed to add read_at column: %w", err)
		}
	}

	// Unread became per-execution: scheduled_tasks.last_read_at is no longer
	// consulted, so a task's watermark can no longer suppress anything.
	//
	// That REMOVES a suppression, which would otherwise inflate the badge on
	// upgrade: executions that finished before the user last opened the task and
	// were never individually opened were counted read only by virtue of the
	// watermark. Without this backfill a long-lived task would suddenly report
	// every run it ever made as unread.
	//
	// So the exact set that the watermark was suppressing is marked read once,
	// and then the watermark is cleared. Clearing it is what makes the step
	// one-time: a second boot finds nothing to migrate (and would otherwise be
	// harmless, since read_at is already set).
	//
	// Only executions strictly at or before the watermark are touched; anything
	// after it was already unread and must stay that way.
	//
	// Guarded on the column existing: `last_read_at` is declared in the
	// scheduled_tasks CREATE TABLE, so a database old enough to predate it has
	// no watermark to migrate at all (CREATE TABLE IF NOT EXISTS leaves such a
	// table untouched).
	var hasTaskLastRead int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('scheduled_tasks') WHERE name='last_read_at'").Scan(&hasTaskLastRead)
	if hasTaskLastRead > 0 {
		var pending int
		if err := store.ReadDB().QueryRow(
			`SELECT COUNT(*) FROM task_executions e
			 JOIN scheduled_tasks s ON s.id = e.task_id
			 WHERE e.read_at IS NULL AND e.status != 'running' AND s.last_read_at IS NOT NULL`,
		).Scan(&pending); err != nil {
			return fmt.Errorf("failed to count executions pending the unread migration: %w", err)
		}
		if pending > 0 {
			if _, err := store.WriteExec(
				`UPDATE task_executions SET read_at = CURRENT_TIMESTAMP
				 WHERE read_at IS NULL AND status != 'running'
				   AND task_id IN (SELECT id FROM scheduled_tasks WHERE last_read_at IS NOT NULL)
				   AND created_at <= (SELECT last_read_at FROM scheduled_tasks WHERE id = task_id)`,
			); err != nil {
				return fmt.Errorf("failed to backfill per-execution read state: %w", err)
			}
			if _, err := store.WriteExec(
				"UPDATE scheduled_tasks SET last_read_at = NULL WHERE last_read_at IS NOT NULL",
			); err != nil {
				return fmt.Errorf("failed to clear the retired last_read_at watermark: %w", err)
			}
			slog.Info("migrated task unread state to per-execution", slog.Int("executions", pending))
		}
	}

	// Migrate: record the forge event that triggered an execution, so the run is
	// traceable back to the originating issue/PR; and the gating script's
	// result, so the execution detail can show what the gate saw.
	for _, col := range []struct{ name, ddl string }{
		{"event_url", "ALTER TABLE task_executions ADD COLUMN event_url TEXT NOT NULL DEFAULT ''"},
		{"event_summary", "ALTER TABLE task_executions ADD COLUMN event_summary TEXT NOT NULL DEFAULT ''"},
		{"script_exit_code", "ALTER TABLE task_executions ADD COLUMN script_exit_code INTEGER"},
		{"script_outcome", "ALTER TABLE task_executions ADD COLUMN script_outcome TEXT NOT NULL DEFAULT ''"},
		{"script_stdout", "ALTER TABLE task_executions ADD COLUMN script_stdout TEXT NOT NULL DEFAULT ''"},
		{"script_stderr", "ALTER TABLE task_executions ADD COLUMN script_stderr TEXT NOT NULL DEFAULT ''"},
		{"script_duration_ms", "ALTER TABLE task_executions ADD COLUMN script_duration_ms INTEGER NOT NULL DEFAULT 0"},
	} {
		var exists int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('task_executions') WHERE name=?", col.name).Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec(col.ddl); err != nil {
				return fmt.Errorf("failed to add task_executions.%s column: %w", col.name, err)
			}
		}
	}

	// Migrate: add summary column for task execution summarization
	var hasSummary int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('task_executions') WHERE name='summary'").Scan(&hasSummary)
	if hasSummary == 0 {
		if _, err := store.WriteExec("ALTER TABLE task_executions ADD COLUMN summary TEXT"); err != nil {
			return fmt.Errorf("failed to add summary column: %w", err)
		}
	}

	// Migrate: add summary_cards column for structured summary card metadata
	var hasSummaryCards int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('summaries') WHERE name='summary_cards'").Scan(&hasSummaryCards)
	if hasSummaryCards == 0 {
		if _, err := store.WriteExec("ALTER TABLE summaries ADD COLUMN summary_cards TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add summary_cards column: %w", err)
		}
	}

	// Migrate: chat_metadata extended token/trace columns (per-agent _meta
	// extensions — CodeBuddy cache detail + trace identity, Claude/Codex
	// reasoning tokens). All default to zero/empty so existing rows remain valid.
	chatMetaCols := []struct {
		name string
		ddl  string
	}{
		{"cached_read_tokens", "ALTER TABLE chat_metadata ADD COLUMN cached_read_tokens INTEGER DEFAULT 0"},
		{"cached_write_tokens", "ALTER TABLE chat_metadata ADD COLUMN cached_write_tokens INTEGER DEFAULT 0"},
		{"thought_tokens", "ALTER TABLE chat_metadata ADD COLUMN thought_tokens INTEGER DEFAULT 0"},
		{"total_tokens", "ALTER TABLE chat_metadata ADD COLUMN total_tokens INTEGER DEFAULT 0"},
		{"request_id", "ALTER TABLE chat_metadata ADD COLUMN request_id TEXT DEFAULT ''"},
		{"trace_id", "ALTER TABLE chat_metadata ADD COLUMN trace_id TEXT DEFAULT ''"},
		{"response_model_id", "ALTER TABLE chat_metadata ADD COLUMN response_model_id TEXT DEFAULT ''"},
		// Per-agent _meta cache splits, cost, category breakdown and full trace
		// identity (added for statistics). All default so old rows stay valid.
		{"cache_creation_tokens", "ALTER TABLE chat_metadata ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0"},
		{"cache_hit_tokens", "ALTER TABLE chat_metadata ADD COLUMN cache_hit_tokens INTEGER DEFAULT 0"},
		{"cache_miss_tokens", "ALTER TABLE chat_metadata ADD COLUMN cache_miss_tokens INTEGER DEFAULT 0"},
		{"credit", "ALTER TABLE chat_metadata ADD COLUMN credit REAL DEFAULT 0"},
		{"usage_by_category", "ALTER TABLE chat_metadata ADD COLUMN usage_by_category TEXT DEFAULT ''"},
		{"session_id", "ALTER TABLE chat_metadata ADD COLUMN session_id TEXT DEFAULT ''"},
		{"agent_message_id", "ALTER TABLE chat_metadata ADD COLUMN agent_message_id TEXT DEFAULT ''"},
		{"message_request_id", "ALTER TABLE chat_metadata ADD COLUMN message_request_id TEXT DEFAULT ''"},
		{"request_model_name", "ALTER TABLE chat_metadata ADD COLUMN request_model_name TEXT DEFAULT ''"},
		{"finish_reason", "ALTER TABLE chat_metadata ADD COLUMN finish_reason TEXT DEFAULT ''"},
		{"outcome", "ALTER TABLE chat_metadata ADD COLUMN outcome TEXT DEFAULT ''"},
		{"agent_phase", "ALTER TABLE chat_metadata ADD COLUMN agent_phase TEXT DEFAULT ''"},
		// NOTE: project_id/backend/agent_id/clawbench_session_id (the ledger
		// attribution columns) are added in the pre-migration block before
		// createTables, because createTables creates an index over them.
	}
	for _, col := range chatMetaCols {
		var hasCol int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_metadata') WHERE name=?", col.name).Scan(&hasCol)
		if hasCol == 0 {
			if _, err := store.WriteExec(col.ddl); err != nil {
				return fmt.Errorf("failed to add chat_metadata.%s column: %w", col.name, err)
			}
		}
	}

	// Migrate: drop the chat_metadata → chat_history foreign key so the usage
	// ledger survives message/session deletion, and backfill the denormalized
	// attribution columns. Runs after the column additions above.
	if err := migrateChatMetadataLedger(); err != nil {
		return fmt.Errorf("failed to migrate chat_metadata to standalone ledger: %w", err)
	}

	// Migrate: add source_session_id column for "continue conversation" feature
	var hasSourceSessionID int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='source_session_id'").Scan(&hasSourceSessionID)
	if hasSourceSessionID == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN source_session_id TEXT DEFAULT NULL"); err != nil {
			return fmt.Errorf("failed to add source_session_id column: %w", err)
		}
		if _, err := store.WriteExec("CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL"); err != nil {
			return fmt.Errorf("failed to create source_session_id index: %w", err)
		}
	}

	// Migrate: add source_session_id column for "continue conversation" feature
	var hasTransport int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='transport'").Scan(&hasTransport)
	if hasTransport == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN transport TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add transport column: %w", err)
		}
	}

	// Migrate: add auto_approve column for per-session auto-approve (甩手掌柜) mode
	var hasAutoApprove int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='auto_approve'").Scan(&hasAutoApprove)
	if hasAutoApprove == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add auto_approve column: %w", err)
		}
	}

	// Migrate: add context_state column for persisting session context info (mode, thinking, usage)
	var hasContextState int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='context_state'").Scan(&hasContextState)
	if hasContextState == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN context_state TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add context_state column: %w", err)
		}
	}

	// Migrate: add compacted column — set when the agent compacts the session's
	// context (auto or user-triggered /compact). Compaction rewrites the
	// conversation into a summary, so the injected system prompt (tool rules,
	// user-interaction contract) may no longer be present in context; the next
	// turn re-injects it once regardless of the periodic interval setting.
	// The flag is consumed (cleared) by BuildChatRequest, so it never becomes a
	// permanent per-turn injection.
	var hasCompacted int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='compacted'").Scan(&hasCompacted)
	if hasCompacted == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN compacted INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add compacted column: %w", err)
		}
	}

	// Migrate: add title_renamed column. Set to 1 when the user manually renames
	// a session, so the first-message auto-title does not overwrite their choice.
	// DEPRECATED: superseded by title_source below; retained for old readers only.
	var hasTitleRenamed int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='title_renamed'").Scan(&hasTitleRenamed)
	if hasTitleRenamed == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN title_renamed INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add title_renamed column: %w", err)
		}
	}

	// Migrate: add title_source column — the source/priority of the session
	// title: 'placeholder' (auto placeholder like "New Session 3", replaceable
	// by the first message) < 'auto' (derived from the first user message) <
	// 'custom' (deliberately chosen: manual rename, meaningful title at
	// creation, task name, fork, imported title). A write may only overwrite a
	// title of strictly lower rank, so a custom title is never clobbered by the
	// first-message auto-title. This replaces title_renamed as the source of
	// truth (see the deprecation note on title_renamed above).
	var hasTitleSource int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='title_source'").Scan(&hasTitleSource)
	if hasTitleSource == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN title_source TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add title_source column: %w", err)
		}
		// Backfill existing rows: title_renamed=1 -> custom; otherwise a session
		// with at least one user message was auto-titled -> auto; a session with
		// no user messages still holds its creation placeholder -> placeholder.
		// (title_renamed alone cannot distinguish placeholder from auto.)
		if _, err := store.WriteExec(`UPDATE chat_sessions SET title_source = CASE
			WHEN title_renamed = 1 THEN 'custom'
			WHEN EXISTS (SELECT 1 FROM chat_history h WHERE h.session_id = chat_sessions.id AND h.role = 'user') THEN 'auto'
			ELSE 'placeholder' END`); err != nil {
			return fmt.Errorf("failed to backfill title_source: %w", err)
		}
	}

	// Migrate: add pinned column for session pin-to-top feature
	var hasPinned int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='pinned'").Scan(&hasPinned)
	if hasPinned == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add pinned column: %w", err)
		}
		// Rebuild covering index to include pinned for optimal ORDER BY pinned DESC, created_at DESC
		if _, err := store.WriteExec("DROP INDEX IF EXISTS idx_sessions_order"); err != nil {
			return fmt.Errorf("failed to drop old idx_sessions_order: %w", err)
		}
		if _, err := store.WriteExec("CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_id, archived, pinned DESC, created_at DESC, id DESC)"); err != nil {
			return fmt.Errorf("failed to create new idx_sessions_order: %w", err)
		}
	}

	// Migrate: add sort_order column for manual session drag-reordering (#492).
	//
	// Ordering is now purely manual: `ORDER BY sort_order ASC, created_at DESC,
	// id DESC`. `pinned` keeps its marker but loses its sort privilege (the user
	// chose a pure manual order), so the list is no longer pinned-first.
	//
	// Every pre-existing row is backfilled with 0 — i.e. "no manual order yet".
	// The created_at DESC tiebreak then reproduces the previous newest-first
	// list exactly; pinned rows do move down, which is the intended consequence
	// of dropping the pin privilege. New sessions also default to 0 and win the
	// tiebreak on being newest, so they land on top.
	//
	// The covering index is rebuilt to lead with sort_order. It deliberately
	// keeps `pinned` out of the key: the column is no longer part of the sort
	// order. That also matters because SQLite refuses to DROP a column
	// referenced by an index — the same reason this index must not gain new
	// droppable columns.
	var hasSortOrder int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='sort_order'").Scan(&hasSortOrder)
	if hasSortOrder == 0 {
		// ADD COLUMN with a non-null default already backfills every existing
		// row with that default, so no separate UPDATE is needed.
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add sort_order column: %w", err)
		}
		if _, err := store.WriteExec("DROP INDEX IF EXISTS idx_sessions_order"); err != nil {
			return fmt.Errorf("failed to drop old idx_sessions_order: %w", err)
		}
		if _, err := store.WriteExec("CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_id, archived, sort_order ASC, created_at DESC, id DESC)"); err != nil {
			return fmt.Errorf("failed to create new idx_sessions_order: %w", err)
		}
	}

	// Migrate: pinned leads the session sort order again.
	//
	// Pinned sessions are a fixed block at the top (`ORDER BY pinned DESC,
	// sort_order ASC, created_at DESC`); a plain session can never sort above
	// them and they are excluded from drag-reordering. The covering index must
	// therefore lead with pinned, so this rebuilds it when the existing
	// definition does not already match.
	//
	// Detected from sqlite_master rather than guarded on a column's existence:
	// installs that already ran the sort_order migration have the column but an
	// index WITHOUT the pinned prefix, so a column check would skip the rebuild.
	// The desired definition is compared by its full SQL text, so this is a
	// no-op on every subsequent startup.
	if err := rebuildSessionOrderIndexIfNeeded(); err != nil {
		return err
	}

	// Migrate: create session tag registry + session↔tag links.
	//
	// Tags are a separate registry (not a JSON column on chat_sessions) so a
	// label can be deleted globally and so the candidate list for a project is
	// a cheap indexed lookup. `scope` is 'project' or 'global':
	//   - project tags are visible/selectable only inside their project_id
	//   - global tags are visible/selectable in every project (project_id = 0)
	//
	// Uniqueness is (name, project_id), NOT name alone: two projects may each
	// own a label called "bug" without one leaking into the other's candidate
	// list. Global tags live at project_id = 0 (store.GlobalScopeProjectID) and
	// therefore never collide with a project row.
	//
	// project_id is NOT NULL deliberately. SQLite treats NULLs as distinct in a
	// UNIQUE constraint, so a nullable column would let two global tags named
	// "bug" coexist and silently break this uniqueness.
	// Both tables are created unconditionally (CREATE TABLE IF NOT EXISTS), so
	// existing databases pick them up on the next startup.
	if _, err := store.WriteExec(`
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
		CREATE INDEX IF NOT EXISTS idx_session_tags_project ON session_tags(project_id, name);
		CREATE INDEX IF NOT EXISTS idx_session_tag_links_session ON session_tag_links(session_id);
		CREATE INDEX IF NOT EXISTS idx_session_tag_links_tag ON session_tag_links(tag_id);
	`); err != nil {
		return fmt.Errorf("failed to create session tag tables: %w", err)
	}

	// Migrate: add host column to forwarded_ports for custom target host
	var hasForwardedPortHost int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='host'").Scan(&hasForwardedPortHost)
	if hasForwardedPortHost == 0 {
		if _, err := store.WriteExec("ALTER TABLE forwarded_ports ADD COLUMN host TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add host column to forwarded_ports: %w", err)
		}
	}

	// Migrate: add local_port column for auto-assigned local port
	// For existing rows, local_port = port (backward compatible)
	var hasForwardedPortLocalPort int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='local_port'").Scan(&hasForwardedPortLocalPort)
	if hasForwardedPortLocalPort == 0 {
		if _, err := store.WriteExec("ALTER TABLE forwarded_ports ADD COLUMN local_port INTEGER"); err != nil {
			return fmt.Errorf("failed to add local_port column to forwarded_ports: %w", err)
		}
		// Backfill: local_port = port for existing rows
		if _, err := store.WriteExec("UPDATE forwarded_ports SET local_port = port WHERE local_port IS NULL"); err != nil {
			return fmt.Errorf("failed to backfill local_port in forwarded_ports: %w", err)
		}
	}

	// Migrate: add enabled column for user-controlled enable/disable.
	// Existing ports default to enabled=true (backward compatible).
	var hasForwardedPortEnabled int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='enabled'").Scan(&hasForwardedPortEnabled)
	if hasForwardedPortEnabled == 0 {
		if _, err := store.WriteExec("ALTER TABLE forwarded_ports ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("failed to add enabled column to forwarded_ports: %w", err)
		}
	}

	// Migrate: add direction column for reverse (ssh -R) port mappings.
	// Existing rows are classic ssh -L forwards (backward compatible).
	var hasForwardedPortDirection int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='direction'").Scan(&hasForwardedPortDirection)
	if hasForwardedPortDirection == 0 {
		if _, err := store.WriteExec("ALTER TABLE forwarded_ports ADD COLUMN direction TEXT NOT NULL DEFAULT 'forward'"); err != nil {
			return fmt.Errorf("failed to add direction column to forwarded_ports: %w", err)
		}
	}

	// Migrate: add custom_system_prompt column to agents for user-editable system prompt
	var hasCustomSystemPrompt int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='custom_system_prompt'").Scan(&hasCustomSystemPrompt)
	if hasCustomSystemPrompt == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN custom_system_prompt TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add custom_system_prompt column to agents: %w", err)
		}
	}

	// Migrate: drop the legacy agents.system_prompt column (see the function for why).
	if err := migrateLegacyAgentPrompts(store.WriteDBRaw()); err != nil {
		return err
	}

	// Migrate: drop the dead ACP state columns from agents (see the function for why).
	if err := migrateLegacyAgentCapabilityColumns(store.WriteDBRaw()); err != nil {
		return err
	}

	// Migrate: drop deleted column from chat_history.
	// Archival is handled at the session level (chat_sessions.archived),
	// so chat_history.deleted is redundant. Removing it simplifies queries
	// and eliminates the need to restore messages when restoring a session.
	var hasHistoryDeleted int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='deleted'").Scan(&hasHistoryDeleted)
	if hasHistoryDeleted > 0 {
		// SQLite DROP COLUMN fails if any index references the column.
		// Drop and recreate idx_history_session_id to avoid the error.
		_, _ = store.WriteExec("DROP INDEX IF EXISTS idx_history_session_id")
		if _, err := store.WriteExec("ALTER TABLE chat_history DROP COLUMN deleted"); err != nil {
			return fmt.Errorf("failed to drop deleted column from chat_history: %w", err)
		}
		_, _ = store.WriteExec("CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at)")
		slog.Info("dropped redundant deleted column from chat_history")
	}

	// Migrate: move queued messages out of chat_history into the dedicated
	// queued_messages table, then drop the now-dead queue_id/queued columns.
	//
	// Runs as ONE transaction so a crash cannot drop the columns without having
	// moved the data (which would silently lose every in-flight queued message
	// on the upgrade). The NOT EXISTS guard makes it idempotent if a previous
	// attempt committed the INSERT but failed before the DROP.
	if err := migrateQueuedMessagesToOwnTable(); err != nil {
		return fmt.Errorf("failed to migrate queued messages to own table: %w", err)
	}

	// Migrate: widen chat_history's role CHECK to allow 'system' (group-chat
	// system events). SQLite cannot ALTER a CHECK, so the table is rebuilt.
	//
	// MUST run AFTER migrateQueuedMessagesToOwnTable: that migration reads the
	// legacy queue_id/queued columns, which the rebuild's explicit column list
	// does not carry — running first would lose every queued message.
	if err := rebuildChatHistoryRoleCheckIfNeeded(); err != nil {
		return fmt.Errorf("failed to rebuild chat_history role CHECK: %w", err)
	}

	// Clean up orphaned streaming messages from previous crashes/restarts.
	// Any message with streaming=1 at startup can never be finalized since
	// its stream no longer exists. Mark them as cancelled so the UI shows
	// an interrupted state instead of silently completing.
	// SKIP when called from CLI subcommands (task/rag) — the server process
	// may still be actively streaming, and these are NOT orphaned messages.
	isServerStartup := len(runFromServer) > 0 && runFromServer[0]

	// Migrate: replace old tts_summaries table (cache_key) with new schema (message_id).
	// The old table has cache_key as primary key; the new table uses message_id.
	// Since we don't do backward compatibility, drop the old table if it exists
	// and recreate with the new schema.
	var hasTTSCacheKey int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('tts_summaries') WHERE name='cache_key'").Scan(&hasTTSCacheKey)
	if hasTTSCacheKey > 0 {
		// Old table exists with cache_key — drop and recreate
		if _, err := store.WriteExec("DROP TABLE tts_summaries"); err != nil {
			return fmt.Errorf("failed to drop old tts_summaries table: %w", err)
		}
		if _, err := store.WriteExec(`
			CREATE TABLE tts_summaries (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				message_id   INTEGER NOT NULL,
				tts_summary  TEXT NOT NULL,
				created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(message_id)
			);
		`); err != nil {
			return fmt.Errorf("failed to create new tts_summaries table: %w", err)
		}
	}
	// Migrate: drop the legacy ai_raw_responses table. Raw ACP/CLI backend
	// output used to be persisted there for debugging, but a single turn could
	// produce a multi-hundred-MB row whose INSERT held the global write lock for
	// tens of seconds — stalling other sessions' streaming flushes and causing
	// their stream events to be dropped (silent assistant-output truncation).
	// The feature is removed, so drop any leftover table (and its indexes).
	// NOTE: DROP TABLE only frees pages to the freelist — the DB file keeps its
	// high-water mark. Reclaiming disk requires an offline `VACUUM`, which is
	// deliberately NOT run here (a full-file rewrite holding writeMu at startup
	// would be the very kind of long blocking write this removal is fixing).
	var hasRawResponses int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ai_raw_responses'").Scan(&hasRawResponses)
	if hasRawResponses > 0 {
		_, _ = store.WriteExec("DROP INDEX IF EXISTS idx_raw_responses_session")
		_, _ = store.WriteExec("DROP INDEX IF EXISTS idx_raw_responses_message")
		if _, err := store.WriteExec("DROP TABLE ai_raw_responses"); err != nil {
			slog.Warn("failed to drop legacy ai_raw_responses table", slog.String("err", err.Error()))
		} else {
			slog.Info("dropped legacy ai_raw_responses table")
		}
	}

	// Create new tts_summaries table if it doesn't exist yet (fresh install)
	var hasTTSSummaries int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tts_summaries'").Scan(&hasTTSSummaries)
	if hasTTSSummaries == 0 {
		if _, err := store.WriteExec(`
			CREATE TABLE tts_summaries (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				message_id   INTEGER NOT NULL,
				tts_summary  TEXT NOT NULL,
				created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(message_id)
			);
		`); err != nil {
			return fmt.Errorf("failed to create tts_summaries table: %w", err)
		}
	}

	// The read connection pool was opened by store.Open before schema creation,
	// so readers throughout InitDB use store.ReadDB() uniformly.
	if isServerStartup {
		rows, err := store.ReadDB().Query("SELECT id, content FROM chat_history WHERE streaming = 1")
		if err != nil {
			return fmt.Errorf("failed to query orphaned streaming messages: %w", err)
		}
		defer func() { _ = rows.Close() }()
		type orphanMsg struct {
			id      int64
			content string
		}
		var orphans []orphanMsg
		for rows.Next() {
			var m orphanMsg
			if err := rows.Scan(&m.id, &m.content); err != nil {
				return fmt.Errorf("failed to scan orphaned streaming message: %w", err)
			}
			orphans = append(orphans, m)
		}

		for _, m := range orphans {
			var contentMap map[string]any
			if err := json.Unmarshal([]byte(m.content), &contentMap); err != nil {
				// Non-JSON content — wrap it
				contentMap = map[string]any{
					"blocks":    []any{map[string]any{"type": "text", "text": m.content}},
					"cancelled": true,
				}
			} else {
				contentMap["cancelled"] = true
				// Append warning block
				blocks, _ := contentMap["blocks"].([]any)
				blocks = append(blocks, map[string]any{
					"type":   "warning",
					"text":   "Server restarted, AI response interrupted",
					"reason": "restart",
				})
				contentMap["blocks"] = blocks
			}
			updatedContent, _ := json.Marshal(contentMap)
			// completed_at is stamped even for a restart-interrupted reply: the
			// row becomes visible (streaming=0) and the user has not seen it, so
			// it must be able to register as unread. Falling back to created_at
			// (turn start, possibly before last_read_at) would hide it.
			if _, err := store.WriteExec("UPDATE chat_history SET content = ?, streaming = 0, completed_at = CURRENT_TIMESTAMP WHERE id = ?", string(updatedContent), m.id); err != nil {
				slog.Error("failed to finalize orphaned streaming message", slog.Int64("id", m.id), slog.String("err", err.Error()))
			}
		}
		if len(orphans) > 0 {
			slog.Info("cleaned up orphaned streaming messages", slog.Int("count", len(orphans)))
		}
	}

	// Migrate: add ACP transport columns to agents table.
	var hasTransportCol int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='transport'").Scan(&hasTransportCol)
	if hasTransportCol == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN transport TEXT NOT NULL DEFAULT 'cli'"); err != nil {
			return fmt.Errorf("failed to add transport column: %w", err)
		}
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_command TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add acp_command column: %w", err)
		}
	}

	// Migrate: add ACP capability columns to agents table for persistent storage
	// of agent-level mode/thinking/commands/config state.
	var hasACPMods int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_available_modes'").Scan(&hasACPMods)
	if hasACPMods == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_available_modes TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_modes column: %w", err)
		}
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_available_thinking_efforts TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_thinking_efforts column: %w", err)
		}
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_available_commands TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_commands column: %w", err)
		}
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_config_options TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add acp_config_options column: %w", err)
		}
	}

	// Migrate: add the ACP-reported model list column. Without it the ACP model
	// list lives only in memory, so an agent's selectable models change across a
	// restart (CLI list before the first ACP session, ACP list after) until a new
	// session repopulates the registry.
	var hasACPModels int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_available_models'").Scan(&hasACPModels)
	if hasACPModels == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_available_models TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_models column: %w", err)
		}
	}

	// Migrate: add ACP LoadSession/ListSessions capability columns to agents table.
	var hasLoadSessionCol int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_load_session'").Scan(&hasLoadSessionCol)
	if hasLoadSessionCol == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_load_session BOOLEAN NOT NULL DEFAULT false"); err != nil {
			return fmt.Errorf("failed to add acp_load_session column: %w", err)
		}
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN acp_list_sessions BOOLEAN NOT NULL DEFAULT false"); err != nil {
			return fmt.Errorf("failed to add acp_list_sessions column: %w", err)
		}
	}

	// Migrate: add is_default column to recent_projects for server-side default project.
	var hasIsDefault int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('recent_projects') WHERE name='is_default'").Scan(&hasIsDefault)
	if hasIsDefault == 0 {
		if _, err := store.WriteExec("ALTER TABLE recent_projects ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add is_default column: %w", err)
		}
		// Backfill: set the most recently accessed project as default
		_, _ = store.WriteExec("UPDATE recent_projects SET is_default = 1 WHERE id = (SELECT id FROM recent_projects ORDER BY accessed_at DESC LIMIT 1)")
	}

	// Migrate: add preferred_mode column to agents for user's default ACP mode preference.
	var hasPreferredMode int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='preferred_mode'").Scan(&hasPreferredMode)
	if hasPreferredMode == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN preferred_mode TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add preferred_mode column: %w", err)
		}
	}

	// Migrate: add auto_approve column to agents for per-agent default of the
	// new-session auto-approve toggle (front-end default only; the session's
	// real flag lives in chat_sessions.auto_approve).
	var hasAgentAutoApprove int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='auto_approve'").Scan(&hasAgentAutoApprove)
	if hasAgentAutoApprove == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add auto_approve column: %w", err)
		}
	}

	// Migrate: add avatar column to agents for user-configured DiceBear avatars
	// (raw SVG string; "" = use the built-in per-backend icon).
	var hasAgentAvatar int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='avatar'").Scan(&hasAgentAvatar)
	if hasAgentAvatar == 0 {
		if _, err := store.WriteExec("ALTER TABLE agents ADD COLUMN avatar TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add avatar column to agents: %w", err)
		}
	}

	// Migrate: ensure the project scope column on the quick-send /
	// quick-command tables (project-scoped / 仅本项目 items). NULL means global;
	// a value scopes the item to that project only.
	if err := migrateQuickProjectScope(); err != nil {
		return err
	}

	// Migrate: drop source column from agents — no longer used for agent origin tracking.
	var hasAgentSource int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='source'").Scan(&hasAgentSource)
	if hasAgentSource > 0 {
		_, _ = store.WriteExec("DROP INDEX IF EXISTS idx_agents_source")
		if _, err := store.WriteExec("ALTER TABLE agents DROP COLUMN source"); err != nil {
			return fmt.Errorf("failed to drop source column from agents: %w", err)
		}
		slog.Info("dropped source column from agents table")
	}

	// Migrate: add duration_ms column to chat_tool_calls for per-tool execution time.
	var hasToolCallDuration int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_tool_calls') WHERE name='duration_ms'").Scan(&hasToolCallDuration)
	if hasToolCallDuration == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_tool_calls ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add duration_ms column to chat_tool_calls: %w", err)
		}
	}

	// Migrate: add message_id column to chat_recommendations so a recommendation
	// can be bound to the exact assistant message it was generated for. This lets
	// the client reject stale recommendations (from an earlier reply) instead of
	// briefly showing the previous reply's recommendation.
	var hasRecMessageID int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_recommendations') WHERE name='message_id'").Scan(&hasRecMessageID)
	if hasRecMessageID == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_recommendations ADD COLUMN message_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add message_id column to chat_recommendations: %w", err)
		}
	}

	// Migrate: add root column to file_shares. Pre-existing rows get '' and
	// readers fall back to the shared file's own directory — the safe
	// direction, since it can only narrow what the link exposes.
	var hasShareRoot int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('file_shares') WHERE name='root'").Scan(&hasShareRoot)
	if hasShareRoot == 0 {
		if _, err := store.WriteExec("ALTER TABLE file_shares ADD COLUMN root TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add root column to file_shares: %w", err)
		}
	}

	// Migrate: add last_session_id to the push subscriber tables. This is the
	// sticky target for messages that carry no "@{shortID}" prefix — a plain
	// text or a file/image sent from DingTalk/Feishu goes to the session the
	// user last addressed. Pre-existing rows get '' and the handler falls back
	// to the "/ls" hint rather than guessing a target.
	for _, tbl := range []string{"dingtalk_subscribers", "feishu_subscribers"} {
		var exists int
		_ = store.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='last_session_id'", tbl,
		).Scan(&exists)
		if exists == 0 {
			if _, err := store.WriteExec(fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN last_session_id TEXT NOT NULL DEFAULT ''", tbl,
			)); err != nil {
				return fmt.Errorf("failed to add %s.last_session_id column: %w", tbl, err)
			}
		}
	}

	// Migrate: extract metadata from chat_history.content into chat_metadata table.
	// This is a one-time migration for existing data; new messages are saved
	// to chat_metadata automatically via SaveMetadata().
	runOnce(migMetadataFromContent, MigrateMetadataFromContent)

	// Migrate: convert task_execution summaries to chat_message summaries.
	// Tasks now store summaries as target_type='chat_message' keyed by
	// the assistant message ID (chat_history.id), same as interactive sessions.
	// This converts any existing 'task_execution' summaries to the new format.
	runOnce(migTaskExecSummaries, MigrateTaskExecutionSummaries)

	// Migrate: extract tool_use input/output from chat_history.content into
	// chat_tool_calls table and rewrite content to slim format (no input/output).
	runOnce(migToolCallsFromContent, MigrateToolCallsFromContent)

	// Migrate: rebuild chat_thinking with a seq column for incremental streaming
	// persistence (existing single rows become seq=0). Must run BEFORE
	// MigrateThinkingFromContent, whose inserts must match the new constraint.
	if err := migrateChatThinkingSeq(); err != nil {
		return fmt.Errorf("failed to migrate chat_thinking schema: %w", err)
	}

	// Migrate: extract thinking text from chat_history.content into chat_thinking
	// and rewrite content to slim format (think_id instead of text).
	runOnce(migThinkingFromContent, MigrateThinkingFromContent)

	return nil
}

// migrateChatMetadataLedger converts chat_metadata into a standalone usage
// ledger that outlives the messages it describes.
//
// Older databases declared `FOREIGN KEY (message_id) REFERENCES chat_history(id)
// ON DELETE CASCADE`, so hard-deleting a session (or rewinding / replacing its
// history) silently destroyed the token/cost record for work that really was
// performed, under-counting usage statistics. SQLite cannot drop a constraint
// with ALTER TABLE, so the table is rebuilt without the FK. Denormalized
// attribution columns (project_id/backend/agent_id/clawbench_session_id) are
// then backfilled from the still-present chat_history/chat_sessions rows.
//
// Idempotent: skips when the table has no foreign key (fresh installs create it
// without one, and a rerun after a partial migration is a no-op). Runs inside a
// single write transaction — any failure rolls back to the original table.
func migrateChatMetadataLedger() error {
	var tableExists int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_metadata'").Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}

	// PRAGMA foreign_key_list returns one row per FK; empty means the ledger is
	// already standalone.
	var fkCount int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list('chat_metadata')").Scan(&fkCount); err != nil {
		return err
	}
	if fkCount > 0 {
		if err := rebuildChatMetadataWithoutFK(); err != nil {
			return err
		}
		slog.Info("migrated chat_metadata to standalone usage ledger (foreign key removed)")
	}

	// Backfill attribution for rows written before these columns existed. Only
	// rows whose chat_history row still exists can be recovered; rows already
	// orphaned by a past cascade delete stay empty. The `h.project_id != 0`
	// term keeps this a true no-op on later startups: a row whose history
	// project is itself unattributed (0) cannot be recovered, so it must not
	// match the predicate again (otherwise the UPDATE would re-run every boot).
	var needsBackfill int
	if err := store.ReadDB().QueryRow(`
		SELECT COUNT(*) FROM chat_metadata m
		WHERE m.project_id = 0
		  AND EXISTS (SELECT 1 FROM chat_history h WHERE h.id = m.message_id AND h.project_id != 0)
	`).Scan(&needsBackfill); err != nil {
		return err
	}
	if needsBackfill == 0 {
		return nil
	}

	_, err := store.WriteExec(`
		UPDATE chat_metadata SET
			project_id = COALESCE((SELECT h.project_id FROM chat_history h WHERE h.id = chat_metadata.message_id), 0),
			backend = COALESCE((SELECT h.backend FROM chat_history h WHERE h.id = chat_metadata.message_id), ''),
			clawbench_session_id = COALESCE((SELECT h.session_id FROM chat_history h WHERE h.id = chat_metadata.message_id), ''),
			agent_id = COALESCE((SELECT s.agent_id FROM chat_history h JOIN chat_sessions s ON s.id = h.session_id WHERE h.id = chat_metadata.message_id), '')
		WHERE project_id = 0
		  AND EXISTS (SELECT 1 FROM chat_history h WHERE h.id = chat_metadata.message_id AND h.project_id != 0)
	`)
	if err != nil {
		return fmt.Errorf("backfill ledger attribution: %w", err)
	}
	slog.Info("backfilled chat_metadata ledger attribution", slog.Int("rows", needsBackfill))
	return nil
}

// rebuildChatMetadataWithoutFK recreates chat_metadata without the message_id
// foreign key, preserving every row. Must run after the ALTER TABLE column
// additions so the source table already carries all current columns.
func rebuildChatMetadataWithoutFK() error {
	const cols = "message_id, mode, thinking_effort, transport, model, input_tokens, output_tokens, " +
		"duration_ms, wall_ms, cost_usd, stop_reason, is_error, error_message, " +
		"cached_read_tokens, cached_write_tokens, thought_tokens, total_tokens, " +
		"cache_creation_tokens, cache_hit_tokens, cache_miss_tokens, credit, " +
		"usage_by_category, session_id, request_id, trace_id, agent_message_id, " +
		"message_request_id, request_model_name, response_model_id, finish_reason, " +
		"outcome, agent_phase, project_id, backend, agent_id, clawbench_session_id, created_at"

	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`CREATE TABLE chat_metadata_new (
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
		project_id INTEGER DEFAULT 0,
		backend TEXT DEFAULT '',
		agent_id TEXT DEFAULT '',
		clawbench_session_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create chat_metadata_new: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO chat_metadata_new (" + cols + ") SELECT " + cols + " FROM chat_metadata"); err != nil {
		return fmt.Errorf("copy chat_metadata rows: %w", err)
	}

	if _, err := tx.Exec("DROP TABLE chat_metadata"); err != nil {
		return fmt.Errorf("drop chat_metadata: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE chat_metadata_new RENAME TO chat_metadata"); err != nil {
		return fmt.Errorf("rename chat_metadata_new: %w", err)
	}

	// Indexes are dropped with the old table — recreate them on the renamed one.
	for _, stmt := range []string{
		"CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model)",
		"CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at)",
		"CREATE INDEX IF NOT EXISTS idx_chat_metadata_project_created ON chat_metadata(project_id, created_at)",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("recreate chat_metadata index: %w", err)
		}
	}

	return tx.Commit()
}

// migrateChatThinkingSeq rebuilds chat_thinking with a seq column for
// incremental streaming persistence. Older databases created chat_thinking
// without seq and with UNIQUE(think_id, message_id) (one full-text row per
// think block). SQLite cannot alter a UNIQUE constraint, so the table is
// rebuilt atomically: existing rows become seq=0 single-chunk records.
//
// Idempotent: skips when the seq column already exists (fresh installs get the
// new schema directly from CREATE TABLE; a rerun after a partial migration is
// a no-op). Runs inside a single write transaction — any failure rolls back to
// the original table.
func migrateChatThinkingSeq() error {
	// Only relevant when the table exists (fresh installs create it with seq).
	var tableExists int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_thinking'").Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}
	var hasSeq int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_thinking') WHERE name='seq'").Scan(&hasSeq); err != nil {
		return err
	}
	if hasSeq > 0 {
		return nil
	}

	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`CREATE TABLE chat_thinking_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
		session_id TEXT NOT NULL,
		think_id TEXT NOT NULL,
		seq INTEGER NOT NULL DEFAULT 0,
		text TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(think_id, message_id, seq)
	)`); err != nil {
		return fmt.Errorf("create chat_thinking_new: %w", err)
	}

	// Existing rows: one per (think_id, message_id) under the old UNIQUE —
	// safe 1:1 copy as seq=0. Preserve ids so AUTOINCREMENT counters don't jump.
	// Orphan rows (message_id with no chat_history row, possible in databases
	// created before foreign keys were enforced) are skipped rather than making
	// the migration fail under the new table's FK constraint.
	if _, err := tx.Exec(`INSERT INTO chat_thinking_new
		(id, message_id, session_id, think_id, seq, text, created_at)
		SELECT t.id, t.message_id, t.session_id, t.think_id, 0, t.text, t.created_at
		FROM chat_thinking t
		WHERE EXISTS (SELECT 1 FROM chat_history h WHERE h.id = t.message_id)`); err != nil {
		return fmt.Errorf("copy chat_thinking rows: %w", err)
	}

	if _, err := tx.Exec("DROP TABLE chat_thinking"); err != nil {
		return fmt.Errorf("drop chat_thinking: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE chat_thinking_new RENAME TO chat_thinking"); err != nil {
		return fmt.Errorf("rename chat_thinking_new: %w", err)
	}

	// Indexes are dropped with the old table — recreate them on the renamed one.
	if _, err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id)"); err != nil {
		return fmt.Errorf("recreate idx_thinking_message: %w", err)
	}
	if _, err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC)"); err != nil {
		return fmt.Errorf("recreate idx_thinking_session: %w", err)
	}

	slog.Info("migrated chat_thinking to chunked storage (seq column added)")
	return tx.Commit()
}

// chatHistoryRoleCheckMigrationCols is the canonical chat_history column set,
// in production order. The rebuilt table always declares exactly these columns;
// the copy step populates only the subset the source actually carries (see
// rebuildChatHistoryRoleCheckIfNeeded), so a legacy database that predates an
// optional column is migrated rather than rejected.
var chatHistoryRoleCheckMigrationCols = []string{
	"id", "project_id", "role", "content", "files", "session_id",
	"backend", "agent_id", "streaming", "indexed", "external_message_id",
	"created_at", "completed_at",
}

// chatHistoryRoleCheckMigrationDDL is the rebuilt table: identical to the
// production CREATE TABLE except the relaxed CHECK. Only the CHECK changes —
// every column, default and NOT NULL must match the production DDL exactly, or
// the migrated database diverges from a fresh one.
const chatHistoryRoleCheckMigrationDDL = `
	CREATE TABLE chat_history_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
		content TEXT NOT NULL,
		files TEXT,
		session_id TEXT,
		backend TEXT NOT NULL DEFAULT 'claude',
		agent_id TEXT DEFAULT '',
		streaming INTEGER NOT NULL DEFAULT 0,
		indexed INTEGER NOT NULL DEFAULT 0,
		external_message_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME
	)`

// chatHistoryRoleCheckMigrationIndexes recreates every index on chat_history.
// DROP TABLE drops the table's indexes with it, so they must be rebuilt by hand.
var chatHistoryRoleCheckMigrationIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_id, backend, session_id, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_history_unread ON chat_history(project_id, role, streaming, created_at)",
	"CREATE INDEX IF NOT EXISTS idx_history_sess_unread ON chat_history(session_id, role, streaming, project_id)",
	"CREATE INDEX IF NOT EXISTS idx_history_indexing ON chat_history(streaming, indexed)",
}

// chatHistoryExistingColumns returns the set of columns currently on
// chat_history. Used by the rebuild to copy only columns that exist, so a
// database predating an optional column migrates instead of failing.
func chatHistoryExistingColumns() (map[string]bool, error) {
	rows, err := store.ReadDB().Query("SELECT name FROM pragma_table_info('chat_history')")
	if err != nil {
		return nil, fmt.Errorf("read chat_history columns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan chat_history column: %w", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat_history columns: %w", err)
	}
	return cols, nil
}

// rebuildChatHistoryRoleCheckIfNeeded widens chat_history's role CHECK from
// ('user','assistant') to include 'system', so group-chat system events (member
// joins/leaves) can be persisted without masquerading as assistant replies
// (which would pollute unread counts, auto-titles and resume detection).
//
// SQLite cannot ALTER a CHECK constraint, so the table is rebuilt atomically:
// CREATE new → INSERT SELECT → DROP → RENAME, all in one transaction.
//
// Two hazards make the naive rebuild wrong, and both are handled here:
//
//  1. DROP TABLE chat_history with PRAGMA foreign_keys=ON performs an implicit
//     DELETE FROM, which fires the ON DELETE CASCADE of chat_thinking and
//     chat_tool_calls — silently destroying every thought and tool call in the
//     database. The rebuild therefore runs on a single pinned connection with
//     foreign_keys=OFF (the procedure the SQLite docs prescribe for ALTER
//     TABLE), restoring it before the connection returns to the pool.
//
//  2. This must run AFTER migrateQueuedMessagesToOwnTable. That migration reads
//     the legacy queue_id/queued columns; the rebuild's explicit column list
//     does not carry them, so running first would make its SELECT fail and
//     permanently lose every in-flight queued message on the upgrade.
//
// Idempotent: skips when the stored DDL already contains 'system' (fresh
// installs get the new CHECK directly from CREATE TABLE). A crash mid-rebuild
// rolls back to the original table because the whole thing is one transaction.
func rebuildChatHistoryRoleCheckIfNeeded() error {
	if !store.DBReady() {
		return nil
	}
	needed, err := chatHistoryRoleCheckRebuildNeeded()
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	colList, err := chatHistoryCopyColumnList()
	if err != nil {
		return err
	}
	if colList == "" {
		return nil
	}

	// Row count before the copy, for the lossless-copy check after COMMIT.
	var rowsBefore int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history").Scan(&rowsBefore); err != nil {
		return fmt.Errorf("count chat_history rows: %w", err)
	}

	ctx := context.Background()
	// Take the global write mutex, then pin a single connection for the whole
	// rebuild: PRAGMA foreign_keys is per-connection, so it must be toggled on
	// the exact connection that runs DROP TABLE, and that connection must not
	// return to the pool with FK off.
	store.WriteLock()
	conn, err := store.WriteDBRaw().Conn(ctx)
	if err != nil {
		store.WriteUnlock()
		return fmt.Errorf("acquire write connection for chat_history rebuild: %w", err)
	}
	defer func() {
		// Restore FK enforcement before the connection goes back to the pool.
		_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
		_ = conn.Close()
		store.WriteUnlock()
	}()

	if err := execChatHistoryRoleCheckRebuild(ctx, conn, colList); err != nil {
		return err
	}

	// Sanity: the copy must be lossless. A row-count mismatch here means the
	// INSERT SELECT silently dropped rows (e.g. a NULL in a NOT NULL column) —
	// fail loudly rather than serve a half-populated history.
	var rowsAfter int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history").Scan(&rowsAfter); err != nil {
		return fmt.Errorf("verify chat_history row count: %w", err)
	}
	if rowsAfter != rowsBefore {
		return fmt.Errorf("chat_history rebuild lost rows: before=%d after=%d", rowsBefore, rowsAfter)
	}
	slog.Info("rebuilt chat_history to allow role='system'", slog.Int("rows", rowsAfter))
	return nil
}

// chatHistoryRoleCheckRebuildNeeded reports whether chat_history still carries a
// role CHECK that lacks 'system'. A missing table, an already-widened CHECK, or
// a table with no role CHECK at all all return false (nothing to do).
func chatHistoryRoleCheckRebuildNeeded() (bool, error) {
	var ddl string
	if err := store.ReadDB().QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='chat_history'",
	).Scan(&ddl); err != nil {
		if err == sql.ErrNoRows {
			return false, nil // no chat_history yet (fresh DB, created by createTables)
		}
		return false, fmt.Errorf("read chat_history DDL: %w", err)
	}
	// A table that already accepts 'system' is left untouched.
	if strings.Contains(ddl, "'system'") {
		return false, nil
	}
	// Only rebuild when there is actually a role CHECK to widen: a table with no
	// role CHECK already accepts 'system', so rebuilding it would be pure churn
	// (and would needlessly rewrite test fixtures that omit the constraint).
	if !strings.Contains(ddl, "CHECK(role") && !strings.Contains(ddl, "CHECK (role") {
		return false, nil
	}
	return true, nil
}

// chatHistoryCopyColumnList returns the comma-separated intersection of the
// canonical column set and the columns the live table actually carries.
//
// Production always has every column by now (the ALTERs in InitDB add the late
// ones), but a very old database can still predate an optional column such as
// files; naming a column that does not exist would make the INSERT ... SELECT
// fail and the server refuse to start after an upgrade.
func chatHistoryCopyColumnList() (string, error) {
	srcCols, err := chatHistoryExistingColumns()
	if err != nil {
		return "", err
	}
	if len(srcCols) == 0 {
		return "", nil
	}
	// id/project_id/role/content are NOT NULL and must be present; a table
	// missing any of them is not a chat_history we can safely rebuild.
	for _, required := range []string{"id", "project_id", "role", "content"} {
		if !srcCols[required] {
			return "", fmt.Errorf("chat_history missing required column %q; refusing to rebuild", required)
		}
	}
	copyCols := make([]string, 0, len(srcCols))
	for _, c := range chatHistoryRoleCheckMigrationCols {
		if srcCols[c] {
			copyCols = append(copyCols, c)
		}
	}
	return strings.Join(copyCols, ", "), nil
}

// execChatHistoryRoleCheckRebuild runs the CREATE → INSERT SELECT → DROP →
// RENAME sequence plus index recreation in one transaction on conn. The caller
// has pinned conn and disabled foreign keys on it.
func execChatHistoryRoleCheckRebuild(ctx context.Context, conn *sql.Conn, colList string) error {
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for chat_history rebuild: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin chat_history rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, chatHistoryRoleCheckMigrationDDL); err != nil {
		return fmt.Errorf("create chat_history_new: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO chat_history_new ("+colList+") SELECT "+colList+" FROM chat_history"); err != nil {
		return fmt.Errorf("copy chat_history rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE chat_history"); err != nil {
		return fmt.Errorf("drop chat_history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE chat_history_new RENAME TO chat_history"); err != nil {
		return fmt.Errorf("rename chat_history_new: %w", err)
	}
	// Indexes are dropped with the old table — recreate them on the renamed one.
	for _, stmt := range chatHistoryRoleCheckMigrationIndexes {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("recreate chat_history index: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit chat_history rebuild: %w", err)
	}
	return nil
}

// migrateQuickProjectScope ensures the quick-send and quick-command tables carry
// the project scope column and rebuilds the per-project auto_execute unique
// index. Idempotent: skips if the column already exists.
//
// The column is project_id, not project_path: migrateProjectsToIDs converts
// these tables along with every other project-scoped one. This function only
// has to cover databases old enough to predate project scoping entirely.
func migrateQuickProjectScope() error {
	for _, table := range []string{"terminal_quick_commands", "chat_quick_send"} {
		var hasCol int
		_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('" + table + "') WHERE name='project_id'").Scan(&hasCol)
		if hasCol == 0 {
			if _, err := store.WriteExec("ALTER TABLE " + table + " ADD COLUMN project_id INTEGER DEFAULT NULL"); err != nil {
				return fmt.Errorf("failed to add project_id column to %s: %w", table, err)
			}
		}
	}
	// Rebuild the auto_execute unique index to be per-project scope. 0 stands in
	// for the NULL (global) scope so a global row and one per project coexist.
	_, _ = store.WriteExec("DROP INDEX IF EXISTS idx_quick_commands_auto_execute")
	if _, err := store.WriteExec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_commands_auto_execute
		ON terminal_quick_commands(COALESCE(project_id, 0), auto_execute)
		WHERE auto_execute = 1`); err != nil {
		return fmt.Errorf("failed to rebuild auto_execute index: %w", err)
	}
	return nil
}

// MigrateMetadataFromContent scans chat_history rows with metadata embedded in
// the content JSON and inserts them into the chat_metadata table.
// Rows already present in chat_metadata are skipped.
// Runs in batches of 500 to avoid excessive memory usage on large databases.
//
// Copy-origin sessions are excluded: ForkSession / continue-conversation copy
// assistant content verbatim (including the embedded metadata), so their
// messages would otherwise be re-migrated here and double-count usage that the
// source session already contributed. ACP replay-replace messages carry only
// {"transport":"acp"} and are likewise skipped.
func MigrateMetadataFromContent() bool {
	// Count how many rows need migration
	var needed int
	if err := store.ReadDB().QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"metadata"%'
		  AND NOT EXISTS (SELECT 1 FROM chat_metadata m WHERE m.message_id = h.id)
		  AND NOT EXISTS (
			SELECT 1 FROM chat_sessions s
			WHERE s.id = h.session_id AND s.source_session_id IS NOT NULL
		  )
	`).Scan(&needed); err != nil {
		slog.Error("metadata migration: count failed", slog.String("err", err.Error()))
		return false
	}
	if needed == 0 {
		return true
	}
	slog.Info("migrating metadata from chat_history to chat_metadata", slog.Int("rows", needed))

	batchSize := 500
	offset := 0
	migrated := 0

	for {
		batch, err := migrateMetadataBatch(batchSize, offset)
		if err != nil {
			slog.Error("metadata migration: query failed", slog.String("err", err.Error()))
			return false
		}

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			var contentMap struct {
				Metadata *ai.Metadata `json:"metadata"`
			}
			if err := json.Unmarshal([]byte(r.Content), &contentMap); err != nil || contentMap.Metadata == nil {
				continue
			}
			// SaveMetadata writes every chat_metadata column from the Metadata
			// struct (token splits, cost, category breakdown, trace identity).
			// INSERT OR IGNORE is implied: SaveMetadata uses INSERT OR REPLACE,
			// which is safe here because the batch only picks rows NOT already
			// present in chat_metadata (see migrateMetadataBatch predicate).
			if err := SaveMetadata(r.ID, contentMap.Metadata); err != nil {
				slog.Warn("metadata migration: insert failed", slog.Int64("message_id", r.ID), slog.String("err", err.Error()))
				continue
			}
			migrated++
		}

		if len(batch) < batchSize {
			break
		}
		offset += batchSize
	}

	slog.Info("metadata migration complete", slog.Int("migrated", migrated), slog.Int("needed", needed))
	return true
}

// migrateMetadataBatch fetches one batch of assistant messages with metadata
// that haven't been migrated to chat_metadata yet.
func migrateMetadataBatch(batchSize, offset int) ([]struct {
	ID      int64
	Content string
}, error,
) {
	rows, err := store.ReadDB().Query(
		`
		SELECT h.id, h.content FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"metadata"%'
		  AND NOT EXISTS (SELECT 1 FROM chat_metadata m WHERE m.message_id = h.id)
		  AND NOT EXISTS (
			SELECT 1 FROM chat_sessions s
			WHERE s.id = h.session_id AND s.source_session_id IS NOT NULL
		  )
		ORDER BY h.id
		LIMIT ? OFFSET ?`,
		batchSize, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var batch []struct {
		ID      int64
		Content string
	}
	for rows.Next() {
		var r struct {
			ID      int64
			Content string
		}
		if err := rows.Scan(&r.ID, &r.Content); err != nil {
			slog.Error("metadata migration: scan failed", slog.String("err", err.Error()))
		}
		batch = append(batch, r)
	}
	return batch, nil
}

// MigrateTaskExecutionSummaries converts existing target_type='task_execution'
// summaries to target_type='chat_message' summaries keyed by the assistant
// message ID in chat_history. After this migration, all summaries use the same
// target_type, and ContinueFromExecution no longer needs to convert between types.
//
// For each task_execution summary, the migration:
//  1. Finds the corresponding chat_history assistant message via session_id
//  2. Inserts a 'chat_message' summary keyed by ch.id (if not already present)
//  3. Deletes the old 'task_execution' summary
func MigrateTaskExecutionSummaries() bool {
	// Check if there are any task_execution summaries to migrate
	var count int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&count); err != nil {
		slog.Error("task_execution summary migration: count failed", slog.String("err", err.Error()))
		return false
	}
	if count == 0 {
		return true
	}
	slog.Info("migrating task_execution summaries to chat_message", slog.Int("count", count))

	// For each task_execution summary, find the corresponding assistant message
	// and create a chat_message summary.
	// Collect all rows first to avoid holding the read connection while writing
	// (SQLite single-writer lock would deadlock if DBRead and DB share the same conn).
	rows, err := store.ReadDB().Query(`
		SELECT sm.target_id, sm.summary, te.session_id
		FROM summaries sm
		JOIN task_executions te ON te.id = sm.target_id
		WHERE sm.target_type = 'task_execution'
	`)
	if err != nil {
		slog.Error("task_execution summary migration: query failed", slog.String("err", err.Error()))
		return false
	}

	type migrationRow struct {
		ExecID    int64
		Summary   string
		SessionID string
	}
	var migrations []migrationRow
	for rows.Next() {
		var m migrationRow
		if err := rows.Scan(&m.ExecID, &m.Summary, &m.SessionID); err != nil {
			slog.Error("task_execution summary migration: scan failed", slog.String("err", err.Error()))
			continue
		}
		migrations = append(migrations, m)
	}
	defer func() { _ = rows.Close() }()

	migrated := 0
	for _, m := range migrations {
		// Find the last non-streaming assistant message for this session
		var msgID int64
		if err := store.ReadDB().QueryRow(
			"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
			m.SessionID,
		).Scan(&msgID); err != nil {
			// No assistant message found — delete the orphaned task_execution summary
			// to prevent it from sticking around forever (it can never be migrated).
			_, _ = store.WriteExec(
				"DELETE FROM summaries WHERE target_type = 'task_execution' AND target_id = ?",
				m.ExecID,
			)
			continue
		}

		// Insert as chat_message summary (if not already present)
		_, _ = store.WriteExec(
			"INSERT OR IGNORE INTO summaries (target_type, target_id, summary, created_at) VALUES ('chat_message', ?, ?, CURRENT_TIMESTAMP)",
			msgID, m.Summary,
		)

		// Delete the old task_execution summary
		_, _ = store.WriteExec(
			"DELETE FROM summaries WHERE target_type = 'task_execution' AND target_id = ?",
			m.ExecID,
		)
		migrated++
	}

	slog.Info("task_execution summary migration complete", slog.Int("migrated", migrated), slog.Int("total", count))
	return true
}

// MigrateLegacyAgentPrompts drops the legacy agents.system_prompt column on the
// package database. Exported for tests; InitDB uses migrateLegacyAgentPrompts so
// it can act on the handle it is currently opening.
func MigrateLegacyAgentPrompts() error {
	return migrateLegacyAgentPrompts(store.WriteDBRaw())
}

// migrateLegacyAgentPrompts drops the legacy agents.system_prompt column and
// clears the prompt text it left behind in custom_system_prompt.
//
// That column used to hold the composed prompt (shared prefix + user text), and
// the read path treated the stored string as the user's own prompt. The frozen
// copy was appended after the freshly composed one, so it won — which made every
// change to the built-in prompt a silent no-op on existing installs. An earlier
// migration then copied that same stored text into custom_system_prompt, so
// clearing only the dropped column would leave the stale copy injected from the
// other one.
//
// The guard is deliberately broad: any row carrying a non-empty legacy
// system_prompt has its custom_system_prompt cleared, because the stored value
// cannot be reliably split back into "shared prefix" and "what the user wrote"
// (recognizing old prefix versions is exactly what the previous migration got
// wrong). Prompts configured under the old scheme are discarded rather than
// guessed at. Only custom_system_prompt is authoritative now, and the shared
// prompt is composed fresh on every read, so anything a user sets from here on
// is unaffected.
//
// The outer guard is the column's existence, so this is a no-op once the column
// is gone.
//
// Writes go through the raw *sql.DB rather than store.WriteExec: this runs only
// from InitDB at startup, before any goroutine can write, so there is no
// concurrent writer for writeMu to serialize against. (The exported
// MigrateLegacyAgentPrompts wrapper exists for tests.)
func migrateLegacyAgentPrompts(d *sql.DB) error {
	var hasLegacy int
	if err := d.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='system_prompt'",
	).Scan(&hasLegacy); err != nil {
		return fmt.Errorf("check legacy system_prompt column: %w", err)
	}
	if hasLegacy == 0 {
		return nil
	}

	if _, err := d.Exec("UPDATE agents SET custom_system_prompt = '' WHERE system_prompt != ''"); err != nil {
		return fmt.Errorf("failed to clear legacy agent prompts: %w", err)
	}
	if _, err := d.Exec("ALTER TABLE agents DROP COLUMN system_prompt"); err != nil {
		return fmt.Errorf("failed to drop system_prompt column from agents: %w", err)
	}
	slog.Info("dropped legacy agents.system_prompt column")
	return nil
}

// legacyAgentCapabilityColumns are dead ACP state columns left over from the
// pre-registry scheme, when each agent's mode/thinking/plan/model state was
// serialized straight into its own row.
//
// That state now lives in the in-memory AgentCapabilityRegistry, persisted via
// the acp_available_* columns (see internal/ai/agent_capability.go). Nothing
// reads or writes these six columns any more, so they are pure dead weight —
// including the JSON blobs (and a cached usage snapshot) they still hold on
// existing installs.
var legacyAgentCapabilityColumns = []string{
	"acp_mode_state",
	"acp_commands",
	"acp_thinking_state",
	"acp_model_list_state",
	"acp_plan_state",
	"acp_cached_usage_state",
}

// MigrateLegacyAgentCapabilityColumns drops the dead ACP state columns on the
// package database. Exported for tests; InitDB uses
// migrateLegacyAgentCapabilityColumns so it can act on the handle it is
// currently opening.
func MigrateLegacyAgentCapabilityColumns() error {
	return migrateLegacyAgentCapabilityColumns(store.WriteDBRaw())
}

// migrateLegacyAgentCapabilityColumns drops the six dead ACP state columns from
// the agents table (see legacyAgentCapabilityColumns for why).
//
// Each column is guarded by its own existence check so the migration is
// idempotent and tolerates databases that only ever received some of them
// (the columns were added piecemeal by separate migrations). SQLite refuses
// "ALTER TABLE ... DROP COLUMN" while an index, trigger, view or constraint
// references the column; none do here — the only agents indexes are on backend
// and sort_order — so no index dance is needed.
func migrateLegacyAgentCapabilityColumns(d *sql.DB) error {
	for _, col := range legacyAgentCapabilityColumns {
		var exists int
		if err := d.QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name=?", col,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check legacy agents.%s column: %w", col, err)
		}
		if exists == 0 {
			continue
		}
		if _, err := d.Exec("ALTER TABLE agents DROP COLUMN " + col); err != nil {
			return fmt.Errorf("failed to drop legacy agents.%s column: %w", col, err)
		}
		slog.Info("dropped legacy agents column", slog.String("column", col))
	}
	return nil
}

// migrateQueuedMessagesToOwnTable moves every queued=1 row of chat_history into
// the dedicated queued_messages table, then drops chat_history.queue_id and
// chat_history.queued.
//
// Why a separate table: a queued message is not a conversation turn yet. While
// it lived in chat_history it needed a pre-allocated DB id (assigned at enqueue,
// before its reply existed) and a queue_id column to re-anchor the reply, which
// was the root of the queue's complexity. Held outside chat_history, the row is
// materialized only at dequeue time, so id order equals conversational order.
//
// The whole thing runs in ONE transaction: a crash between "INSERT moved rows"
// and "DROP columns" must not be possible, or the in-flight queue is lost. The
// NOT EXISTS guard additionally makes the INSERT idempotent for the case where
// a previous attempt committed the INSERT but failed before the DROP.
//
// A row with an empty queue_id (should not happen on current code — every
// enqueue mints one — but historic rows may) is given a deterministic
// q-migrated-<id> id so the NOT EXISTS guard can recognize it on a retry.
func migrateQueuedMessagesToOwnTable() error {
	if !store.DBReady() {
		return nil
	}
	var hasQueuedCol int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='queued'",
	).Scan(&hasQueuedCol); err != nil {
		return fmt.Errorf("check chat_history.queued column: %w", err)
	}
	if hasQueuedCol == 0 {
		return nil // already migrated (or a fresh database)
	}

	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO queued_messages (session_id, project_id, backend, queue_id, content, files, created_at)
		SELECT COALESCE(h.session_id, ''), h.project_id, h.backend,
		       CASE WHEN h.queue_id = '' OR h.queue_id IS NULL THEN 'q-migrated-' || h.id ELSE h.queue_id END,
		       h.content, h.files, h.created_at
		FROM chat_history h
		WHERE h.queued = 1
		  AND NOT EXISTS (
		      SELECT 1 FROM queued_messages q
		      WHERE q.session_id = COALESCE(h.session_id, '')
		        AND q.queue_id = CASE WHEN h.queue_id = '' OR h.queue_id IS NULL
		                              THEN 'q-migrated-' || h.id ELSE h.queue_id END
		  )`); err != nil {
		return fmt.Errorf("copy queued rows into queued_messages: %w", err)
	}

	// Delete the moved rows from chat_history. They are not conversation
	// records: they were never answered, and leaving them behind would surface
	// them as ordinary (duplicated) user messages once queued is gone.
	if _, err := tx.Exec("DELETE FROM chat_history WHERE queued = 1"); err != nil {
		return fmt.Errorf("delete moved queued rows from chat_history: %w", err)
	}

	// No index references queue_id/queued (verified against the index list in
	// InitDB), so DROP COLUMN needs no DROP INDEX dance here — unlike the
	// chat_history.deleted migration, which had to drop idx_history_session_id.
	if _, err := tx.Exec("ALTER TABLE chat_history DROP COLUMN queue_id"); err != nil {
		return fmt.Errorf("drop chat_history.queue_id: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE chat_history DROP COLUMN queued"); err != nil {
		return fmt.Errorf("drop chat_history.queued: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("migrated queued messages into queued_messages table")
	return nil
}

// MigrateToolCallsFromContent scans assistant messages that contain tool_use blocks
// with input/output still embedded in content JSON, extracts them into chat_tool_calls,
// and rewrites content to the slim format (no input/output).
// This is a one-time migration for data created before the tool-call-split feature.
// Runs in batches to avoid excessive memory usage on large databases.
func MigrateToolCallsFromContent() bool {
	// Find assistant messages that have tool_use blocks with input field in content,
	// but have no entries in chat_tool_calls yet.
	// We detect old-format data by checking for "input" key inside tool_use blocks,
	// which the slim format does not include.
	var needed int
	_ = store.ReadDB().QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"tool_use"%'
		  AND h.content LIKE '%"input"%'
		  AND h.streaming = 0
		  AND NOT EXISTS (
		    SELECT 1 FROM chat_tool_calls tc
		    WHERE tc.message_id = h.id
		    LIMIT 1
		  )
	`).Scan(&needed)
	if needed == 0 {
		return true
	}
	slog.Info("migrating tool_use input/output from chat_history to chat_tool_calls", slog.Int("rows", needed))

	batchSize := 200
	// Keyset cursor pagination: rows are slimmed (and thus removed from the
	// matching set) as they are processed, so a fixed OFFSET would drift ahead
	// and permanently skip rows. Cursor by id guarantees each row is visited
	// exactly once.
	lastID := int64(0)
	migrated := 0
	failed := 0

	for {
		rows, err := store.ReadDB().Query(
			`
			SELECT h.id, h.session_id, h.content FROM chat_history h
			WHERE h.role = 'assistant'
			  AND h.content LIKE '%"tool_use"%'
			  AND h.content LIKE '%"input"%'
			  AND h.streaming = 0
			  AND NOT EXISTS (
			    SELECT 1 FROM chat_tool_calls tc
			    WHERE tc.message_id = h.id
			    LIMIT 1
			  )
			  AND h.id > ?
			ORDER BY h.id
			LIMIT ?`,
			lastID, batchSize,
		)
		if err != nil {
			slog.Error("tool_use migration: query failed", slog.String("err", err.Error()))
			return false
		}

		type msgRow struct {
			ID        int64
			SessionID string
			Content   string
		}
		var batch []msgRow
		for rows.Next() {
			var r msgRow
			if err := rows.Scan(&r.ID, &r.SessionID, &r.Content); err != nil {
				slog.Error("tool_use migration: scan failed", slog.String("err", err.Error()))
				continue
			}
			batch = append(batch, r)
		}
		_ = rows.Close() //nolint:sqlclosecheck // batched loop: cannot defer inside for-loop

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if err := migrateToolCallsForRow(r.ID, r.SessionID, r.Content); err != nil {
				slog.Error("tool_use migration: row failed",
					slog.Int64("id", r.ID),
					slog.String("err", err.Error()))
				failed++
			} else {
				migrated++
			}
			// Advance the cursor for every visited row so a row that cannot be
			// migrated (e.g. a literal "tool_use"/"input" in a text block, or a
			// persistent DB error) is visited only once.
			lastID = r.ID
		}

		slog.Info("tool_use migration progress",
			slog.Int("migrated", migrated),
			slog.Int("failed", failed),
			slog.Int("total", needed),
			slog.Int("remaining", max(0, needed-migrated)))

		if len(batch) < batchSize {
			break
		}
	}

	slog.Info("tool_use migration complete",
		slog.Int("migrated", migrated),
		slog.Int("failed", failed),
		slog.Int("needed", needed))
	return true
}

// migrateToolCallsForRow processes a single chat_history row:
// 1. Parse content JSON, find tool_use blocks with input/output
// 2. Insert into chat_tool_calls
// 3. Rewrite content to slim format (remove input/output from tool_use blocks)
func migrateToolCallsForRow(msgID int64, sessionID, content string) error {
	var contentMap struct {
		Blocks []model.ContentBlock `json:"blocks"`
		Meta   any                  `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal([]byte(content), &contentMap); err != nil {
		return fmt.Errorf("unmarshal content: %w", err)
	}

	hasToolUse := false
	needsRewrite := false
	for i := range contentMap.Blocks {
		b := &contentMap.Blocks[i]
		if b.Type != "tool_use" || b.ID == "" {
			continue
		}
		hasToolUse = true

		// Check if this block still has input (old format)
		// Slim format blocks have nil/empty input
		if len(b.Input) > 0 {
			needsRewrite = true

			// Extract metadata before stripping input/output
			meta := ai.ExtractToolCallMetaFromInput(b.Name, b.ID, b.Input)
			b.Summary = meta.Summary
			b.DisplayName = meta.DisplayName
			b.FilePath = meta.FilePath

			// Upsert to chat_tool_calls
			inputJSON, _ := json.Marshal(b.Input)
			if err := UpsertToolCall(msgID, sessionID, b.ID, b.Name, inputJSON, b.Output, b.Status, b.Summary, b.Done, b.DurationMs); err != nil {
				// Log but continue — don't block the whole migration
				slog.Warn("tool_use migration: upsert failed",
					slog.String("toolID", b.ID),
					slog.String("err", err.Error()))
			}
		} else if b.Output != "" {
			// Block has no input but has output — still need to save output and strip it
			needsRewrite = true
			meta := ai.ExtractToolCallMetaFromInput(b.Name, b.ID, b.Input)
			b.Summary = meta.Summary
			b.DisplayName = meta.DisplayName
			b.FilePath = meta.FilePath
			inputJSON, _ := json.Marshal(b.Input)
			_ = UpsertToolCall(msgID, sessionID, b.ID, b.Name, inputJSON, b.Output, b.Status, b.Summary, b.Done, b.DurationMs)
		}
	}

	if !hasToolUse || !needsRewrite {
		return nil
	}

	// Rewrite content: MarshalJSON on each block produces slim format for tool_use
	newContentMap := map[string]any{
		"blocks": contentMap.Blocks,
	}
	if contentMap.Meta != nil {
		newContentMap["metadata"] = contentMap.Meta
	}
	newContent, err := json.Marshal(newContentMap)
	if err != nil {
		return fmt.Errorf("marshal slim content: %w", err)
	}

	_, err = store.WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", string(newContent), msgID)
	return err
}

// sessionOrderIndexSQL is the desired definition of idx_sessions_order: the
// covering index for the session list's ORDER BY (pinned DESC, sort_order ASC,
// created_at DESC, id DESC) under the WHERE (session_type, project_id,
// archived) prefix.
const sessionOrderIndexSQL = "CREATE INDEX idx_sessions_order ON chat_sessions(session_type, project_id, archived, pinned DESC, sort_order ASC, created_at DESC, id DESC)"

// rebuildSessionOrderIndexIfNeeded rebuilds idx_sessions_order when its stored
// definition does not match sessionOrderIndexSQL.
//
// The index has been reshaped twice (created_at-only → +pinned → +sort_order →
// now pinned + sort_order), and each shape change needs a rebuild because
// CREATE INDEX IF NOT EXISTS silently keeps whatever already exists. Comparing
// the stored SQL from sqlite_master is what makes the migration idempotent: an
// install already on the current shape is a no-op, while one on any older shape
// is rebuilt exactly once.
//
// Failure is returned, not swallowed: a missing/mismatched index only costs
// query planning (the list still returns correctly), but silently leaving a
// stale index would hide a broken migration from every future startup.
func rebuildSessionOrderIndexIfNeeded() error {
	var existing string
	err := store.ReadDB().QueryRow(
		"SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='index' AND name='idx_sessions_order'",
	).Scan(&existing)
	if err == nil && existing == sessionOrderIndexSQL {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to read idx_sessions_order definition: %w", err)
	}
	if _, err := store.WriteExec("DROP INDEX IF EXISTS idx_sessions_order"); err != nil {
		return fmt.Errorf("failed to drop old idx_sessions_order: %w", err)
	}
	if _, err := store.WriteExec(sessionOrderIndexSQL); err != nil {
		return fmt.Errorf("failed to create new idx_sessions_order: %w", err)
	}
	return nil
}

// GetSummary looks up a reading summary by target type and target ID.
// Returns (summary, found). Empty summary = text was too short.
func GetSummary(targetType string, targetID int64) (string, bool) {
	s, _, ok := GetSummaryWithCards(targetType, targetID)
	return s, ok
}

// SaveSummary persists a reading summary for a target (chat message or task execution).
// summary = "" means text was too short; non-empty is the actual summary.
func SaveSummary(targetType string, targetID int64, summary string) error {
	return SaveSummaryWithCards(targetType, targetID, summary, nil)
}

// GetSummaryWithCards returns summary text and card metadata.
// Returns (summary, cards, found). cards is nil when no cards persisted.
func GetSummaryWithCards(targetType string, targetID int64) (string, *model.SummaryCards, bool) {
	var summary string
	var cardsJSON string
	err := store.ReadDB().QueryRow(
		"SELECT summary, COALESCE(summary_cards, '') FROM summaries WHERE target_type = ? AND target_id = ?",
		targetType, targetID,
	).Scan(&summary, &cardsJSON)
	if err != nil {
		return "", nil, false
	}
	var cards *model.SummaryCards
	if cardsJSON != "" {
		cards = &model.SummaryCards{}
		if jerr := json.Unmarshal([]byte(cardsJSON), cards); jerr != nil {
			cards = nil
		}
	}
	return summary, cards, true
}

// SaveSummaryWithCards persists summary text and card metadata.
func SaveSummaryWithCards(targetType string, targetID int64, summary string, cards *model.SummaryCards) error {
	cardsJSON := ""
	if cards != nil {
		raw, err := json.Marshal(cards)
		if err != nil {
			return err
		}
		cardsJSON = string(raw)
	}
	_, err := store.WriteExec(
		"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, summary_cards, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)",
		targetType, targetID, summary, cardsJSON,
	)
	return err
}

// GetTTSSummaryByMessageID looks up a TTS summary by message ID.
// Returns (ttsSummary, found).
func GetTTSSummaryByMessageID(messageID int64) (string, bool) {
	var ttsSummary string
	err := store.ReadDB().QueryRow(
		"SELECT tts_summary FROM tts_summaries WHERE message_id = ?",
		messageID,
	).Scan(&ttsSummary)
	if err != nil {
		return "", false
	}
	return ttsSummary, true
}

// SaveTTSSummaryByMessageID persists a TTS summary for a chat message.
func SaveTTSSummaryByMessageID(messageID int64, ttsSummary string) error {
	_, err := store.WriteExec(
		"INSERT OR REPLACE INTO tts_summaries (message_id, tts_summary, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		messageID, ttsSummary,
	)
	return err
}

// quickCommandExtra holds the additional fields needed for terminal_quick_commands
// beyond the shared (label, command, sort_order) triplet.
type quickCommandExtra struct {
	hidden, autoExec int
	projectPath      string
}

// chatQuickSendExtra holds the additional fields needed for chat_quick_send
// beyond the shared (label, command, sort_order) triplet.
type chatQuickSendExtra struct{ projectPath string }

// QuickCommandHelpers exposes the shared CRUD helpers for terminal_quick_commands.
//
// The scan list resolves project_id back to its path with a scalar subquery, so
// scanFn and the JSON contract (project_path / project_only) are unchanged even
// though the column now holds an id. A global row has project_id IS NULL, which
// COALESCE turns into ” — i.e. exactly the old "no project" reading.
var QuickCommandHelpers = crudHelpers[QuickCommand, quickCommandExtra]{
	table:     "terminal_quick_commands",
	scanCols:  "id, label, command, hidden, auto_execute, sort_order, COALESCE((SELECT path FROM projects WHERE projects.id = terminal_quick_commands.project_id), '')",
	insertSQL: "INSERT INTO terminal_quick_commands (label, command, hidden, auto_execute, sort_order, project_id) VALUES (?, ?, ?, ?, ?, ?)",
	updateSQL: "UPDATE terminal_quick_commands SET label = ?, command = ?, hidden = ?, auto_execute = ?, project_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
	scanFn: func(rows *sql.Rows) (QuickCommand, error) {
		var cmd QuickCommand
		var hidden, autoExec int
		var proj sql.NullString
		if err := rows.Scan(&cmd.ID, &cmd.Label, &cmd.Command, &hidden, &autoExec, &cmd.SortOrder, &proj); err != nil {
			return cmd, err
		}
		cmd.Hidden = hidden == 1
		cmd.AutoExecute = autoExec == 1
		cmd.ProjectPath = proj.String
		cmd.ProjectOnly = proj.Valid && proj.String != ""
		return cmd, nil
	},
	addFn: func(cmd QuickCommand) (label string, command string, sortOrder int, extra quickCommandExtra) {
		hidden := 0
		if cmd.Hidden {
			hidden = 1
		}
		autoExec := 0
		if cmd.AutoExecute {
			autoExec = 1
		}
		return cmd.Label, cmd.Command, cmd.SortOrder, quickCommandExtra{hidden: hidden, autoExec: autoExec, projectPath: cmd.ProjectPath}
	},
}

// ChatQuickSendHelpers exposes the shared CRUD helpers for chat_quick_send.
// See QuickCommandHelpers for why the scan list resolves the path back out.
var ChatQuickSendHelpers = crudHelpers[ChatQuickSendItem, chatQuickSendExtra]{
	table:     "chat_quick_send",
	scanCols:  "id, label, command, sort_order, COALESCE((SELECT path FROM projects WHERE projects.id = chat_quick_send.project_id), '')",
	insertSQL: "INSERT INTO chat_quick_send (label, command, sort_order, project_id) VALUES (?, ?, ?, ?)",
	updateSQL: "UPDATE chat_quick_send SET label = ?, command = ?, project_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
	scanFn: func(rows *sql.Rows) (ChatQuickSendItem, error) {
		var item ChatQuickSendItem
		var proj sql.NullString
		if err := rows.Scan(&item.ID, &item.Label, &item.Command, &item.SortOrder, &proj); err != nil {
			return item, err
		}
		item.ProjectPath = proj.String
		item.ProjectOnly = proj.Valid && proj.String != ""
		return item, nil
	},
	addFn: func(item ChatQuickSendItem) (label string, command string, sortOrder int, extra chatQuickSendExtra) {
		return item.Label, item.Command, item.SortOrder, chatQuickSendExtra{projectPath: item.ProjectPath}
	},
}

// crudHelpers[T, E] holds the table-specific operations needed for CRUD on typed struct [T].
// E carries table-specific extra data for Insert/Update beyond (label, command, sortOrder).
type crudHelpers[T any, E any] struct {
	table     string
	scanCols  string // columns for SELECT (must match field order in scanFn)
	scanFn    func(*sql.Rows) (T, error)
	addFn     func(T) (label string, command string, sortOrder int, extra E)
	insertSQL string
	updateSQL string
}

// list returns all rows from the helper's table for the given project scope,
// ordered by sort_order. Global rows (project_id IS NULL) are always included;
// when projectPath is non-empty, that project's scoped rows are included too.
func (h crudHelpers[T, E]) list(projectPath string) ([]T, error) {
	query := "SELECT " + h.scanCols + " FROM " + h.table
	var rows *sql.Rows
	var err error
	if projectPath == "" {
		query += " WHERE project_id IS NULL ORDER BY sort_order"
		rows, err = store.ReadDB().Query(query)
	} else {
		// Resolve path -> id WITHOUT creating a row: a read must not register a
		// project as a side effect. An unknown project has no scoped rows, so it
		// sees the global ones only — the same as before the id refactor.
		projectID, _, idErr := store.ProjectIDByPath(projectPath)
		if idErr != nil {
			return nil, idErr
		}
		query += " WHERE project_id IS NULL OR project_id = ? ORDER BY sort_order"
		rows, err = store.ReadDB().Query(query, projectID)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []T
	for rows.Next() {
		item, err := h.scanFn(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// extraProjectPath returns the project path carried by a table-specific extra.
func extraProjectPath(e any) string {
	switch v := e.(type) {
	case quickCommandExtra:
		return v.projectPath
	case chatQuickSendExtra:
		return v.projectPath
	}
	return ""
}

// projectIDArg converts a project path to the value bound to project_id: nil for
// the global scope (so the column stays NULL and the partial unique index's
// COALESCE treats it as its own scope), otherwise the resolved id.
//
// This is a WRITE path, so it registers an unknown project (store.ProjectIDForPath
// upserts). That matters for the quick-command/quick-send tables: scoping a row
// to a project that has never had a session must still work, and the read side
// resolves the id back to a path through the same registry.
func projectIDArg(projectPath string) (any, error) {
	if projectPath == "" {
		return nil, nil
	}
	return store.ProjectIDForPath(projectPath)
}

// clearAutoExecuteForScope clears the auto_execute flag on other rows in the
// same project scope (global scope when projectPath is empty), enforcing the
// single-auto-execute-per-scope invariant. excludeID>0 skips that row (used by update).
func clearAutoExecuteForScope(table string, excludeID int64, projectPath string) error {
	projectID, err := projectIDArg(projectPath)
	if err != nil {
		return err
	}
	if projectID == nil {
		if excludeID > 0 {
			_, err := store.WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_id IS NULL AND id != ?", excludeID)
			return err
		}
		_, err := store.WriteExec("UPDATE " + table + " SET auto_execute = 0 WHERE auto_execute = 1 AND project_id IS NULL")
		return err
	}
	if excludeID > 0 {
		_, err := store.WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_id = ? AND id != ?", projectID, excludeID)
		return err
	}
	_, err = store.WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_id = ?", projectID)
	return err
}

// insert adds a new row. For tables with an auto_execute column (E=quickCommandExtra),
// any existing auto_execute=1 rows in the same project scope are cleared first to
// enforce the single-active-invariant.
func (h crudHelpers[T, E]) insert(item T) (int64, error) {
	// Capture addFn result so we can inspect extra (for auto_execute check)
	// without calling the closure twice.
	label, command, sortOrder, extra := h.addFn(item)
	projectPath := extraProjectPath(extra)
	if e, ok := any(extra).(quickCommandExtra); ok && e.autoExec == 1 {
		if err := clearAutoExecuteForScope(h.table, 0, projectPath); err != nil {
			return 0, err
		}
	}
	var maxOrder sql.NullInt64
	_ = store.ReadDB().QueryRow("SELECT MAX(sort_order) FROM " + h.table).Scan(&maxOrder)
	if maxOrder.Valid {
		sortOrder = int(maxOrder.Int64) + 1
	}
	projectArg, err := projectIDArg(projectPath)
	if err != nil {
		return 0, err
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, sortOrder, projectArg}
	} else {
		args = []any{label, command, sortOrder, projectArg}
	}
	result, err := store.WriteExec(h.insertSQL, args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// update modifies an existing row by id. For tables with an auto_execute column,
// clears auto_execute on other rows in the same project scope to enforce the
// single-active-invariant.
func (h crudHelpers[T, E]) update(id int64, item T) error {
	label, command, _, extra := h.addFn(item)
	projectPath := extraProjectPath(extra)
	if e, ok := any(extra).(quickCommandExtra); ok && e.autoExec == 1 {
		if err := clearAutoExecuteForScope(h.table, id, projectPath); err != nil {
			return err
		}
	}
	projectArg, err := projectIDArg(projectPath)
	if err != nil {
		return err
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, projectArg, id}
	} else {
		args = []any{label, command, projectArg, id}
	}
	_, err = store.WriteExec(h.updateSQL, args...)
	return err
}

// delete removes a row by id.
func (h crudHelpers[T, E]) delete(id int64) error {
	_, err := store.WriteExec("DELETE FROM "+h.table+" WHERE id = ?", id)
	return err
}

// reorder updates sort_order for all rows matching the given id list.
func (h crudHelpers[T, E]) reorder(ids []int64) error {
	tx, err := store.WriteBegin() //nolint:noctx // DB global, context not applicable
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE "+h.table+" SET sort_order = ? WHERE id = ?", i, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// QuickCommand represents a terminal quick command stored in the database.
type QuickCommand struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	Hidden      bool   `json:"hidden"`
	AutoExecute bool   `json:"auto_execute"`
	SortOrder   int    `json:"sort_order"`
	ProjectPath string `json:"project_path"`
	ProjectOnly bool   `json:"project_only"`
}

// GetQuickCommands returns quick commands for the given project scope,
// ordered by sort_order. projectPath=="" returns only global commands.
func GetQuickCommands(projectPath string) ([]QuickCommand, error) {
	return QuickCommandHelpers.list(projectPath)
}

// AddQuickCommand inserts a new quick command and returns its ID.
// If autoExecute is true, other commands' auto_execute flag in the same
// project scope is cleared first.
func AddQuickCommand(label, command string, hidden, autoExecute bool, projectPath string) (int64, error) {
	return QuickCommandHelpers.insert(QuickCommand{Label: label, Command: command, Hidden: hidden, AutoExecute: autoExecute, ProjectPath: projectPath})
}

// UpdateQuickCommand updates an existing quick command.
// If autoExecute is true, other commands' auto_execute flag in the same
// project scope is cleared first.
func UpdateQuickCommand(id int64, label, command string, hidden, autoExecute bool, projectPath string) error {
	return QuickCommandHelpers.update(id, QuickCommand{Label: label, Command: command, Hidden: hidden, AutoExecute: autoExecute, ProjectPath: projectPath})
}

// DeleteQuickCommand deletes a quick command by ID.
func DeleteQuickCommand(id int64) error {
	return QuickCommandHelpers.delete(id)
}

// ReorderQuickCommands updates sort_order for all commands based on the given ID order.
func ReorderQuickCommands(ids []int64) error {
	return QuickCommandHelpers.reorder(ids)
}

// ChatQuickSendItem represents a chat quick-send item stored in the database.
type ChatQuickSendItem struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	SortOrder   int    `json:"sort_order"`
	ProjectPath string `json:"project_path"`
	ProjectOnly bool   `json:"project_only"`
}

// GetChatQuickSend returns quick-send items for the given project scope,
// ordered by sort_order. projectPath=="" returns only global items.
func GetChatQuickSend(projectPath string) ([]ChatQuickSendItem, error) {
	return ChatQuickSendHelpers.list(projectPath)
}

// AddChatQuickSend inserts a new quick-send item and returns its ID.
func AddChatQuickSend(label, command, projectPath string) (int64, error) {
	return ChatQuickSendHelpers.insert(ChatQuickSendItem{Label: label, Command: command, ProjectPath: projectPath})
}

// UpdateChatQuickSend updates an existing quick-send item.
func UpdateChatQuickSend(id int64, label, command, projectPath string) error {
	return ChatQuickSendHelpers.update(id, ChatQuickSendItem{Label: label, Command: command, ProjectPath: projectPath})
}

// DeleteChatQuickSend deletes a quick-send item by ID.
func DeleteChatQuickSend(id int64) error {
	return ChatQuickSendHelpers.delete(id)
}

// ReorderChatQuickSend updates sort_order for all items based on the given ID order.
func ReorderChatQuickSend(ids []int64) error {
	return ChatQuickSendHelpers.reorder(ids)
}

// GetQuickSendCommands returns all command strings from chat_quick_send ordered by sort_order.
func GetQuickSendCommands() []string {
	rows, err := store.ReadDB().Query("SELECT command FROM chat_quick_send ORDER BY sort_order")
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var commands []string
	for rows.Next() {
		var cmd string
		if err := rows.Scan(&cmd); err != nil {
			return nil
		}
		commands = append(commands, cmd)
	}
	return commands
}

// KeyConfigItem represents a terminal key/symbol configuration entry.
type KeyConfigItem struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	KeyID     string `json:"key_id"`
	SortOrder int    `json:"sort_order"`
}

// GetKeyConfig returns all key config items of the given type, ordered by sort_order.
func GetKeyConfig(typeFilter string) ([]KeyConfigItem, error) {
	rows, err := store.ReadDB().Query("SELECT id, type, key_id, sort_order FROM terminal_key_config WHERE type = ? ORDER BY sort_order", typeFilter)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []KeyConfigItem
	for rows.Next() {
		var item KeyConfigItem
		if err := rows.Scan(&item.ID, &item.Type, &item.KeyID, &item.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// ReplaceKeyConfig replaces all items of the given type with the provided key IDs.
// The sort_order is set by the position in the slice.
func ReplaceKeyConfig(typeVal string, keyIDs []string) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	if _, err := tx.Exec("DELETE FROM terminal_key_config WHERE type = ?", typeVal); err != nil {
		_ = tx.Rollback()
		return err
	}
	for i, keyID := range keyIDs {
		if _, err := tx.Exec("INSERT INTO terminal_key_config (type, key_id, sort_order) VALUES (?, ?, ?)", typeVal, keyID, i); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
