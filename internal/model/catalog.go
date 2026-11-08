package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CatalogURL is the authoritative third-party model catalog (models.dev —
// the same source OpenCode embeds). It is a single JSON document keyed by
// provider id, each entry carrying the provider's model list with context
// limits and pricing.
const CatalogURL = "https://models.dev/api.json"

// catalogURL is CatalogURL, overridable in tests.
var catalogURL = CatalogURL

// CatalogTTL is how long a snapshot is considered fresh. The catalog changes
// on the order of days, so a week keeps us current without re-downloading ~6 MB
// on every boot.
const CatalogTTL = 7 * 24 * time.Hour

// CatalogModel is one model in the snapshot. Limits are best-effort: the
// catalog omits them for some entries.
type CatalogModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Context int    `json:"context,omitempty"`
	Output  int    `json:"output,omitempty"`
	// InputPrice / OutputPrice are USD per million tokens, as published by
	// models.dev. Zero means the catalog has no price for this entry (or the
	// provider bills by subscription, which the upstream marks as 0/0).
	InputPrice  float64 `json:"inputPrice,omitempty"`
	OutputPrice float64 `json:"outputPrice,omitempty"`
}

// CatalogProvider is one provider in the snapshot, reduced to the fields this
// app can use (the same protocol filter as the embedded presets).
type CatalogProvider struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Protocol string         `json:"protocol"`
	BaseURL  string         `json:"baseUrl"`
	Models   []CatalogModel `json:"models"`
}

// Catalog is the on-disk/in-memory snapshot.
type Catalog struct {
	FetchedAt time.Time                  `json:"fetched_at"`
	Source    string                     `json:"source"`
	Providers map[string]CatalogProvider `json:"providers"`
	// PriceSchema records which extraction schema produced this snapshot.
	// Older files (0) predate price parsing and are refetched once — see
	// CatalogStale.
	PriceSchema int `json:"price_schema,omitempty"`
}

var (
	catalogPtr atomic.Pointer[Catalog]
	// catalogMu serializes refreshes; readers never take it.
	catalogMu sync.Mutex
	// catalogRefresh is the single in-flight refresh, so a burst of callers
	// (startup + a manual click) shares one download.
	catalogRefresh *catalogFlight
)

type catalogFlight struct {
	done chan struct{}
	cat  *Catalog
	err  error
}

// ReplaceCatalog swaps the in-memory snapshot. Production loads it from disk or
// the network; tests use it to install a fixture (or clear one).
func ReplaceCatalog(cat *Catalog) {
	catalogPtr.Store(cat)
}

// CatalogSnapshot returns the current snapshot, or nil when none is loaded.
func CatalogSnapshot() *Catalog {
	return catalogPtr.Load()
}

// CatalogStale reports whether the snapshot is missing or older than TTL.
func CatalogStale() bool {
	cat := CatalogSnapshot()
	if cat == nil {
		return true
	}
	if time.Since(cat.FetchedAt) > CatalogTTL {
		return true
	}
	// A snapshot written before prices were parsed carries PriceSchema 0 and no
	// prices at all. Without this it would keep the model picker price-less for
	// up to a full TTL after the upgrade, which reads as "the feature does not
	// work". Keyed on the schema marker rather than "has no prices" so a
	// catalog that genuinely has none cannot force a download on every boot.
	return cat.PriceSchema < CatalogPriceSchema
}

// CatalogPriceSchema is bumped when the fields extracted from models.dev
// change, so older snapshots are refetched once instead of being read with
// silently missing data.
const CatalogPriceSchema = 1

// CatalogPath is where the snapshot lives.
func CatalogPath() string {
	return filepath.Join(DataDir, "catalog", "models.json")
}

// LoadCatalogFromDisk reads the snapshot written by a previous run. A missing
// or corrupt file is not an error: it just means the catalog is unavailable
// until a refresh succeeds.
func LoadCatalogFromDisk() error {
	data, err := os.ReadFile(CatalogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("parse model catalog: %w", err)
	}
	catalogPtr.Store(&cat)
	return nil
}

// CatalogModelsForPreset returns the catalog models for a provider created
// from a preset. Nil when the preset is unknown or the snapshot is absent.
func CatalogModelsForPreset(presetID string) []AgentModel {
	cat := CatalogSnapshot()
	if cat == nil || presetID == "" {
		return nil
	}
	p, ok := cat.Providers[presetID]
	if !ok || len(p.Models) == 0 {
		return nil
	}
	out := make([]AgentModel, 0, len(p.Models))
	for i, m := range p.Models {
		out = append(out, AgentModel{ID: m.ID, Name: m.Name, Default: i == 0})
	}
	return out
}

// CatalogProviderForPreset returns the provider entry itself (used by the
// provider-models endpoint when the live probe fails).
func CatalogProviderForPreset(presetID string) *CatalogProvider {
	cat := CatalogSnapshot()
	if cat == nil || presetID == "" {
		return nil
	}
	p, ok := cat.Providers[presetID]
	if !ok {
		return nil
	}
	return &p
}

// RefreshCatalog downloads the catalog and replaces the snapshot. Concurrent
// callers share one download.
func RefreshCatalog(ctx context.Context) (*Catalog, error) {
	catalogMu.Lock()
	if catalogRefresh != nil {
		flight := catalogRefresh
		catalogMu.Unlock()
		select {
		case <-flight.done:
			return flight.cat, flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &catalogFlight{done: make(chan struct{})}
	catalogRefresh = flight
	catalogMu.Unlock()

	cat, err := fetchCatalog(ctx)

	catalogMu.Lock()
	catalogRefresh = nil
	catalogMu.Unlock()

	flight.cat, flight.err = cat, err
	close(flight.done)
	if err != nil {
		return nil, err
	}
	catalogPtr.Store(cat)
	if werr := writeCatalog(cat); werr != nil {
		slog.Warn("failed to persist model catalog", "error", werr)
	}
	return cat, nil
}

func fetchCatalog(ctx context.Context) (*Catalog, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, catalogURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch model catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch model catalog: HTTP %d", resp.StatusCode)
	}
	// The document is ~6 MB today; 32 MiB leaves room to grow without letting a
	// hostile/broken mirror exhaust memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read model catalog: %w", err)
	}
	return parseCatalog(body)
}

// catalogDocument mirrors the parts of models.dev/api.json we consume. Unknown
// fields are ignored; a provider we cannot classify is skipped.
type catalogDocument map[string]struct {
	NPM    string `json:"npm"`
	Name   string `json:"name"`
	API    string `json:"api"`
	Models map[string]struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Limit struct {
			Context int `json:"context"`
			Output  int `json:"output"`
		} `json:"limit"`
		// Cost is the per-million-token price. Providers that bill by
		// subscription publish 0/0 here; those are dropped in parseCatalog so a
		// "$0" tag never reaches the picker.
		Cost struct {
			Input  float64 `json:"input"`
			Output float64 `json:"output"`
		} `json:"cost"`
	} `json:"models"`
}

func parseCatalog(body []byte) (*Catalog, error) {
	var doc catalogDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse model catalog: %w", err)
	}
	cat := &Catalog{
		FetchedAt:   time.Now().UTC(),
		Source:      catalogURL,
		Providers:   make(map[string]CatalogProvider, len(doc)),
		PriceSchema: CatalogPriceSchema,
	}
	for id, entry := range doc {
		protocol := protocolForNPM(entry.NPM)
		baseURL := strings.TrimSpace(entry.API)
		if !strings.HasPrefix(baseURL, "http") {
			// models.dev omits `api` for providers that rely on their SDK's
			// built-in endpoint (the official Anthropic/OpenAI/Google entries).
			baseURL = catalogDefaultBaseURL(id)
		}
		if protocol == "" || len(entry.Models) == 0 {
			continue
		}
		if protocol == ProviderProtocolAnthropic && strings.HasSuffix(baseURL, "/v1") {
			baseURL = strings.TrimSuffix(baseURL, "/v1")
		}
		models := make([]CatalogModel, 0, len(entry.Models))
		for modelID, m := range entry.Models {
			mid := m.ID
			if mid == "" {
				mid = modelID
			}
			name := strings.TrimSpace(m.Name)
			if name == "" {
				name = mid
			}
			models = append(models, CatalogModel{
				ID: mid, Name: name, Context: m.Limit.Context, Output: m.Limit.Output,
				InputPrice: m.Cost.Input, OutputPrice: m.Cost.Output,
			})
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
		cat.Providers[id] = CatalogProvider{
			ID: id, Name: entry.Name, Protocol: protocol, BaseURL: baseURL, Models: models,
		}
	}
	if len(cat.Providers) == 0 {
		return nil, fmt.Errorf("model catalog contains no usable providers")
	}
	return cat, nil
}

// catalogDefaultBaseURL fills in the endpoint models.dev leaves implicit for
// the official providers. Unknown ids stay empty: guessing a host would point a
// third-party provider at someone else's API.
func catalogDefaultBaseURL(providerID string) string {
	switch providerID {
	case "anthropic":
		return "https://api.anthropic.com"
	case "openai":
		return "https://api.openai.com/v1"
	case "google":
		return "https://generativelanguage.googleapis.com"
	}
	return ""
}

func protocolForNPM(npm string) string {
	switch npm {
	case "@ai-sdk/anthropic":
		return ProviderProtocolAnthropic
	case "@ai-sdk/openai", "@ai-sdk/openai-compatible":
		return ProviderProtocolOpenAI
	case "@ai-sdk/google", "@ai-sdk/google-vertex":
		return ProviderProtocolGemini
	}
	return ""
}

func writeCatalog(cat *Catalog) error {
	path := CatalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cat)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RefreshCatalogIfStale fetches in the background when the snapshot is missing
// or old. Startup calls this so a slow download never delays boot.
func RefreshCatalogIfStale() {
	if !CatalogStale() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := RefreshCatalog(ctx); err != nil {
			slog.Warn("model catalog refresh failed", "error", err)
		}
	}()
}
