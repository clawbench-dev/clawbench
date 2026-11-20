package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"clawbench/internal/kb"
	"clawbench/internal/model"
	plugkb "clawbench/internal/plugin/kb"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// setupKBHandlerTest installs a temporary knowledge base and restores it.
//
// 这个函数原先定义在 kb_test.go 里，随知识库搬进 internal/plugin/kb 一起
// 移走了。但 handler 里还有不依赖 kb 接口、只需要 kb 状态的用例
// （/cb-kbsearch 的命令预检），所以在这里保留一份最小实现。
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

// mustProjectID resolves a project path to its numeric id (test helper that
// moved to the live package with the meeting tests).
func mustProjectID(t *testing.T, project string) int64 {
	t.Helper()
	id, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	return id
}

// waitKBDocumentReady polls until the document finishes indexing.
func waitKBDocumentReady(t *testing.T, docID int64) kb.Document {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := newRequest(t, http.MethodGet, "/api/kb/documents?id="+strconv.FormatInt(docID, 10), nil)
		w := callHandler(plugkb.ServeKBDocuments, req)
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
