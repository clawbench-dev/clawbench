package service

import (
	"context"
	"database/sql"

	"clawbench/internal/store"
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

	// Model providers: agent PATCH validates the binding and the delete guard
	// counts references, so the table must exist in the shared fixture.
	if _, err := db.ExecContext(context.Background(), ProvidersDDL); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Relay configuration: the live-share handler reads it on every create, so
	// the fixture has to have it or every meeting test fails on a missing table.
	if _, err := db.ExecContext(context.Background(), LiveSFUDDL); err != nil {
		_ = db.Close()
		return nil, err
	}

	// The projects registry: agent project-scope resolution (ProjectIDForPath /
	// ProjectIDByPath) reads it, so handler tests that exercise scoping need it.
	if _, err := db.ExecContext(context.Background(), store.ProjectsDDL); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Minimal tables for the agent usage / session-listing queries. Only the
	// columns those queries read are needed; defaults mirror production so a
	// test cannot pass on a shape the real schema does not have.
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			agent_source TEXT NOT NULL DEFAULT 'default',
			agent_takeover_from TEXT NOT NULL DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			external_session_id TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0,
			context_state TEXT DEFAULT '',
			compacted INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL DEFAULT '',
			cron_expr TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
