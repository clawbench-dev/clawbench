package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"clawbench/internal/kb"

	"github.com/stretchr/testify/require"
)

// TestKBReasonKey pins the reason → i18n key table. Every extraction reason has
// its own sentence in the panel; a missing case silently degrades to the
// generic "unsupported file", which is what made legacy .doc look like a bug.
func TestKBReasonKey(t *testing.T) {
	cases := map[string]string{
		kb.ReasonLegacyDoc:      "KBReasonLegacyDoc",
		kb.ReasonLegacyXls:      "KBReasonLegacyXls",
		kb.ReasonLegacyPpt:      "KBReasonLegacyPpt",
		kb.ReasonWPSDoc:         "KBReasonWPSDoc",
		kb.ReasonWPSSheet:       "KBReasonWPSSheet",
		kb.ReasonWPSSlides:      "KBReasonWPSSlides",
		kb.ReasonPages:          "KBReasonPages",
		kb.ReasonNumbers:        "KBReasonNumbers",
		kb.ReasonKeynote:        "KBReasonKeynote",
		kb.ReasonImage:          "KBReasonImage",
		kb.ReasonSVG:            "KBReasonSVG",
		kb.ReasonAudioVideo:     "KBReasonAudioVideo",
		kb.ReasonArchive:        "KBReasonArchive",
		kb.ReasonExecutable:     "KBReasonExecutable",
		kb.ReasonBinary:         "KBReasonBinary",
		kb.ReasonFont:           "KBReasonFont",
		kb.ReasonPDFEncrypted:   "KBReasonPDFEncrypted",
		kb.ReasonPDFNoText:      "KBReasonPDFNoText",
		kb.ReasonNoText:         "KBReasonNoText",
		kb.ReasonFileTooLarge:   "KBReasonFileTooLarge",
		kb.ReasonNotRegular:     "KBReasonNotRegular",
		kb.ReasonUnknownFormat:  "KBReasonUnknownFormat",
		kb.ReasonPrivateAddress: "KBReasonPrivateAddress",
		"":                      "KBUnsupportedFile",
		"future_reason":         "KBUnsupportedFile",
	}
	for reason, wantKey := range cases {
		require.Equal(t, wantKey, kbReasonKey(reason), reason)
	}
}

// TestKBFileErrorStatus: only "too large" is a 413; every other rejection is a
// client mistake the user can fix by picking another file.
func TestKBFileErrorStatus(t *testing.T) {
	require.Equal(t, http.StatusRequestEntityTooLarge, kbFileErrorStatus(kb.ErrFileTooLarge))
	require.Equal(t, http.StatusBadRequest, kbFileErrorStatus(kb.ErrUnsupportedFile))
	require.Equal(t, http.StatusBadRequest, kbFileErrorStatus(errors.New("other")))
}

// TestWriteKBError_MapsSentinelsToStatuses pins the whole classification: each
// kb sentinel answers with the status the panel reacts to (404 opens the empty
// state, 409 offers "refresh", 503 disables the tab). A mis-mapped status turns
// a recoverable condition into a generic red toast.
func TestWriteKBError_MapsSentinelsToStatuses(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{kb.ErrCollectionNotFound, http.StatusNotFound},
		{kb.ErrDocumentNotFound, http.StatusNotFound},
		{kb.ErrDuplicateDocument, http.StatusConflict},
		{kb.ErrAccessDenied, http.StatusForbidden},
		{kb.ErrInvalidScope, http.StatusBadRequest},
		{kb.ErrNotDirectory, http.StatusBadRequest},
		{kb.ErrSourceMissing, http.StatusConflict},
		{kb.ErrInvalidURL, http.StatusBadRequest},
		{kb.ErrFetchFailed, http.StatusBadGateway},
		{kb.ErrUnavailable, http.StatusServiceUnavailable},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/kb/documents", http.NoBody)
		writeKBError(w, req, tc.err)
		require.Equal(t, tc.status, w.Code, tc.err.Error())
	}
	require.Equal(t, 0, func() int {
		status, _ := kbErrorStatus(errors.New("nope"))
		return status
	}())

	// A private-address rejection carries the offending host so the panel can
	// say which address it refused, instead of "check the URL".
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/kb/documents/url", http.NoBody)
	writeKBError(w, req, &kb.FileError{Reason: kb.ReasonPrivateAddress, Detail: "169.254.169.254", Err: kb.ErrPrivateAddress})
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "169.254.169.254")

	// A typed extraction error renders its REASON (so the panel shows the
	// right sentence), and "too large" reports both the file's size and the
	// configured limit.
	w = httptest.NewRecorder()
	writeKBError(w, req, &kb.FileError{Reason: kb.ReasonLegacyDoc, Detail: ".doc", Err: kb.ErrUnsupportedFile})
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "KBReasonLegacyDoc")

	w = httptest.NewRecorder()
	writeKBError(w, req, &kb.FileError{Reason: kb.ReasonFileTooLarge, Detail: "80", Err: kb.ErrFileTooLarge})
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "KBReasonFileTooLarge")
	require.Contains(t, body, "50", "the limit must be part of the message, or the user cannot act on it")

	// A cancellation has nobody left to read a body: no content, 499.
	w = httptest.NewRecorder()
	writeKBError(w, req, context.Canceled)
	require.Equal(t, 499, w.Code)
	require.Empty(t, w.Body.String())
}

// TestKBQueryIDRejectsBadIDs: the id query parameter is the only way in for
// several endpoints; a zero or negative id would otherwise address row 0.
func TestKBQueryIDRejectsBadIDs(t *testing.T) {
	for _, raw := range []string{"", "0", "-3", "abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/kb/documents?id="+raw, http.NoBody)
		w := httptest.NewRecorder()
		id, ok := kbQueryID(w, req)
		require.False(t, ok, raw)
		require.Zero(t, id)
		require.Equal(t, http.StatusBadRequest, w.Code, raw)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/kb/documents?id=7", http.NoBody)
	id, ok := kbQueryID(httptest.NewRecorder(), req)
	require.True(t, ok)
	require.Equal(t, int64(7), id)
}

// TestKBDocumentURLEndpoint covers the URL import endpoint's own contract: the
// guard refuses a private target with the address in the body, an invalid URL
// is a 400, and a well-formed body for a known collection is accepted.
func TestKBDocumentURLEndpoint(t *testing.T) {
	setupKBHandlerTest(t)
	col, err := kb.CreateCollection("网页", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	// Missing body fields.
	w := callHandler(ServeKBDocumentURL, newRequest(t, http.MethodPost, "/api/kb/documents/url", map[string]any{"url": "https://example.com"}))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Not an http(s) URL.
	w = callHandler(ServeKBDocumentURL, newRequest(t, http.MethodPost, "/api/kb/documents/url", map[string]any{
		"collection_id": col.ID, "url": "file:///etc/passwd",
	}))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "KBInvalidURL")

	// A private address is refused with the address itself in the message.
	w = callHandler(ServeKBDocumentURL, newRequest(t, http.MethodPost, "/api/kb/documents/url", map[string]any{
		"collection_id": col.ID, "url": "http://169.254.169.254/latest/meta-data/",
	}))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "169.254.169.254")

	// Method guard.
	w = callHandler(ServeKBDocumentURL, newRequest(t, http.MethodGet, "/api/kb/documents/url", http.NoBody))
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestKBMoveEndpoint: both ends of a move must be visible from the current
// project, and an unknown document is a 404 rather than a silent no-op.
func TestKBMoveEndpoint(t *testing.T) {
	env := setupKBHandlerTest(t)
	source, err := kb.CreateCollection("来源", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	target, err := kb.CreateCollection("目标", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	src := filepath.Join(env.ProjectDir, "move.md")
	require.NoError(t, os.WriteFile(src, []byte("制度正文"), 0o644))
	doc, err := kb.AddDocumentFromPath(source.ID, src)
	require.NoError(t, err)

	w := callHandler(ServeKBDocumentMove, newRequest(t, http.MethodPost, "/api/kb/documents/move", map[string]any{
		"id": doc.ID, "collection_id": target.ID,
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	moved, err := kb.GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, moved.CollectionID)

	// Unknown document.
	w = callHandler(ServeKBDocumentMove, newRequest(t, http.MethodPost, "/api/kb/documents/move", map[string]any{
		"id": 9999, "collection_id": target.ID,
	}))
	require.Equal(t, http.StatusNotFound, w.Code)

	// Missing fields.
	w = callHandler(ServeKBDocumentMove, newRequest(t, http.MethodPost, "/api/kb/documents/move", map[string]any{"id": doc.ID}))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Moving the same content into a collection that already holds it is a
	// conflict the panel turns into "already there".
	dup := filepath.Join(env.ProjectDir, "dup.md")
	require.NoError(t, os.WriteFile(dup, []byte("另一份"), 0o644))
	first, err := kb.AddDocumentFromPath(source.ID, dup)
	require.NoError(t, err)
	_, err = kb.AddDocumentFromPath(target.ID, dup)
	require.NoError(t, err)
	w = callHandler(ServeKBDocumentMove, newRequest(t, http.MethodPost, "/api/kb/documents/move", map[string]any{
		"id": first.ID, "collection_id": target.ID,
	}))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

// TestKBDocumentTagsAndDeleteEndpoints: PATCH without a tags field is a client
// bug worth a 400 (a "clear" and a "forgot the field" must not look alike), and
// DELETE reports not-found only after the access check.
func TestKBDocumentTagsAndDeleteEndpoints(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("标记", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	src := filepath.Join(env.ProjectDir, "tags.md")
	require.NoError(t, os.WriteFile(src, []byte("内容"), 0o644))
	doc, err := kb.AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10), map[string]any{"tags": " 制度 , 报销 "})
	w := callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var updated kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	require.Equal(t, "制度 , 报销", updated.Tags)

	req = newRequest(t, http.MethodPatch, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10), map[string]any{})
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	req = newRequest(t, http.MethodDelete, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10), nil)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"deleted":true`)
}

// TestKBUploadEndpointErrors: the upload endpoint must distinguish "no file",
// "no collection" and "malformed multipart", because the panel words them
// differently and only the last one is retriable by the browser.
func TestKBUploadEndpointErrors(t *testing.T) {
	setupKBHandlerTest(t)
	col, err := kb.CreateCollection("上传", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	// No collection id at all.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "a.txt")
	require.NoError(t, err)
	_, _ = part.Write([]byte("x"))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "InvalidRequestBody")

	// Collection given, but no file part.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("collection_id", strconv.FormatInt(col.ID, 10)))
	require.NoError(t, mw.Close())
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "NoFileProvided")

	// Not multipart at all.
	req = newRequest(t, http.MethodPost, "/api/kb/documents/upload", map[string]any{"collection_id": col.ID})
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "FileTooLargeOrInvalid")

	// An unsupported file type is refused with its reason, not a crash.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	part, err = mw.CreateFormFile("file", "photo.png")
	require.NoError(t, err)
	_, _ = part.Write([]byte("\x89PNG"))
	require.NoError(t, mw.Close())
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload?collection_id="+strconv.FormatInt(col.ID, 10), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "KBReasonImage")
}

// TestKBDocumentListAndProgressErrors: the two polling endpoints share the same
// parameter validation, and an unknown collection is a 404 rather than an empty
// list (the panel keeps polling an empty list forever otherwise).
func TestKBDocumentListAndProgressErrors(t *testing.T) {
	setupKBHandlerTest(t)

	for _, path := range []string{"/api/kb/documents", "/api/kb/documents?collection_id=0"} {
		w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, path, http.NoBody))
		require.Equal(t, http.StatusBadRequest, w.Code, path)
	}

	w := callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, "/api/kb/documents?collection_id=999", http.NoBody))
	require.Equal(t, http.StatusNotFound, w.Code)

	w = callHandler(ServeKBDocumentProgress, newRequest(t, http.MethodGet, "/api/kb/documents/progress?collection_id=x", http.NoBody))
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = callHandler(ServeKBDocumentProgress, newRequest(t, http.MethodGet, "/api/kb/documents/progress?collection_id=999", http.NoBody))
	require.Equal(t, http.StatusNotFound, w.Code)

	w = callHandler(ServeKBDocumentUsage, newRequest(t, http.MethodGet, "/api/kb/documents/usage?collection_id=", http.NoBody))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// An empty usage list is `[]`, never `null`: the panel maps over it.
	col, err := kb.CreateCollection("用量", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	w = callHandler(ServeKBDocumentUsage, newRequest(t, http.MethodGet, "/api/kb/documents/usage?collection_id="+strconv.FormatInt(col.ID, 10), nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"usage":[]`)

	// Bad chunk ids are rejected instead of falling back to the head — for an
	// existing document (the id is looked up first, so a missing one is a 404).
	src := filepath.Join(t.TempDir(), "chunks.md")
	require.NoError(t, os.WriteFile(src, []byte("内容"), 0o644))
	doc, err := kb.AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	w = callHandler(ServeKBDocuments, newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10)+"&chunk_id=abc", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestKBCollectionsAccessAndErrors: a collection owned by another project is
// invisible (403) to this one, and the three write verbs answer the same way.
func TestKBCollectionsAccessAndErrors(t *testing.T) {
	env := setupKBHandlerTest(t)
	// A collection that belongs to a *different* project.
	foreign, err := kb.CreateCollection("别人的库", "", kb.ScopeProject, 424242)
	require.NoError(t, err)

	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodPatch, map[string]any{"name": "改名"}},
		{http.MethodDelete, nil},
	} {
		req := newRequest(t, tc.method, "/api/kb/collections?id="+strconv.FormatInt(foreign.ID, 10), tc.body)
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeKBCollections, req)
		require.Equal(t, http.StatusForbidden, w.Code, tc.method)
	}

	// An over-long name is refused rather than truncated: the panel shows the
	// limit, and a truncated name would be a silently different collection.
	long := make([]rune, 101)
	for i := range long {
		long[i] = '库'
	}
	w := callHandler(ServeKBCollections, newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": string(long), "scope": "global",
	}))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// PATCH with an empty name is refused too (the store would accept it).
	col, err := kb.CreateCollection("原名", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, "/api/kb/collections?id="+strconv.FormatInt(col.ID, 10), map[string]any{"name": "  "}))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown id is a 404.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPatch, "/api/kb/collections?id=999", map[string]any{"name": "x"}))
	require.Equal(t, http.StatusNotFound, w.Code)

	// Method guard.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodPut, "/api/kb/collections", http.NoBody))
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestKBReindexEndpoint covers the three shapes of the reindex request: vector
// only, one collection, and "everything visible here". The last one must report
// honestly when there is nothing to do instead of accepting a request that
// touches nothing.
func TestKBReindexEndpoint(t *testing.T) {
	env := setupKBHandlerTest(t)

	w := callHandler(ServeKBReindex, newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{"kind": "vector"}))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	// No collections visible from this project: queued:false with a reason.
	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{}), env.ProjectDir)
	w = callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusOK, w.Code)
	var res struct {
		Queued bool   `json:"queued"`
		Detail string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.False(t, res.Queued)
	require.Equal(t, "no_collections", res.Detail)

	// An empty body is a valid "reindex everything".
	req = withProjectCookie(httptest.NewRequest(http.MethodPost, "/api/kb/reindex", http.NoBody), env.ProjectDir)
	w = callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusOK, w.Code)

	// With a collection the request is accepted.
	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	w = callHandler(ServeKBReindex, newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{"collection_id": col.ID}))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	// A collection from another project is forbidden.
	foreign, err := kb.CreateCollection("别人的", "", kb.ScopeProject, 777)
	require.NoError(t, err)
	req = withProjectCookie(newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{"collection_id": foreign.ID}), env.ProjectDir)
	w = callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// TestKBStatusWhenDisabled: with the feature off the endpoint still answers 200
// (the panel needs to know it is off) with zeroed stats, instead of a 503 that
// the settings page would render as a broken section.
func TestKBStatusWhenDisabled(t *testing.T) {
	setupKBHandlerTest(t)
	kb.Shutdown()

	w := callHandler(ServeKBStatus, newRequest(t, http.MethodGet, "/api/kb/status", http.NoBody))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var status struct {
		Available bool          `json:"available"`
		Stats     kb.IndexStats `json:"stats"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.False(t, status.Available)
	require.Zero(t, status.Stats)

	// The collection list reports the typed 503 instead, because there is
	// nothing meaningful to list.
	w = callHandler(ServeKBCollections, newRequest(t, http.MethodGet, "/api/kb/collections", http.NoBody))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestKBDocumentImportEndpointErrors: a cancelled request (client hung up) gets
// no response at all, because there is nobody to read it; other failures keep
// their classified status.
func TestKBDocumentImportEndpointErrors(t *testing.T) {
	env := setupKBHandlerTest(t)
	col, err := kb.CreateCollection("导入", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	w := callHandler(ServeKBDocumentImport, newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID,
	}))
	require.Equal(t, http.StatusBadRequest, w.Code)

	// A cancelled context: the import stops and the handler returns without
	// writing a body, which the recorder shows as the default 200 with empty
	// body. What matters is that it does not try to write an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": env.ProjectDir,
	}).WithContext(ctx)
	w = callHandler(ServeKBDocumentImport, req)
	require.Empty(t, w.Body.String())

	// A path outside every root is refused by the shared resolver.
	w = callHandler(ServeKBDocumentImport, newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": "/etc",
	}))
	require.Equal(t, http.StatusForbidden, w.Code)
}
