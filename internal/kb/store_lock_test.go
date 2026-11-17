package kb

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"github.com/stretchr/testify/require"
)

// realLocker stands in for the process-wide write lock (serviceWriteLocker) in
// tests. It is deliberately a real sync.Mutex: the no-op fake used by
// newTestStore cannot tell a reentrant-lock deadlock from correct code.
type realLocker struct{ mu sync.Mutex }

func (l *realLocker) Lock()   { l.mu.Lock() }
func (l *realLocker) Unlock() { l.mu.Unlock() }

// TestEnsureDimensionMismatchDoesNotDeadlock pins the regression that hung the
// whole application: ResetVector holds the write lock and then asked
// ensureVecTable for the recreated table, which tried to take the same
// non-reentrant lock again. Every other writer in the process (chat
// persistence, tasks, the project registry) waits on that same lock, so the
// symptom was not "embedding is slow" but "the server stopped accepting
// writes".
func TestEnsureDimensionMismatchDoesNotDeadlock(t *testing.T) {
	st, err := newStore(filepath.Join(t.TempDir(), "kb.db"), &realLocker{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	setTestGlobals(t, st, model.KBConfig{})

	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: colID, ChunkIndex: 0,
		ChunkText: "x", ChunkTextSegmented: "x",
	}}, "", colID))
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))

	// Run on a goroutine so a deadlock fails the test instead of hanging the
	// package until the go test timeout.
	done := make(chan error, 1)
	go func() { done <- st.EnsureDimension(4) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("EnsureDimension deadlocked on the write lock (reentrant lock in ResetVector)")
	}

	// The rebuild really happened: the table is back at the new width and the
	// lock was released (this next call would block forever otherwise).
	dim, ok := st.vecDimFromSchema()
	require.True(t, ok)
	require.Equal(t, 4, dim)
	require.NoError(t, st.SetDocumentStatus(docID, StatusReady, "", ""))
}
