package kb

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/kb"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// indexKBChunks adds a document with one searchable passage. Extraction is not
// what these tests are about, and a chunk written directly is searchable
// immediately — no indexer, no waiting.
func indexKBChunks(t *testing.T, collectionID int64, title, text string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), title+".txt")
	require.NoError(t, os.WriteFile(src, []byte(text), 0o644))
	doc, err := kb.AddDocumentFromPath(collectionID, src)
	require.NoError(t, err)
	// The indexer runs in the background (setupKBHandlerTest starts it) and
	// produces the same chunk, so waiting for it is what the panel does too.
	require.Eventually(t, func() bool {
		d, getErr := kb.GetDocument(doc.ID)
		return getErr == nil && d.Status == kb.StatusReady
	}, 5*time.Second, 20*time.Millisecond)
}

// TestServeKBSearch_ScopesToCookieProject pins the whole chain from the cookie
// to the query scope: a project collection is searchable only when the request
// carries that project's cookie, and a collection id from another project is
// refused rather than silently searched.
//
// The kb package is handed a projectID and cannot test how it is resolved; that
// happens here, and getting it wrong is a cross-project leak — the AI would cite
// another project's documents.
func TestServeKBSearch_ScopesToCookieProject(t *testing.T) {
	env := setupKBHandlerTest(t)

	projectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	projectCol, err := kb.CreateCollection("项目库", "", kb.ScopeProject, projectID)
	require.NoError(t, err)
	globalCol, err := kb.CreateCollection("全局库", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	indexKBChunks(t, projectCol.ID, "项目制度", "报销流程：先提交申请。")
	indexKBChunks(t, globalCol.ID, "公司制度", "报销流程：财务三天内审批。")

	search := func(req *http.Request) (*kb.SearchResult, int) {
		w := callHandler(ServeKBSearch, req)
		if w.Code != http.StatusOK {
			return nil, w.Code
		}
		var res kb.SearchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		return &res, w.Code
	}

	// No project cookie: only the global collection is in scope.
	res, code := search(newRequest(t, http.MethodPost, "/api/kb/search", map[string]any{"q": "报销流程"}))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, res.Results, 1)
	require.Equal(t, globalCol.ID, res.Results[0].CollectionID)

	// With the project cookie both are in scope.
	res, code = search(withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/search", map[string]any{"q": "报销流程"}), env.ProjectDir))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, res.Results, 2)

	// Asking for another project's collection explicitly is forbidden.
	foreign, err := kb.CreateCollection("别人的库", "", kb.ScopeProject, projectID+999)
	require.NoError(t, err)
	_, code = search(withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/search", map[string]any{
		"q": "报销流程", "collection_ids": []int64{foreign.ID},
	}), env.ProjectDir))
	require.Equal(t, http.StatusForbidden, code)
}

// TestKBProjectID_UnknownPathsResolveToZero: listing collections must not
// register a project as a side effect. An unknown cookie path behaves like "no
// project" (global libraries still work) instead of failing the request.
func TestKBProjectID_UnknownPathsResolveToZero(t *testing.T) {
	env := setupKBHandlerTest(t)

	unknown := filepath.Join(t.TempDir(), "never-registered")
	require.Zero(t, kbProjectID(withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/collections", nil), unknown)))
	_, ok, err := store.ProjectIDByPath(unknown)
	require.NoError(t, err)
	require.False(t, ok, "a read must not create the project row")

	id, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	require.Equal(t, id, kbProjectID(withProjectCookie(newRequest(t, http.MethodGet, "/", nil), env.ProjectDir)))

	require.Zero(t, kbProjectID(newRequest(t, http.MethodGet, "/", nil)))
}
