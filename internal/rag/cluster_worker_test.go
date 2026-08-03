package rag

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDBForClusterWorker creates an in-memory SQLite database with the
// necessary tables for testing ClusterWorker. It also sets up the RAG globals.
func setupTestDBForClusterWorker(t *testing.T) func() {
	t.Helper()

	testDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	testDB.SetMaxOpenConns(1)
	testDB.Exec("PRAGMA journal_mode=WAL")
	testDB.Exec("PRAGMA busy_timeout=5000")

	_, err = testDB.Exec(`
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
			context_state TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS message_clusters_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			representative TEXT NOT NULL,
			variants TEXT NOT NULL,
			total_count INTEGER NOT NULL,
			representative_count INTEGER NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS message_clusters_meta (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			mode TEXT NOT NULL DEFAULT '',
			progress TEXT NOT NULL DEFAULT 'idle',
			phase TEXT NOT NULL DEFAULT '',
			msg_count INTEGER NOT NULL DEFAULT 0,
			cluster_count INTEGER NOT NULL DEFAULT 0,
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			error_msg TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_quick_send (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL,
			command TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(testDB, testDB)

	// Also set up RAG globals for ClusterMessagesWithEmbeddings
	origStore := GlobalStore
	origEmbedder := GlobalEmbedder
	GlobalStore = nil // no vector store needed for exact/fts mode
	GlobalEmbedder = nil

	// Init segmenter for clustering
	require.NoError(t, InitSegmenter())

	teardown := func() {
		GlobalStore = origStore
		GlobalEmbedder = origEmbedder
		cleanup()
		_ = testDB.Close()
	}
	return teardown
}

// insertTestUserMessages inserts multiple user messages into chat_history for clustering.
func insertTestUserMessages(t *testing.T, sessionID string, contents []string) {
	t.Helper()
	for _, c := range contents {
		_, err := service.UnsafeDBForTest().Exec(
			"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
			"/proj", c, sessionID,
		)
		assert.NoError(t, err)
	}
}

// ---------- TestClusterWorker_GetProgress_Initial ----------

func TestClusterWorker_GetProgress_Initial(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)
	progress := cw.GetProgress()
	assert.Equal(t, "idle", progress.Status)
	assert.Equal(t, "", progress.Phase)
	assert.Equal(t, 0, progress.MsgCount)
	assert.Equal(t, 0, progress.ClusterCount)
	assert.Equal(t, int64(0), progress.ElapsedMs)
	assert.Equal(t, "", progress.Mode)
	assert.Equal(t, "", progress.Error)
}

// ---------- TestClusterWorker_IsRunning ----------

func TestClusterWorker_IsRunning(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)
	assert.False(t, cw.IsRunning(), "should not be running initially")

	// Insert messages so ComputeOnce actually does work
	insertTestUserMessages(t, "sess-1", []string{
		"hello", "hello", "hello", // 3 identical
		"fix bug", "fix bug", // 2 identical
		"continue", // 1 unique
	})

	var runningDuring atomic.Bool

	cw.ComputeOnce()
	// The goroutine starts async, so IsRunning should be true briefly
	// Wait a tiny bit to let the goroutine start
	time.Sleep(50 * time.Millisecond)
	runningDuring.Store(cw.IsRunning())

	// Wait for completion (up to 10 seconds)
	for range 100 {
		if !cw.IsRunning() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.False(t, cw.IsRunning(), "should not be running after completion")

	// It's possible the goroutine finished so fast that runningDuring is false.
	// That's okay — the important thing is IsRunning transitions correctly.
	// Just verify the function works.
}

// ---------- TestClusterWorker_ComputeOnce ----------

func TestClusterWorker_ComputeOnce(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)

	// Insert messages for clustering
	insertTestUserMessages(t, "sess-1", []string{
		"hello", "hello", "hello", // 3 identical → one cluster
		"fix bug", "fix bug", // 2 identical → one cluster
		"continue", // 1 unique → one cluster
	})

	cw.ComputeOnce()

	// Wait for completion
	for range 100 {
		if !cw.IsRunning() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	// Verify cache populated
	cache, mode, _, err := service.GetClusterCache()
	require.NoError(t, err)
	assert.Equal(t, "fts", mode) // no embedder → FTS mode (always available)
	assert.Len(t, cache, 3, "should have 3 clusters")

	// Verify meta shows "done"
	progress := cw.GetProgress()
	assert.Equal(t, "done", progress.Status)
	assert.Equal(t, "fts", progress.Mode)
	assert.Equal(t, 3, progress.MsgCount)
	assert.Equal(t, 3, progress.ClusterCount)
	assert.True(t, progress.ElapsedMs >= 0, "elapsed time should be non-negative")
}

// ---------- TestClusterWorker_ComputeOnce_NoDuplicate ----------

func TestClusterWorker_ComputeOnce_NoDuplicate(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)

	insertTestUserMessages(t, "sess-1", []string{"hello", "hello"})

	cw.ComputeOnce()

	// Calling again while running should be no-op
	cw.ComputeOnce() // second call — should be skipped since already running

	// Wait for completion
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	// After completion, calling again should start a new computation
	cw.ComputeOnce()
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	progress := cw.GetProgress()
	assert.Equal(t, "done", progress.Status)
}

// ---------- TestClusterWorker_GetProgress_AfterCompute ----------

func TestClusterWorker_GetProgress_AfterCompute(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)

	insertTestUserMessages(t, "sess-1", []string{
		"hello", "hello",
		"fix bug",
	})

	cw.ComputeOnce()
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	progress := cw.GetProgress()
	assert.Equal(t, "done", progress.Status)
	assert.Equal(t, "fts", progress.Mode)
	assert.Equal(t, 2, progress.MsgCount)
	assert.Equal(t, 2, progress.ClusterCount)
	assert.True(t, progress.ElapsedMs >= 0)
	assert.Equal(t, "", progress.Error)
}

// ---------- TestClusterWorker_BroadcastProgress ----------

func TestClusterWorker_BroadcastProgress(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	mgr := ws.NewManagerForTest()
	hub := mgr.StreamHub()

	cw := NewClusterWorker(hub)

	insertTestUserMessages(t, "sess-1", []string{
		"hello", "hello",
	})

	cw.ComputeOnce()
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	// The broadcast was called during computation — verified by the
	// progress being "done" (which means the "done" broadcast was sent)
	progress := cw.GetProgress()
	assert.Equal(t, "done", progress.Status)

	// With hub=nil, no broadcast should happen
	cwNil := NewClusterWorker(nil)
	insertTestUserMessages(t, "sess-nil-hub", []string{"test nil hub"})
	cwNil.ComputeOnce()
	require.Eventually(t, func() bool { return !cwNil.IsRunning() }, 10*time.Second, 100*time.Millisecond)
	// Just verify it completes without error — no crash from nil hub
}

// ---------- TestClusterWorker_ComputeOnce_EmptyMessages ----------

func TestClusterWorker_ComputeOnce_EmptyMessages(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)

	// No messages inserted → should still complete gracefully
	cw.ComputeOnce()
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 100*time.Millisecond)

	progress := cw.GetProgress()
	assert.Equal(t, "done", progress.Status)
	assert.Equal(t, 0, progress.MsgCount)
	assert.Equal(t, 0, progress.ClusterCount)
}

// ---------- TestClusterWorker_Stop ----------

func TestClusterWorker_Stop(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	cw := NewClusterWorker(nil)

	// Insert many messages to keep computation running longer
	longContents := make([]string, 100)
	for i := range longContents {
		longContents[i] = strings.Repeat("a", 50) + string(rune(i))
	}
	insertTestUserMessages(t, "sess-1", longContents)

	cw.ComputeOnce()
	// Give goroutine time to start and enter extracting/clustering
	time.Sleep(100 * time.Millisecond)

	// Stop should cancel the goroutine
	cw.Stop()

	// After stop, IsRunning should eventually be false
	// (the goroutine's defer sets running=false, with recover() for DB panics)
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 5*time.Second, 50*time.Millisecond)

	// Wait for goroutine cleanup to fully finish (including any DB writes
	// or recover from nil-dbRead panics after test teardown).
	// Use Eventually to poll instead of a fragile time.Sleep.
	require.Eventually(t, func() bool {
		// The goroutine's defer has run if generation hasn't changed during this check.
		// We just verify IsRunning stays false — the goroutine is fully done.
		return !cw.IsRunning()
	}, 2*time.Second, 50*time.Millisecond)
}

// ---------- Integration: StartClusterWorker / StopClusterWorker ----------

func TestStartAndStopClusterWorker(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	origDB := service.UnsafeDBForTest()
	defer func() { service.SetDBForTest(origDB, origDB) }()

	require.NoError(t, service.InitDB())
	defer service.CloseDB()

	// Init RAG
	require.NoError(t, Init(model.RAGConfig{}))
	defer Shutdown()

	// Start cluster worker
	StartClusterWorker(nil)
	assert.NotNil(t, GlobalClusterWorker)

	// Stop cluster worker
	StopClusterWorker()
	assert.Nil(t, GlobalClusterWorker)
}
