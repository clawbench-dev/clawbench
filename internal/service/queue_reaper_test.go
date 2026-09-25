package service

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupReaperTestDB creates an in-memory DB with the two tables the reaper
// queries: chat_history (for queued rows) and chat_sessions (for the archived
// filter and GetSessionFullInfo).
func setupReaperTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1) // :memory: is per-connection — one conn or tables vanish
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_path TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		files TEXT,
		session_id TEXT,
		backend TEXT NOT NULL DEFAULT 'claude',
		streaming INTEGER NOT NULL DEFAULT 0,
		indexed INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS queued_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		project_path TEXT NOT NULL,
		backend TEXT NOT NULL DEFAULT '',
		queue_id TEXT NOT NULL,
		content TEXT NOT NULL,
		files TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS chat_sessions (
		id TEXT PRIMARY KEY,
		project_path TEXT NOT NULL DEFAULT '',
		backend TEXT NOT NULL DEFAULT 'claude',
		title TEXT NOT NULL DEFAULT '',
		title_source TEXT NOT NULL DEFAULT '',
		agent_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		transport TEXT NOT NULL DEFAULT '',
		auto_approve INTEGER NOT NULL DEFAULT 0,
		archived INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)
	return db
}

// insertReaperSession creates the chat_sessions row the reaper joins against.
func insertReaperSession(t *testing.T, db *sql.DB, sessionID string, archived int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, archived)
		VALUES (?, '/test', 'claude', 't', 'claude', ?)`, sessionID, archived)
	require.NoError(t, err)
}

// insertQueuedRow inserts a queued message with an explicit created_at so tests
// can control whether it is inside or outside the grace window. created_at is
// stored the way SQLite's CURRENT_TIMESTAMP writes it (UTC).
func insertQueuedRow(t *testing.T, db *sql.DB, sessionID, queueID string, age time.Duration) {
	t.Helper()
	createdAt := time.Now().UTC().Add(-age).Format("2006-01-02 15:04:05")
	_, err := db.Exec(`INSERT INTO queued_messages
		(project_path, session_id, backend, queue_id, content, created_at)
		VALUES ('/test', ?, 'claude', ?, 'hello', ?)`,
		sessionID, queueID, createdAt)
	require.NoError(t, err)
}

// stubConsumer replaces launchConsumerExecution with a recorder so the tests
// exercise the reaper's decision logic without starting a real AI backend.
func stubConsumer(t *testing.T) *[]LaunchConfig {
	t.Helper()
	var calls []LaunchConfig
	orig := launchConsumerExecution
	launchConsumerExecution = func(cfg LaunchConfig) { calls = append(calls, cfg) }
	t.Cleanup(func() { launchConsumerExecution = orig })
	return &calls
}

func TestQueueReaper_RecoversStrandedQueuedRow(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-stranded", 0)
	insertQueuedRow(t, db, "sess-stranded", "q-1", time.Minute) // older than grace

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()

	require.Len(t, *calls, 1, "stranded message must be handed to a consumer exactly once")
	assert.Equal(t, "sess-stranded", (*calls)[0].SessionID)
	assert.Equal(t, "hello", (*calls)[0].Message)
	// The row was claimed by the reaper (removed from queued_messages and
	// materialized into chat_history) so it cannot be delivered twice.
	assert.Equal(t, 0, queuedCountInDB(t, db, "sess-stranded"), "the recovered row must be claimed")
	var historyRows int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?", "sess-stranded").Scan(&historyRows))
	assert.Equal(t, 1, historyRows, "the recovered row must be materialized into chat_history")
	// The stub replaced LaunchSessionExecution, so the running flag stays set —
	// in production the launched execution clears it when the run finishes.
	SetSessionRunning("sess-stranded", false, true)
}

func TestQueueReaper_SkipsRowWithinGrace(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-fresh", 0)
	insertQueuedRow(t, db, "sess-fresh", "q-fresh", time.Second) // inside grace

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()

	assert.Empty(t, *calls, "a freshly queued row is still owned by its sending request")
}

func TestQueueReaper_SkipsRunningSession(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-running", 0)
	insertQueuedRow(t, db, "sess-running", "q-run", time.Minute)

	// Simulate a live consumer: the session is running, so its drain loop owns
	// the queued row and the reaper must not start a second consumer.
	SetSessionRunning("sess-running", true, true)
	defer SetSessionRunning("sess-running", false, true)

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()

	assert.Empty(t, *calls, "a running session must not get a second consumer")
}

func TestQueueReaper_SkipsArchivedSession(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-archived", 1) // archived
	insertQueuedRow(t, db, "sess-archived", "q-arch", time.Minute)

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()

	assert.Empty(t, *calls, "archived sessions are never resumed")
}

func TestQueueReaper_NoopWhenDBNotReady(t *testing.T) {
	// Deliberately no SetDBForTest: db is nil, as during teardown.
	cleanupAllSessionState()

	calls := stubConsumer(t)
	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	assert.NotPanics(t, func() { w.reap() }, "reap must be a no-op without a DB")
	assert.Empty(t, *calls)
}

// TestQueueReaper_RecoversOnlyEligibleSessions covers the mixed case in one
// pass: exactly the stranded, non-archived, non-running session is recovered.
func TestQueueReaper_RecoversOnlyEligibleSessions(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-ok", 0)
	insertQueuedRow(t, db, "sess-ok", "q-ok", time.Minute)
	insertReaperSession(t, db, "sess-arch", 1)
	insertQueuedRow(t, db, "sess-arch", "q-arch", time.Minute)
	insertReaperSession(t, db, "sess-run", 0)
	insertQueuedRow(t, db, "sess-run", "q-run", time.Minute)
	SetSessionRunning("sess-run", true, true)
	defer SetSessionRunning("sess-run", false, true)

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()

	require.Len(t, *calls, 1)
	assert.Equal(t, "sess-ok", (*calls)[0].SessionID)
}

// TestEnsureConsumer_ReleasesClaimWhenQueueEmptied verifies the defensive path:
// the row was deleted (e.g. user cancel) between the scan and the claim, so the
// reaper must release its claim instead of leaving the session stuck "running"
// with no consumer.
func TestEnsureConsumer_ReleasesClaimWhenQueueEmptied(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-empty", 0)
	// No queued row at all — the queue is already empty (e.g. cleared by a
	// cancel between the scan and the claim).

	// EnsureConsumer must claim, find nothing, and release the claim rather
	// than leaving the session stuck "running" with no consumer.
	ok := EnsureConsumer("sess-empty")

	assert.False(t, ok)
	assert.Empty(t, *calls)
	assert.False(t, IsSessionRunning("sess-empty"), "a claim with nothing to run must be released")
}

// TestEnsureConsumer_SkipsUnknownSession covers a session row that is gone
// (deleted concurrently). Nothing must be launched.
func TestEnsureConsumer_SkipsUnknownSession(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	assert.False(t, EnsureConsumer("sess-missing"))
	assert.Empty(t, *calls)
}

// TestQueueReaper_StopIsIdempotent guards the worker lifecycle wiring.
func TestQueueReaper_StopIsIdempotent(t *testing.T) {
	w := &QueueReaper{
		grace:    time.Minute,
		interval: time.Hour,
		reapFn:   func(string) bool { return false },
	}
	w.Start()
	// Stop twice: the second call must not panic on a closed channel.
	assert.NotPanics(t, func() {
		w.Stop()
		w.Stop()
	})
}

// TestQueueReaper_ConcurrentStopClosesOnce is the regression test for the
// double-close panic: Stop used to clear `running` only after waiting, so two
// concurrent callers both passed the guard and both reached close(stopCh) —
// "panic: close of closed channel". Shutdown paths can race, so the guard has
// to be part of the same critical section that decides who closes.
func TestQueueReaper_ConcurrentStopClosesOnce(t *testing.T) {
	w := &QueueReaper{
		grace:    time.Minute,
		interval: time.Hour,
		reapFn:   func(string) bool { return false },
	}
	w.Start()

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			assert.NotPanics(t, func() { w.Stop() })
		}()
	}
	wg.Wait()
}

// TestQueueReaper_RestartAfterStop is the regression test for the other
// double-close: run() closes doneCh on exit, so a Stop→Start pair reused the
// already-closed channels and panicked inside the new goroutine ("panic: close
// of closed channel" at run's deferred close). Each start must own fresh
// channels.
func TestQueueReaper_RestartAfterStop(t *testing.T) {
	w := &QueueReaper{
		grace:    time.Minute,
		interval: time.Hour,
		reapFn:   func(string) bool { return false },
	}
	w.Start()
	w.Stop()
	w.Start()

	// The restarted loop must still be responsive to Stop.
	assert.NotPanics(t, func() { w.Stop() })
}

// TestQueueReaper_ConcurrentStartStop mixes both transitions, which is where the
// two panics above interact: a Start racing a Stop must never leave the worker
// running=true with closed channels, nor close a channel twice.
func TestQueueReaper_ConcurrentStartStop(t *testing.T) {
	for range 50 {
		w := &QueueReaper{
			grace:    time.Minute,
			interval: time.Hour,
			reapFn:   func(string) bool { return false },
		}
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); assert.NotPanics(t, w.Start) }()
		go func() { defer wg.Done(); assert.NotPanics(t, w.Start) }()
		go func() { defer wg.Done(); assert.NotPanics(t, w.Stop) }()
		wg.Wait()
		assert.NotPanics(t, w.Stop)
	}
}

// TestQueueReaper_ReapFnFailureIsNotFatal ensures one failing recovery does not
// abort the pass for other sessions.
func TestQueueReaper_ReapFnFailureIsNotFatal(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	insertReaperSession(t, db, "sess-a", 0)
	insertQueuedRow(t, db, "sess-a", "q-a", time.Minute)
	insertReaperSession(t, db, "sess-b", 0)
	insertQueuedRow(t, db, "sess-b", "q-b", time.Minute)

	var seen []string
	w := &QueueReaper{
		grace:    30 * time.Second,
		interval: time.Hour,
		reapFn: func(sid string) bool {
			seen = append(seen, sid)
			return false
		},
	}
	w.reap()

	assert.ElementsMatch(t, []string{"sess-a", "sess-b"}, seen,
		"every eligible session must be attempted")
}

// TestQueueReaper_RecoveredMessageIsClaimedExactlyOnce is the regression test
// for the reported bug: a message stranded at queued=1 must end up claimed
// (queued=0) and handed to a consumer, exactly once.
func TestQueueReaper_RecoveredMessageIsClaimedExactlyOnce(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-once", 0)
	insertQueuedRow(t, db, "sess-once", "q-once", time.Minute)

	w := &QueueReaper{grace: 30 * time.Second, interval: time.Hour, reapFn: EnsureConsumer}
	w.reap()
	// A second pass must not re-deliver the same message.
	SetSessionRunning("sess-once", false, true)
	w.reap()

	require.Len(t, *calls, 1, "the message must be delivered exactly once")
	assert.Equal(t, 0, queuedCountInDB(t, db, "sess-once"), "the recovered row must no longer be queued")
}

// TestEnsureConsumer_BackendInfoIsPassedThrough verifies the launched execution
// receives the session's persisted project/backend/agent, not empty strings.
func TestEnsureConsumer_BackendInfoIsPassedThrough(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	_, err := db.Exec(`INSERT INTO chat_sessions
		(id, project_path, backend, title, agent_id, archived)
		VALUES ('sess-info', '/proj/x', 'codebuddy', 't', 'codebuddy', 0)`)
	require.NoError(t, err)
	insertQueuedRow(t, db, "sess-info", "q-info", time.Minute)

	require.True(t, EnsureConsumer("sess-info"))
	require.Len(t, *calls, 1)
	assert.Equal(t, "/proj/x", (*calls)[0].ProjectPath)
	assert.Equal(t, "codebuddy", (*calls)[0].BackendName)
	assert.Equal(t, "codebuddy", (*calls)[0].AgentID)
}

// TestEnsureConsumer_CarriesAttachments is the regression test for silently
// dropped attachments on the recovery path: executeStreamRunShared builds its
// own prompt and never goes through the handler's builder, so the recovered
// message's files have to travel on LaunchConfig. Omitting them made the AI
// answer as if the user had sent text only, while the bubble still showed the
// files — no error, just a wrong answer.
func TestEnsureConsumer_CarriesAttachments(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	_, err := db.Exec(`INSERT INTO chat_sessions
		(id, project_path, backend, title, agent_id, archived)
		VALUES ('sess-files', '/proj/x', 'claude', 't', 'claude', 0)`)
	require.NoError(t, err)

	// Stored the way AddQueuedMessage stores them: a JSON array of FileEntry.
	filesJSON := `[{"path":"/proj/x/a.png"},{"path":"/proj/x/b.go","startLine":3,"endLine":9}]`
	createdAt := time.Now().UTC().Add(-time.Minute).Format("2006-01-02 15:04:05")
	_, err = db.Exec(`INSERT INTO queued_messages
		(project_path, session_id, backend, queue_id, content, files, created_at)
		VALUES ('/proj/x', 'sess-files', 'claude', 'q-files', 'look at these', ?, ?)`,
		filesJSON, createdAt)
	require.NoError(t, err)

	require.True(t, EnsureConsumer("sess-files"))
	require.Len(t, *calls, 1)
	require.Len(t, (*calls)[0].Files, 2, "the recovered message's attachments must reach the execution")
	assert.Equal(t, "/proj/x/a.png", (*calls)[0].Files[0].Path)
	assert.Equal(t, "/proj/x/b.go", (*calls)[0].Files[1].Path)
	assert.Equal(t, 3, (*calls)[0].Files[1].StartLine)
	assert.Equal(t, 9, (*calls)[0].Files[1].EndLine)
}

// TestEnsureConsumer_NoDoubleConsumerWhenAlreadyRunning verifies the claim is
// the serialization point: a concurrent consumer wins and the reaper stands down.
func TestEnsureConsumer_NoDoubleConsumerWhenAlreadyRunning(t *testing.T) {
	db := setupReaperTestDB(t)
	cleanup := SetDBForTest(db, db)
	defer cleanup()
	cleanupAllSessionState()

	calls := stubConsumer(t)
	insertReaperSession(t, db, "sess-busy", 0)
	insertQueuedRow(t, db, "sess-busy", "q-busy", time.Minute)
	SetSessionRunning("sess-busy", true, true)
	defer SetSessionRunning("sess-busy", false, true)

	assert.False(t, EnsureConsumer("sess-busy"))
	assert.Empty(t, *calls)
}

// queuedCountInDB reports how many rows remain in a session's queue.
func queuedCountInDB(t *testing.T, db *sql.DB, sessionID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM queued_messages WHERE session_id = ?", sessionID).Scan(&n))
	return n
}

var _ = ai.StreamEvent{} // keep the ai import stable if assertions change
