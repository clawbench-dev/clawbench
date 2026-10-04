package rag_test

import (
	"testing"
	"time"

	"clawbench/internal/rag"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForClusterWorkerIdle polls until the worker leaves the running state.
func waitForClusterWorkerIdle(t *testing.T, cw *rag.ClusterWorker) {
	t.Helper()
	require.Eventually(t, func() bool { return !cw.IsRunning() }, 10*time.Second, 50*time.Millisecond)
}

// TestClusterWorker_ComputeOnce_ExtractingError covers the phase-1 failure
// branch: when the user-message query fails, the worker must record an error
// state and phase rather than silently finishing. Dropping chat_history makes
// GetUserMessageStats fail while the meta table stays writable.
func TestClusterWorker_ComputeOnce_ExtractingError(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("DROP TABLE chat_history")
	require.NoError(t, err)

	cw := rag.NewClusterWorker(nil)
	cw.ComputeOnce()
	waitForClusterWorkerIdle(t, cw)

	progress := cw.GetProgress()
	assert.Equal(t, "error", progress.Status)
	assert.Equal(t, "extracting", progress.Phase)
	assert.NotEmpty(t, progress.Error)
}

// TestClusterWorker_ComputeOnce_SavingError covers the phase-3 failure branch:
// extracting and clustering succeed but persisting the cache fails. Dropping
// message_clusters_cache makes SaveClusterCache fail after the cluster list is
// built, so the worker must report an error in the "saving" phase.
func TestClusterWorker_ComputeOnce_SavingError(t *testing.T) {
	teardown := setupTestDBForClusterWorker(t)
	defer teardown()

	insertTestUserMessages(t, "sess-1", []string{"hello", "hello", "fix bug"})

	_, err := store.UnsafeDBForTest().Exec("DROP TABLE message_clusters_cache")
	require.NoError(t, err)

	cw := rag.NewClusterWorker(nil)
	cw.ComputeOnce()
	waitForClusterWorkerIdle(t, cw)

	progress := cw.GetProgress()
	assert.Equal(t, "error", progress.Status)
	assert.Equal(t, "saving", progress.Phase)
	assert.NotEmpty(t, progress.Error)
}
