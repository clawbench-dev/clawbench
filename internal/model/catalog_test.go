package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const catalogFixture = `{
  "anthropic": {
    "id": "anthropic", "name": "Anthropic", "npm": "@ai-sdk/anthropic",
    "api": "https://api.anthropic.com/v1",
    "models": {
      "claude-a": {"id": "claude-a", "name": "Claude A", "limit": {"context": 200000, "output": 64000}},
      "claude-b": {"id": "claude-b", "name": "Claude B"}
    }
  },
  "deepseek": {
    "id": "deepseek", "name": "DeepSeek", "npm": "@ai-sdk/openai-compatible",
    "api": "https://api.deepseek.com/v1",
    "models": {"deepseek-chat": {"id": "deepseek-chat", "name": "DeepSeek Chat"}}
  },
  "unusable": {
    "id": "unusable", "name": "Unusable", "npm": "@ai-sdk/azure",
    "api": "https://x.example", "models": {"m": {"id": "m", "name": "m"}}
  }
}`

func TestParseCatalog_ReducesAndFilters(t *testing.T) {
	cat, err := parseCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cat.Providers) != 2 {
		t.Fatalf("providers = %d, want 2 (azure entry must be skipped)", len(cat.Providers))
	}
	a := cat.Providers["anthropic"]
	if a.Protocol != ProviderProtocolAnthropic {
		t.Fatalf("protocol = %q", a.Protocol)
	}
	// Anthropic base URLs must not end in /v1: the CLI appends the path itself.
	if a.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("base = %q", a.BaseURL)
	}
	if len(a.Models) != 2 || a.Models[0].Name != "Claude A" {
		t.Fatalf("models = %+v", a.Models)
	}
	if a.Models[0].Context != 200000 {
		t.Fatalf("context = %d", a.Models[0].Context)
	}
}

func TestRefreshCatalog_StoresSnapshotAndModels(t *testing.T) {
	origURL, origDataDir := catalogURL, DataDir
	t.Cleanup(func() { catalogURL, DataDir = origURL, origDataDir; catalogPtr.Store(nil) })
	DataDir = t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(catalogFixture))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	cat, err := RefreshCatalog(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !cat.FetchedAt.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("fetched_at = %v", cat.FetchedAt)
	}
	if _, err := os.Stat(CatalogPath()); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	models := CatalogModelsForPreset("deepseek")
	if len(models) != 1 || models[0].ID != "deepseek-chat" || !models[0].Default {
		t.Fatalf("models = %+v", models)
	}
	if CatalogModelsForPreset("unknown") != nil {
		t.Fatal("unknown preset must yield nil")
	}
	if CatalogStale() {
		t.Fatal("a fresh snapshot must not be stale")
	}

	// Reload from disk in a new process-equivalent state.
	catalogPtr.Store(nil)
	if err := LoadCatalogFromDisk(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := CatalogModelsForPreset("anthropic"); len(got) != 2 {
		t.Fatalf("reloaded models = %+v", got)
	}
}

func TestRefreshCatalog_HTTPErrorKeepsPreviousSnapshot(t *testing.T) {
	origURL, origDataDir := catalogURL, DataDir
	t.Cleanup(func() { catalogURL, DataDir = origURL, origDataDir; catalogPtr.Store(nil) })
	DataDir = t.TempDir()

	cat, _ := parseCatalog([]byte(catalogFixture))
	catalogPtr.Store(cat)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	catalogURL = srv.URL

	if _, err := RefreshCatalog(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if CatalogSnapshot() == nil || len(CatalogSnapshot().Providers) != 2 {
		t.Fatal("a failed refresh must keep the previous snapshot")
	}
}

func TestCatalogStale_MissingSnapshot(t *testing.T) {
	orig := CatalogSnapshot()
	t.Cleanup(func() { catalogPtr.Store(orig) })
	catalogPtr.Store(nil)
	if !CatalogStale() {
		t.Fatal("no snapshot must read as stale")
	}
}

func TestLoadCatalogFromDisk_CorruptFileIsNotFatal(t *testing.T) {
	origDataDir := DataDir
	DataDir = t.TempDir()
	t.Cleanup(func() { DataDir = origDataDir })

	path := CatalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadCatalogFromDisk(); err == nil {
		t.Fatal("expected a parse error")
	}
}
