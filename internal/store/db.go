// Package store owns ClawBench's SQLite data layer: the connection handles, the
// global write mutex, the shared write/read primitives, the project registry and
// the session/cluster queries that more than one business package needs.
//
// It is deliberately a leaf package (only model + dbutil) so that both
// internal/service and internal/rag can depend on it without forming a cycle.
// The write mutex MUST be this package's single process-global: it serializes
// writes across BOTH the service write pool and the RAG store's own *sql.DB on
// the same file. Creating a second mutex anywhere would break that invariant.
//
//nolint:noctx,govet // db global singleton, context not applicable; shadowed err is standard Go pattern; legacy db.Query pattern
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"clawbench/internal/dbutil"

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

// slowWriteThreshold is how long a write may wait for writeMu, or spend
// executing, before it is reported. Every write in the process serializes on
// writeMu, so one slow statement stalls every other writer — including the
// Finalize path a user-cancel must finish before the UI can clear its
// "stopping" state. Set well above normal SQLite latency so only pathological
// writes are logged.
const slowWriteThreshold = 200 * time.Millisecond

// sqlTargetSkip holds the keywords that can sit between a statement's verb and
// its target (table/index/view) name. writeOpLabel skips them so the label
// keeps the target — without it "INSERT OR REPLACE INTO summaries" would
// reduce to "INSERT OR REPLACE", which does not say what was written and so
// cannot identify the slow statement.
var sqlTargetSkip = map[string]bool{
	"OR": true, "REPLACE": true, "IGNORE": true, "INTO": true, "FROM": true,
	"TABLE": true, "INDEX": true, "VIEW": true, "TRIGGER": true,
	"IF": true, "NOT": true, "EXISTS": true, "TEMPORARY": true, "TEMP": true,
	"UNIQUE": true,
}

// writeOpLabel reduces a SQL statement to a short, log-safe label of the form
// "<verb> <target>" ("UPDATE chat_history", "INSERT summaries",
// "PRAGMA journal_mode=WAL"). Only the leading keywords are kept, so a
// statement carrying user content can never leak it into the logs.
func writeOpLabel(query string) string {
	const maxLen = 48
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	verb := fields[0]
	for _, f := range fields[1:] {
		if sqlTargetSkip[strings.ToUpper(f)] {
			continue
		}
		label := verb + " " + f
		if len(label) > maxLen {
			label = label[:maxLen]
		}
		return label
	}
	if len(verb) > maxLen {
		verb = verb[:maxLen]
	}
	return verb
}

// slowWrite captures a write worth reporting: how long it waited for writeMu
// and how long the statement itself took.
type slowWrite struct {
	op   string
	wait time.Duration
	exec time.Duration
}

// reportSlowWrite logs a slow write, distinguishing the two axes: waiting for
// writeMu means contention (another goroutine holds the global write lock),
// while a slow exec means the statement itself (e.g. rewriting a
// multi-megabyte content column). They call for opposite fixes, so a single
// undifferentiated "slow write" line would not be actionable.
//
// Callers must have released writeMu: logging writes to a file, and doing that
// under the global write lock would add log I/O to the very critical section
// this instrumentation exists to measure.
func reportSlowWrite(s slowWrite) {
	if s.wait < slowWriteThreshold && s.exec < slowWriteThreshold {
		return
	}
	slog.Warn("db: slow write",
		slog.String("op", s.op),
		slog.Duration("lock_wait", s.wait),
		slog.Duration("exec", s.exec),
	)
}

// timedWrite runs one write statement under writeMu, reporting it when either
// the lock wait or the execution exceeds slowWriteThreshold.
//
// The unlock is deferred rather than written inline: exec() reaches into
// database/sql and can panic (a nil *sql.DB dereferences inside db.Exec, which
// is exactly what the backfill path does when the DB is torn down between
// tests). An inline Unlock after exec() would be skipped by the panic and leave
// writeMu held forever, so every later writer blocks on Lock — the whole
// process wedges with no CPU use and no error, and the run dies on the test
// timeout instead of surfacing the original panic.
func timedWrite(query string, exec func() (sql.Result, error)) (sql.Result, error) {
	// Derived before locking so the label work is not inside the critical
	// section either.
	op := writeOpLabel(query)

	waitStart := time.Now()
	writeMu.Lock()
	lockedAt := time.Now()
	defer func() {
		wait, execDur := lockedAt.Sub(waitStart), time.Since(lockedAt)
		writeMu.Unlock()
		reportSlowWrite(slowWrite{op: op, wait: wait, exec: execDur})
	}()

	return exec()
}

// WriteLock acquires the global write mutex.
// Callers MUST call WriteUnlock after the write operation completes.
// Use this for write transactions that span multiple SQL statements:
//
//	store.WriteLock()
//	tx, err := ... // e.g. a raw Begin on the write pool
//	// ... tx.Exec ...
//	tx.Commit()
//	store.WriteUnlock()
func WriteLock() { writeMu.Lock() }

// WriteUnlock releases the global write mutex.
func WriteUnlock() { writeMu.Unlock() }

// WriteExec executes a write statement on DB under the write mutex.
// Use this for all INSERT/UPDATE/DELETE/DDL operations instead of DB.Exec directly.
func WriteExec(query string, args ...any) (sql.Result, error) {
	return timedWrite(query, func() (sql.Result, error) { return db.Exec(query, args...) })
}

// WriteExecContext executes a write statement on DB under the write mutex with context support.
// Use this instead of DB.ExecContext for writes that may need request-scoped cancellation.
func WriteExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return timedWrite(query, func() (sql.Result, error) { return db.ExecContext(ctx, query, args...) })
}

// WriteBegin starts a write transaction on DB under the write mutex.
// The caller MUST call tx.Commit() or tx.Rollback() to release the mutex.
// Typical usage:
//
//	tx, err := store.WriteBegin()
//	if err != nil { return err }
//	defer store.WriteUnlock() // ensure mutex is released on any return path
//	// ... tx.Exec, tx.Query ...
//	if err := tx.Commit(); err != nil { return err }
//
// Only the lock wait is reported here: the transaction stays open for as long
// as the caller keeps the mutex, so its execution time is not measurable at
// this boundary. A long lock_wait still proves contention — some other writer
// (or a previous multi-statement transaction) held writeMu.
func WriteBegin() (*sql.Tx, error) {
	waitStart := time.Now()
	writeMu.Lock()
	lockedAt := time.Now()

	// On success the lock is deliberately LEFT HELD: the caller runs a
	// multi-statement transaction under it and registers its own
	// `defer store.WriteUnlock()`. But db.Begin() panics when db is nil (the
	// teardown condition the summary backfill path hits), and unlocking only on
	// the returned-error path would skip that panic — leaving the global write
	// mutex held forever, with every later writer blocked in Lock and no CPU use
	// or error to explain it. Same wedge timedWrite was fixed for, so the panic
	// path is covered by a guard that releases the lock unless the caller has
	// taken ownership of it.
	//
	// A slow wait is still reported on the success path (as before): a BEGIN
	// that queued behind writeMu is the contention worth seeing. It is logged
	// synchronously and thus while the lock is held — accepted here because this
	// path runs once per transaction rather than once per statement, so the
	// added log I/O is not measurable against the wait it describes.
	callerOwnsLock := false
	defer func() {
		if callerOwnsLock {
			if wait := lockedAt.Sub(waitStart); wait >= slowWriteThreshold {
				slog.Warn("db: slow write", slog.String("op", "BEGIN tx"), slog.Duration("lock_wait", wait))
			}
			return
		}
		wait, execDur := lockedAt.Sub(waitStart), time.Since(lockedAt)
		writeMu.Unlock()
		reportSlowWrite(slowWrite{op: "BEGIN tx", wait: wait, exec: execDur})
	}()

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	callerOwnsLock = true
	return tx, nil
}

// DBReady returns true if the write database handle has been initialized.
func DBReady() bool { return db != nil }

// ReadDBReady reports whether the read pool is available. Callers guard read
// queries with it so a standalone/teardown state returns empty instead of
// dereferencing a nil handle.
func ReadDBReady() bool { return dbRead != nil }

// ReadDB returns the read connection pool as a dbutil.Reader (read-only, no Exec).
func ReadDB() dbutil.Reader { return dbRead }

// WriteDB returns a dbutil.Writer that acquires writeMu on every Exec/ExecContext call.
// Query/QueryRow calls use the read pool without the mutex.
func WriteDB() dbutil.Writer { return mutexDBWriter{} }

// UnsafeDBForTest returns the raw write *sql.DB handle for test code.
// Must only be called from _test.go files.
func UnsafeDBForTest() *sql.DB { return db }

// UnsafeReadDBForTest returns the raw read *sql.DB handle for test code.
// Must only be called from _test.go files.
func UnsafeReadDBForTest() *sql.DB { return dbRead }

// WriteDBRaw returns the raw write *sql.DB handle.
//
// Prefer WriteExec/WriteBegin for ordinary writes: this bypasses writeMu and
// must not be used for concurrent runtime writes. It exists for the schema
// migration helpers that operate on the handle directly during startup
// (InitDB), where the process is single-threaded and the callers take
// *sql.DB for a DDL rebuild.
func WriteDBRaw() *sql.DB { return db }

// SnapshotDBForTest returns a function that restores the current write and read
// handles. Use it to save the handles before an operation that replaces them
// (e.g. service.InitDB), so the test can put its own fixture DB back afterward.
// Must only be called from _test.go files.
func SnapshotDBForTest() func() {
	writeDB, readDB := db, dbRead
	return func() { db, dbRead = writeDB, readDB }
}

// SetDBForTest sets the database handles for test code.
// It returns a cleanup function that restores the original values.
// Must only be called from _test.go files.
func SetDBForTest(writeDB, readDB *sql.DB) func() {
	origDB, origDBRead := db, dbRead
	db, dbRead = writeDB, readDB
	// The path -> id cache is keyed by canonical path, so ids from the previous
	// database would resolve against the new one and silently address rows that
	// do not exist. Clear it whenever the handle changes.
	ResetProjectIDCacheForTest()
	return func() {
		db, dbRead = origDB, origDBRead
		ResetProjectIDCacheForTest()
	}
}

// Open opens (or re-opens) both the write and read connection pools on dbPath
// and applies the SQLite PRAGMAs. It closes any previously opened handles
// first, so it is safe to call more than once (migration rerun tests, restart
// flows); leaking the old pool keeps the old SQLite file handle open, which
// blocks file removal on Windows and wastes descriptors.
//
// The read pool is opened UP FRONT rather than after schema creation: every
// reader in the process can then use store.ReadDB() from the very first
// statement, which removes the "dbRead not yet initialized during migration"
// hazard the old code worked around by reading through the write pool.
//
// The caller owns schema creation; Open only establishes connectivity.
//
// On any error after the write pool is opened, the write pool is closed before
// returning, so a failed Open never leaves a half-open handle behind (the
// handle is left non-nil only on full success).
func Open(dbPath string) error {
	Close()

	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	// From here on, any error must close the write pool we just opened.
	fail := func(e error) error {
		_ = db.Close()
		return e
	}

	// SQLite concurrency: WAL mode + write mutex + busy_timeout (defense-in-depth)
	// All writes go through WriteExec/WriteBegin which acquire writeMu, serializing
	// writes at the Go level and preventing SQLITE_BUSY. Reads bypass the mutex entirely
	// since WAL mode allows concurrent reads during writes.
	// busy_timeout=10s is kept as a fallback for any code path that bypasses the mutex
	// (e.g., RAG store which has its own *sql.DB on a separate database file).
	// MaxOpenConns must be > 1 to avoid deadlocks when iterating rows (which holds
	// a connection) and performing writes (which needs a separate connection) in the
	// same loop — e.g., the agent prompt migrations' SELECT + UPDATE pattern.
	db.SetMaxOpenConns(2)

	// Enable WAL mode for concurrent reads during writes
	if _, err := WriteExec("PRAGMA journal_mode=WAL"); err != nil {
		return fail(fmt.Errorf("failed to set WAL mode: %w", err))
	}
	// Enable foreign key enforcement (required for ON DELETE CASCADE)
	if _, err := WriteExec("PRAGMA foreign_keys = ON"); err != nil {
		return fail(fmt.Errorf("failed to enable foreign keys: %w", err))
	}

	// Wait up to 10 seconds when database is locked (defense-in-depth fallback)
	if _, err := WriteExec("PRAGMA busy_timeout=10000"); err != nil {
		return fail(fmt.Errorf("failed to set busy_timeout: %w", err))
	}

	// Initialize read connection pool for concurrent reads (WAL mode).
	// WAL contract: DB (MaxOpenConns=2) serializes writes + avoids deadlocks; DBRead (MaxOpenConns=2)
	// allows concurrent reads that never block writes and vice versa.
	// Both pools must use WAL mode + busy_timeout for this to work correctly.
	dbRead, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fail(fmt.Errorf("failed to open read database: %w", err))
	}
	dbRead.SetMaxOpenConns(2)
	dbRead.SetMaxIdleConns(2)                   // match MaxOpenConns to avoid churn
	dbRead.SetConnMaxLifetime(0)                // unlimited — SQLite file DB, no reconnection needed
	dbRead.SetConnMaxIdleTime(30 * time.Minute) // close idle conns after 30min
	if _, err := dbRead.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fail(fmt.Errorf("failed to set read DB WAL mode: %w", err))
	}
	if _, err := dbRead.Exec("PRAGMA busy_timeout=10000"); err != nil {
		return fail(fmt.Errorf("failed to set read DB busy_timeout: %w", err))
	}
	return nil
}

// Close closes both write and read database connections. The handles are left
// non-nil (closed, not cleared) so that a query issued afterward returns an
// error rather than panicking on a nil *sql.DB — the contract callers rely on
// to exercise their error paths.
func Close() {
	if db != nil {
		_ = db.Close()
	}
	if dbRead != nil {
		_ = dbRead.Close()
	}
}

// mutexDBWriter implements dbutil.Writer. Exec/ExecContext acquire writeMu
// and use the write pool (DB). Query/QueryRow use the read pool (dbRead)
// without the mutex — WAL mode allows concurrent reads during writes.
type mutexDBWriter struct{}

func (mutexDBWriter) Exec(query string, args ...any) (sql.Result, error) {
	return timedWrite(query, func() (sql.Result, error) { return db.Exec(query, args...) })
}

func (mutexDBWriter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return timedWrite(query, func() (sql.Result, error) { return db.ExecContext(ctx, query, args...) })
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

// WriteMuAcquirableForTest reports whether the global write lock can be taken
// within timeout. It is the assertion used by the panic-safety tests: a leaked
// lock exposes no return value, so the only way to observe the leak is to try
// to acquire it from another goroutine.
//
// The probe goroutine takes the lock and releases it via defer; that take and
// release IS the measurement, so the lock is deliberately not held across the
// rest of the test. Must only be called from _test.go files.
func WriteMuAcquirableForTest(timeout time.Duration) bool {
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

// SetReadDBForTest replaces only the read handle and returns a function that
// restores the previous one. Use it to point reads at a closed or stub handle.
// Must only be called from _test.go files.
func SetReadDBForTest(readDB *sql.DB) func() {
	orig := dbRead
	dbRead = readDB
	return func() { dbRead = orig }
}
