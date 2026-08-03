package service

import (
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionCleanupSvc implements sessionCleanupSvc for testing.
type mockSessionCleanupSvc struct {
	expiredSessions []string
	expiredErr      error
	purgeSessions   int64
	purgeMessages   int64
	purgeErr        error
	calledGet       atomic.Int32
	calledPurge     atomic.Int32
}

func (m *mockSessionCleanupSvc) GetExpiredArchivedSessions(cutoff time.Time) ([]string, error) {
	m.calledGet.Add(1)
	if m.expiredErr != nil {
		return nil, m.expiredErr
	}
	return m.expiredSessions, nil
}

func (m *mockSessionCleanupSvc) PurgeArchivedData(sessionIDs []string) (int64, int64, error) {
	m.calledPurge.Add(1)
	if m.purgeErr != nil {
		return 0, 0, m.purgeErr
	}
	return m.purgeSessions, m.purgeMessages, nil
}

// ---------- cleanup() method ----------

func TestSessionCleanup_SkipsWhenDisabled(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = false
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(0), mock.calledGet.Load(), "should not query when disabled")
}

func TestSessionCleanup_SkipsWhenRetentionDaysZero(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 0

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(0), mock.calledGet.Load(), "should not query when days is 0 (keep forever)")
}

func TestSessionCleanup_SkipsWhenRetentionDaysNegative(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = -5

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(0), mock.calledGet.Load(), "should not query when days is negative")
}

func TestSessionCleanup_NoExpiredSessions(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{
		expiredSessions: []string{},
	}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load(), "should query for expired sessions")
	assert.Equal(t, int32(0), mock.calledPurge.Load(), "should not purge when no expired sessions")
}

func TestSessionCleanup_PurgesExpiredSessions(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{
		expiredSessions: []string{"sess-1", "sess-2"},
		purgeSessions:   2,
		purgeMessages:   10,
	}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(1), mock.calledPurge.Load())
}

func TestSessionCleanup_GetExpiredFails(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{
		expiredErr: errors.New("db error"),
	}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(0), mock.calledPurge.Load(), "should not purge when query fails")
}

func TestSessionCleanup_PurgeFails(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{
		expiredSessions: []string{"sess-1"},
		purgeErr:        errors.New("db error"),
	}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(1), mock.calledPurge.Load(), "should attempt purge even if it fails")
}

func TestSessionCleanup_ContinuesPurgeAfterChunkDeletionFails(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{
		expiredSessions: []string{"sess-1"},
		purgeSessions:   1,
		purgeMessages:   5,
	}

	w := newSessionCleanupWorkerWithSvc(cfg, mock)

	// Set a failing RAG chunk callback — should not block session purge
	purgeRAGChunksFn = func(ids []string) (int64, error) {
		return 0, errors.New("rag error")
	}
	defer func() { purgeRAGChunksFn = nil }()

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledPurge.Load(), "should still purge session data even if chunk deletion fails")
}

// ---------- Start/Stop lifecycle ----------

func TestSessionCleanupWorker_StartStop(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)
	w.startup = 10 * time.Millisecond

	w.Start()
	assert.True(t, w.running)

	// Wait for startup delay
	time.Sleep(50 * time.Millisecond)

	w.Stop()
	assert.False(t, w.running)
}

func TestSessionCleanupWorker_StartIdempotent(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)
	w.startup = 10 * time.Millisecond

	w.Start()
	w.Start() // second Start should be no-op

	w.Stop()
}

func TestSessionCleanupWorker_StopWhenNotRunning(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	w := NewSessionCleanupWorker(cfg)
	// Stop when not running should be no-op
	w.Stop()
}

func TestSessionCleanupWorker_StopBeforeStartup(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = true
	cfg.Session.ArchiveRetentionDays = 30

	mock := &mockSessionCleanupSvc{}
	w := newSessionCleanupWorkerWithSvc(cfg, mock)
	w.startup = 1 * time.Hour // Long startup delay
	w.interval = 1 * time.Hour

	w.Start()
	time.Sleep(20 * time.Millisecond)
	w.Stop()

	assert.Equal(t, int32(0), mock.calledGet.Load(), "should not call cleanup when stopped before startup")
}

// ---------- Global worker lifecycle ----------

func TestStartStopReconfigureSessionCleanupWorker(t *testing.T) {
	cfg := model.Config{}
	cfg.Session.ArchiveRetentionEnabled = false

	// Start the global worker (startup delay is 5m so cleanup never runs in-test).
	StartSessionCleanupWorker(cfg)
	t.Cleanup(func() { StopSessionCleanupWorker() })
	assert.NotNil(t, globalSessionCleanup)

	// Reconfigure stops the old worker and starts a fresh one with new config.
	ReconfigureSessionCleanup(cfg)
	assert.NotNil(t, globalSessionCleanup)

	StopSessionCleanupWorker()
	assert.Nil(t, globalSessionCleanup, "global worker should be cleared after stop")

	// Stop when nothing is running should be a no-op.
	StopSessionCleanupWorker()
	assert.Nil(t, globalSessionCleanup)
}

// ---------- PurgeRAGChunksBySessionIDs ----------

func TestPurgeRAGChunksBySessionIDs_NoCallback(t *testing.T) {
	purgeRAGChunksFn = nil
	count, err := PurgeRAGChunksBySessionIDs([]string{"sess-1"})
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count, "should return 0 when no callback is set")
}

func TestPurgeRAGChunksBySessionIDs_WithCallback(t *testing.T) {
	purgeRAGChunksFn = func(ids []string) (int64, error) {
		return int64(len(ids)), nil
	}
	defer func() { purgeRAGChunksFn = nil }()

	count, err := PurgeRAGChunksBySessionIDs([]string{"sess-1", "sess-2"})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestSetPurgeRAGChunksFn(t *testing.T) {
	purgeRAGChunksFn = nil
	defer func() { purgeRAGChunksFn = nil }()

	SetPurgeRAGChunksFn(func(ids []string) (int64, error) {
		return int64(len(ids)), nil
	})
	assert.NotNil(t, purgeRAGChunksFn, "callback should be stored")

	count, err := purgeRAGChunksFn([]string{"a", "b"})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

// ---------- realSessionCleanupSvc against a real DB ----------

func TestRealSessionCleanupSvc_PurgesExpiredArchived(t *testing.T) {
	testDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	testDB.SetMaxOpenConns(1)
	_, _ = testDB.Exec("PRAGMA journal_mode=WAL")
	_, _ = testDB.Exec("PRAGMA busy_timeout=5000")

	_, err = testDB.Exec(`
		CREATE TABLE chat_sessions (
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
		CREATE TABLE chat_history (
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
	`)
	require.NoError(t, err)
	cleanup := SetDBForTest(testDB, testDB)
	defer cleanup()
	defer testDB.Close()

	// Insert one archived session (expired) and one archived session (recent).
	_, err = testDB.Exec(`
		INSERT INTO chat_sessions (id, project_path, backend, title, archived, updated_at) VALUES
		('sess-old', '/proj', 'claude', 'Old', 1, datetime('now', '-10 days')),
		('sess-new', '/proj', 'claude', 'New', 1, datetime('now'))
	`)
	require.NoError(t, err)
	_, err = testDB.Exec(`
		INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES
		('/proj', 'user', 'msg1', 'sess-old', 'claude'),
		('/proj', 'user', 'msg2', 'sess-old', 'claude'),
		('/proj', 'user', 'msg3', 'sess-new', 'claude')
	`)
	require.NoError(t, err)

	svc := &realSessionCleanupSvc{}
	cutoff := time.Now().AddDate(0, 0, -5)

	expired, err := svc.GetExpiredArchivedSessions(cutoff)
	require.NoError(t, err)
	assert.Equal(t, []string{"sess-old"}, expired)

	sessionsPurged, messagesPurged, err := svc.PurgeArchivedData(expired)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)

	// Session and its messages are gone; the recent one is untouched.
	var remaining int
	require.NoError(t, testDB.QueryRow("SELECT COUNT(*) FROM chat_sessions").Scan(&remaining))
	assert.Equal(t, 1, remaining)
	require.NoError(t, testDB.QueryRow("SELECT COUNT(*) FROM chat_history").Scan(&remaining))
	assert.Equal(t, 1, remaining)
}
