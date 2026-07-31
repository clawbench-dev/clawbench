package service

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
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
