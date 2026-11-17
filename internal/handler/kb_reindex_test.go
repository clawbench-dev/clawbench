package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/kb"
	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// newCollectionInProjectDataDir opens a KB store on the project's real database
// file so this package can seed collections owned by that project. The handler
// resolves the project id through the same registry, which is how a
// project-scoped request finds them.
func newCollectionInProjectDataDir(t *testing.T, projectPath, name string) *kb.Collection {
	t.Helper()
	st, err := kb.NewStoreForTest(filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(projectPath)
	require.NoError(t, err)
	id, err := st.CreateCollection(kb.Collection{
		Name: name, Scope: kb.ScopeProject, ProjectID: pid, AutoInject: true,
	})
	require.NoError(t, err)
	col, err := st.GetCollection(id)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	return col
}

// seedReadyDocument writes one ready document with one chunk into a collection.
func seedReadyDocument(t *testing.T, colID int64, title string) int64 {
	t.Helper()
	st, err := kb.NewStoreForTest(filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	docID, err := st.AddDocument(kb.Document{
		CollectionID: colID, Title: title, Ext: ".md",
		ContentHash: "hash-" + title, Status: kb.StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []kb.Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkIndex:         0,
		ChunkText:          "报销流程：先提交申请，再等财务审批。",
		ChunkTextSegmented: "报销 流程 先 提交 申请",
	}}, "", colID))
	return docID
}

// TestKBReindexAllInEmptyProjectKeepsOtherProjects covers the worst case of the
// "rebuild everything" button: a project with nothing visible used to resolve
// to an empty id list, which the store read as "every collection", so pressing
// the button in an empty project wiped the index of every other project.
func TestKBReindexAllInEmptyProjectKeepsOtherProjects(t *testing.T) {
	setupKBHandlerTest(t)

	otherDir := t.TempDir()
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	otherCol := newCollectionInProjectDataDir(t, otherDir, "另一个项目的库")
	docID := seedReadyDocument(t, otherCol.ID, "other.md")

	// A fresh project with no library visible presses "rebuild all".
	emptyDir := t.TempDir()
	require.NoError(t, os.MkdirAll(emptyDir, 0o755))
	req := newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{})
	req = withProjectCookie(req, emptyDir)
	w := callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res struct {
		Queued bool   `json:"queued"`
		Detail string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.False(t, res.Queued)
	require.Equal(t, "no_collections", res.Detail)

	// The other project's document is untouched: still ready, chunk intact.
	check, err := kb.NewStoreForTest(filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	defer func() { _ = check.Close() }()
	doc, err := check.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, kb.StatusReady, doc.Status)
	require.Equal(t, 1, doc.ChunkCount)
	chunks, err := check.ListChunks(docID, 10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// The same guard one layer down, without the handler in the way.
	require.NoError(t, kb.ReindexCollections([]int64{}))
	doc, err = check.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, kb.StatusReady, doc.Status)
}

// TestKBReindexAllRebuildsVisibleCollections is the control: with a collection
// visible from the current project, "rebuild all" still queues it.
func TestKBReindexAllRebuildsVisibleCollections(t *testing.T) {
	env := setupKBHandlerTest(t)
	col := newCollectionInProjectDataDir(t, env.ProjectDir, "本项目库")
	docID := seedReadyDocument(t, col.ID, "a.md")

	// Read the pre-existing chunk BEFORE the handler runs. The reindex worker
	// starts with the request, and on a fast machine it has already deleted and
	// re-created the chunks by the time this test looks — so the "before"
	// snapshot has to be taken first for the identity check to mean anything.
	check, err := kb.NewStoreForTest(filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	defer func() { _ = check.Close() }()
	oldChunk, err := check.ListChunks(docID, 10)
	require.NoError(t, err)
	require.Len(t, oldChunk, 1, "the fixture must seed exactly one chunk to be reset")

	req := newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	// The reindex worker runs concurrently with this test: it may have moved the
	// document from "pending" through "extracting" and all the way back to
	// "ready" with a freshly embedded chunk before this reads it. The handler's
	// contract is "this document was reset and queued for a rebuild", NOT a
	// snapshot of one status string — asserting "pending" (or "no chunks") made
	// this fail on a correct reindex whenever the worker was fast.
	//
	// The reset is visible either as "chunks gone" or as "a different chunk row
	// exists": ReplaceChunks deletes the old rows inside its transaction, so a
	// row that survives with the SAME id means the reindex never ran.
	require.Eventually(t, func() bool {
		chunks, listErr := check.ListChunks(docID, 10)
		if listErr != nil {
			return false
		}
		if len(chunks) == 0 {
			return true
		}
		return chunks[0].ID != oldChunk[0].ID
	}, 10*time.Second, 20*time.Millisecond,
		"the visible collection must be reset for a rebuild (its old chunk is gone)")
	doc, err := check.GetDocument(docID)
	require.NoError(t, err)
	require.NotEqual(t, kb.StatusFailed, doc.Status,
		"the rebuild must not fail the document, got %q", doc.Status)
}
