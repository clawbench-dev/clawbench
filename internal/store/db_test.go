package store

import (
	"database/sql"
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
