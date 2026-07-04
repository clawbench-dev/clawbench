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
			deleted INTEGER NOT NULL DEFAULT 0,
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
		CREATE INDEX IF NOT EXISTS idx_sessions_type ON chat_sessions(session_type, project_path, deleted);

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tool_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON chat_tool_calls(message_id);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON chat_tool_calls(session_id, created_at DESC);
		-- Covering index for session list ORDER BY + cursor pagination:
		-- WHERE session_type = 'chat' AND project_path = ? AND deleted = 0 ORDER BY updated_at DESC, id DESC
		-- Without this, idx_sessions_type covers WHERE but requires a filesort for ORDER BY.
		CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_path, deleted, updated_at DESC, id DESC);

		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE UNIQUE INDEX IF NOT EXISTS idx_quick_commands_auto_execute
			ON terminal_quick_commands(auto_execute) WHERE auto_execute = 1;

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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (message_id) REFERENCES chat_history(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);

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
	`)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Create agent store tables (agents + agent_api_keys).
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

	// Migrate: add custom_system_prompt column to agents for user-editable system prompt
	var hasCustomSystemPrompt int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='custom_system_prompt'").Scan(&hasCustomSystemPrompt)
	if hasCustomSystemPrompt == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN custom_system_prompt TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add custom_system_prompt column to agents: %w", err)
		}
	}

	// Migrate: drop deleted column from chat_history.
	// Soft-delete is handled at the session level (chat_sessions.deleted),
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

	// Migrate: add ACP cached usage state column to agents table for persistent
	// storage of agent-level usage state (best-effort fallback for session switch).
	var hasCachedUsage int
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='acp_cached_usage_state'").Scan(&hasCachedUsage)
	if hasCachedUsage == 0 {
		if _, err := WriteExec("ALTER TABLE agents ADD COLUMN acp_cached_usage_state TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add acp_cached_usage_state column: %w", err)
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

	return nil
}

// MigrateMetadataFromContent scans chat_history rows with metadata embedded in
// the content JSON and inserts them into the chat_metadata table.
// Rows already present in chat_metadata are skipped.
// Runs in batches of 500 to avoid excessive memory usage on large databases.
func MigrateMetadataFromContent() {
	// Count how many rows need migration
	var needed int
	_ = dbRead.QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND h.content LIKE '%"metadata"%'
		  AND NOT EXISTS (SELECT 1 FROM chat_metadata m WHERE m.message_id = h.id)
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
				Metadata *struct {
					Mode           string  `json:"mode,omitempty"`
					ThinkingEffort string  `json:"thinkingEffort,omitempty"`
					Transport      string  `json:"transport,omitempty"`
					Model          string  `json:"model,omitempty"`
					InputTokens    int     `json:"inputTokens,omitempty"`
					OutputTokens   int     `json:"outputTokens,omitempty"`
					DurationMs     int     `json:"durationMs,omitempty"`
					WallMs         int     `json:"wallMs,omitempty"`
					CostUSD        float64 `json:"costUsd,omitempty"`
					StopReason     string  `json:"stopReason,omitempty"`
					IsError        bool    `json:"isError,omitempty"`
					ErrorMessage   string  `json:"errorMessage,omitempty"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal([]byte(r.Content), &contentMap); err != nil || contentMap.Metadata == nil {
				continue
			}
			m := contentMap.Metadata
			isError := 0
			if m.IsError {
				isError = 1
			}
			_, _ = WriteExec(
				`
				INSERT OR IGNORE INTO chat_metadata
					(message_id, mode, thinking_effort, transport, model, input_tokens, output_tokens,
					 duration_ms, wall_ms, cost_usd, stop_reason, is_error, error_message)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				r.ID, m.Mode, m.ThinkingEffort, m.Transport, m.Model,
				m.InputTokens, m.OutputTokens, m.DurationMs, m.WallMs,
				m.CostUSD, m.StopReason, isError, m.ErrorMessage,
			)
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
	offset := 0
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
			ORDER BY h.id
			LIMIT ? OFFSET ?`,
			batchSize, offset,
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
				continue
			}
			migrated++
		}

		slog.Info("tool_use migration progress",
			slog.Int("migrated", migrated),
			slog.Int("failed", failed),
			slog.Int("total", needed),
			slog.Int("remaining", max(0, needed-migrated)))

		if len(batch) < batchSize {
			break
		}
		offset += batchSize
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
			if err := UpsertToolCall(msgID, sessionID, b.ID, b.Name, inputJSON, b.Output, b.Status, b.Summary, b.Done); err != nil {
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
			_ = UpsertToolCall(msgID, sessionID, b.ID, b.Name, inputJSON, b.Output, b.Status, b.Summary, b.Done)
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
	var summary string
	err := dbRead.QueryRow(
		"SELECT summary FROM summaries WHERE target_type = ? AND target_id = ?",
		targetType, targetID,
	).Scan(&summary)
	if err != nil {
		return "", false
	}
	return summary, true
}

// SaveSummary persists a reading summary for a target (chat message or task execution).
// summary = "" means text was too short; non-empty is the actual summary.
func SaveSummary(targetType string, targetID int64, summary string) error {
	_, err := WriteExec(
		"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)",
		targetType, targetID, summary,
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
type quickCommandExtra struct{ hidden, autoExec int }

// QuickCommandHelpers exposes the shared CRUD helpers for terminal_quick_commands.
var QuickCommandHelpers = crudHelpers[QuickCommand, quickCommandExtra]{
	table:     "terminal_quick_commands",
	scanCols:  "id, label, command, hidden, auto_execute, sort_order",
	insertSQL: "INSERT INTO terminal_quick_commands (label, command, hidden, auto_execute, sort_order) VALUES (?, ?, ?, ?, ?)",
	updateSQL: "UPDATE terminal_quick_commands SET label = ?, command = ?, hidden = ?, auto_execute = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
	scanFn: func(rows *sql.Rows) (QuickCommand, error) {
		var cmd QuickCommand
		var hidden, autoExec int
		if err := rows.Scan(&cmd.ID, &cmd.Label, &cmd.Command, &hidden, &autoExec, &cmd.SortOrder); err != nil {
			return cmd, err
		}
		cmd.Hidden = hidden == 1
		cmd.AutoExecute = autoExec == 1
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
		return cmd.Label, cmd.Command, cmd.SortOrder, quickCommandExtra{hidden: hidden, autoExec: autoExec}
	},
}

// ChatQuickSendHelpers exposes the shared CRUD helpers for chat_quick_send.
var ChatQuickSendHelpers = crudHelpers[ChatQuickSendItem, struct{}]{
	table:     "chat_quick_send",
	scanCols:  "id, label, command, sort_order",
	insertSQL: "INSERT INTO chat_quick_send (label, command, sort_order) VALUES (?, ?, ?)",
	updateSQL: "UPDATE chat_quick_send SET label = ?, command = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
	scanFn: func(rows *sql.Rows) (ChatQuickSendItem, error) {
		var item ChatQuickSendItem
		return item, rows.Scan(&item.ID, &item.Label, &item.Command, &item.SortOrder)
	},
	addFn: func(item ChatQuickSendItem) (label string, command string, sortOrder int, _ struct{}) {
		return item.Label, item.Command, item.SortOrder, struct{}{}
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

// list returns all rows from the helper's table ordered by sort_order.
func (h crudHelpers[T, E]) list() ([]T, error) {
	rows, err := dbRead.Query("SELECT " + h.scanCols + " FROM " + h.table + " ORDER BY sort_order")
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

// insert adds a new row. For tables with an auto_execute column (E=quickCommandExtra),
// any existing auto_execute=1 rows are cleared first to enforce the single-active invariant.
func (h crudHelpers[T, E]) insert(item T) (int64, error) {
	// Capture addFn result so we can inspect extra (for auto_execute check)
	// without calling the closure twice.
	label, command, sortOrder, extra := h.addFn(item)
	if e, ok := any(extra).(quickCommandExtra); ok && e.autoExec == 1 {
		if _, err := WriteExec("UPDATE " + h.table + " SET auto_execute = 0 WHERE auto_execute = 1"); err != nil {
			return 0, err
		}
	}
	var maxOrder sql.NullInt64
	_ = dbRead.QueryRow("SELECT MAX(sort_order) FROM " + h.table).Scan(&maxOrder)
	if maxOrder.Valid {
		sortOrder = int(maxOrder.Int64) + 1
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, sortOrder}
	} else {
		args = []any{label, command, sortOrder}
	}
	result, err := WriteExec(h.insertSQL, args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// update modifies an existing row by id. For tables with an auto_execute column,
// clears auto_execute on other rows to enforce the single-active invariant.
func (h crudHelpers[T, E]) update(id int64, item T) error {
	label, command, _, extra := h.addFn(item)
	if e, ok := any(extra).(quickCommandExtra); ok && e.autoExec == 1 {
		if _, err := WriteExec("UPDATE "+h.table+" SET auto_execute = 0 WHERE auto_execute = 1 AND id != ?", id); err != nil {
			return err
		}
	}
	var args []any
	if e, ok := any(extra).(quickCommandExtra); ok {
		args = []any{label, command, e.hidden, e.autoExec, id}
	} else {
		args = []any{label, command, id}
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
}

// GetQuickCommands returns all quick commands ordered by sort_order.
func GetQuickCommands() ([]QuickCommand, error) {
	return QuickCommandHelpers.list()
}

// AddQuickCommand inserts a new quick command and returns its ID.
// If autoExecute is true, other commands' auto_execute flag is cleared first.
func AddQuickCommand(label, command string, hidden, autoExecute bool) (int64, error) {
	return QuickCommandHelpers.insert(QuickCommand{Label: label, Command: command, Hidden: hidden, AutoExecute: autoExecute})
}

// UpdateQuickCommand updates an existing quick command.
// If autoExecute is true, other commands' auto_execute flag is cleared first.
func UpdateQuickCommand(id int64, label, command string, hidden, autoExecute bool) error {
	return QuickCommandHelpers.update(id, QuickCommand{Label: label, Command: command, Hidden: hidden, AutoExecute: autoExecute})
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
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	Command   string `json:"command"`
	SortOrder int    `json:"sort_order"`
}

// GetChatQuickSend returns all quick-send items ordered by sort_order.
func GetChatQuickSend() ([]ChatQuickSendItem, error) {
	return ChatQuickSendHelpers.list()
}

// AddChatQuickSend inserts a new quick-send item and returns its ID.
func AddChatQuickSend(label, command string) (int64, error) {
	return ChatQuickSendHelpers.insert(ChatQuickSendItem{Label: label, Command: command})
}

// UpdateChatQuickSend updates an existing quick-send item.
func UpdateChatQuickSend(id int64, label, command string) error {
	return ChatQuickSendHelpers.update(id, ChatQuickSendItem{Label: label, Command: command})
}

// DeleteChatQuickSend deletes a quick-send item by ID.
func DeleteChatQuickSend(id int64) error {
	return ChatQuickSendHelpers.delete(id)
}

// ReorderChatQuickSend updates sort_order for all items based on the given ID order.
func ReorderChatQuickSend(ids []int64) error {
	return ChatQuickSendHelpers.reorder(ids)
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
