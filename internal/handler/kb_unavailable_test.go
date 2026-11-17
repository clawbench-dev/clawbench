package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"clawbench/internal/kb"

	"github.com/stretchr/testify/require"
)

// TestKBIndexingDisabledReportsUnavailable: with kb.enabled=false every endpoint
// answers the typed 503. That is what the panel keys off to show "knowledge base
// is off" instead of an empty library that looks broken — and it must hold for
// the write verbs too, not only the lists.
func TestKBIndexingDisabledReportsUnavailable(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	// Shut the feature down AFTER the collection exists, so the 503 cannot be
	// explained by "there was nothing there".
	kb.Shutdown()

	id := strconv.FormatInt(col.ID, 10)
	cases := []struct {
		name   string
		handle http.HandlerFunc
		method string
		path   string
		body   any
	}{
		{"list collections", ServeKBCollections, http.MethodGet, "/api/kb/collections", nil},
		{"create collection", ServeKBCollections, http.MethodPost, "/api/kb/collections", map[string]any{"name": "x", "scope": "global"}},
		{"update collection", ServeKBCollections, http.MethodPatch, "/api/kb/collections?id=" + id, map[string]any{"name": "y"}},
		{"delete collection", ServeKBCollections, http.MethodDelete, "/api/kb/collections?id=" + id, nil},
		{"list documents", ServeKBDocuments, http.MethodGet, "/api/kb/documents?collection_id=" + id, nil},
		{"add document", ServeKBDocuments, http.MethodPost, "/api/kb/documents", map[string]any{"collection_id": col.ID, "path": "/tmp/x.txt"}},
		{"move document", ServeKBDocumentMove, http.MethodPost, "/api/kb/documents/move", map[string]any{"id": 1, "collection_id": col.ID}},
		{"fetch url", ServeKBDocumentURL, http.MethodPost, "/api/kb/documents/url", map[string]any{"collection_id": col.ID, "url": "https://example.com"}},
		{"import folder", ServeKBDocumentImport, http.MethodPost, "/api/kb/documents/import", map[string]any{"collection_id": col.ID, "path": env.ProjectDir}},
		{"progress", ServeKBDocumentProgress, http.MethodGet, "/api/kb/documents/progress?collection_id=" + id, nil},
		{"usage", ServeKBDocumentUsage, http.MethodGet, "/api/kb/documents/usage?collection_id=" + id, nil},
		{"search", ServeKBSearch, http.MethodPost, "/api/kb/search", map[string]any{"q": "报销"}},
		{"reindex vector", ServeKBReindex, http.MethodPost, "/api/kb/reindex", map[string]any{"kind": "vector"}},
	}
	for _, tc := range cases {
		req := newRequest(t, tc.method, tc.path, tc.body)
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandler(tc.handle, req)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// TestKBSearchRejectsForeignCollectionWithoutProject: with no project cookie a
// request that names a project collection is refused rather than quietly
// searched — the same rule the project-scoped case uses.
func TestKBSearchRejectsForeignCollectionWithoutProject(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("项目库", "", kb.ScopeProject, 4242)
	require.NoError(t, err)

	w := callHandler(ServeKBSearch, withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/search", map[string]any{
		"q": "报销", "collection_ids": []int64{col.ID},
	}), env.ProjectDir))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

// TestKBAddDocumentFromPath_RefusesOutsideEveryRoot: the path body is resolved by
// the shared resolver, so a path outside the project roots is a 403 before the
// KB ever sees it. This is the only guard between "add a file" and "read any
// file on the server into an index that is injected into prompts".
func TestKBAddDocumentFromPath_RefusesOutsideEveryRoot(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	// An absolute path outside every root, built from this machine rather than
	// written as "/etc/passwd": on Windows a leading separator resolves against
	// the current drive, which is not necessarily outside the project.
	outside := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	w := callHandler(ServeKBDocuments, withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": outside,
	}), env.ProjectDir))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// A relative path resolves against the project cookie and works.
	createTestFile(t, env.ProjectDir, "docs/policy.txt", "报销流程")
	w = callHandler(ServeKBDocuments, withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": "docs/policy.txt",
	}), env.ProjectDir))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// A relative path with no project cookie cannot be resolved at all.
	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": "docs/policy.txt",
	}))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}
