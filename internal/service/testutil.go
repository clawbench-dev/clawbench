//nolint:noctx,govet // test code: db local shadows package-level db, context not applicable
package service

import (
	"database/sql"
)

// InitInMemoryDB creates an in-memory SQLite database with the agents and
// agent_api_keys tables. Returns the db handle. The caller is responsible
// for closing it and for setting/restoring service.db and service.dbRead.
// This is exported for use by handler tests and other external test packages.
func InitInMemoryDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(AgentDDL); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
