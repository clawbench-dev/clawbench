//nolint:noctx,govet,goconst,rowserrcheck // db global singleton, context not applicable; shadowed err is standard Go pattern; JSON/SQL field names are domain strings; legacy db.Query pattern
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/dbutil"
	"clawbench/internal/model"

	_ "modernc.org/sqlite" // register SQLite driver
)

var db *sql.DB

// dbRead is the read-only connection pool (MaxOpenConns=2) for SELECT queries.
// In WAL mode, reads never block writes and vice versa.
// Unexported to prevent external packages from bypassing the write mutex via dbRead.Exec().
// External callers should use ReadDB() to access the read pool.
var dbRead *sql.DB

// writeMu serializes all write operations (INSERT/UPDATE/DELETE/DDL) to prevent
// SQLITE_BUSY errors under concurrent goroutines. Reads (Query/QueryRow) are NOT
// locked — WAL mode allows reads and writes to proceed concurrently.
var writeMu sync.Mutex

// WriteLock acquires the global write mutex.
// Callers MUST call WriteUnlock after the write operation completes.
// Use this for write transactions that span multiple SQL statements:
//
//	service.WriteLock()
//	tx, err := db.Begin()
//	// ... tx.Exec ...
//	tx.Commit()
//	service.WriteUnlock()
func WriteLock() { writeMu.Lock() }

// WriteUnlock releases the global write mutex.
func WriteUnlock() { writeMu.Unlock() }

// WriteExec executes a write statement on DB under the write mutex.
// Use this for all INSERT/UPDATE/DELETE/DDL operations instead of DB.Exec directly.
func WriteExec(query string, args ...any) (sql.Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return db.Exec(query, args...)
}

// WriteExecContext executes a write statement on DB under the write mutex with context support.
// Use this instead of DB.ExecContext for writes that may need request-scoped cancellation.
func WriteExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return db.ExecContext(ctx, query, args...)
}

// WriteBegin starts a write transaction on DB under the write mutex.
// The caller MUST call tx.Commit() or tx.Rollback() to release the mutex.
// Typical usage:
//
//	tx, err := WriteBegin()
//	if err != nil { return err }
//	defer writeMu.Unlock() // ensure mutex is released on any return path
//	// ... tx.Exec, tx.Query ...
//	if err := tx.Commit(); err != nil { return err }
func WriteBegin() (*sql.Tx, error) {
	writeMu.Lock()
	tx, err := db.Begin()
	if err != nil {
		writeMu.Unlock()
	}
	return tx, err
}

// DBReady returns true if the database has been initialized.
func DBReady() bool { return db != nil }

// ReadDB returns the read connection pool as a dbutil.Reader (read-only, no Exec).
func ReadDB() dbutil.Reader { return dbRead }

// WriteDB returns a dbutil.Writer that acquires writeMu on every Exec/ExecContext call.
// Query/QueryRow calls use the read pool without the mutex.
func WriteDB() dbutil.Writer { return mutexDBWriter{} }

// UnsafeDBForTest returns the raw write *sql.DB handle for test code.
// Must only be called from _test.go files.
func UnsafeDBForTest() *sql.DB { return db }

// SetDBForTest sets the database handles for test code.
// It returns a cleanup function that restores the original values.
// Must only be called from _test.go files.
func SetDBForTest(writeDB, readDB *sql.DB) func() {
	origDB, origDBRead := db, dbRead
	db, dbRead = writeDB, readDB
	return func() { db, dbRead = origDB, origDBRead }
}

// mutexDBWriter implements dbutil.Writer. Exec/ExecContext acquire writeMu
// and use the write pool (DB). Query/QueryRow use the read pool (dbRead)
// without the mutex — WAL mode allows concurrent reads during writes.
type mutexDBWriter struct{}

func (mutexDBWriter) Exec(query string, args ...any) (sql.Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return db.Exec(query, args...)
}

func (mutexDBWriter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return db.ExecContext(ctx, query, args...)
}

func (mutexDBWriter) Query(query string, args ...any) (*sql.Rows, error) {
	return dbRead.Query(query, args...)
}

func (mutexDBWriter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return dbRead.QueryContext(ctx, query, args...)
}

func (mutexDBWriter) QueryRow(query string, args ...any) *sql.Row {
	return dbRead.QueryRow(query, args...)
}

func (mutexDBWriter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return dbRead.QueryRowContext(ctx, query, args...)
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
	// Close any previously opened handles before re-opening. InitDB may be
	// called more than once (migration rerun tests, restart flows); leaking the
	// old pool keeps the old SQLite file handle open, which blocks file removal
	// on Windows and wastes descriptors.
	CloseDB()
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// SQLite concurrency: WAL mode + write mutex + busy_timeout (defense-in-depth)
	// All writes go through WriteExec/WriteBegin which acquire writeMu, serializing
	// writes at the Go level and preventing SQLITE_BUSY. Reads bypass the mutex entirely
	// since WAL mode allows concurrent reads during writes.
	// busy_timeout=10s is kept as a fallback for any code path that bypasses the mutex
	// (e.g., RAG store which has its own *sql.DB on a separate database file).
	// MaxOpenConns must be > 1 to avoid deadlocks when iterating rows (which holds
	// a connection) and performing writes (which needs a separate connection) in the
	// same loop — e.g., MigrateCustomSystemPrompt's SELECT + UPDATE pattern.
	db.SetMaxOpenConns(2)

	// Enable WAL mode for concurrent reads during writes
	if _, err := WriteExec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}
	// Enable foreign key enforcement (required for ON DELETE CASCADE)
	if _, err := WriteExec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Wait up to 10 seconds when database is locked (defense-in-depth fallback)
	if _, err := WriteExec("PRAGMA busy_timeout=10000"); err != nil {
		return fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Pre-migration: add columns that must exist before createTables runs
	// (because createTables creates indexes referencing these columns).
	// Only apply when the table already exists (upgrading from an older schema).
	// chat_history.indexed — added for RAG indexing progress tracking
	var chatHistoryExists int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_history'").Scan(&chatHistoryExists)
	if chatHistoryExists > 0 {
		var hasIndexed int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='indexed'").Scan(&hasIndexed)
		if hasIndexed == 0 {
			if _, err := WriteExec("ALTER TABLE chat_history ADD COLUMN indexed INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add indexed column: %w", err)
			}
		}

		// chat_history.external_message_id — external ACP messageId for incremental ACP sync dedup
		var hasExternalMsgID int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='external_message_id'").Scan(&hasExternalMsgID)
		if hasExternalMsgID == 0 {
			if _, err := WriteExec("ALTER TABLE chat_history ADD COLUMN external_message_id TEXT DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add external_message_id column: %w", err)
			}
		}

		// chat_history.queue_id / queued — queued-message persistence.
		// queue_id stores the frontend-generated queueId for matching queued
		// messages to optimistic pending bubbles; queued=1 marks a message that
		// is still waiting for the drain loop to consume it. The drain loop
		// flips queued=0 when it picks the message up (the row stays as a
		// normal conversation record).
		var hasQueueID int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='queue_id'").Scan(&hasQueueID)
		if hasQueueID == 0 {
			if _, err := WriteExec("ALTER TABLE chat_history ADD COLUMN queue_id TEXT DEFAULT ''"); err != nil {
				return fmt.Errorf("failed to add queue_id column: %w", err)
			}
		}
		var hasQueued int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='queued'").Scan(&hasQueued)
		if hasQueued == 0 {
			if _, err := WriteExec("ALTER TABLE chat_history ADD COLUMN queued INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("failed to add queued column: %w", err)
			}
		}
	}

	// Pre-migration: rename chat_sessions.deleted to archived.
	// Session "delete" is actually an archive; the column name
	// should reflect that. Must run before createTables because its indexes
	// reference the archived column. SQLite RENAME COLUMN also rewrites any
	// index definitions referencing the old column name.
	var hasSessionDeleted int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='deleted'").Scan(&hasSessionDeleted)
	if hasSessionDeleted > 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions RENAME COLUMN deleted TO archived"); err != nil {
			return fmt.Errorf("failed to rename chat_sessions.deleted to archived: %w", err)
		}
		slog.Info("renamed chat_sessions.deleted column to archived")
	}

	// Pre-migration: add the chat_metadata ledger attribution columns before
	// createTables runs. On an existing database the CREATE TABLE below is a
	// no-op, so the new index on (project_path, created_at) would reference a
	// column that does not exist yet and abort the whole multi-statement Exec,
	// breaking startup. Mirrors the chat_history.indexed handling above.
	var chatMetadataExists int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_metadata'").Scan(&chatMetadataExists)
	if chatMetadataExists > 0 {
		for _, col := range []struct{ name, ddl string }{
			{"project_path", "ALTER TABLE chat_metadata ADD COLUMN project_path TEXT DEFAULT ''"},
			{"backend", "ALTER TABLE chat_metadata ADD COLUMN backend TEXT DEFAULT ''"},
			{"agent_id", "ALTER TABLE chat_metadata ADD COLUMN agent_id TEXT DEFAULT ''"},
			{"clawbench_session_id", "ALTER TABLE chat_metadata ADD COLUMN clawbench_session_id TEXT DEFAULT ''"},
		} {
			var hasCol int
			_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_metadata') WHERE name=?", col.name).Scan(&hasCol)
			if hasCol == 0 {
				if _, err := WriteExec(col.ddl); err != nil {
					return fmt.Errorf("failed to add chat_metadata.%s column: %w", col.name, err)
				}
			}
		}
	}

	// Create tables with latest schema
	_, err = WriteExec(`
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			queue_id TEXT DEFAULT '',
			queued INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			model TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS recent_projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT UNIQUE NOT NULL,
			accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_default INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS project_meta (
			project_path TEXT PRIMARY KEY,
			next_session_number INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			name TEXT NOT NULL,
			cron_expr TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS ai_raw_responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			message_id INTEGER NOT NULL REFERENCES chat_history(id),
			backend TEXT NOT NULL DEFAULT '',
			raw_output TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Create indexes for efficient queries
		CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
		CREATE INDEX IF NOT EXISTS idx_raw_responses_session ON ai_raw_responses(session_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_raw_responses_message ON ai_raw_responses(message_id);
		CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_type ON chat_sessions(session_type, project_path, archived);

		-- Covering index for session-based queries (GetChatMessageCount, GetAssistantMessageCount,
		-- unread subquery, GetChatHistoryPaged) — avoids full table scan through large content rows.
		CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at);
		-- Index for task listing by project
		CREATE INDEX IF NOT EXISTS idx_tasks_project ON scheduled_tasks(project_path, created_at DESC);
		-- Covering index for unread count subquery in GetSessions/GetSessionsPaged:
		-- WHERE project_path = ? AND role = 'assistant' AND streaming = 0 AND created_at > ?
		-- Without this, the unread subquery can only use the project_path prefix of idx_history_session,
		-- requiring a full scan of all messages in the project to filter by role and streaming.
		CREATE INDEX IF NOT EXISTS idx_history_unread ON chat_history(project_path, role, streaming, created_at);
		-- Covering index for RAG indexing progress queries:
		-- TotalMessageCount (WHERE streaming = 0) and IndexedMessageCount (WHERE indexed = 1 AND streaming = 0)
		CREATE INDEX IF NOT EXISTS idx_history_indexing ON chat_history(streaming, indexed);

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
		-- WHERE session_type = 'chat' AND project_path = ? AND archived = 0 ORDER BY created_at DESC, id DESC
		-- Without this, idx_sessions_type covers WHERE but requires a filesort for ORDER BY.
		CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_path, archived, created_at DESC, id DESC);

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS terminal_quick_commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL,
			command TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			auto_execute INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			project_path TEXT DEFAULT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- One auto_execute command per project scope: COALESCE(NULL->'') lets a
		-- single global command and one per project coexist.
		CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_commands_auto_execute
			ON terminal_quick_commands(COALESCE(project_path, ''), auto_execute)
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
			project_path TEXT DEFAULT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Usage ledger. Deliberately has NO foreign key to chat_history: a row
		-- records tokens/cost that were actually consumed and must survive the
		-- session/message being deleted (hard-delete, rewind, ACP replay
		-- replace) so usage statistics never under-count. project_path/backend/
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
			project_path TEXT DEFAULT '',
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_project_created ON chat_metadata(project_path, created_at);

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
			project_path TEXT NOT NULL DEFAULT '',
			message_id INTEGER NOT NULL DEFAULT 0,
			recommendation TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_chat_rec_session ON chat_recommendations(session_id, id);

		-- Public file share links (capability tokens). A row maps an opaque,
		-- unguessable token to an absolute file path. Public endpoints accept the
		-- token WITHOUT auth; removing the row revokes the link immediately.
		CREATE TABLE IF NOT EXISTS file_shares (
			token TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_file_shares_path ON file_shares(path);
	`)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Create agent store tables.
	// Defined in agent_store.go as AgentDDL constant.
	if _, err := WriteExec(AgentDDL); err != nil {
		return fmt.Errorf("failed to create agent tables: %w", err)
	}

	// Schema migrations: add columns that may not exist in older databases.
	// NOTE: Migration reads use db (write pool) directly, NOT dbRead, because
	// dbRead is not initialized until after all migrations complete.
	var hasReadAt int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('task_executions') WHERE name='read_at'").Scan(&hasReadAt)
	if hasReadAt == 0 {
		if _, err := WriteExec("ALTER TABLE task_executions ADD COLUMN read_at DATETIME"); err != nil {
			return fmt.Errorf("failed to add read_at column: %w", err)
		}
	}

	// Migrate: add summary column for task execution summarization
	var hasSummary int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('task_executions') WHERE name='summary'").Scan(&hasSummary)
	if hasSummary == 0 {
		if _, err := WriteExec("ALTER TABLE task_executions ADD COLUMN summary TEXT"); err != nil {
			return fmt.Errorf("failed to add summary column: %w", err)
		}
	}

	// Migrate: add summary_cards column for structured summary card metadata
	var hasSummaryCards int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('summaries') WHERE name='summary_cards'").Scan(&hasSummaryCards)
	if hasSummaryCards == 0 {
		if _, err := WriteExec("ALTER TABLE summaries ADD COLUMN summary_cards TEXT NOT NULL DEFAULT ''"); err != nil {
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
		// NOTE: project_path/backend/agent_id/clawbench_session_id (the ledger
		// attribution columns) are added in the pre-migration block before
		// createTables, because createTables creates an index over them.
	}
	for _, col := range chatMetaCols {
		var hasCol int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_metadata') WHERE name=?", col.name).Scan(&hasCol)
		if hasCol == 0 {
			if _, err := WriteExec(col.ddl); err != nil {
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
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='source_session_id'").Scan(&hasSourceSessionID)
	if hasSourceSessionID == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN source_session_id TEXT DEFAULT NULL"); err != nil {
			return fmt.Errorf("failed to add source_session_id column: %w", err)
		}
		if _, err := WriteExec("CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL"); err != nil {
			return fmt.Errorf("failed to create source_session_id index: %w", err)
		}
	}

	// Migrate: add source_session_id column for "continue conversation" feature
	var hasTransport int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='transport'").Scan(&hasTransport)
	if hasTransport == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN transport TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add transport column: %w", err)
		}
	}

	// Migrate: add auto_approve column for per-session auto-approve (甩手掌柜) mode
	var hasAutoApprove int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='auto_approve'").Scan(&hasAutoApprove)
	if hasAutoApprove == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add auto_approve column: %w", err)
		}
	}

	// Migrate: add context_state column for persisting session context info (mode, thinking, usage)
	var hasContextState int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='context_state'").Scan(&hasContextState)
	if hasContextState == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN context_state TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add context_state column: %w", err)
		}
	}

	// Migrate: add title_renamed column. Set to 1 when the user manually renames
	// a session, so the first-message auto-title does not overwrite their choice.
	// DEPRECATED: superseded by title_source below; retained for old readers only.
	var hasTitleRenamed int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='title_renamed'").Scan(&hasTitleRenamed)
	if hasTitleRenamed == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN title_renamed INTEGER NOT NULL DEFAULT 0"); err != nil {
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
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='title_source'").Scan(&hasTitleSource)
	if hasTitleSource == 0 {
		if _, err := WriteExec("ALTER TABLE chat_sessions ADD COLUMN title_source TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add title_source column: %w", err)
		}
		// Backfill existing rows: title_renamed=1 -> custom; otherwise a session
		// with at least one user message was auto-titled -> auto; a session with
		// no user messages still holds its creation placeholder -> placeholder.
		// (title_renamed alone cannot distinguish placeholder from auto.)
		if _, err := WriteExec(`UPDATE chat_sessions SET title_source = CASE
			WHEN title_renamed = 1 THEN 'custom'
			WHEN EXISTS (SELECT 1 FROM chat_history h WHERE h.session_id = chat_sessions.id AND h.role = 'user') THEN 'auto'
			ELSE 'placeholder' END`); err != nil {
			return fmt.Errorf("failed to backfill title_source: %w", err)
		}
	}

	// Migrate: add host column to forwarded_ports for custom target host
	var hasForwardedPortHost int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='host'").Scan(&hasForwardedPortHost)
	if hasForwardedPortHost == 0 {
		if _, err := WriteExec("ALTER TABLE forwarded_ports ADD COLUMN host TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add host column to forwarded_ports: %w", err)
		}
	}

	// Migrate: add local_port column for auto-assigned local port
	// For existing rows, local_port = port (backward compatible)
	var hasForwardedPortLocalPort int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='local_port'").Scan(&hasForwardedPortLocalPort)
	if hasForwardedPortLocalPort == 0 {
		if _, err := WriteExec("ALTER TABLE forwarded_ports ADD COLUMN local_port INTEGER"); err != nil {
			return fmt.Errorf("failed to add local_port column to forwarded_ports: %w", err)
		}
		// Backfill: local_port = port for existing rows
		if _, err := WriteExec("UPDATE forwarded_ports SET local_port = port WHERE local_port IS NULL"); err != nil {
			return fmt.Errorf("failed to backfill local_port in forwarded_ports: %w", err)
		}
	}

	// Migrate: add enabled column for user-controlled enable/disable.
	// Existing ports default to enabled=true (backward compatible).
	var hasForwardedPortEnabled int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('forwarded_ports') WHERE name='enabled'").Scan(&hasForwardedPortEnabled)
	if hasForwardedPortEnabled == 0 {
		if _, err := WriteExec("ALTER TABLE forwarded_ports ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("failed to add enabled column to forwarded_ports: %w", err)
		}
	}

	// Migrate: add custom_system_prompt column to agents for user-editable system prompt
	var hasCustomSystemPrompt int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='custom_system_prompt'").Scan(&hasCustomSystemPrompt)
	if hasCustomSystemPrompt == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN custom_system_prompt TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add custom_system_prompt column to agents: %w", err)
		}
	}

	// Migrate: drop deleted column from chat_history.
	// Archival is handled at the session level (chat_sessions.archived),
	// so chat_history.deleted is redundant. Removing it simplifies queries
	// and eliminates the need to restore messages when restoring a session.
	var hasHistoryDeleted int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='deleted'").Scan(&hasHistoryDeleted)
	if hasHistoryDeleted > 0 {
		// SQLite DROP COLUMN fails if any index references the column.
		// Drop and recreate idx_history_session_id to avoid the error.
		_, _ = WriteExec("DROP INDEX IF EXISTS idx_history_session_id")
		if _, err := WriteExec("ALTER TABLE chat_history DROP COLUMN deleted"); err != nil {
			return fmt.Errorf("failed to drop deleted column from chat_history: %w", err)
		}
		_, _ = WriteExec("CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at)")
		slog.Info("dropped redundant deleted column from chat_history")
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
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tts_summaries') WHERE name='cache_key'").Scan(&hasTTSCacheKey)
	if hasTTSCacheKey > 0 {
		// Old table exists with cache_key — drop and recreate
		if _, err := WriteExec("DROP TABLE tts_summaries"); err != nil {
			return fmt.Errorf("failed to drop old tts_summaries table: %w", err)
		}
		if _, err := WriteExec(`
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
	// Create new tts_summaries table if it doesn't exist yet (fresh install)
	var hasTTSSummaries int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tts_summaries'").Scan(&hasTTSSummaries)
	if hasTTSSummaries == 0 {
		if _, err := WriteExec(`
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

	// Initialize read connection pool for concurrent reads (WAL mode).
	// WAL contract: DB (MaxOpenConns=2) serializes writes + avoids deadlocks; DBRead (MaxOpenConns=2)
	// allows concurrent reads that never block writes and vice versa.
	// Both pools must use WAL mode + busy_timeout for this to work correctly.
	dbRead, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open read database: %w", err)
	}
	dbRead.SetMaxOpenConns(2)
	dbRead.SetMaxIdleConns(2)                   // match MaxOpenConns to avoid churn
	dbRead.SetConnMaxLifetime(0)                // unlimited — SQLite file DB, no reconnection needed
	dbRead.SetConnMaxIdleTime(30 * time.Minute) // close idle conns after 30min
	if _, err := dbRead.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to set read DB WAL mode: %w", err)
	}
	if _, err := dbRead.Exec("PRAGMA busy_timeout=10000"); err != nil {
		return fmt.Errorf("failed to set read DB busy_timeout: %w", err)
	}
	if isServerStartup {
		// Uses db (write pool) because dbRead is not yet initialized at this point in InitDB.
		rows, err := db.Query("SELECT id, content FROM chat_history WHERE streaming = 1")
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
			if _, err := WriteExec("UPDATE chat_history SET content = ?, streaming = 0 WHERE id = ?", string(updatedContent), m.id); err != nil {
				slog.Error("failed to finalize orphaned streaming message", slog.Int64("id", m.id), slog.String("err", err.Error()))
			}
		}
		if len(orphans) > 0 {
			slog.Info("cleaned up orphaned streaming messages", slog.Int("count", len(orphans)))
		}
	}

	// Prune ai_raw_responses: keep only recent 200 rows for debugging.
	if isServerStartup {
		PruneRawResponses(200)
	}

	// Migrate: add ACP transport columns to agents table.
	var hasTransportCol int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='transport'").Scan(&hasTransportCol)
	if hasTransportCol == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN transport TEXT NOT NULL DEFAULT 'cli'"); err != nil {
			return fmt.Errorf("failed to add transport column: %w", err)
		}
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_command TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add acp_command column: %w", err)
		}
	}

	// Migrate: add ACP capability columns to agents table for persistent storage
	// of agent-level mode/thinking/commands/config state.
	var hasACPMods int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_available_modes'").Scan(&hasACPMods)
	if hasACPMods == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_available_modes TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_modes column: %w", err)
		}
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_available_thinking_efforts TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_thinking_efforts column: %w", err)
		}
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_available_commands TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("failed to add acp_available_commands column: %w", err)
		}
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_config_options TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add acp_config_options column: %w", err)
		}
	}

	// Migrate: add ACP LoadSession/ListSessions capability columns to agents table.
	var hasLoadSessionCol int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_load_session'").Scan(&hasLoadSessionCol)
	if hasLoadSessionCol == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_load_session BOOLEAN NOT NULL DEFAULT false"); err != nil {
			return fmt.Errorf("failed to add acp_load_session column: %w", err)
		}
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_list_sessions BOOLEAN NOT NULL DEFAULT false"); err != nil {
			return fmt.Errorf("failed to add acp_list_sessions column: %w", err)
		}
	}

	// Migrate: add is_default column to recent_projects for server-side default project.
	var hasIsDefault int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('recent_projects') WHERE name='is_default'").Scan(&hasIsDefault)
	if hasIsDefault == 0 {
		if _, err := WriteExec("ALTER TABLE recent_projects ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add is_default column: %w", err)
		}
		// Backfill: set the most recently accessed project as default
		_, _ = WriteExec("UPDATE recent_projects SET is_default = 1 WHERE id = (SELECT id FROM recent_projects ORDER BY accessed_at DESC LIMIT 1)")
	}

	// Migrate: add preferred_mode column to agents for user's default ACP mode preference.
	var hasPreferredMode int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='preferred_mode'").Scan(&hasPreferredMode)
	if hasPreferredMode == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN preferred_mode TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add preferred_mode column: %w", err)
		}
	}

	// Migrate: add auto_approve column to agents for per-agent default of the
	// new-session auto-approve toggle (front-end default only; the session's
	// real flag lives in chat_sessions.auto_approve).
	var hasAgentAutoApprove int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='auto_approve'").Scan(&hasAgentAutoApprove)
	if hasAgentAutoApprove == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add auto_approve column: %w", err)
		}
	}

	// Migrate: add project_path to quick-send / quick-command tables for
	// project-scoped (仅本项目) items. NULL means global; a value scopes the
	// item to that project only.
	if err := migrateQuickProjectScope(); err != nil {
		return err
	}

	// Migrate: drop source column from agents — no longer used for agent origin tracking.
	var hasAgentSource int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='source'").Scan(&hasAgentSource)
	if hasAgentSource > 0 {
		_, _ = WriteExec("DROP INDEX IF EXISTS idx_agents_source")
		if _, err := WriteExec("ALTER TABLE agents DROP COLUMN source"); err != nil {
			return fmt.Errorf("failed to drop source column from agents: %w", err)
		}
		slog.Info("dropped source column from agents table")
	}

	// Migrate: add duration_ms column to chat_tool_calls for per-tool execution time.
	var hasToolCallDuration int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_tool_calls') WHERE name='duration_ms'").Scan(&hasToolCallDuration)
	if hasToolCallDuration == 0 {
		if _, err := WriteExec("ALTER TABLE chat_tool_calls ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add duration_ms column to chat_tool_calls: %w", err)
		}
	}

	// Migrate: add message_id column to chat_recommendations so a recommendation
	// can be bound to the exact assistant message it was generated for. This lets
	// the client reject stale recommendations (from an earlier reply) instead of
	// briefly showing the previous reply's recommendation.
	var hasRecMessageID int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_recommendations') WHERE name='message_id'").Scan(&hasRecMessageID)
	if hasRecMessageID == 0 {
		if _, err := WriteExec("ALTER TABLE chat_recommendations ADD COLUMN message_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed to add message_id column to chat_recommendations: %w", err)
		}
	}

	// Migrate: extract metadata from chat_history.content into chat_metadata table.
	// This is a one-time migration for existing data; new messages are saved
	// to chat_metadata automatically via SaveMetadata().
	MigrateMetadataFromContent()

	// Migrate: convert task_execution summaries to chat_message summaries.
	// Scheduled tasks now store summaries as target_type='chat_message' keyed by
	// the assistant message ID (chat_history.id), same as interactive sessions.
	// This converts any existing 'task_execution' summaries to the new format.
	MigrateTaskExecutionSummaries()

	// Migrate: extract tool_use input/output from chat_history.content into
	// chat_tool_calls table and rewrite content to slim format (no input/output).
	MigrateToolCallsFromContent()

	// Migrate: rebuild chat_thinking with a seq column for incremental streaming
	// persistence (existing single rows become seq=0). Must run BEFORE
	// MigrateThinkingFromContent, whose inserts must match the new constraint.
	if err := migrateChatThinkingSeq(); err != nil {
		return fmt.Errorf("failed to migrate chat_thinking schema: %w", err)
	}

	// Migrate: extract thinking text from chat_history.content into chat_thinking
	// and rewrite content to slim format (think_id instead of text).
	MigrateThinkingFromContent()

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
// attribution columns (project_path/backend/agent_id/clawbench_session_id) are
// then backfilled from the still-present chat_history/chat_sessions rows.
//
// Idempotent: skips when the table has no foreign key (fresh installs create it
// without one, and a rerun after a partial migration is a no-op). Runs inside a
// single write transaction — any failure rolls back to the original table.
func migrateChatMetadataLedger() error {
	var tableExists int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_metadata'").Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}

	// PRAGMA foreign_key_list returns one row per FK; empty means the ledger is
	// already standalone.
	var fkCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list('chat_metadata')").Scan(&fkCount); err != nil {
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
	// orphaned by a past cascade delete stay empty. The `h.project_path != ''`
	// term keeps this a true no-op on later startups: a row whose history
	// project_path is itself empty cannot be attributed, so it must not match
	// the predicate again (otherwise the UPDATE would re-run every boot).
	var needsBackfill int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM chat_metadata m
		WHERE m.project_path = ''
		  AND EXISTS (SELECT 1 FROM chat_history h WHERE h.id = m.message_id AND h.project_path != '')
	`).Scan(&needsBackfill); err != nil {
		return err
	}
	if needsBackfill == 0 {
		return nil
	}

	_, err := WriteExec(`
		UPDATE chat_metadata SET
			project_path = COALESCE((SELECT h.project_path FROM chat_history h WHERE h.id = chat_metadata.message_id), ''),
			backend = COALESCE((SELECT h.backend FROM chat_history h WHERE h.id = chat_metadata.message_id), ''),
			clawbench_session_id = COALESCE((SELECT h.session_id FROM chat_history h WHERE h.id = chat_metadata.message_id), ''),
			agent_id = COALESCE((SELECT s.agent_id FROM chat_history h JOIN chat_sessions s ON s.id = h.session_id WHERE h.id = chat_metadata.message_id), '')
		WHERE project_path = ''
		  AND EXISTS (SELECT 1 FROM chat_history h WHERE h.id = chat_metadata.message_id AND h.project_path != '')
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
		"outcome, agent_phase, project_path, backend, agent_id, clawbench_session_id, created_at"

	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
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
		project_path TEXT DEFAULT '',
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
		"CREATE INDEX IF NOT EXISTS idx_chat_metadata_project_created ON chat_metadata(project_path, created_at)",
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
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chat_thinking'").Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}
	var hasSeq int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_thinking') WHERE name='seq'").Scan(&hasSeq); err != nil {
		return err
	}
	if hasSeq > 0 {
		return nil
	}

	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
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

// migrateQuickProjectScope adds the project_path column to the quick-send and
// quick-command tables (for existing databases) and rebuilds the per-project
// auto_execute unique index. Idempotent: skips if the column already exists.
func migrateQuickProjectScope() error {
	for _, table := range []string{"terminal_quick_commands", "chat_quick_send"} {
		var hasCol int
		_ = dbRead.QueryRow("SELECT COUNT(*) FROM pragma_table_info('" + table + "') WHERE name='project_path'").Scan(&hasCol)
		if hasCol == 0 {
			if _, err := WriteExec("ALTER TABLE " + table + " ADD COLUMN project_path TEXT DEFAULT NULL"); err != nil {
				return fmt.Errorf("failed to add project_path column to %s: %w", table, err)
			}
		}
	}
	// Rebuild the auto_execute unique index to be per-project scope.
	_, _ = WriteExec("DROP INDEX IF EXISTS idx_quick_commands_auto_execute")
	if _, err := WriteExec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_commands_auto_execute
		ON terminal_quick_commands(COALESCE(project_path, ''), auto_execute)
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
func MigrateMetadataFromContent() {
	// Count how many rows need migration
	var needed int
	_ = dbRead.QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"metadata"%'
		  AND NOT EXISTS (SELECT 1 FROM chat_metadata m WHERE m.message_id = h.id)
		  AND NOT EXISTS (
			SELECT 1 FROM chat_sessions s
			WHERE s.id = h.session_id AND s.source_session_id IS NOT NULL
		  )
	`).Scan(&needed)
	if needed == 0 {
		return
	}
	slog.Info("migrating metadata from chat_history to chat_metadata", slog.Int("rows", needed))

	batchSize := 500
	offset := 0
	migrated := 0

	for {
		batch, err := migrateMetadataBatch(batchSize, offset)
		if err != nil {
			slog.Error("metadata migration: query failed", slog.String("err", err.Error()))
			return
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
}

// migrateMetadataBatch fetches one batch of assistant messages with metadata
// that haven't been migrated to chat_metadata yet.
func migrateMetadataBatch(batchSize, offset int) ([]struct {
	ID      int64
	Content string
}, error,
) {
	rows, err := dbRead.Query(
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
func MigrateTaskExecutionSummaries() {
	// Check if there are any task_execution summaries to migrate
	var count int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&count)
	if count == 0 {
		return
	}
	slog.Info("migrating task_execution summaries to chat_message", slog.Int("count", count))

	// For each task_execution summary, find the corresponding assistant message
	// and create a chat_message summary.
	// Collect all rows first to avoid holding the read connection while writing
	// (SQLite single-writer lock would deadlock if DBRead and DB share the same conn).
	rows, err := dbRead.Query(`
		SELECT sm.target_id, sm.summary, te.session_id
		FROM summaries sm
		JOIN task_executions te ON te.id = sm.target_id
		WHERE sm.target_type = 'task_execution'
	`)
	if err != nil {
		slog.Error("task_execution summary migration: query failed", slog.String("err", err.Error()))
		return
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
		if err := dbRead.QueryRow(
			"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
			m.SessionID,
		).Scan(&msgID); err != nil {
			// No assistant message found — delete the orphaned task_execution summary
			// to prevent it from sticking around forever (it can never be migrated).
			_, _ = WriteExec(
				"DELETE FROM summaries WHERE target_type = 'task_execution' AND target_id = ?",
				m.ExecID,
			)
			continue
		}

		// Insert as chat_message summary (if not already present)
		_, _ = WriteExec(
			"INSERT OR IGNORE INTO summaries (target_type, target_id, summary, created_at) VALUES ('chat_message', ?, ?, CURRENT_TIMESTAMP)",
			msgID, m.Summary,
		)

		// Delete the old task_execution summary
		_, _ = WriteExec(
			"DELETE FROM summaries WHERE target_type = 'task_execution' AND target_id = ?",
			m.ExecID,
		)
		migrated++
	}

	slog.Info("task_execution summary migration complete", slog.Int("migrated", migrated), slog.Int("total", count))
}

// MigrateToolCallsFromContent scans assistant messages that contain tool_use blocks
// with input/output still embedded in content JSON, extracts them into chat_tool_calls,
// and rewrites content to the slim format (no input/output).
// This is a one-time migration for data created before the tool-call-split feature.
// Runs in batches to avoid excessive memory usage on large databases.
func MigrateToolCallsFromContent() {
	// Find assistant messages that have tool_use blocks with input field in content,
	// but have no entries in chat_tool_calls yet.
	// We detect old-format data by checking for "input" key inside tool_use blocks,
	// which the slim format does not include.
	var needed int
	_ = dbRead.QueryRow(`
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
		return
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
		rows, err := dbRead.Query(
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
			return
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

	_, err = WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", string(newContent), msgID)
	return err
}

// UserMessageStat represents a distinct user message text and its occurrence count.
type UserMessageStat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// GetUserMessageStats returns distinct non-empty user messages across all sessions
// (including archived), grouped by content and ordered by recency + frequency.
// Messages are filtered to exclude: streaming messages, empty content, long content
// (>200 chars), file-attached messages, slash/@-prefixed commands, and messages
// already in quick-send (matched by label OR command).
// limit caps the number of distinct message types returned (default 500 when limit <= 0).
// Results are ordered by the latest occurrence timestamp descending (recent first),
// so clustering prioritizes recent data. The O(n²) comparison in clustering makes
// large limits impractical — 500 types ≈ 125K comparisons, completes in seconds.
func GetUserMessageStats(limit int) ([]UserMessageStat, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := dbRead.Query(`
		SELECT content, COUNT(*) AS cnt
		FROM chat_history
		WHERE role = 'user'
		  AND streaming = 0
		  AND content != ''
		  AND LENGTH(content) <= 200
		  AND (files IS NULL OR files = '')
		  AND NOT (content LIKE '/%' OR content LIKE '@%')
		  AND content NOT IN (SELECT label FROM chat_quick_send UNION SELECT command FROM chat_quick_send)
		GROUP BY content
		ORDER BY MAX(created_at) DESC, cnt DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stats []UserMessageStat
	for rows.Next() {
		var s UserMessageStat
		if err := rows.Scan(&s.Text, &s.Count); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// CloseDB closes both write and read database connections.
func CloseDB() {
	if db != nil {
		_ = db.Close()
	}
	if dbRead != nil {
		_ = dbRead.Close()
	}
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
	err := dbRead.QueryRow(
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
	_, err := WriteExec(
		"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, summary_cards, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)",
		targetType, targetID, summary, cardsJSON,
	)
	return err
}

// GetTTSSummaryByMessageID looks up a TTS summary by message ID.
// Returns (ttsSummary, found).
func GetTTSSummaryByMessageID(messageID int64) (string, bool) {
	var ttsSummary string
	err := dbRead.QueryRow(
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
	_, err := WriteExec(
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
var QuickCommandHelpers = crudHelpers[QuickCommand, quickCommandExtra]{
	table:     "terminal_quick_commands",
	scanCols:  "id, label, command, hidden, auto_execute, sort_order, project_path",
	insertSQL: "INSERT INTO terminal_quick_commands (label, command, hidden, auto_execute, sort_order, project_path) VALUES (?, ?, ?, ?, ?, ?)",
	updateSQL: "UPDATE terminal_quick_commands SET label = ?, command = ?, hidden = ?, auto_execute = ?, project_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
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
var ChatQuickSendHelpers = crudHelpers[ChatQuickSendItem, chatQuickSendExtra]{
	table:     "chat_quick_send",
	scanCols:  "id, label, command, sort_order, project_path",
	insertSQL: "INSERT INTO chat_quick_send (label, command, sort_order, project_path) VALUES (?, ?, ?, ?)",
	updateSQL: "UPDATE chat_quick_send SET label = ?, command = ?, project_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
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
// ordered by sort_order. Global rows (project_path IS NULL) are always included;
// when projectPath is non-empty, that project's scoped rows are included too.
func (h crudHelpers[T, E]) list(projectPath string) ([]T, error) {
	query := "SELECT " + h.scanCols + " FROM " + h.table
	var rows *sql.Rows
	var err error
	if projectPath == "" {
		query += " WHERE project_path IS NULL ORDER BY sort_order"
		rows, err = dbRead.Query(query)
	} else {
		query += " WHERE project_path IS NULL OR project_path = ? ORDER BY sort_order"
		rows, err = dbRead.Query(query, projectPath)
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

// extraProjectPath returns the project_path carried by a table-specific extra.
func extraProjectPath(e any) string {
	switch v := e.(type) {
	case quickCommandExtra:
		return v.projectPath
	case chatQuickSendExtra:
		return v.projectPath
	}
	return ""
}

// clearAutoExecuteForScope clears the auto_execute flag on other rows in the
// same project scope (global scope when projectPath is empty), enforcing the
// single-auto-execute-per-scope invariant. excludeID>0 skips that row (used by update).
func clearAutoExecuteForScope(table string, excludeID int64, projectPath string) error {
	if projectPath == "" {
		if excludeID > 0 {
			_, err := WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_path IS NULL AND id != ?", excludeID)
			return err
		}
		_, err := WriteExec("UPDATE " + table + " SET auto_execute = 0 WHERE auto_execute = 1 AND project_path IS NULL")
		return err
	}
	if excludeID > 0 {
		_, err := WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_path = ? AND id != ?", projectPath, excludeID)
		return err
	}
	_, err := WriteExec("UPDATE "+table+" SET auto_execute = 0 WHERE auto_execute = 1 AND project_path = ?", projectPath)
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
	_ = dbRead.QueryRow("SELECT MAX(sort_order) FROM " + h.table).Scan(&maxOrder)
	if maxOrder.Valid {
		sortOrder = int(maxOrder.Int64) + 1
	}
	var projectArg any
	if projectPath != "" {
		projectArg = projectPath
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, sortOrder, projectArg}
	} else {
		args = []any{label, command, sortOrder, projectArg}
	}
	result, err := WriteExec(h.insertSQL, args...)
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
	var projectArg any
	if projectPath != "" {
		projectArg = projectPath
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, projectArg, id}
	} else {
		args = []any{label, command, projectArg, id}
	}
	_, err := WriteExec(h.updateSQL, args...)
	return err
}

// delete removes a row by id.
func (h crudHelpers[T, E]) delete(id int64) error {
	_, err := WriteExec("DELETE FROM "+h.table+" WHERE id = ?", id)
	return err
}

// reorder updates sort_order for all rows matching the given id list.
func (h crudHelpers[T, E]) reorder(ids []int64) error {
	tx, err := WriteBegin() //nolint:noctx // DB global, context not applicable
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
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

// ClusterCacheEntry represents a row in message_clusters_cache.
type ClusterCacheEntry struct {
	ID                  int64  `json:"id"`
	Representative      string `json:"representative"`
	Variants            string `json:"variants"` // JSON array stored as string
	TotalCount          int    `json:"total_count"`
	RepresentativeCount int    `json:"representative_count"`
	SortOrder           int    `json:"sort_order"`
}

// SaveClusterCache deletes old cache and meta rows, inserts new entries,
// and writes a meta row with progress="done". Uses WriteLock + transaction.
func SaveClusterCache(entries []ClusterCacheEntry, mode string) error {
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()

	if _, err := tx.Exec("DELETE FROM message_clusters_cache"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec("DELETE FROM message_clusters_meta"); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(
			"INSERT INTO message_clusters_cache (representative, variants, total_count, representative_count, sort_order) VALUES (?, ?, ?, ?, ?)",
			e.Representative, e.Variants, e.TotalCount, e.RepresentativeCount, e.SortOrder,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(
		"INSERT INTO message_clusters_meta (id, mode, progress, msg_count, cluster_count, elapsed_ms) VALUES (1, ?, 'done', ?, ?, 0)",
		mode, 0, len(entries),
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// GetClusterCache returns all cache entries ordered by sort_order,
// along with the mode and updated_at from the meta row.
func GetClusterCache() ([]ClusterCacheEntry, string, time.Time, error) {
	rows, err := dbRead.Query("SELECT id, representative, variants, total_count, representative_count, sort_order FROM message_clusters_cache ORDER BY sort_order")
	if err != nil {
		return nil, "", time.Time{}, err
	}
	defer func() { _ = rows.Close() }()

	var entries []ClusterCacheEntry
	for rows.Next() {
		var e ClusterCacheEntry
		if err := rows.Scan(&e.ID, &e.Representative, &e.Variants, &e.TotalCount, &e.RepresentativeCount, &e.SortOrder); err != nil {
			return nil, "", time.Time{}, err
		}
		entries = append(entries, e)
	}

	var mode string
	var updatedAt time.Time
	err = dbRead.QueryRow("SELECT mode, updated_at FROM message_clusters_meta WHERE id = 1").Scan(&mode, &updatedAt)
	if err != nil {
		// No meta row → return empty mode and zero time
		return entries, "", time.Time{}, nil
	}
	return entries, mode, updatedAt, nil
}

// SaveClusterMeta inserts or replaces the meta row with computation progress info.
// If mode is empty string, the previous mode value is preserved (useful during
// computing phases when the final mode is not yet known).
// If phase is empty string, the previous phase value is preserved.
func SaveClusterMeta(progress, mode string, msgCount, clusterCount, elapsedMs int, phase ...string) error {
	p := ""
	if len(phase) > 0 {
		p = phase[0]
	}
	_, err := WriteExec(
		"INSERT OR REPLACE INTO message_clusters_meta (id, mode, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg, updated_at) "+
			"VALUES (1, COALESCE(NULLIF(?, ''), (SELECT mode FROM message_clusters_meta WHERE id = 1), ''), ?, COALESCE(NULLIF(?, ''), (SELECT phase FROM message_clusters_meta WHERE id = 1), ''), ?, ?, ?, '', CURRENT_TIMESTAMP)",
		mode, progress, p, msgCount, clusterCount, elapsedMs,
	)
	return err
}

// SaveClusterMetaError inserts or replaces the meta row with error info.
func SaveClusterMetaError(progress, phase, errMsg string) error {
	// Preserve existing mode from the meta row
	var mode string
	_ = dbRead.QueryRow("SELECT mode FROM message_clusters_meta WHERE id = 1").Scan(&mode)

	_, err := WriteExec(
		"INSERT OR REPLACE INTO message_clusters_meta (id, mode, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg, updated_at) VALUES (1, ?, ?, ?, 0, 0, 0, ?, CURRENT_TIMESTAMP)",
		mode, progress, phase, errMsg,
	)
	return err
}

// ClusterMeta is the persisted message-clusters computation metadata row.
type ClusterMeta struct {
	Mode         string
	UpdatedAt    time.Time
	Progress     string
	Phase        string
	MsgCount     int
	ClusterCount int
	ElapsedMs    int
	ErrorMsg     string
}

// GetClusterMeta returns the meta row values. If no row exists, returns defaults.
func GetClusterMeta() ClusterMeta {
	var m ClusterMeta
	err := dbRead.QueryRow(
		"SELECT mode, updated_at, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg FROM message_clusters_meta WHERE id = 1",
	).Scan(&m.Mode, &m.UpdatedAt, &m.Progress, &m.Phase, &m.MsgCount, &m.ClusterCount, &m.ElapsedMs, &m.ErrorMsg)
	if err != nil {
		return ClusterMeta{Progress: "idle"}
	}
	return m
}

// GetQuickSendCommands returns all command strings from chat_quick_send ordered by sort_order.
func GetQuickSendCommands() []string {
	rows, err := dbRead.Query("SELECT command FROM chat_quick_send ORDER BY sort_order")
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
	rows, err := dbRead.Query("SELECT id, type, key_id, sort_order FROM terminal_key_config WHERE type = ? ORDER BY sort_order", typeFilter)
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
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
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
