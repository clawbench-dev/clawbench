package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// catalogTestEnv isolates the catalog globals (URL, data dir, snapshot) for one
// test and restores them afterwards.
func catalogTestEnv(t *testing.T) {
	t.Helper()
	origURL, origDataDir := catalogURL, DataDir
	origSnapshot := CatalogSnapshot()
	t.Cleanup(func() {
		catalogURL, DataDir = origURL, origDataDir
		catalogPtr.Store(origSnapshot)
	})
	DataDir = t.TempDir()
	catalogPtr.Store(nil)
}

func TestReplaceCatalog_SwapAndClear(t *testing.T) {
	orig := CatalogSnapshot()
	t.Cleanup(func() { catalogPtr.Store(orig) })

	cat := &Catalog{Providers: map[string]CatalogProvider{"p": {ID: "p"}}}
	ReplaceCatalog(cat)
	if CatalogSnapshot() != cat {
		t.Fatal("ReplaceCatalog must install the snapshot")
	}
	ReplaceCatalog(nil)
	if CatalogSnapshot() != nil {
		t.Fatal("ReplaceCatalog(nil) must clear the snapshot")
	}
	if !CatalogStale() {
		t.Fatal("no snapshot must read as stale")
	}
}

func TestLoadCatalogFromDisk_UnreadableFileIsAnError(t *testing.T) {
	catalogTestEnv(t)

	// A directory where the snapshot should be: os.ReadFile fails with EISDIR
	// for every uid (chmod 000 does not stop root, which CI may run as).
	if err := os.MkdirAll(CatalogPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := LoadCatalogFromDisk(); err == nil {
		t.Fatal("an unreadable snapshot must be reported, not treated as absent")
	}
}

func TestCatalogModelsForPreset_NoSnapshotOrEmptyID(t *testing.T) {
	catalogTestEnv(t)

	if got := CatalogModelsForPreset("anthropic"); got != nil {
		t.Fatalf("no snapshot must yield nil, got %+v", got)
	}

	cat, err := parseCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatal(err)
	}
	ReplaceCatalog(cat)
	if got := CatalogModelsForPreset(""); got != nil {
		t.Fatalf("an empty preset id must yield nil, got %+v", got)
	}
	// A known provider with no models (impossible from parseCatalog, but the
	// accessor is also used with hand-built snapshots) must not panic.
	ReplaceCatalog(&Catalog{Providers: map[string]CatalogProvider{"empty": {ID: "empty"}}})
	if got := CatalogModelsForPreset("empty"); got != nil {
		t.Fatalf("a provider without models must yield nil, got %+v", got)
	}
}

// The provider-models endpoint falls back to this when the live probe finds no
// catalog, so it has to return the entry itself (base URL and all).
func TestCatalogProviderForPreset(t *testing.T) {
	catalogTestEnv(t)

	if CatalogProviderForPreset("anthropic") != nil {
		t.Fatal("no snapshot must yield nil")
	}

	cat, err := parseCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatal(err)
	}
	ReplaceCatalog(cat)

	if CatalogProviderForPreset("") != nil {
		t.Fatal("an empty preset id must yield nil")
	}
	if CatalogProviderForPreset("unknown") != nil {
		t.Fatal("an unknown preset id must yield nil")
	}
	got := CatalogProviderForPreset("deepseek")
	if got == nil {
		t.Fatal("a known preset must resolve")
	}
	if got.ID != "deepseek" || got.Protocol != ProviderProtocolOpenAI ||
		got.BaseURL != "https://api.deepseek.com/v1" || len(got.Models) != 1 {
		t.Fatalf("provider = %+v", got)
	}
}

// A burst of callers (startup plus a manual click) must share one download:
// two concurrent refreshes would double the ~6 MB transfer and race the file.
func TestRefreshCatalog_SharesInFlightDownload(t *testing.T) {
	catalogTestEnv(t)

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = w.Write([]byte(catalogFixture))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	type result struct {
		cat *Catalog
		err error
	}
	firstCh := make(chan result, 1)
	go func() {
		cat, err := RefreshCatalog(context.Background())
		firstCh <- result{cat, err}
	}()
	<-started

	// A caller whose context dies while the download is in flight gets its own
	// context error, not a hung call.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RefreshCatalog(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	// A patient caller waits for the same flight and gets the same snapshot.
	secondCh := make(chan result, 1)
	go func() {
		cat, err := RefreshCatalog(context.Background())
		secondCh <- result{cat, err}
	}()
	close(release)

	first := <-firstCh
	second := <-secondCh
	if first.err != nil || second.err != nil {
		t.Fatalf("errors: %v / %v", first.err, second.err)
	}
	if first.cat != second.cat {
		t.Fatal("concurrent callers must share one download")
	}
	if _, err := os.Stat(CatalogPath()); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
}

// Persisting the snapshot is best-effort: an unwritable data dir must not make
// a successful refresh look like a failure (the in-memory snapshot is what the
// process uses).
func TestRefreshCatalog_WriteFailureStillReturnsSnapshot(t *testing.T) {
	catalogTestEnv(t)

	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// CatalogPath() now resolves under a regular file, so MkdirAll fails.
	DataDir = file

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(catalogFixture))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	cat, err := RefreshCatalog(context.Background())
	if err != nil {
		t.Fatalf("refresh must succeed even when the snapshot cannot be written: %v", err)
	}
	if cat == nil || CatalogSnapshot() != cat {
		t.Fatal("the snapshot must still be installed in memory")
	}
}

func TestFetchCatalog_RequestAndTransportErrors(t *testing.T) {
	catalogTestEnv(t)

	// A URL the request builder rejects (space in the host).
	catalogURL = "http://exa mple.com/api.json"
	if _, err := fetchCatalog(context.Background()); err == nil {
		t.Fatal("expected a request-construction error")
	}

	// A URL that parses but cannot be dialed.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()
	catalogURL = dead
	if _, err := fetchCatalog(context.Background()); err == nil {
		t.Fatal("expected a transport error")
	} else if !strings.Contains(err.Error(), "fetch model catalog") {
		t.Fatalf("error %q must name the operation", err)
	}
}

// A truncated body is a read failure, not an empty catalog: silently treating
// it as "no providers" would wipe the snapshot on a flaky connection.
func TestFetchCatalog_TruncatedBodyIsAnError(t *testing.T) {
	catalogTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte(`{"anthropic":`))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	if _, err := fetchCatalog(context.Background()); err == nil {
		t.Fatal("expected a read error for a truncated body")
	} else if !strings.Contains(err.Error(), "read model catalog") {
		t.Fatalf("error %q must name the read step", err)
	}
}

func TestParseCatalog_RejectsUnusableDocuments(t *testing.T) {
	if _, err := parseCatalog([]byte("{not json")); err == nil {
		t.Fatal("expected a parse error")
	}
	// Every provider skipped (unknown npm package) is a failed refresh, not an
	// empty catalog that would replace a good snapshot.
	if _, err := parseCatalog([]byte(`{"x":{"npm":"@ai-sdk/azure","models":{"m":{}}}}`)); err == nil {
		t.Fatal("expected an error when no provider is usable")
	}
}

// models.dev omits `api` for the official providers (their SDKs carry the
// endpoint) and leaves the model id/name blank when they match the key.
func TestParseCatalog_FillsDefaultsAndSkipsUnknownHosts(t *testing.T) {
	body := `{
	  "anthropic": {"name": "Anthropic", "npm": "@ai-sdk/anthropic",
	    "models": {"claude-x": {"name": ""}}},
	  "openai": {"name": "OpenAI", "npm": "@ai-sdk/openai",
	    "models": {"gpt-x": {"id": "gpt-x"}}},
	  "google": {"name": "Google", "npm": "@ai-sdk/google",
	    "models": {"gemini-x": {}}},
	  "vertex": {"name": "Vertex", "npm": "@ai-sdk/google-vertex",
	    "models": {"gemini-y": {}}},
	  "mystery": {"name": "Mystery", "npm": "@ai-sdk/openai-compatible",
	    "models": {"m": {}}}
	}`
	cat, err := parseCatalog([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cat.Providers["anthropic"].BaseURL; got != "https://api.anthropic.com" {
		t.Fatalf("anthropic base = %q", got)
	}
	if got := cat.Providers["openai"].BaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("openai base = %q", got)
	}
	if got := cat.Providers["google"].BaseURL; got != "https://generativelanguage.googleapis.com" {
		t.Fatalf("google base = %q", got)
	}
	if got := cat.Providers["google"].Protocol; got != ProviderProtocolGemini {
		t.Fatalf("google protocol = %q", got)
	}
	if got := cat.Providers["vertex"].Protocol; got != ProviderProtocolGemini {
		t.Fatalf("vertex protocol = %q", got)
	}
	// An unknown provider with no api field keeps an empty base URL: guessing a
	// host would point it at someone else's API.
	if got := cat.Providers["mystery"].BaseURL; got != "" {
		t.Fatalf("mystery base = %q, want empty", got)
	}

	// A model with no id/name falls back to its key in both slots.
	m := cat.Providers["anthropic"].Models[0]
	if m.ID != "claude-x" || m.Name != "claude-x" {
		t.Fatalf("model = %+v, want the key in both slots", m)
	}
}

func TestWriteCatalog_ReportsFailures(t *testing.T) {
	catalogTestEnv(t)
	cat := &Catalog{Providers: map[string]CatalogProvider{}}

	// The parent path is a regular file: MkdirAll cannot create the directory.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	DataDir = file
	if err := writeCatalog(cat); err == nil {
		t.Fatal("expected a directory-creation error")
	}

	// The temp path is occupied by a directory: the write itself fails.
	DataDir = t.TempDir()
	if err := os.MkdirAll(CatalogPath()+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeCatalog(cat); err == nil {
		t.Fatal("expected a write error")
	}
}

// A fresh snapshot means no download at all: the startup call must not add a
// request every boot.
func TestRefreshCatalogIfStale_FreshSnapshotDoesNothing(t *testing.T) {
	catalogTestEnv(t)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(catalogFixture))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	cat, err := parseCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatal(err)
	}
	ReplaceCatalog(cat)

	RefreshCatalogIfStale()
	if hits != 0 {
		t.Fatalf("a fresh snapshot must not trigger a download, got %d requests", hits)
	}
}

// A missing snapshot triggers a background refresh that installs itself; the
// call must return immediately so a slow download never delays boot.
func TestRefreshCatalogIfStale_MissingSnapshotRefreshes(t *testing.T) {
	catalogTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(catalogFixture))
	}))
	defer srv.Close()
	catalogURL = srv.URL

	RefreshCatalogIfStale()
	if CatalogSnapshot() != nil {
		t.Fatal("the refresh must run in the background, not synchronously")
	}
	deadline := time.Now().Add(10 * time.Second)
	for CatalogSnapshot() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the background refresh never installed a snapshot")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(CatalogSnapshot().Providers) != 2 {
		t.Fatalf("providers = %d", len(CatalogSnapshot().Providers))
	}
}

// A first run has no snapshot at all: that is "unavailable until a refresh
// succeeds", not an error the caller has to handle.
func TestLoadCatalogFromDisk_MissingFileIsNotAnError(t *testing.T) {
	catalogTestEnv(t)

	if err := LoadCatalogFromDisk(); err != nil {
		t.Fatalf("a missing snapshot must not be an error: %v", err)
	}
	if CatalogSnapshot() != nil {
		t.Fatal("a missing snapshot must not install one")
	}
}

// A snapshot written before prices were parsed has none at all; without this
// the picker would stay price-less for a full TTL after the upgrade.
func TestCatalogStale_PrePriceSnapshotIsRefetched(t *testing.T) {
	fresh := &Catalog{
		FetchedAt: time.Now().UTC(),
		Providers: map[string]CatalogProvider{
			"zai": {ID: "zai", Models: []CatalogModel{{ID: "glm-5.2", InputPrice: 1.4}}},
		},
		PriceSchema: CatalogPriceSchema,
	}
	ReplaceCatalog(fresh)
	t.Cleanup(func() { ReplaceCatalog(nil) })
	if CatalogStale() {
		t.Fatal("a fresh, price-bearing snapshot must not be stale")
	}

	// Same timestamp, but produced by the old parser: no schema marker.
	legacy := *fresh
	legacy.PriceSchema = 0
	ReplaceCatalog(&legacy)
	if !CatalogStale() {
		t.Fatal("a snapshot predating price parsing must be refetched once")
	}
}
