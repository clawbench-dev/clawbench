package kb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// failingEmbedServer answers every embedding request with a 500. It is what a
// misconfigured or temporarily dead embedding endpoint looks like to the client.
func failingEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not loaded"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedEmbeddedCollection creates a collection with one embedded document whose
// chunk text contains query, so both search legs have something to return.
func seedEmbeddedCollection(t *testing.T, st *Store) int64 {
	t.Helper()
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "handbook.md", "报销流程：先提交申请，再等财务审批。", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NotEmpty(t, pending)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))
	return docID
}

// A dead embedding endpoint must degrade, not fail: the keyword hits are still a
// valid answer, and the reason the semantic half is missing has to reach the
// panel instead of a bare "hybrid" label.
func TestSearch_DeadEmbeddingEndpointDegradesBothModes(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	seedEmbeddedCollection(t, st)

	srv := failingEmbedServer(t)
	mu.Lock()
	globalEmbedder = rag.NewEmbeddingClient(srv.URL, "test-model", "")
	mu.Unlock()

	for _, mode := range []string{SearchModeVector, SearchModeHybrid, ""} {
		res, err := Search(context.Background(), SearchParams{Query: "报销流程", Mode: mode})
		require.NoError(t, err, "mode %q must not fail the request", mode)
		require.True(t, res.VectorDegraded, "mode %q must be marked degraded", mode)
		require.Equal(t, SearchModeFTS, res.Mode, "mode %q falls back to keywords", mode)
		require.Contains(t, res.VectorError, "embedding service unavailable")
		require.NotEmpty(t, res.Results, "mode %q still answers with keyword hits", mode)
	}
}

// An explicit collection list must be checked against what the project may see.
// A list that names someone else's collection is refused as a whole rather than
// silently narrowed: the caller asked for that collection specifically.
func TestSearch_ExplicitCollectionListIsAccessChecked(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	mine := seedEmbeddedCollection(t, st)

	foreign, err := st.CreateCollection(Collection{Name: "theirs", Scope: ScopeProject, ProjectID: 99})
	require.NoError(t, err)

	_, err = Search(context.Background(), SearchParams{Query: "报销流程", CollectionIDs: []int64{foreign}})
	require.ErrorIs(t, err, ErrAccessDenied)

	// The visible collection is fine.
	res, err := Search(context.Background(), SearchParams{Query: "报销流程", CollectionIDs: []int64{mine}})
	require.NoError(t, err)
	require.NotEmpty(t, res.Results)
}

// With no collection visible at all the search answers with an empty keyword
// result rather than querying the index with an empty id list (which would match
// everything).
func TestSearch_NoVisibleCollectionsIsAnEmptyAnswer(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})

	res, err := Search(context.Background(), SearchParams{Query: "报销流程"})
	require.NoError(t, err)
	require.Equal(t, SearchModeFTS, res.Mode)
	require.Empty(t, res.Results)
	require.Zero(t, res.Total)

	res, err = Search(context.Background(), SearchParams{Query: "报销流程", AutoInjectOnly: true})
	require.NoError(t, err)
	require.Empty(t, res.Results)
}

// AutoInjectOnly restricts the implicit list to collections that opted in: a
// collection the user added for manual search must not be injected into chat.
func TestSearch_AutoInjectOnlySkipsCollectionsThatOptedOut(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "manual", Scope: ScopeGlobal, AutoInject: false})
	require.NoError(t, err)
	seedDoc(t, st, colID, "handbook.md", "报销流程：先提交申请。", "")

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", AutoInjectOnly: true})
	require.NoError(t, err)
	require.Empty(t, res.Results, "an opted-out collection must not be auto-injected")

	res, err = Search(context.Background(), SearchParams{Query: "报销流程"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Results, "but a manual search still finds it")
}

// A limit above the cap is clamped instead of being passed to SQLite: the panel
// sends whatever the user typed into "results per page".
func TestSearch_ClampsTheLimitTo100(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	for i := range 120 {
		seedDoc(t, st, colID, "doc"+itoa(i)+".md", "报销流程 第"+itoa(i)+"条", "")
	}

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", Limit: 5000})
	require.NoError(t, err)
	require.LessOrEqual(t, len(res.Results), 100)
	require.NotEmpty(t, res.Results)

	res, err = Search(context.Background(), SearchParams{Query: "报销流程", Limit: -3})
	require.NoError(t, err, "a negative limit falls back to the default instead of an error")
	require.NotEmpty(t, res.Results)
}

// errNoVectorIndex distinguishes the three reasons the semantic leg cannot run:
// they need different fixes, and the panel shows them differently.
func TestErrNoVectorIndex_NamesTheActualReason(t *testing.T) {
	st := newTestStore(t)

	require.Contains(t, errNoVectorIndex(nil, st).Error(), "no embedding service")

	// A model is configured but nothing has been embedded: the library is still
	// being indexed, which is normal right after a rebuild.
	noEmbeddings := rag.NewEmbeddingClient("http://127.0.0.1:1", "m", "")
	require.Contains(t, errNoVectorIndex(noEmbeddings, st).Error(), "still being indexed")

	// Vectors exist but kb_vec is missing.
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	_ = seedDoc(t, st, colID, "a.md", "报销流程", "")
	require.False(t, st.HasVecData())
}

// explainVectorError turns SQLite text into an instruction the user can act on,
// and passes anything it does not recognize through unchanged rather than
// inventing a cause.
func TestExplainVectorError(t *testing.T) {
	require.Empty(t, explainVectorError(nil))

	require.Contains(t, explainVectorError(errString("Dimension mismatch: kb_vec expects 3")), "model changed")
	require.Contains(t, explainVectorError(errString("dimension mismatch")), "model changed")
	require.Contains(t, explainVectorError(errString("no such table: kb_vec")), "still being built")

	// Unknown errors keep their own message: a made-up explanation is worse than
	// the raw driver text.
	require.Equal(t, "disk I/O error", explainVectorError(errString("disk I/O error")))
}

type errString string

func (e errString) Error() string { return string(e) }

// The per-document cap and the character budget are what keep one long file from
// crowding out the rest of the injected context.
func TestBuildContext_CapsPerDocumentAndTotal(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	// One document with four chunks, plus a second document with one. One
	// ReplaceChunks call carries all four: the unique index is per document.
	longID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "long.md", ContentHash: "long.md", Status: StatusReady,
	})
	require.NoError(t, err)
	chunks := make([]Chunk, 0, 4)
	for i := range 4 {
		text := "报销流程 段落" + itoa(i) + " 内容内容内容内容内容内容内容内容内容内容"
		chunks = append(chunks, Chunk{
			DocumentID:         longID,
			CollectionID:       colID,
			ChunkText:          text,
			ChunkTextSegmented: rag.SegmentText(text),
			ChunkIndex:         i,
			TokenCount:         10,
		})
	}
	require.NoError(t, st.ReplaceChunks(longID, chunks, "", colID))
	seedDoc(t, st, colID, "other.md", "报销流程 另一个文档", "")

	// Each document contributes at most two chunks.
	text, refs := BuildContext(context.Background(), "报销流程", 0, model.KBConfig{
		AutoInject: true, InjectTopN: 5, InjectMaxChars: 10000,
	})
	require.NotEmpty(t, text)
	perDoc := map[int64]int{}
	for _, r := range refs {
		perDoc[r.DocumentID]++
	}
	for docID, n := range perDoc {
		require.LessOrEqual(t, n, 2, "document %d contributed %d chunks", docID, n)
	}

	// A tight character budget truncates the text instead of dropping it all.
	_, refs = BuildContext(context.Background(), "报销流程", 0, model.KBConfig{
		AutoInject: true, InjectTopN: 5, InjectMaxChars: 400,
	})
	require.NotEmpty(t, refs)
	total := 0
	for _, r := range refs {
		total += len(r.Text)
	}
	require.LessOrEqual(t, total, 400, "the character budget must hold")
}

// A budget too small to hold even the first chunk yields no context at all
// rather than a single truncated fragment.
func TestBuildContext_BudgetTooSmallForTheFirstChunk(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, colID, "handbook.md", strings.Repeat("报销流程", 100), "")

	text, refs := BuildContext(context.Background(), "报销流程", 0, model.KBConfig{
		AutoInject: true, InjectTopN: 2, InjectMaxChars: 120,
	})
	require.Empty(t, text)
	require.Empty(t, refs)
}

// Defaults: no topN and no character budget configured still produce a bounded
// context (4 refs, 2000 characters).
func TestBuildContext_UsesDefaultsWhenUnset(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	for i := range 6 {
		seedDoc(t, st, colID, "doc"+itoa(i)+".md", "报销流程 "+strings.Repeat("内容", 50), "")
	}

	text, refs := BuildContext(context.Background(), "报销流程", 0, model.KBConfig{AutoInject: true})
	require.NotEmpty(t, text)
	require.LessOrEqual(t, len(refs), 4, "the default top-N is 4")
	total := 0
	for _, r := range refs {
		total += len(r.Text)
	}
	require.LessOrEqual(t, total, 2000, "the default character budget is 2000")
}

// A project-scoped query only injects that project's collections plus the global
// ones, so two projects with similar documents do not see each other's context.
func TestBuildContext_IsScopedToTheProject(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	mine, err := st.CreateCollection(Collection{Name: "mine", Scope: ScopeProject, ProjectID: 7, AutoInject: true})
	require.NoError(t, err)
	theirs, err := st.CreateCollection(Collection{Name: "theirs", Scope: ScopeProject, ProjectID: 8, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, mine, "mine.md", "报销流程 属于项目七", "")
	seedDoc(t, st, theirs, "theirs.md", "报销流程 属于项目八", "")

	text, _ := BuildContext(context.Background(), "报销流程", 7, model.KBConfig{AutoInject: true, InjectTopN: 4})
	require.Contains(t, text, "属于项目七")
	require.NotContains(t, text, "属于项目八")
}
