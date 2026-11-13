package kb

import (
	"context"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// setTestGlobals installs a store as the package's active one for the duration
// of a test. The KB lifecycle functions are process-global by design (one
// knowledge base per server), so tests must install and restore them.
func setTestGlobals(t *testing.T, st *Store, cfg model.KBConfig) {
	t.Helper()
	mu.Lock()
	oldStore, oldEmb, oldCfg, oldRAG := globalStore, globalEmbedder, globalCfg, globalRAG
	globalStore = st
	globalEmbedder = nil
	globalCfg = cfg
	globalRAG = model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		globalStore, globalEmbedder, globalCfg, globalRAG = oldStore, oldEmb, oldCfg, oldRAG
		mu.Unlock()
	})
}

func seedDoc(t *testing.T, st *Store, colID int64, title, text, location string) int64 {
	t.Helper()
	docID, err := st.AddDocument(Document{
		CollectionID: colID,
		Title:        title,
		ContentHash:  title,
		Status:       StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          text,
		ChunkTextSegmented: rag.SegmentText(text),
		Location:           location,
		ChunkIndex:         0,
		TokenCount:         10,
	}}, ""))
	return docID
}

func TestSearchFTSAndContext(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{AutoInject: true, InjectTopN: 2, InjectMaxChars: 500, InjectMinSimilarity: 0.35})

	colID, err := st.CreateCollection(Collection{Name: "公司制度", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, colID, "员工手册.pdf", "公司的报销流程是先提交申请，再等财务审批。", "第 12 页")

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0})
	require.NoError(t, err)
	require.Equal(t, SearchModeFTS, res.Mode)
	require.NotEmpty(t, res.Results)

	// Explicit modes and an explicit collection list.
	forcedFTS, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0, Mode: SearchModeFTS})
	require.NoError(t, err)
	require.Equal(t, SearchModeFTS, forcedFTS.Mode)
	require.NotEmpty(t, forcedFTS.Results)

	// No embedder configured: vector mode degrades to keyword search.
	forcedVec, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0, Mode: SearchModeVector})
	require.NoError(t, err)
	require.Equal(t, SearchModeFTS, forcedVec.Mode)

	explicit, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0, CollectionIDs: []int64{colID}})
	require.NoError(t, err)
	require.NotEmpty(t, explicit.Results)
	require.Equal(t, "员工手册.pdf", res.Results[0].DocumentTitle)
	require.Equal(t, "第 12 页", res.Results[0].Location)
	require.NotEmpty(t, res.Results[0].MatchPositions)

	ctxText, refs := BuildContext(context.Background(), "报销流程是什么", 0, model.KBConfig{
		AutoInject: true, InjectTopN: 2, InjectMaxChars: 500, InjectMinSimilarity: 0.35,
	})
	require.NotEmpty(t, ctxText)
	require.Contains(t, ctxText, "[知识库参考]")
	require.Contains(t, ctxText, "员工手册.pdf")
	require.Contains(t, ctxText, "第 12 页")
	require.NotEmpty(t, refs)
}

func TestBuildContextSkipsShortAndSlashQueries(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{AutoInject: true, InjectTopN: 2, InjectMaxChars: 500})
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, colID, "a.txt", "知识库内容", "")

	cfg := model.KBConfig{AutoInject: true, InjectTopN: 2, InjectMaxChars: 500}
	text, refs := BuildContext(context.Background(), "a", 0, cfg)
	require.Empty(t, text)
	require.Empty(t, refs)

	text, refs = BuildContext(context.Background(), "/compact", 0, cfg)
	require.Empty(t, text)
	require.Empty(t, refs)

	// AutoInject off means no retrieval at all.
	text, refs = BuildContext(context.Background(), "知识库内容", 0, model.KBConfig{AutoInject: false})
	require.Empty(t, text)
	require.Empty(t, refs)
}

func TestBuildContextRespectsAutoInjectFlag(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{AutoInject: true, InjectTopN: 2, InjectMaxChars: 500})

	onID, err := st.CreateCollection(Collection{Name: "参与", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	offID, err := st.CreateCollection(Collection{Name: "不参与", Scope: ScopeGlobal, AutoInject: false})
	require.NoError(t, err)

	seedDoc(t, st, onID, "公开.txt", "蓝色鲸鱼的生活习性", "")
	seedDoc(t, st, offID, "私密.txt", "红色鲸鱼的内部记录", "")

	text, refs := BuildContext(context.Background(), "鲸鱼", 0, model.KBConfig{
		AutoInject: true, InjectTopN: 4, InjectMaxChars: 500,
	})
	require.NotEmpty(t, text)
	require.Contains(t, text, "公开.txt")
	require.NotContains(t, text, "私密.txt")
	require.Len(t, refs, 1)
}

func TestSearchRejectsForeignCollection(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{AutoInject: true})

	otherID, err := st.CreateCollection(Collection{Name: "别人的", Scope: ScopeProject, ProjectID: 99, AutoInject: true})
	require.NoError(t, err)

	_, err = Search(context.Background(), SearchParams{
		Query:         "测试",
		ProjectID:     7,
		CollectionIDs: []int64{otherID},
	})
	require.ErrorIs(t, err, ErrAccessDenied)
}

func TestSearchEmptyQuery(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	res, err := Search(context.Background(), SearchParams{Query: "   ", ProjectID: 0})
	require.NoError(t, err)
	require.Empty(t, res.Results)
}

func TestFuseRRF(t *testing.T) {
	vec := []SearchHit{
		{ChunkID: 1, ChunkText: "a", VectorScore: 0.9},
		{ChunkID: 2, ChunkText: "b", VectorScore: 0.8},
	}
	fts := []SearchHit{
		{ChunkID: 2, ChunkText: "b", FTSScore: 3.1},
		{ChunkID: 3, ChunkText: "c", FTSScore: 2.0},
	}
	out := fuseRRF(vec, fts, 10)
	require.Len(t, out, 3)
	// Chunk 2 appears in both lists, so it must rank first.
	require.Equal(t, int64(2), out[0].ChunkID)
	require.Greater(t, out[0].Score, out[1].Score)
}

func TestTruncateUTF8(t *testing.T) {
	require.Equal(t, "abc", truncateUTF8("abcdef", 3))
	require.Equal(t, "", truncateUTF8("abc", 0))
	// "知" is 3 bytes; cutting at 2 must not split it.
	require.Equal(t, "", truncateUTF8("知识", 2))
	require.Equal(t, "知", truncateUTF8("知识", 3))
}
