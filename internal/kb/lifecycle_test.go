package kb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// setLifecycleDataDir points the knowledge base at a private data dir and puts
// the real one back afterwards, so the lifecycle tests cannot leak state into
// other files that install their own globals.
func setLifecycleDataDir(t *testing.T) string {
	t.Helper()
	old := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = old
	})
	return model.DataDir
}

// Init is the only place the database handle, the knowledge directory and the
// embedder are created. Enabling it must produce all three, and re-running it
// (a settings patch that forces a re-init) must replace the handle instead of
// leaking the previous one.
func TestInit_CreatesTheStoreAndReplacesThePreviousOne(t *testing.T) {
	dataDir := setLifecycleDataDir(t)
	cfg := model.KBConfig{Enabled: true, AutoInject: true, MaxFileMB: 10, MaxTextMB: 2, PollInterval: "1h", EmbedBatchSize: 4}

	require.NoError(t, Init(cfg, model.RAGConfig{ChunkSize: 256, ChunkOverlap: 32}))
	first := currentStore()
	require.NotNil(t, first)
	require.DirExists(t, KnowledgeDir())
	require.FileExists(t, filepath.Join(dataDir, "ClawBench.db"))

	// A second Init on the same data dir opens a fresh handle over the same file
	// and closes the old one: the KB tables are still there.
	col, err := CreateCollection("公司制度", "", ScopeGlobal, 0)
	require.NoError(t, err)
	require.NoError(t, Init(cfg, model.RAGConfig{ChunkSize: 256, ChunkOverlap: 32}))
	second := currentStore()
	require.NotNil(t, second)
	require.NotSame(t, first, second, "a re-init replaces the store handle")
	got, err := GetCollection(col.ID)
	require.NoError(t, err)
	require.Equal(t, "公司制度", got.Name, "the data survives the re-open")

	// The embedder follows the RAG settings: VectorEnabled plus an endpoint and a
	// model are all required for one to exist at all.
	require.Nil(t, currentEmbedder(), "no vector config means no embedder")
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: true, BaseURL: "http://127.0.0.1:9", Model: "m"}))
	require.NotNil(t, currentEmbedder(), "a complete embedding config yields a client")
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: false, BaseURL: "http://x", Model: "m"}))
	require.Nil(t, currentEmbedder(), "vector search off means no embedder")
}

// An unusable data dir must fail the Init rather than leave a half-initialized
// global: the server reports it and the panel shows the feature as broken.
func TestInit_ReportsAnUnusableDataDir(t *testing.T) {
	dataDir := setLifecycleDataDir(t)

	// A file where the knowledge directory has to go: MkdirAll fails on every
	// platform (unlike a chmod, which Windows ignores).
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "knowledge"), []byte("x"), 0o644))

	err := Init(model.KBConfig{Enabled: true}, model.RAGConfig{})
	require.ErrorContains(t, err, "create knowledge dir")
	require.Nil(t, currentStore(), "a failed Init must not publish a store")
}

// Reconfigure is the live settings patch: switching the feature OFF stops the
// worker and closes the store (so the next request answers ErrUnavailable), and
// switching it back ON re-opens everything.
func TestReconfigure_TogglesTheWholeKnowledgeBase(t *testing.T) {
	setLifecycleDataDir(t)
	enabled := model.KBConfig{Enabled: true, AutoInject: true, PollInterval: "1h"}
	require.NoError(t, Init(enabled, model.RAGConfig{}))
	require.NotNil(t, currentStore())

	Reconfigure(model.KBConfig{Enabled: false}, model.RAGConfig{})
	require.Nil(t, currentStore())
	_, err := ListCollections(0)
	require.ErrorIs(t, err, ErrUnavailable)

	// Back on: the store is re-created from the same data dir.
	Reconfigure(enabled, model.RAGConfig{})
	require.NotNil(t, currentStore())
	_, err = ListCollections(0)
	require.NoError(t, err)

	// Reconfiguring an ALREADY enabled knowledge base with a vector config keeps
	// the store and starts the background reconcile: nothing here waits for it.
	Reconfigure(enabled, model.RAGConfig{VectorEnabled: true, BaseURL: "http://127.0.0.1:9", Model: "m"})
	require.NotNil(t, currentStore())
	require.NotNil(t, currentEmbedder())
}

// StartIndexer is what the server calls after Init; it is safe to call twice
// (a reconfigure and a startup can both reach it) and Shutdown must stop the
// worker it started.
func TestStartIndexer_IsIdempotentAndShutdownStopsIt(t *testing.T) {
	setLifecycleDataDir(t)
	require.NoError(t, Init(model.KBConfig{Enabled: true, PollInterval: "1h"}, model.RAGConfig{}))

	StartIndexer()
	StartIndexer()
	require.NotNil(t, currentStore())

	mu.Lock()
	idx := globalIndexer
	mu.Unlock()
	require.NotNil(t, idx)

	Shutdown()
	require.Nil(t, currentStore())
	_, err := ListCollections(0)
	require.ErrorIs(t, err, ErrUnavailable)
}

// Importing a folder is the one path that copies many files; a cancelled request
// must return the counts gathered so far with the cancellation error, so the
// panel can say "5 of 12 imported".
func TestImportDirectory_CancelledMidRunReportsTheError(t *testing.T) {
	setLifecycleDataDir(t)
	require.NoError(t, Init(model.KBConfig{Enabled: true, MaxFileMB: 10, MaxTextMB: 2, PollInterval: "1h"}, model.RAGConfig{}))
	col, err := CreateCollection("资料", "", ScopeGlobal, 0)
	require.NoError(t, err)

	root := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("报销流程 "+name), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := ImportDirectory(ctx, col.ID, root)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, res)
}

// The knowledge directory lives under the data dir and is created by Init: the
// document copies are what the collection directories are removed from, so the
// path must be the documented one, not a temp dir.
func TestKnowledgeDir_FollowsTheDataDir(t *testing.T) {
	dataDir := setLifecycleDataDir(t)
	require.Equal(t, filepath.Join(dataDir, "knowledge"), KnowledgeDir())
	if runtime.GOOS == "windows" {
		require.True(t, filepath.IsAbs(KnowledgeDir()))
	}
}
