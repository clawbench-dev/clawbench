package store

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// captureLogs redirects the default slog logger into a buffer and returns a
// reader for what was written. Mirrors the helper used elsewhere in the repo.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return buf.String
}

// setupInMemoryDB installs a fresh in-memory database as the global handle and
// restores the previous handles when the test finishes.
func setupInMemoryDB(t *testing.T) {
	t.Helper()
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	raw.SetMaxOpenConns(1)
	// A minimal chat_history so the write-instrumentation tests have a real
	// table to write to (the fast/slow cases issue UPDATE chat_history).
	if _, err := raw.Exec("CREATE TABLE IF NOT EXISTS chat_history (id INTEGER PRIMARY KEY, content TEXT)"); err != nil {
		t.Fatalf("create chat_history: %v", err)
	}
	restore := SetDBForTest(raw, raw)
	t.Cleanup(func() { restore(); _ = raw.Close() })
}

// writeOpLabel must reduce a statement to a short label without leaking any
// content. A SQL statement here can carry a whole assistant reply as a bound
// parameter substitute, so keeping only leading keywords is the guarantee that
// matters.
func TestWriteOpLabel(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"update", "UPDATE chat_history SET content = ? WHERE id = ?", "UPDATE chat_history"},
		{"insert or replace keeps target", "INSERT OR REPLACE INTO summaries (target_type) VALUES (?, ?)", "INSERT summaries"},
		{"insert or ignore keeps target", "INSERT OR IGNORE INTO chat_thinking (id) VALUES (?)", "INSERT chat_thinking"},
		{"delete from", "DELETE FROM chat_thinking WHERE message_id = ?", "DELETE chat_thinking"},
		{"create table if not exists", "CREATE TABLE IF NOT EXISTS chat_history (id INTEGER)", "CREATE chat_history"},
		{"pragma", "PRAGMA journal_mode=WAL", "PRAGMA journal_mode=WAL"},
		{"single word", "VACUUM", "VACUUM"},
		{"empty", "", ""},
		{"verb only", "DELETE", "DELETE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := writeOpLabel(tc.query); got != tc.want {
				t.Fatalf("writeOpLabel(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

// A statement long enough to be truncated must still be capped, so a malformed
// or generated query cannot flood the log with an unbounded prefix.
func TestWriteOpLabel_TruncatesLongLabel(t *testing.T) {
	long := "UPDATE " + strings.Repeat("x", 500)
	got := writeOpLabel(long)
	if len(got) > 48 {
		t.Fatalf("label length %d exceeds cap 48: %q", len(got), got)
	}
}

// parseSlogDuration extracts a duration attribute (e.g. "lock_wait=500ms")
// from a slog text-handler line, in milliseconds. Returns ok=false when absent.
func parseSlogDuration(t *testing.T, line, key string) (float64, bool) {
	t.Helper()
	re := regexp.MustCompile(key + `=([0-9.]+)(ns|µs|ms|s)\b`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("unparseable duration in %q: %v", line, err)
	}
	switch m[2] {
	case "ns":
		v /= 1e6
	case "µs":
		v /= 1e3
	case "s":
		v *= 1e3
	}
	return v, true
}

// blockingLogHandler signals when a log record is handled and blocks until
// released, so a test can observe whether the write lock is still held while
// the slow-write report is being emitted.
type blockingLogHandler struct {
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	messages    []string
}

func (h *blockingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *blockingLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.messages = append(h.messages, r.Message)
	h.mu.Unlock()
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}

// unblock releases the handler exactly once, safe to call from both the test
// body and a cleanup (double-close of a channel panics).
func (h *blockingLogHandler) unblock() { h.releaseOnce.Do(func() { close(h.release) }) }

func (h *blockingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockingLogHandler) WithGroup(string) slog.Handler      { return h }

// WriteBegin must not leave writeMu held when db.Begin() panics.
//
// WriteBegin is the transaction twin of timedWrite: it locks writeMu and then
// calls into database/sql. It released the lock only on the returned-error
// path, so a panic inside db.Begin() (db is nil — the same teardown condition
// that wedged timedWrite) skipped the unlock entirely. Its callers register
// `defer WriteUnlock()` only AFTER WriteBegin returns, so they cannot cover
// this window either: the global write mutex stays held forever, every later
// writer blocks in Lock with no CPU and no error, and the process wedges.
//
// This asserts the lock is reacquirable after a panic, which no return value
// exposes.
func TestWriteBegin_ReleasesLockOnPanic(t *testing.T) {
	origDB := db
	db = nil // db.Begin() on a nil *sql.DB panics
	t.Cleanup(func() { db = origDB })

	assert.Panics(t, func() {
		_, _ = WriteBegin()
	})

	if !WriteMuAcquirableForTest(2 * time.Second) {
		t.Fatal("writeMu is still held after db.Begin() panicked — every later writer would block forever")
	}
}

// WriteBegin must LEAVE writeMu held on success: the caller runs a
// multi-statement transaction under it. This is the opposite direction of
// TestWriteBegin_ReleasesLockOnPanic, and it is the reason the fix uses an
// ownership flag rather than an unconditional deferred unlock — a plain
// `defer WriteUnlock()` would release the lock before the caller's
// transaction commits, silently destroying the mutual exclusion that
// serializes every write in the process.
func TestWriteBegin_KeepsLockOnSuccess(t *testing.T) {
	setupInMemoryDB(t)

	tx, err := WriteBegin()
	if err != nil {
		t.Fatalf("WriteBegin: %v", err)
	}
	if tx == nil {
		t.Fatal("WriteBegin returned a nil tx with a nil error")
	}
	// Held if a concurrent acquisition cannot complete.
	if WriteMuAcquirableForTest(300 * time.Millisecond) {
		t.Fatal("writeMu was released on the success path — the caller's transaction is no longer protected")
	}

	_ = tx.Rollback()
	WriteUnlock()
}

// WriteBegin must still report a slow wait on the success path.
//
// The panic fix routes the success path through an ownership flag, which makes
// it easy to drop the report by accident (the first version of the fix did:
// it returned early on the success path and a BEGIN that queued behind writeMu
// for seconds became invisible). A slow BEGIN is the contention worth seeing —
// it is the moment a user-cancel's Finalize transaction is stuck — so this
// pins that the report survives.
func TestWriteBegin_ReportsSlowWaitOnSuccess(t *testing.T) {
	setupInMemoryDB(t)

	readLogs := captureLogs(t)

	// Hold the lock past the threshold so the BEGIN below records a slow wait.
	WriteLock()
	go func() {
		time.Sleep(300 * time.Millisecond)
		WriteUnlock()
	}()

	tx, err := WriteBegin()
	if err != nil {
		t.Fatalf("WriteBegin: %v", err)
	}
	logs := readLogs()
	_ = tx.Rollback()
	WriteUnlock()

	if !strings.Contains(logs, "BEGIN tx") {
		t.Fatalf("a BEGIN that waited past %v was not reported; logs:\n%s", slowWriteThreshold, logs)
	}
}

// timedWrite must release writeMu before emitting its slow-write report.
//
// Logging writes to a file. Emitting that report under the global write lock
// would add log I/O to the very critical section this instrumentation exists to
// measure — the observer would slow down the thing it observes. The test
// blocks inside the log handler and asserts the lock is free at that moment,
// which a plain timing assertion cannot establish.
func TestTimedWrite_ReleasesLockBeforeLogging(t *testing.T) {
	setupInMemoryDB(t)

	handler := &blockingLogHandler{entered: make(chan struct{}), release: make(chan struct{})}
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() {
		handler.unblock()
		slog.SetDefault(orig)
	})

	// Hold the lock long enough for the write below to record a slow wait.
	WriteLock()
	go func() {
		time.Sleep(300 * time.Millisecond)
		WriteUnlock()
	}()

	writeDone := make(chan error, 1)
	go func() {
		_, err := WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", "x", 1)
		writeDone <- err
	}()

	// Wait until the slow-write report is being handled (so the write has
	// finished and the report is in flight).
	select {
	case <-handler.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow-write report was never emitted")
	}

	// The handler is now blocked. If timedWrite still held writeMu, this
	// acquisition would block until the handler is released.
	acquired := make(chan struct{})
	go func() {
		WriteLock()
		close(acquired)
		WriteUnlock()
	}()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("writeMu is held while the slow-write report is being logged")
	}

	handler.unblock()
	if err := <-writeDone; err != nil {
		t.Fatalf("WriteExec failed: %v", err)
	}
}

// A fast write must not emit any report, so the log stays useful.
func TestTimedWrite_FastWriteIsSilent(t *testing.T) {
	setupInMemoryDB(t)

	read := captureLogs(t)
	if _, err := WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", "x", 1); err != nil {
		t.Fatalf("WriteExec failed: %v", err)
	}
	if out := read(); strings.Contains(out, "db: slow write") {
		t.Fatalf("expected no slow-write log, got: %s", out)
	}
}

// timedWrite must still surface the statement's error while reporting it.
func TestTimedWrite_ReturnsError(t *testing.T) {
	setupInMemoryDB(t)
	_, err := WriteExec("UPDATE nonexistent_table_xyz SET a = ?", 1)
	if err == nil {
		t.Fatal("expected an error from a write to a missing table")
	}
}

// trackWrite-style reporting: the two slow axes are logged separately, since
// contention and a slow statement need opposite fixes.
func TestTrackWrite_ReportsSlowLockWaitAndSlowExec(t *testing.T) {
	tests := []struct {
		name    string
		wait    time.Duration
		exec    time.Duration
		wantLog bool
	}{
		{name: "fast write is silent", wait: 10 * time.Millisecond, exec: 10 * time.Millisecond, wantLog: false},
		{name: "slow lock wait is reported", wait: 500 * time.Millisecond, exec: time.Millisecond, wantLog: true},
		{name: "slow exec is reported", wait: time.Millisecond, exec: 500 * time.Millisecond, wantLog: true},
		{name: "both slow reports both", wait: 300 * time.Millisecond, exec: 400 * time.Millisecond, wantLog: true},
	}

	// Durations are measured around a real clock, so a few hundred
	// microseconds of scheduling noise is expected; compare with tolerance.
	const toleranceMs = 20.0

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			read := captureLogs(t)
			reportSlowWrite(slowWrite{
				op:   "UPDATE chat_history",
				wait: tc.wait,
				exec: tc.exec,
			})

			out := read()
			if !tc.wantLog {
				if strings.Contains(out, "db: slow write") {
					t.Fatalf("expected no slow-write log, got: %s", out)
				}
				return
			}
			if !strings.Contains(out, "db: slow write") {
				t.Fatalf("expected a slow-write log, got: %s", out)
			}
			if !strings.Contains(out, `op="UPDATE chat_history"`) {
				t.Fatalf("expected the op label in the log, got: %s", out)
			}

			// The key behavior: each axis is reported with its own measured
			// duration, so a reader can tell contention from a slow statement.
			gotWait, okWait := parseSlogDuration(t, out, "lock_wait")
			gotExec, okExec := parseSlogDuration(t, out, "exec")
			if !okWait || !okExec {
				t.Fatalf("expected both lock_wait and exec in the log, got: %s", out)
			}
			wantWait := float64(tc.wait) / float64(time.Millisecond)
			wantExec := float64(tc.exec) / float64(time.Millisecond)
			if math.Abs(gotWait-wantWait) > toleranceMs {
				t.Fatalf("lock_wait = %.2fms, want ~%.2fms", gotWait, wantWait)
			}
			if math.Abs(gotExec-wantExec) > toleranceMs {
				t.Fatalf("exec = %.2fms, want ~%.2fms", gotExec, wantExec)
			}
		})
	}
}
