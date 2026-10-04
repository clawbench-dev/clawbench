package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WriteOpLabel's single-word truncation branch: a lone token longer than the
// cap must still be capped so it cannot flood the log.
func TestWriteOpLabel_SingleLongWordTruncated(t *testing.T) {
	long := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 60 chars, no space
	got := writeOpLabel(long)
	assert.LessOrEqual(t, len(got), 48)
	assert.Equal(t, long[:48], got)
}

// ---------- handle accessors ----------

func TestDBReadyAndReadDBReady(t *testing.T) {
	setupStoreTestDB(t)
	assert.True(t, DBReady())
	assert.True(t, ReadDBReady())
	assert.NotNil(t, ReadDB())
	assert.NotNil(t, WriteDB())
	assert.NotNil(t, UnsafeDBForTest())
	assert.NotNil(t, UnsafeReadDBForTest())
	assert.NotNil(t, WriteDBRaw())

	restore := SetDBForTest(nil, nil)
	defer restore()
	assert.False(t, DBReady())
	assert.False(t, ReadDBReady())
}

// SetReadDBForTest swaps only the read handle and restores it.
func TestSetReadDBForTest(t *testing.T) {
	setupStoreTestDB(t)
	origRead := UnsafeReadDBForTest()

	other, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer other.Close()

	restore := SetReadDBForTest(other)
	assert.Same(t, other, UnsafeReadDBForTest())
	restore()
	assert.Same(t, origRead, UnsafeReadDBForTest())
}

// SnapshotDBForTest captures the handles and restores them on demand.
func TestSnapshotDBForTest(t *testing.T) {
	setupStoreTestDB(t)
	origWrite := UnsafeDBForTest()
	origRead := UnsafeReadDBForTest()

	snap := SnapshotDBForTest()
	other, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer other.Close()
	SetDBForTest(other, other)

	snap()
	assert.Same(t, origWrite, UnsafeDBForTest())
	assert.Same(t, origRead, UnsafeReadDBForTest())
}

// ---------- mutexDBWriter ----------

func TestMutexDBWriter(t *testing.T) {
	setupStoreTestDB(t)

	w := WriteDB()
	// Exec/ExecContext take the write mutex and hit the write pool.
	_, err := w.Exec("INSERT INTO chat_quick_send (label, command) VALUES ('l', '/c')")
	require.NoError(t, err)
	_, err = w.ExecContext(context.Background(), "INSERT INTO chat_quick_send (label, command) VALUES ('l2', '/c2')")
	require.NoError(t, err)

	// Query/QueryRow/QueryContext use the read pool without the mutex.
	labels := func() []string {
		rows, err := w.Query("SELECT label FROM chat_quick_send ORDER BY id")
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		var out []string
		for rows.Next() {
			var l string
			require.NoError(t, rows.Scan(&l))
			out = append(out, l)
		}
		require.NoError(t, rows.Err())
		return out
	}()
	assert.Equal(t, []string{"l", "l2"}, labels)

	var count int
	require.NoError(t, w.QueryRow("SELECT COUNT(*) FROM chat_quick_send").Scan(&count))
	assert.Equal(t, 2, count)

	func() {
		rows, err := w.QueryContext(context.Background(), "SELECT COUNT(*) FROM chat_quick_send")
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()
		require.True(t, rows.Next())
		require.NoError(t, rows.Err())
	}()

	var n int
	require.NoError(t, w.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM chat_quick_send").Scan(&n))
	assert.Equal(t, 2, n)

	// A write against a missing table surfaces the driver error.
	_, err = w.Exec("INSERT INTO no_such_table (x) VALUES (1)")
	assert.Error(t, err)
}

// WriteExecContext runs a statement on the write pool with a context.
func TestWriteExecContext(t *testing.T) {
	setupStoreTestDB(t)

	_, err := WriteExecContext(context.Background(), "INSERT INTO chat_quick_send (label, command) VALUES ('l', '/c')")
	require.NoError(t, err)

	var count int
	require.NoError(t, ReadDB().QueryRow("SELECT COUNT(*) FROM chat_quick_send").Scan(&count))
	assert.Equal(t, 1, count)
}

// ---------- Open / Close ----------

// Open establishes both pools on a fresh file and applies the PRAGMAs; Close
// releases them. A reopen must succeed (the old handles are closed first).
func TestOpenAndClose(t *testing.T) {
	// Preserve and restore whatever handles the test harness installed.
	snap := SnapshotDBForTest()
	t.Cleanup(snap)

	dbPath := filepath.Join(t.TempDir(), "open-test.db")
	require.NoError(t, Open(dbPath))
	assert.True(t, DBReady())
	assert.True(t, ReadDBReady())

	// WAL mode is one of the PRAGMAs Open applies.
	var mode string
	require.NoError(t, UnsafeDBForTest().QueryRow("PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)

	// Reopening is safe and closes the previous handles first.
	require.NoError(t, Open(dbPath))
	assert.True(t, DBReady())

	Close()
	// Close leaves the handles non-nil (closed) so callers get an error, not a
	// panic, when they query afterwards.
	assert.True(t, DBReady())
	_, err := UnsafeDBForTest().Exec("SELECT 1")
	assert.Error(t, err, "a closed handle must return an error")
}

// Open on an unwritable path fails and must not leave a half-open write handle.
func TestOpen_InvalidPath(t *testing.T) {
	snap := SnapshotDBForTest()
	t.Cleanup(snap)

	err := Open(filepath.Join(t.TempDir(), "no-such-dir", "x.db"))
	require.Error(t, err)
}

// ---------- WriteBegin / WriteLock ----------

// WriteBegin leaves the lock held on success; the caller must release it. The
// lock being held is observed by a probe that cannot acquire it.
func TestWriteBegin_HoldsLockUntilUnlock(t *testing.T) {
	setupStoreTestDB(t)

	tx, err := WriteBegin()
	require.NoError(t, err)
	require.NotNil(t, tx)

	// The lock is held: a probe cannot take it.
	assert.False(t, WriteMuAcquirableForTest(50*1000*1000)) // 50ms

	require.NoError(t, tx.Rollback())
	WriteUnlock()
	assert.True(t, WriteMuAcquirableForTest(1_000_000_000))
}

// A nil write handle makes db.Begin panic; WriteBegin must release the lock on
// that path rather than wedging every later writer.
func TestWriteBegin_ReleasesLockWhenDBIsNil(t *testing.T) {
	restore := SetDBForTest(nil, nil)
	defer restore()

	assert.Panics(t, func() { _, _ = WriteBegin() })
	assert.True(t, WriteMuAcquirableForTest(1_000_000_000),
		"writeMu must be released after db.Begin panics on a nil handle")
}

// ---------- WriteLock / WriteUnlock ----------

func TestWriteLockUnlock(t *testing.T) {
	WriteLock()
	// Held — a probe cannot acquire it.
	assert.False(t, WriteMuAcquirableForTest(50*1000*1000))
	WriteUnlock()
	assert.True(t, WriteMuAcquirableForTest(1_000_000_000))
}
