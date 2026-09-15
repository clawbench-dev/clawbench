package service

import (
	"context"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

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

// timedWrite must release writeMu before emitting its slow-write report.
//
// Logging writes to a file. Emitting that report under the global write lock
// would add log I/O to the very critical section this instrumentation exists to
// measure — the observer would slow down the thing it observes. The test
// blocks inside the log handler and asserts the lock is free at that moment,
// which a plain timing assertion cannot establish.
func TestTimedWrite_ReleasesLockBeforeLogging(t *testing.T) {
	setupExecutorDB(t)

	handler := &blockingLogHandler{entered: make(chan struct{}), release: make(chan struct{})}
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() {
		handler.unblock()
		slog.SetDefault(orig)
	})

	// Hold the lock long enough for the write below to record a slow wait.
	writeMu.Lock()
	go func() {
		time.Sleep(300 * time.Millisecond)
		writeMu.Unlock()
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
		writeMu.Lock()
		close(acquired)
		writeMu.Unlock()
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
	setupExecutorDB(t)

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
	setupExecutorDB(t)
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

// --- finalizeTimer ---

// A fast Finalize must stay silent: it runs on every turn's completion path,
// so logging it unconditionally would drown the log in normal traffic.
func TestFinalizeTimer_FastFinalizeIsSilent(t *testing.T) {
	read := captureLogs(t)
	ft := newFinalizeTimer("sess-fast")
	done := ft.phase("drain_events")
	done()
	ft.done("user", 10)

	if out := read(); strings.Contains(out, "finalize: slow") {
		t.Fatalf("expected no log for a fast finalize, got: %s", out)
	}
}

// A slow Finalize must log its phase breakdown, so the slow step is
// identifiable from the log alone.
func TestFinalizeTimer_SlowFinalizeLogsPhases(t *testing.T) {
	read := captureLogs(t)
	ft := newFinalizeTimer("sess-slow")
	// Backdate the start so the total exceeds the threshold without sleeping.
	ft.start = time.Now().Add(-2 * time.Second)

	doneDrain := ft.phase("drain_events")
	doneDrain()
	doneFinalize := ft.phase("finalize_row")
	doneFinalize()

	ft.done("user", 42)

	out := read()
	if !strings.Contains(out, "finalize: slow") {
		t.Fatalf("expected a slow-finalize log, got: %s", out)
	}
	for _, sub := range []string{"session=sess-slow", "cancel_reason=user", "blocks=42", "drain_events=", "finalize_row=", "total="} {
		if !strings.Contains(out, sub) {
			t.Fatalf("expected log to contain %q, got: %s", sub, out)
		}
	}
}

// Phases must be reported in execution order: the point of the breakdown is to
// see where the time went, which requires a stable ordering.
func TestFinalizeTimer_PhasesKeepExecutionOrder(t *testing.T) {
	ft := newFinalizeTimer("sess-order")
	ft.start = time.Now().Add(-2 * time.Second)
	for _, name := range []string{"drain_events", "flush_pending", "finalize_row"} {
		done := ft.phase(name)
		done()
	}

	if len(ft.phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(ft.phases))
	}
	// Each phase is stored as an slog.Attr whose Key is the phase name.
	want := []string{"drain_events", "flush_pending", "finalize_row"}
	for i, attr := range ft.phases {
		a, ok := attr.(slog.Attr)
		if !ok {
			t.Fatalf("phase %d is not an slog.Attr: %T", i, attr)
		}
		if a.Key != want[i] {
			t.Fatalf("phase %d = %q, want %q", i, a.Key, want[i])
		}
	}
}
