package service

import (
	"context"
	"database/sql"
)

// InitInMemoryDB creates an in-memory SQLite database with the agents table.
// Returns the db handle. The caller is responsible
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

	return db, nil
}
