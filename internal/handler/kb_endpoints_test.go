package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"clawbench/internal/kb"

	"github.com/stretchr/testify/require"
)

// kbCalls drives one KB handler and returns the recorder.
type kbCase struct {
	name   string
	path   string
	body   any
	handle http.HandlerFunc
	want   int
	method string
}

func runKBCase(t *testing.T, env *testEnv, c kbCase) *httptest.ResponseRecorder {
	t.Helper()
	method := c.method
	if method == "" {
		method = http.MethodPost
	}
	req := newRequest(t, method, c.path, c.body)
	if env != nil {
		req = withProjectCookie(req, env.ProjectDir)
	}
	w := callHandler(c.handle, req)
	require.Equal(t, c.want, w.Code, "%s: %s", c.name, w.Body.String())
	return w
}

// TestKBDocumentEndpoints_MissingDocumentAndBadParams pins the shared
// "address the document first, then check access" order every per-document
// endpoint uses. A missing id must be a 404 (the panel reloads the list) and a
// malformed one a 400 — getting these two mixed makes a stale tab look like a
// client bug.
func TestKBDocumentEndpoints_MissingDocumentAndBadParams(t *testing.T) {
	env := setupKBHandlerTest(t)

	handlers := []struct {
		name   string
		handle http.HandlerFunc
		method string
		path   string
	}{
		{"reindex", ServeKBDocumentReindex, http.MethodPost, "/api/kb/documents/reindex?id="},
		{"refresh", ServeKBDocumentRefresh, http.MethodPost, "/api/kb/documents/refresh?id="},
		{"delete", ServeKBDocuments, http.MethodDelete, "/api/kb/documents?id="},
		{"patch", ServeKBDocuments, http.MethodPatch, "/api/kb/documents?id="},
		{"detail", ServeKBDocuments, http.MethodGet, "/api/kb/documents?id="},
	}
	for _, tc := range handlers {
		// Missing document: 404, not "queued" / "refreshed".
		req := newRequest(t, tc.method, tc.path+"9999", map[string]any{"tags": "x"})
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandler(tc.handle, req)
		require.Equal(t, http.StatusNotFound, w.Code, "%s: %s", tc.name, w.Body.String())

		// Non-numeric id: 400 before any lookup.
		req = newRequest(t, tc.method, tc.path+"abc", map[string]any{"tags": "x"})
		req = withProjectCookie(req, env.ProjectDir)
		w = callHandler(tc.handle, req)
		require.Equal(t, http.StatusBadRequest, w.Code, tc.name)
	}
}

// TestKBDocumentEndpoints_SuccessShapes pins the success bodies the panel reads
// field by field (queued / changed / deleted / moved). A rename here breaks the
// UI silently, because the status stays 200.
func TestKBDocumentEndpoints_SuccessShapes(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("库", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	createTestFile(t, env.ProjectDir, "policy.txt", "报销流程：先提交申请。")
	doc, err := kb.AddDocumentFromPath(col.ID, filepath.Join(env.ProjectDir, "policy.txt"))
	require.NoError(t, err)

	w := runKBCase(t, env, kbCase{name: "reindex", path: "/api/kb/documents/reindex?id=" + strconv.FormatInt(doc.ID, 10), handle: ServeKBDocumentReindex, want: http.StatusAccepted})
	require.JSONEq(t, `{"queued":true}`, w.Body.String())

	w = runKBCase(t, env, kbCase{name: "refresh", path: "/api/kb/documents/refresh?id=" + strconv.FormatInt(doc.ID, 10), handle: ServeKBDocumentRefresh, want: http.StatusOK})
	var refreshed struct {
		Changed bool `json:"changed"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refreshed))
	require.False(t, refreshed.Changed, "the original was not touched")

	// The detail endpoint always answers with all three keys, even when the
	// document has no chunks yet (the panel does `chunks.length`).
	w = runKBCase(t, env, kbCase{name: "detail", path: "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10), handle: ServeKBDocuments, want: http.StatusOK, method: http.MethodGet})
	require.Contains(t, w.Body.String(), `"document":`)
	require.Contains(t, w.Body.String(), `"focus_chunk_id":`)

	w = runKBCase(t, env, kbCase{name: "delete", path: "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10), handle: ServeKBDocuments, want: http.StatusOK, method: http.MethodDelete})
	require.JSONEq(t, `{"deleted":true}`, w.Body.String())
}

// TestKBMoveEndpoint_CrossProjectIsForbidden: both ends of a move are checked
// against the caller's project. A one-sided check lets a user pull another
// project's document into theirs (or push one out) by id.
func TestKBMoveEndpoint_CrossProjectIsForbidden(t *testing.T) {
	env := setupKBHandlerTest(t)
	mine, err := kb.CreateCollection("我的", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 987654)
	require.NoError(t, err)

	createTestFile(t, env.ProjectDir, "mine.txt", "内容")
	src := filepath.Join(env.ProjectDir, "mine.txt")
	doc, err := kb.AddDocumentFromPath(mine.ID, src)
	require.NoError(t, err)

	// Moving INTO a foreign collection.
	w := runKBCase(t, env, kbCase{name: "in", path: "/api/kb/documents/move", body: map[string]any{
		"id": doc.ID, "collection_id": foreign.ID,
	}, handle: ServeKBDocumentMove, want: http.StatusForbidden})
	require.Contains(t, w.Body.String(), "AccessDenied")

	// A document that lives in a foreign collection cannot be moved out either.
	foreignDoc, err := kb.AddDocumentFromPath(foreign.ID, src)
	if err == nil {
		w = runKBCase(t, env, kbCase{name: "out", path: "/api/kb/documents/move", body: map[string]any{
			"id": foreignDoc.ID, "collection_id": mine.ID,
		}, handle: ServeKBDocumentMove, want: http.StatusForbidden})
		require.Contains(t, w.Body.String(), "AccessDenied")
	}

	// The same-collection move is a no-op success (the panel re-sends it).
	runKBCase(t, env, kbCase{name: "same", path: "/api/kb/documents/move", body: map[string]any{
		"id": doc.ID, "collection_id": mine.ID,
	}, handle: ServeKBDocumentMove, want: http.StatusOK})
}

// TestKBCollections_ListIsNeverNull: the panel maps over the list, so an empty
// installation must send [] and not null.
func TestKBCollections_ListIsNeverNull(t *testing.T) {
	env := setupKBHandlerTest(t)
	w := callHandler(ServeKBCollections, withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/collections", nil), env.ProjectDir))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"collections":[]`)

	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	w = callHandler(ServeKBDocuments, withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/documents?collection_id="+strconv.FormatInt(col.ID, 10), nil), env.ProjectDir))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"documents":[]`)

	w = callHandler(ServeKBDocumentProgress, withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/documents/progress?collection_id="+strconv.FormatInt(col.ID, 10), nil), env.ProjectDir))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"documents":[]`)
}

// TestServeKBDocumentUpload_CollectionFromQueryOrForm: the id may arrive either
// in the query string (the panel's upload URL) or as a form field (a client that
// builds the body first). Both must work; neither must be read from the filename.
func TestServeKBDocumentUpload_CollectionFromQueryOrForm(t *testing.T) {
	setupKBHandlerTest(t)
	col, err := kb.CreateCollection("上传", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	// Query string form.
	buf, contentType := multipartFile(t, "query.txt", "来自查询串", "")
	req := httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload?collection_id="+strconv.FormatInt(col.ID, 10), buf)
	req.Header.Set("Content-Type", contentType)
	w := callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// Form field form.
	buf, contentType = multipartFile(t, "form.txt", "来自表单", strconv.FormatInt(col.ID, 10))
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", buf)
	req.Header.Set("Content-Type", contentType)
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	docs, err := kb.ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, docs, 2)

	// Neither id nor file: the shared "invalid body" answer.
	buf, contentType = multipartFile(t, "lonely.txt", "x", "")
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", buf)
	req.Header.Set("Content-Type", contentType)
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// multipartFile builds a one-file multipart body, optionally carrying the
// collection id as a form field.
func multipartFile(t *testing.T, name, body, collectionField string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if collectionField != "" {
		require.NoError(t, mw.WriteField("collection_id", collectionField))
	}
	part, err := mw.CreateFormFile("file", name)
	require.NoError(t, err)
	_, err = part.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}
