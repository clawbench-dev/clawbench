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
