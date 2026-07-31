package rag

import (
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCleanupService implements cleanupService for testing.
type mockCleanupService struct {
	expiredSessions []string
	expiredErr      error
	purgeSessions   int64
	purgeMessages   int64
	purgeErr        error
	calledGet       atomic.Int32
	calledPurge     atomic.Int32
}

func (m *mockCleanupService) GetExpiredArchivedSessions(cutoff time.Time) ([]string, error) {
	m.calledGet.Add(1)
	if m.expiredErr != nil {
		return nil, m.expiredErr
	}
	return m.expiredSessions, nil
}

func (m *mockCleanupService) PurgeArchivedData(sessionIDs []string) (int64, int64, error) {
	m.calledPurge.Add(1)
	if m.purgeErr != nil {
		return 0, 0, m.purgeErr
	}
	return m.purgeSessions, m.purgeMessages, nil
}

// ---------- NewCleanupWorker ----------

func TestNewCleanupWorker(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}
	w := NewCleanupWorker(store, cfg)
	assert.NotNil(t, w)
	assert.Equal(t, store, w.store)
	assert.Equal(t, 5*time.Minute, w.startup)
	assert.Equal(t, 24*time.Hour, w.interval)
}

// ---------- Start/Stop ----------

func TestCleanupWorker_StartStop(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}
	w := NewCleanupWorker(store, cfg)
	// Use short delays for testing
	w.startup = 10 * time.Millisecond
	w.interval = 1 * time.Hour
	// Use mock service to avoid nil service.DB panic
	w.svc = &mockCleanupService{}

	w.Start()
	assert.True(t, w.running)

	// Wait for startup delay to pass
	time.Sleep(50 * time.Millisecond)

	w.Stop()
	assert.False(t, w.running)
}

func TestCleanupWorker_StartIdempotent(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}
	w := NewCleanupWorker(store, cfg)
	w.startup = 10 * time.Millisecond
	w.interval = 1 * time.Hour
	w.svc = &mockCleanupService{}

	w.Start()
	// Second Start should be no-op
	w.Start()

	w.Stop()
}

func TestCleanupWorker_StopWhenNotRunning(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}
	w := NewCleanupWorker(store, cfg)

	// Stop when not running should be no-op
	w.Stop()
}

// ---------- cleanup ----------

func TestCleanup_Cleanup_DeletesChunks(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}

	// Insert test chunks
	chunks := []Chunk{
		makeTestChunk("sess-expired", 1, 0, "expired content"),
		makeTestChunk("sess-active", 2, 0, "active content"),
	}
	require.NoError(t, store.InsertChunks(chunks))

	mock := &mockCleanupService{
		expiredSessions: []string{"sess-expired"},
		purgeSessions:   1,
		purgeMessages:   3,
	}

	w := NewCleanupWorker(store, cfg)
	w.svc = mock

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(1), mock.calledPurge.Load())

	// Only the active session's chunk should remain
	count, err := store.ChunkCount()
	assert.NoError(t, err)
	assert.Equal(t, 1, count, "expired session chunks should be deleted")
}

func TestCleanup_Cleanup_NoExpiredSessions(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}

	mock := &mockCleanupService{
		expiredSessions: []string{},
	}

	w := NewCleanupWorker(store, cfg)
	w.svc = mock

	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(0), mock.calledPurge.Load(), "should not purge when no expired sessions")
}

func TestCleanup_Cleanup_GetExpiredFails(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}

	mock := &mockCleanupService{
		expiredErr: assert.AnError,
	}

	w := NewCleanupWorker(store, cfg)
	w.svc = mock

	// Should not panic
	w.cleanup()

	assert.Equal(t, int32(1), mock.calledGet.Load())
	assert.Equal(t, int32(0), mock.calledPurge.Load())
}

func TestCleanup_Cleanup_PurgeFails(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}

	chunks := []Chunk{makeTestChunk("sess-expired", 1, 0, "expired content")}
	require.NoError(t, store.InsertChunks(chunks))

	mock := &mockCleanupService{
		expiredSessions: []string{"sess-expired"},
		purgeErr:        assert.AnError,
	}

	w := NewCleanupWorker(store, cfg)
	w.svc = mock

	// Should not panic
	w.cleanup()

	assert.Equal(t, int32(1), mock.calledPurge.Load())
}

func TestCleanup_Cleanup_NilStore(t *testing.T) {
	cfg := model.RAGConfig{RetentionDays: 90}

	mock := &mockCleanupService{
		expiredSessions: []string{"sess-expired"},
		purgeSessions:   1,
		purgeMessages:   3,
	}

	w := NewCleanupWorker(nil, cfg)
	w.svc = mock

	// Should not panic with nil store
	w.cleanup()

	assert.Equal(t, int32(1), mock.calledPurge.Load())
}

// ---------- run with stop before startup ----------

func TestCleanupWorker_StopBeforeStartup(t *testing.T) {
	store := setupSQLiteStore(t)
	cfg := model.RAGConfig{RetentionDays: 90}
	w := NewCleanupWorker(store, cfg)
	w.startup = 1 * time.Hour // Long startup delay
	w.interval = 1 * time.Hour

	mock := &mockCleanupService{}
	w.svc = mock

	w.Start()
	// Stop immediately — should exit before first cleanup
	time.Sleep(20 * time.Millisecond)
	w.Stop()

	assert.Equal(t, int32(0), mock.calledGet.Load(), "should not call cleanup when stopped before startup")
}
