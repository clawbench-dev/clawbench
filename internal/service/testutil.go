package service

import (
	"context"
	"database/sql"
)

// InitInMemoryDB creates an in-memory SQLite database with the agents table
// plus the minimal chat_sessions / scheduled_tasks tables the agent usage
// guard reads (deletion is refused while an agent still has sessions, tasks,
// or group memberships). Returns the db handle. The caller is responsible
// for closing it and for setting/restoring the store handles (store.SetDBForTest).
// This is exported for use by handler tests and other external test packages.
func InitInMemoryDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), AgentDDL); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Minimal tables for GetAgentUsage. Only the columns its COUNT queries
	// filter on are needed (agent_id / session_type / archived).
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL DEFAULT '',
			cron_expr TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active'
		);
	`); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
