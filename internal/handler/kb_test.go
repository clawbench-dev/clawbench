package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"clawbench/internal/kb"
	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// setupKBHandlerTest installs a temporary knowledge base and restores it.
func setupKBHandlerTest(t *testing.T) *testEnv {
	t.Helper()
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	oldDir := model.DataDir
	oldCfg := model.ConfigInstance.KB
	model.DataDir = t.TempDir()
	model.ConfigInstance.KB = model.KBConfig{
		Enabled:        true,
		AutoInject:     false,
		InjectTopN:     4,
		InjectMaxChars: 2000,
		MaxFileMB:      50,
		MaxTextMB:      20,
		PollInterval:   "20ms",
		EmbedBatchSize: 4,
	}
	t.Cleanup(func() {
		kb.Shutdown()
		model.DataDir = oldDir
		model.ConfigInstance.KB = oldCfg
	})

	require.NoError(t, kb.Init(model.ConfigInstance.KB, model.RAGConfig{
		VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64,
	}))
	kb.StartIndexer()
	return env
}

func waitKBDocumentReady(t *testing.T, docID int64) kb.Document {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(docID, 10), nil)
		w := callHandler(ServeKBDocuments, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body struct {
			Document kb.Document `json:"document"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		if body.Document.Status == kb.StatusFailed {
			t.Fatalf("indexing failed: %s", body.Document.Error)
		}
		if body.Document.Status == kb.StatusReady {
			return body.Document
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("document never became ready")
	return kb.Document{}
}

func TestKBHandlersEndToEnd(t *testing.T) {
	env := setupKBHandlerTest(t)

	// Create a global collection.
	req := newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "公司制度", "description": "测试", "scope": "global",
	})
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var col kb.Collection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &col))
	require.NotZero(t, col.ID)
	require.True(t, col.AutoInject)

	// List returns it.
	req = newRequest(t, http.MethodGet, "/api/kb/collections", nil)
	w = callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Collections []kb.Collection `json:"collections"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Collections, 1)

	// Add a file by path.
	filePath := filepath.Join(env.ProjectDir, "handbook.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# 手册\n\n报销流程：先提交申请，再等财务审批。"), 0o644))
	req = newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": filePath,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var doc kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	require.Equal(t, "handbook.md", doc.Title)

	ready := waitKBDocumentReady(t, doc.ID)
	require.True(t, ready.FTSReady)
	require.Greater(t, ready.ChunkCount, 0)

	// Search.
	req = newRequest(t, http.MethodPost, "/api/kb/search", map[string]any{"q": "报销流程"})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBSearch, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res kb.SearchResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.NotEmpty(t, res.Results)
	require.Equal(t, "handbook.md", res.Results[0].DocumentTitle)

	// Document detail without a chunk id returns the head of the document.
	req = newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detail struct {
		Document     kb.Document `json:"document"`
		Chunks       []kb.Chunk  `json:"chunks"`
		FocusChunkID int64       `json:"focus_chunk_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Chunks)
	require.Zero(t, detail.FocusChunkID)

	// With a chunk id it returns that passage and marks it as the focus.
	hitChunk := res.Results[0].ChunkID
	req = newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10)+"&chunk_id="+strconv.FormatInt(hitChunk, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Equal(t, hitChunk, detail.FocusChunkID)
	require.Len(t, detail.Chunks, 1)
	require.Equal(t, hitChunk, detail.Chunks[0].ID)

	// A chunk id that no longer exists falls back to the head, not an error.
	req = newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(doc.ID, 10)+"&chunk_id=999999", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Zero(t, detail.FocusChunkID)
	require.NotEmpty(t, detail.Chunks)

	// Status.
	req = newRequest(t, http.MethodGet, "/api/kb/status", nil)
	w = callHandler(ServeKBStatus, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var status struct {
		Available bool          `json:"available"`
		Stats     kb.IndexStats `json:"stats"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.True(t, status.Available)
	require.Equal(t, 1, status.Stats.Documents)

	// Upload a second, different file.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("collection_id", strconv.FormatInt(col.ID, 10)))
	part, err := mw.CreateFormFile("file", "price.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("产品报价：标准版 199 元。"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var uploaded kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &uploaded))
	require.Equal(t, "price.txt", uploaded.Title)
	waitKBDocumentReady(t, uploaded.ID)

	// Duplicate upload is refused.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("collection_id", strconv.FormatInt(col.ID, 10)))
	part, err = mw.CreateFormFile("file", "price.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("产品报价：标准版 199 元。"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req = httptest.NewRequest(http.MethodPost, "/api/kb/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = callHandler(ServeKBDocumentUpload, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	// Reindex one document.
	req = newRequest(t, http.MethodPost, "/api/kb/documents/reindex?id="+strconv.FormatInt(doc.ID, 10), nil)
	w = callHandler(ServeKBDocumentReindex, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	waitKBDocumentReady(t, doc.ID)

	// Reindex the collection.
	req = newRequest(t, http.MethodPost, "/api/kb/reindex", map[string]any{"collection_id": col.ID})
	w = callHandler(ServeKBReindex, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	waitKBDocumentReady(t, doc.ID)

	// Delete the uploaded document, then the collection.
	req = newRequest(t, http.MethodDelete, "/api/kb/documents?id="+strconv.FormatInt(uploaded.ID, 10), nil)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = newRequest(t, http.MethodDelete, "/api/kb/collections?id="+strconv.FormatInt(col.ID, 10), nil)
	w = callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestKBProjectScopeRequiresProject(t *testing.T) {
	env := setupKBHandlerTest(t)

	req := newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "项目库", "scope": "project",
	})
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// With a project cookie and a registered project, the same request works.
	_, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	req = newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "项目库", "scope": "project",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var col kb.Collection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &col))
	require.Equal(t, kb.ScopeProject, col.Scope)
}

func TestKBHandlersRejectBadRequests(t *testing.T) {
	env := setupKBHandlerTest(t)

	// Missing name.
	req := newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{"scope": "global"})
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// Invalid scope.
	req = newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{"name": "x", "scope": "team"})
	w = callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// Unknown document id.
	req = newRequest(t, http.MethodGet, "/api/kb/documents?id=999", nil)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// Unsupported file type.
	col, err := kb.CreateCollection("制度", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)
	bad := filepath.Join(env.ProjectDir, "old.doc")
	require.NoError(t, os.WriteFile(bad, []byte("x"), 0o644))
	req = newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": bad,
	})
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestKBDocumentImportEndpoint(t *testing.T) {
	env := setupKBHandlerTest(t)

	col, err := kb.CreateCollection("资料", "", kb.ScopeGlobal, 0)
	require.NoError(t, err)

	dir := filepath.Join(env.ProjectDir, "kb-import")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 手册\n\n报销流程。"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("差旅标准。"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pic.png"), []byte("png"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": dir,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeKBDocumentImport, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var res kb.DirImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Equal(t, 2, res.Added)
	require.Equal(t, 1, res.Skipped)
	require.Equal(t, 0, res.Failed)
	require.False(t, res.Truncated)

	// A file is not a folder.
	req = newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": filepath.Join(dir, "a.md"),
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentImport, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestKBRefreshProgressAndImportPreview(t *testing.T) {
	env := setupKBHandlerTest(t)

	req := newRequest(t, http.MethodPost, "/api/kb/collections", map[string]any{
		"name": "刷新测试", "scope": "global",
	})
	w := callHandler(ServeKBCollections, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var col kb.Collection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &col))

	src := filepath.Join(env.ProjectDir, "refresh.md")
	require.NoError(t, os.WriteFile(src, []byte("第一版：报销流程。"), 0o644))
	req = newRequest(t, http.MethodPost, "/api/kb/documents", map[string]any{
		"collection_id": col.ID, "path": src,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var doc kb.Document
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	waitKBDocumentReady(t, doc.ID)

	// Progress returns the volatile fields only.
	req = newRequest(t, http.MethodGet, "/api/kb/documents/progress?collection_id="+strconv.FormatInt(col.ID, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentProgress, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var progress struct {
		Documents []kb.DocumentProgress `json:"documents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &progress))
	require.Len(t, progress.Documents, 1)
	require.Equal(t, doc.ID, progress.Documents[0].ID)
	require.Equal(t, kb.StatusReady, progress.Documents[0].Status)

	// Unchanged source: nothing to reindex.
	req = newRequest(t, http.MethodPost, "/api/kb/documents/refresh?id="+strconv.FormatInt(doc.ID, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentRefresh, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var refreshed struct {
		Changed bool `json:"changed"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refreshed))
	require.False(t, refreshed.Changed)

	// Changed source: the stored copy is replaced and reindexed.
	require.NoError(t, os.WriteFile(src, []byte("第二版：报销流程已更新。"), 0o644))
	req = newRequest(t, http.MethodPost, "/api/kb/documents/refresh?id="+strconv.FormatInt(doc.ID, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentRefresh, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refreshed))
	require.True(t, refreshed.Changed)
	waitKBDocumentReady(t, doc.ID)

	// A folder preview reports counts without copying anything.
	dir := filepath.Join(env.ProjectDir, "preview-dir")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("甲"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "b.md"), []byte("乙"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.png"), []byte("png"), 0o644))

	req = newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": dir, "dry_run": true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentImport, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview kb.DirImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, 2, preview.Added)
	require.Equal(t, 1, preview.Skipped)

	// Preview wrote nothing.
	req = newRequest(t, http.MethodGet, "/api/kb/documents?collection_id="+strconv.FormatInt(col.ID, 10), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocuments, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Documents []kb.Document `json:"documents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Documents, 1)

	// The real import adds both files.
	req = newRequest(t, http.MethodPost, "/api/kb/documents/import", map[string]any{
		"collection_id": col.ID, "path": dir,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeKBDocumentImport, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var imported kb.DirImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &imported))
	require.Equal(t, 2, imported.Added)
}
