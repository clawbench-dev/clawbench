package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTimedWrite_ReleasesLockOnPanic is the regression test for a wedged
// process: timedWrite used to call writeMu.Unlock() inline after exec(), so a
// panic inside exec() (db.Exec on a nil *sql.DB, which the summary backfill
// path hits when the DB is torn down) skipped the unlock and left the global
// write mutex held forever. Every later writer then blocked in Lock with no CPU
// use and no error — the run died on the test timeout instead of reporting the
// original panic.
func TestTimedWrite_ReleasesLockOnPanic(t *testing.T) {
	assert.Panics(t, func() {
		_, _ = timedWrite("SELECT 1", func() (sql.Result, error) {
			panic("boom")
		})
	})

	// The lock must be free again: acquiring it with a deadline is the assertion.
	if !WriteMuAcquirableForTest(2 * time.Second) {
		t.Fatal("writeMu is still held after exec panicked — later writers would block forever")
	}
}

// TestOpen_AppliesBusyTimeoutToEveryConnection is the regression test for the
// `database is locked (261)` flake: Open used to set PRAGMAs with one-off Exec
// calls, which only configure the single connection that served the call. The
// read pool then ran `PRAGMA journal_mode=WAL` BEFORE `PRAGMA busy_timeout`, so
// the WAL switch had no busy wait and failed the moment another connection held
// the file. Both PRAGMAs now come from the DSN, which modernc.org/sqlite
// applies (busy_timeout first) to every connection the pool creates.
//
// The assertion opens a real file DB, drains the pool so a SECOND connection is
// created, and reads the pragmas back on that fresh connection.
func TestOpen_AppliesBusyTimeoutToEveryConnection(t *testing.T) {
	dir := t.TempDir()
	restore := SnapshotDBForTest()
	t.Cleanup(restore)

	if err := Open(filepath.Join(dir, "test.db")); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { Close() })

	// Force a fresh connection: MaxOpenConns is 2, so opening a second
	// concurrent connection (and holding it) guarantees at least one new
	// connection created AFTER Open's Exec calls would have run.
	conns := make([]*sql.Conn, 0, 2)
	for i := range 2 {
		c, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i, c := range conns {
		var busy int
		if err := c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if busy < 10000 {
			t.Errorf("conn %d busy_timeout = %d, want >= 10000 (PRAGMA must be in the DSN, not a one-off Exec)", i, busy)
		}
		var mode string
		if err := c.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("conn %d journal_mode: %v", i, err)
		}
		if !strings.EqualFold(mode, "wal") {
			t.Errorf("conn %d journal_mode = %q, want wal", i, mode)
		}
	}
}
