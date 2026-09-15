//nolint:noctx,govet // test code: db local shadows package-level db, context not applicable
package service

import (
	"database/sql"
	"time"
)

// InitInMemoryDB creates an in-memory SQLite database with the agents table.
// Returns the db handle. The caller is responsible
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

// writeMuAcquirable reports whether the global write lock can be taken within
// timeout. It is the assertion used by the panic-safety tests: a leaked lock
// exposes no return value, so the only way to observe the leak is to try to
// acquire it from another goroutine.
//
// The probe goroutine takes the lock and releases it via defer; that take and
// release IS the measurement, so the lock is deliberately not held across the
// rest of the test.
func writeMuAcquirable(timeout time.Duration) bool {
	acquired := make(chan struct{})
	go func() {
		writeMu.Lock()
		defer writeMu.Unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return true
	case <-time.After(timeout):
		return false
	}
}
