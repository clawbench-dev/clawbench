// Package dbutil defines database interfaces for read and write access.
// These interfaces break the import cycle between service, model, and ai packages.
// *sql.DB satisfies both Reader and Writer automatically.
package dbutil

import (
	"context"
	"database/sql"
)

// Reader provides read-only database access (Query + QueryRow, no Exec).
// Use this for functions that only need to read from the database.
type Reader interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Writer provides write + read database access.
// Use this for functions that need to both read and write.
// In production, Exec/ExecContext calls are protected by a write mutex;
// Query/QueryRow calls use the read connection pool without the mutex.
type Writer interface {
	Reader
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
