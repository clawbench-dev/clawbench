package kb

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"clawbench/internal/kb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这些用例原先在 handler/coverage_extra_test.go 里，随知识库一起搬过来：
// 它们测的是 KB 的参数校验与错误映射，属于这个功能本身。

// A blank or over-long name is refused for create AND rename: an empty name
// renders as an unclickable row in the panel, and the two paths must agree.

// A blank or over-long name is refused for create AND rename: an empty name
// renders as an unclickable row in the panel, and the two paths must agree.
func TestServeKBCollections_NameIsRequiredOnCreateAndUpdate(t *testing.T) {
	setupKBHandlerTest(t)

	for _, name := range []string{"", "   ", strings.Repeat("名", 101)} {
		w := callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
			"name": name, "scope": "global",
		}))
		assert.Equal(t, http.StatusBadRequest, w.Code, "create with name %q: %s", name, w.Body.String())
		assert.Contains(t, w.Body.String(), "KBNameRequired")
	}

	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	query := "/api/kb/collections?id=" + strconv.FormatInt(col.ID, 10)
	for _, name := range []string{"", " ", strings.Repeat("x", 101)} {
		w := callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, query, map[string]any{"name": name}))
		assert.Equal(t, http.StatusBadRequest, w.Code, "rename to %q: %s", name, w.Body.String())
	}

	// Every refused rename left the stored name alone.
	stored, err := kb.GetCollection(col.ID)
	require.NoError(t, err)
	assert.Equal(t, "资料", stored.Name)

	// A valid name is applied trimmed, not verbatim.
	w := callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, query, map[string]any{"name": "  新名字  "}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "新名字", kbCollectionJSON(t, w).Name)
}

// An omitted scope means "this project", not "global": the default decides who
// else can see the collection, so it must be pinned.

// An omitted scope means "this project", not "global": the default decides who
// else can see the collection, so it must be pinned.
func TestServeKBCollections_OmittedScopeDefaultsToTheProject(t *testing.T) {
	env := setupKBHandlerTest(t)

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "项目资料",
	}), env.ProjectDir)
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	col := kbCollectionJSON(t, w)
	assert.Equal(t, kb.ScopeProject, col.Scope, "an omitted scope must not mean global")
	assert.Equal(t, "项目资料", col.Name)

	// Without a cookie there is no project to own it, so the request is refused
	// (403 NoProjectSelected) rather than silently creating a global collection.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "无项目",
	}))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "NoProjectSelected")

	// An unknown scope is a client bug, not a license to create something global.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "误解", "scope": "workspace",
	}))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InvalidRequestBody")
}

// A collection that belongs to another project must be invisible even for
// PATCH/DELETE: those are the two verbs that would otherwise let one project
// rename or destroy another's data by guessing a small integer id.

// A collection that belongs to another project must be invisible even for
// PATCH/DELETE: those are the two verbs that would otherwise let one project
// rename or destroy another's data by guessing a small integer id.
func TestServeKBCollections_ForeignCollectionIsForbidden(t *testing.T) {
	env := setupKBHandlerTest(t)

	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 424242)
	require.NoError(t, err)

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		var body any
		if method == http.MethodPatch {
			body = map[string]any{"auto_inject": false}
		}
		req := withProjectCookie(newRequest(t, method,
			"/api/kb/collections?id="+strconv.FormatInt(foreign.ID, 10), body), env.ProjectDir)
		w := callHandler(ServeKBCollections, req)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s: %s", method, w.Body.String())
	}

	still, err := kb.GetCollection(foreign.ID)
	require.NoError(t, err, "a forbidden rename/delete must leave the row in place")
	assert.Equal(t, "别人的", still.Name)
	assert.True(t, still.AutoInject)
}

// An unknown project sees the global collections and nothing else — it must
// never fall back to "everything", which would leak every project's data.

// An unknown project sees the global collections and nothing else — it must
// never fall back to "everything", which would leak every project's data.
func TestServeKBCollections_UnknownProjectSeesOnlyGlobalOnes(t *testing.T) {
	env := setupKBHandlerTest(t)

	global, err := kb.CreateCollection("全局", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	mine, err := kb.CreateCollection("我的", "", kb.ScopeProject, mustProjectID(t, env.ProjectDir))
	require.NoError(t, err)
	other, err := kb.CreateCollection("其他项目", "", kb.ScopeProject, 999999)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/kb/collections", nil), env.ProjectDir)
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Collections []kb.Collection `json:"collections"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	names := make([]string, 0, len(body.Collections))
	for _, c := range body.Collections {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{global.Name, mine.Name}, names, "another project's collection must not appear")
	assert.NotContains(t, names, other.Name)
}

// The document detail endpoint is reachable by two lookups (id, and id +
// chunk_id). A chunk id that no longer exists — a reindex since the search —
// must fall back to the document's first page instead of failing the request,
// because the user clicked a hit that WAS valid a second ago.

// The document detail endpoint is reachable by two lookups (id, and id +
// chunk_id). A chunk id that no longer exists — a reindex since the search —
// must fall back to the document's first page instead of failing the request,
// because the user clicked a hit that WAS valid a second ago.
func TestServeKBDocuments_ChunkWindowFallsBackWhenTheChunkIsGone(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	path := writeKBTextFile(t, env.ProjectDir, "notes.md", "# 笔记\n\n第一段内容\n\n第二段内容\n")
	doc, err := kb.AddDocumentFromPath(col.ID, path)
	require.NoError(t, err)
	waitKBDocumentReady(t, doc.ID)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query+"&chunk_id=999999", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detail struct {
		Chunks       []kb.Chunk `json:"chunks"`
		FocusChunkID int64      `json:"focus_chunk_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Zero(t, detail.FocusChunkID, "a missing chunk must not be claimed as the focus")
	assert.NotEmpty(t, detail.Chunks, "the document's first page is the fallback")

	// A non-positive / non-numeric chunk id is a malformed request, not a
	// "missing chunk".
	for _, bad := range []string{"0", "-3", "abc"} {
		w = callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query+"&chunk_id="+bad, nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, "chunk_id=%s", bad)
	}
}

// Editing tags needs the field to be present. `{}` is not "clear the tags": a
// client that forgot the key would otherwise silently wipe them.

// Editing tags needs the field to be present. `{}` is not "clear the tags": a
// client that forgot the key would otherwise silently wipe them.
func TestServeKBDocuments_TagsFieldIsRequired(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	path := writeKBTextFile(t, env.ProjectDir, "tagged.md", "内容\n")
	doc, err := kb.AddDocumentFromPath(col.ID, path)
	require.NoError(t, err)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{}))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InvalidRequestBody")

	// A present-but-empty string IS an intentional clear and is stored.
	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{"tags": "  a, b  "}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var updated kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "a, b", updated.Tags, "tags are stored trimmed")
}

// A document in another project's collection is unreachable through the detail,
// tag and delete endpoints — the same boundary the collection endpoints enforce.

// A document in another project's collection is unreachable through the detail,
// tag and delete endpoints — the same boundary the collection endpoints enforce.
func TestServeKBDocuments_ForeignDocumentIsForbidden(t *testing.T) {
	setupKBHandlerTest(t)
	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 888888)
	require.NoError(t, err)
	filePath := writeKBTextFile(t, t.TempDir(), "secret.md", "别人的内容\n")
	doc, err := kb.AddDocumentFromPath(foreign.ID, filePath)
	require.NoError(t, err)
	query := "/api/kb/documents?id=" + strconv.FormatInt(doc.ID, 10)

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, query, nil))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodPatch, query, map[string]any{"tags": "x"}))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodDelete, query, nil))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, err = kb.GetDocument(doc.ID)
	assert.NoError(t, err, "a forbidden delete must leave the document in place")
}

// An upload without the file part is a client bug; the panel shows
// "NoFileProvided" rather than a generic failure, and nothing is indexed.

// An upload without the file part is a client bug; the panel shows
// "NoFileProvided" rather than a generic failure, and nothing is indexed.
func TestServeKBDocumentUpload_RequiresTheFilePart(t *testing.T) {
	setupKBHandlerTest(t)
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("collection_id", strconv.FormatInt(col.ID, 10)))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := callHandler(ServeKBDocumentUpload, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "NoFileProvided")
	docs, err := kb.ListDocuments(col.ID)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

// The detail of a private-address rejection is what tells the user WHY a URL was
// refused; a plain "BadRequest" would look like a client bug.

// The detail of a private-address rejection is what tells the user WHY a URL was
// refused; a plain "BadRequest" would look like a client bug.
func TestWriteKBError_PrivateAddressCarriesTheDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeKBError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents/url", http.NoBody),
		&kb.FileError{Reason: kb.ReasonPrivateAddress, Detail: "127.0.0.1", Err: kb.ErrPrivateAddress})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "KBPrivateAddress")
	assert.Contains(t, rec.Body.String(), "127.0.0.1")

	// A typed "too large" rejection reports 413 and the configured limit, so the
	// user knows what to shrink the file to.
	rec = httptest.NewRecorder()
	writeKBError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents", http.NoBody),
		&kb.FileError{Reason: kb.ReasonFileTooLarge, Detail: "80", Err: kb.ErrFileTooLarge})
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "KBReasonFileTooLarge")
	assert.Contains(t, rec.Body.String(), strconv.FormatInt(kb.MaxFileBytes()>>20, 10))
}

// writeKBFileError is also reachable with a sentinel that carries no FileError
// (ErrUnsupportedFile / ErrNoText): it must answer the generic key rather than
// dereferencing a nil reason.

// writeKBFileError is also reachable with a sentinel that carries no FileError
// (ErrUnsupportedFile / ErrNoText): it must answer the generic key rather than
// dereferencing a nil reason.
func TestWriteKBFileError_SentinelWithoutAFileError(t *testing.T) {
	for _, err := range []error{kb.ErrNoText, kb.ErrUnsupportedFile} {
		rec := httptest.NewRecorder()
		writeKBFileError(rec, httptest.NewRequest(http.MethodPost, "/api/kb/documents", http.NoBody), err)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "KBUnsupportedFile")
	}
}

// A malformed JSON body is a 400 from decodeJSON; the handler must not continue
// with a zero request (for documents/url a zero collection means "global").

// A malformed JSON body is a 400 from decodeJSON; the handler must not continue
// with a zero request (for documents/url a zero collection means "global").
func TestKBMalformedBodiesAreRejected(t *testing.T) {
	setupKBHandlerTest(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"collections create", ServeKBCollections, "/api/kb/collections"},
		{"document move", ServeKBDocumentMove, "/api/kb/documents/move"},
		{"document url", ServeKBDocumentURL, "/api/kb/documents/url"},
		{"document import", ServeKBDocumentImport, "/api/kb/documents/import"},
		{"search", ServeKBSearch, "/api/kb/search"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"collection_id":`))
		req.Header.Set("Content-Type", "application/json")
		w := callHandler(tc.handler, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// ── mail: the client-id classification ─────────────────────────────────────

// A server that refuses a command until the client identifies itself is an
// upstream problem (502 with its own key), not something the user can fix by
// re-entering their password.

// ── kb: guarded inputs ──────────────────────────────────────────────────────
//
// Most of the knowledge-base surface is a thin request → kb mapping. What is
// worth pinning is the behavior a client can observe being WRONG: a body that
// is accepted when it should be refused, a collection from another project that
// is visible, or a lookup that fails where the panel needs a fallback. Each case
// names the status (and the message key) the caller must see.

func kbCollectionJSON(t *testing.T, rec *httptest.ResponseRecorder) kb.Collection {
	t.Helper()
	var col kb.Collection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &col), rec.Body.String())
	return col
}

func writeKBTextFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
