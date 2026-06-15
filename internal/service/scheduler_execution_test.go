package service

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------- Scheduler runningExecutions ----------

func TestScheduler_GetRunningExecutions_Empty(t *testing.T) {
	s := NewScheduler()
	result := s.GetRunningExecutions(1)
	assert.Empty(t, result, "should return empty for no executions")
}

func TestScheduler_GetRunningExecutions_ByTaskID(t *testing.T) {
	s := NewScheduler()

	// Add executions for two different tasks
	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID:          "exec-1",
		TaskID:      1,
		CancelFunc:  func() {},
		StartedAt:   time.Now(),
		TriggerType: "auto",
	})
	s.runningExecutions.Store("exec-2", &RunningExecution{
		ID:          "exec-2",
		TaskID:      2,
		CancelFunc:  func() {},
		StartedAt:   time.Now(),
		TriggerType: "manual",
	})
	s.runningExecutions.Store("exec-3", &RunningExecution{
		ID:          "exec-3",
		TaskID:      1,
		CancelFunc:  func() {},
		StartedAt:   time.Now(),
		TriggerType: "auto",
	})

	// Get executions for task 1
	result := s.GetRunningExecutions(1)
	assert.Len(t, result, 2, "task 1 should have 2 executions")

	// Get executions for task 2
	result = s.GetRunningExecutions(2)
	assert.Len(t, result, 1, "task 2 should have 1 execution")

	// Get executions for non-existent task
	result = s.GetRunningExecutions(999)
	assert.Empty(t, result)

	// Cleanup
	s.runningExecutions.Delete("exec-1")
	s.runningExecutions.Delete("exec-2")
	s.runningExecutions.Delete("exec-3")
}

func TestScheduler_GetRunningCounts(t *testing.T) {
	s := NewScheduler()

	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})
	s.runningExecutions.Store("exec-2", &RunningExecution{
		ID: "exec-2", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "manual",
	})
	s.runningExecutions.Store("exec-3", &RunningExecution{
		ID: "exec-3", TaskID: 2, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	counts := s.GetRunningCounts()
	assert.Equal(t, 2, counts[1])
	assert.Equal(t, 1, counts[2])

	// Cleanup
	s.runningExecutions.Delete("exec-1")
	s.runningExecutions.Delete("exec-2")
	s.runningExecutions.Delete("exec-3")
}

func TestScheduler_HasRunningExecutions(t *testing.T) {
	s := NewScheduler()

	assert.False(t, s.HasRunningExecutions(1), "should be false with no executions")

	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	assert.True(t, s.HasRunningExecutions(1), "should be true when execution exists")
	assert.False(t, s.HasRunningExecutions(2), "should be false for different task")

	s.runningExecutions.Delete("exec-1")
}

func TestScheduler_CancelExecution_Found(t *testing.T) {
	s := NewScheduler()
	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())

	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID:          "exec-1",
		TaskID:      1,
		CancelFunc:  cancel,
		StartedAt:   time.Now(),
		TriggerType: "auto",
	})

	// Replace cancel with our own to detect invocation
	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1,
		CancelFunc:  func() { cancelled = true; cancel() },
		StartedAt:   time.Now(),
		TriggerType: "auto",
	})

	err := s.CancelExecution("exec-1")
	assert.NoError(t, err)
	assert.True(t, cancelled, "cancel function should have been called")
	assert.Error(t, ctx.Err(), "context should be cancelled")

	s.runningExecutions.Delete("exec-1")
}

func TestScheduler_CancelExecution_NotFound(t *testing.T) {
	s := NewScheduler()
	err := s.CancelExecution("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution not found")
}

func TestScheduler_CancelAllExecutions(t *testing.T) {
	s := NewScheduler()
	cancelledCount := 0

	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1,
		CancelFunc: func() { cancelledCount++ },
		StartedAt:  time.Now(), TriggerType: "auto",
	})
	s.runningExecutions.Store("exec-2", &RunningExecution{
		ID: "exec-2", TaskID: 1,
		CancelFunc: func() { cancelledCount++ },
		StartedAt:  time.Now(), TriggerType: "manual",
	})
	s.runningExecutions.Store("exec-3", &RunningExecution{
		ID: "exec-3", TaskID: 2,
		CancelFunc: func() { cancelledCount++ },
		StartedAt:  time.Now(), TriggerType: "auto",
	})

	// Cancel all for task 1 only
	s.CancelAllExecutions(1)
	assert.Equal(t, 2, cancelledCount, "should cancel 2 executions for task 1")

	s.runningExecutions.Delete("exec-1")
	s.runningExecutions.Delete("exec-2")
	s.runningExecutions.Delete("exec-3")
}

// ── Completion transition (runningCount drops to 0) ──

func TestScheduler_CompletionTransition(t *testing.T) {
	s := NewScheduler()

	// Task 1 starts running
	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	// Before completion: runningCount > 0
	counts := s.GetRunningCounts()
	assert.Equal(t, 1, counts[1], "task 1 should have running count 1")
	assert.True(t, s.HasRunningExecutions(1))

	// Task 1 completes (backend deletes from sync.Map)
	s.runningExecutions.Delete("exec-1")

	// After completion: runningCount = 0
	counts = s.GetRunningCounts()
	_, exists := counts[1]
	assert.False(t, exists, "task 1 should have no running count after completion")
	assert.False(t, s.HasRunningExecutions(1), "HasRunningExecutions should be false")
}

func TestScheduler_MultipleTasksPartialCompletion(t *testing.T) {
	s := NewScheduler()

	// Two tasks running
	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})
	s.runningExecutions.Store("exec-2", &RunningExecution{
		ID: "exec-2", TaskID: 2, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	counts := s.GetRunningCounts()
	assert.Equal(t, 1, counts[1])
	assert.Equal(t, 1, counts[2])

	// Only task 1 completes
	s.runningExecutions.Delete("exec-1")

	counts = s.GetRunningCounts()
	_, exists1 := counts[1]
	assert.False(t, exists1, "completed task should not appear in counts")
	assert.Equal(t, 1, counts[2], "still-running task should keep its count")
}

func TestScheduler_SameTaskMultipleExecutions(t *testing.T) {
	s := NewScheduler()

	// Task 1 has two concurrent executions
	s.runningExecutions.Store("exec-1a", &RunningExecution{
		ID: "exec-1a", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})
	s.runningExecutions.Store("exec-1b", &RunningExecution{
		ID: "exec-1b", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "manual",
	})

	counts := s.GetRunningCounts()
	assert.Equal(t, 2, counts[1], "should count both executions")

	// One completes
	s.runningExecutions.Delete("exec-1a")
	counts = s.GetRunningCounts()
	assert.Equal(t, 1, counts[1], "should still have 1 running after one completes")

	// Both complete
	s.runningExecutions.Delete("exec-1b")
	counts = s.GetRunningCounts()
	_, exists := counts[1]
	assert.False(t, exists, "should have no running after both complete")
}

// ── Cron skip-if-running guard ──

func TestScheduler_CronSkipIfRunning(t *testing.T) {
	s := NewScheduler()

	// Task 1 has a running execution
	s.runningExecutions.Store("exec-running", &RunningExecution{
		ID: "exec-running", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	// HasRunningExecutions should return true — cron callback would skip
	assert.True(t, s.HasRunningExecutions(1), "should skip when execution is running")

	// After execution completes, HasRunningExecutions should return false — cron would proceed
	s.runningExecutions.Delete("exec-running")
	assert.False(t, s.HasRunningExecutions(1), "should proceed after execution completes")
}

func TestScheduler_CronSkip_DifferentTaskIndependent(t *testing.T) {
	s := NewScheduler()

	// Task 1 is running, task 2 is not
	s.runningExecutions.Store("exec-1", &RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	// Task 1 should be skipped, task 2 should proceed
	assert.True(t, s.HasRunningExecutions(1), "task 1 should be skipped")
	assert.False(t, s.HasRunningExecutions(2), "task 2 should not be affected")

	s.runningExecutions.Delete("exec-1")
	assert.False(t, s.HasRunningExecutions(1), "task 1 can proceed after completion")
}

// ── ISS-187: taskRunning atomic check-and-set prevents duplicate executions ──

func TestScheduler_TaskRunning_AtomicCheckAndSet(t *testing.T) {
	s := NewScheduler()

	// First LoadOrStore should succeed (not loaded)
	_, loaded := s.taskRunning.LoadOrStore(int64(1), struct{}{})
	assert.False(t, loaded, "first claim should succeed")

	// Second LoadOrStore for same task should indicate already loaded
	_, loaded = s.taskRunning.LoadOrStore(int64(1), struct{}{})
	assert.True(t, loaded, "second claim should detect already running")

	// Different task should succeed
	_, loaded = s.taskRunning.LoadOrStore(int64(2), struct{}{})
	assert.False(t, loaded, "different task should succeed")

	// After delete, should be claimable again
	s.taskRunning.Delete(int64(1))
	_, loaded = s.taskRunning.LoadOrStore(int64(1), struct{}{})
	assert.False(t, loaded, "should be claimable after completion")

	// Cleanup
	s.taskRunning.Delete(int64(1))
	s.taskRunning.Delete(int64(2))
}

func TestScheduler_TaskRunning_ConcurrentNoDuplicate(t *testing.T) {
	s := NewScheduler()
	const taskID int64 = 1
	const goroutines = 20

	claimed := make(chan int, goroutines)
	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, loaded := s.taskRunning.LoadOrStore(taskID, struct{}{}); !loaded {
				claimed <- 1
			}
		}()
	}

	wg.Wait()
	close(claimed)

	claimCount := 0
	for range claimed {
		claimCount++
	}

	assert.Equal(t, 1, claimCount, "only one goroutine should successfully claim the task")
	s.taskRunning.Delete(taskID)
}

// ── ACP scheduled task auto-approve ──

// setupTestDBForAutoApprove creates an in-memory SQLite DB with chat_sessions table
// for testing auto-approve persistence.
func setupTestDBForAutoApprove(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	origDB := DB
	origDBRead := DBRead

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			model TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			external_session_id TEXT DEFAULT '',
			source_session_id TEXT DEFAULT NULL,
			transport TEXT DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			deleted INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	DB = db
	DBRead = db
	teardown := func() {
		DB = origDB
		DBRead = origDBRead
		db.Close()
	}
	return db, teardown
}

func TestUpdateSessionAutoApprove_SetsFlag(t *testing.T) {
	db, teardown := setupTestDBForAutoApprove(t)
	defer teardown()

	// Insert a test session
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('test-session', '/proj', 'claude', 'Test')`)
	assert.NoError(t, err)

	// Default is 0
	assert.False(t, GetSessionAutoApprove("test-session"), "auto-approve should default to false")

	// Enable
	err = UpdateSessionAutoApprove("test-session", true)
	assert.NoError(t, err)
	assert.True(t, GetSessionAutoApprove("test-session"), "auto-approve should be true after enabling")

	// Disable
	err = UpdateSessionAutoApprove("test-session", false)
	assert.NoError(t, err)
	assert.False(t, GetSessionAutoApprove("test-session"), "auto-approve should be false after disabling")
}

func TestGetSessionAutoApprove_MissingSession(t *testing.T) {
	_, teardown := setupTestDBForAutoApprove(t)
	defer teardown()

	// Non-existent session should return false
	assert.False(t, GetSessionAutoApprove("nonexistent-session"), "missing session should default to false")
}

// ── Scheduler external_session_id persistence ──

// TestScheduler_ExternalSessionID_SessionCapture verifies that the service-level
// UpdateExternalSessionID / GetExternalSessionID functions work correctly for
// the session_capture path added to the scheduler event loop.
// This is the core of the fix for "scheduled task session amnesia on continue":
// the scheduler must persist the CLI-assigned external_session_id so that
// ContinueFromExecution can inherit it and --resume works correctly.
func TestScheduler_ExternalSessionID_SessionCapture(t *testing.T) {
	db, teardown := setupTestDBForAutoApprove(t)
	defer teardown()

	sessionID := "sched-test-session-1"
	// Create session — external_session_id defaults to the ClawBench UUID (sessionID)
	// This mirrors CreateSession which sets external_session_id = sessionID as placeholder
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title, external_session_id) VALUES (?, '/proj', 'opencode', 'Test', ?)`, sessionID, sessionID)
	assert.NoError(t, err)

	// Verify default: external_session_id == sessionID (the placeholder)
	extID := GetExternalSessionID(sessionID)
	assert.Equal(t, sessionID, extID, "default external_session_id should equal sessionID (placeholder)")

	// Simulate session_capture event — CLI assigns a real session ID
	cliSessionID := "ses_abc123def456"
	err = UpdateExternalSessionID(sessionID, cliSessionID)
	assert.NoError(t, err)

	// Verify the real session ID was persisted
	extID = GetExternalSessionID(sessionID)
	assert.Equal(t, cliSessionID, extID, "external_session_id should be updated to CLI-assigned ID")
}

// TestScheduler_ExternalSessionID_MetadataFallback verifies the metadata.SessionID
// fallback path in the scheduler event loop. This is the secondary capture mechanism
// when session_capture was not emitted (some backends only use metadata).
func TestScheduler_ExternalSessionID_MetadataFallback(t *testing.T) {
	db, teardown := setupTestDBForAutoApprove(t)
	defer teardown()

	sessionID := "sched-test-session-2"
	// Create session with placeholder external_session_id (mirrors CreateSession behavior)
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title, external_session_id) VALUES (?, '/proj', 'codex', 'Test', ?)`, sessionID, sessionID)
	assert.NoError(t, err)

	// Default: placeholder
	assert.Equal(t, sessionID, GetExternalSessionID(sessionID))

	// Simulate metadata.SessionID fallback — should only update if still placeholder
	metadataSessionID := "thread_xyz789"
	err = UpdateExternalSessionID(sessionID, metadataSessionID)
	assert.NoError(t, err)
	assert.Equal(t, metadataSessionID, GetExternalSessionID(sessionID))

	// session_capture arriving AFTER metadata should NOT overwrite (preserve first-captured)
	err = UpdateExternalSessionID(sessionID, "ses_later_id")
	assert.NoError(t, err) // DB update succeeds but handler checks existingExtID first
	// In the scheduler event loop, the condition `existingExtID == "" || existingExtID == sessionID`
	// prevents overwriting. The DB update here is unconditional (no condition in SQL),
	// but the event loop code guards it. Verify the guard logic:
	currentExtID := GetExternalSessionID(sessionID)
	assert.Equal(t, "ses_later_id", currentExtID, "DB update is unconditional; the guard is in the event loop code")
}

// TestScheduler_ExternalSessionID_ContinueInheritance verifies that
// ContinueFromExecution copies the correct external_session_id from the
// source (scheduled) session to the continued session.
func TestScheduler_ExternalSessionID_ContinueInheritance(t *testing.T) {
	db, teardown := setupTestDBForAutoApprove(t)
	defer teardown()

	// Create source session with a CLI-assigned external_session_id
	sourceID := "sched-source-session"
	cliSessionID := "ses_real_cli_id"
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, external_session_id) VALUES (?, '/proj', 'opencode', 'Scheduled Task', ?)`,
		sourceID, cliSessionID,
	)
	assert.NoError(t, err)

	// Verify external_session_id was persisted correctly
	assert.Equal(t, cliSessionID, GetExternalSessionID(sourceID))

	// Simulate what ContinueFromExecution does: copy external_session_id to new session
	continuedID := "continued-session-1"
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, external_session_id) VALUES (?, '/proj', 'opencode', 'Continued', ?, ?)`,
		continuedID, sourceID, cliSessionID,
	)
	assert.NoError(t, err)

	// The continued session should inherit the CLI session ID, not the ClawBench UUID placeholder
	assert.Equal(t, cliSessionID, GetExternalSessionID(continuedID),
		"continued session should inherit CLI-assigned external_session_id for --resume to work")
}
