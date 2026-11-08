package model

// Model pricing for the model picker (issue #643).
//
// The picker used to show only model names, so a user comparing "which model
// is cheaper" had to open each vendor's client. The catalog snapshot
// (models.dev) already carries USD-per-million-token prices, but they were
// discarded at parse time; this file turns them into the numbers the picker
// renders.
//
// Three rules keep a wrong number off the screen:
//
//  1. The same model id is resold by dozens of gateways at wildly different
//     prices (GLM-5.2 ranged $0.34–$2.02 across 28 providers). We therefore
//     prefer the price published by the model's OWN vendor, decided by id
//     family, and only fall back to a consensus price when that vendor has no
//     entry.
//
//  2. A price that came from anywhere but the owning vendor is marked
//     approximate and rendered with a "~" prefix. Qoder's opaque ids
//     (smodel/qmodel) can only ever be matched this way, and the number is
//     another vendor's list price for a model of the same name — close, but
//     not what that agent bills.
//
//  3. Fewer than two independent sources, or a 0/0 price (a subscription
//     plan, not a per-token price), yields no price at all. Showing nothing
//     beats showing a made-up number.
//
// Prices are deliberately NOT persisted: they live only in the catalog
// snapshot, and the snapshot refreshes weekly. Writing them into
// agents.models would freeze last month's price forever.

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// ModelPrice is a per-million-token price in USD.
type ModelPrice struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	// Approx reports that the price did not come from the model's own vendor:
	// it is a consensus across resellers, or a name match rather than an exact
	// id match. The picker renders these with a "~".
	Approx bool `json:"approx,omitempty"`
}

// vendorFamilies maps a model-id prefix to the models.dev provider id that
// publishes that family's list price. Order matters only for overlapping
// prefixes (none today); first match wins.
var vendorFamilies = []struct {
	prefix   string
	provider string
}{
	{"claude", "anthropic"},
	{"gpt-", "openai"},
	{"o3", "openai"},
	{"o4-", "openai"},
	{"gemini", "google"},
	{"gemma", "google"},
	{"deepseek", "deepseek"},
	{"kimi", "moonshotai"},
	{"moonshot", "moonshotai"},
	{"glm", "zai"},
	{"qwen", "alibaba"},
	{"grok", "xai"},
	{"minimax", "minimax"},
	{"mimo", "xiaomi"},
	{"doubao", "volcengine"},
}

// vendorOf returns the provider id that owns the given model id, or "" when
// the family is unknown.
func vendorOf(modelID string) string {
	lower := strings.ToLower(modelID)
	for _, f := range vendorFamilies {
		if strings.HasPrefix(lower, f.prefix) {
			return f.provider
		}
	}
	return ""
}

// genericModelIDs are ids that name a routing slot rather than a model, so any
// price attached to them belongs to a different product. Matching them by id
// would put a random gateway's number on e.g. CodeBuddy's "auto".
var genericModelIDs = map[string]struct{}{
	"auto": {}, "default": {}, "latest": {}, "fast": {},
}

// nonAlnum collapses a display name to a comparison key, so "GLM-5.2",
// "glm 5.2" and "GLM 5.2 " all match.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func nameKey(name string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(name), "")
}

// priceIndex is a snapshot-derived lookup, rebuilt whenever the catalog
// snapshot changes. Reads never block: a stale pointer is swapped atomically.
type priceIndex struct {
	// byID holds every price published for an exact model id.
	byID map[string][]pricedEntry
	// byName holds every price published under a display name.
	byName map[string][]pricedEntry
}

type pricedEntry struct {
	provider string
	input    float64
	output   float64
}

// cachedPriceIndex pairs an index with the snapshot it was built from. The two
// live in ONE atomic value on purpose: stored separately, a reader could pair a
// new index with an old snapshot (or vice versa) and rebuild over the fresh
// index with stale data.
type cachedPriceIndex struct {
	source *Catalog
	index  *priceIndex
}

var (
	priceIndexCache atomic.Pointer[cachedPriceIndex]
	priceIndexMu    sync.Mutex
)

// LookupModelPrice returns the price to show next to a model in the picker.
// ok is false when no trustworthy price exists — the caller then renders no
// tag at all.
func LookupModelPrice(modelID, name string) (ModelPrice, bool) {
	idx := currentPriceIndex()
	if idx == nil {
		return ModelPrice{}, false
	}
	// Exact id first. Some agents prefix the id with a routing namespace
	// ("deepseek/deepseek-v4-pro"), so the tail is tried as well.
	if !isGenericModelID(modelID) {
		for _, candidate := range idCandidates(modelID) {
			if entries, ok := idx.byID[strings.ToLower(candidate)]; ok {
				if p, ok := choosePrice(candidate, entries); ok {
					return p, true
				}
			}
		}
	}
	if key := nameKey(name); key != "" && !isGenericModelID(key) {
		if entries, ok := idx.byName[key]; ok {
			if p, ok := choosePrice(name, entries); ok {
				// A name match is NEVER an exact quote, even when it lands on
				// the owning vendor's entry: the id is different (Qoder's
				// "smodel" vs "claude-sonnet-4-5"), so all we know is that a
				// model with this display name costs this much somewhere.
				// Leaving Approx false here made opaque-id agents show a bare
				// "$3 / $15" that looked like their own list price.
				p.Approx = true
				return p, true
			}
		}
	}
	return ModelPrice{}, false
}

// isGenericModelID reports whether an id names a routing slot ("auto") rather
// than a concrete model.
func isGenericModelID(modelID string) bool {
	_, ok := genericModelIDs[strings.ToLower(strings.TrimSpace(modelID))]
	return ok
}

// idCandidates expands a model id into the forms the catalog may store it
// under: as-is, without a "vendor/" namespace, and without a "vendor:" prefix
// (CodeBuddy's proxy models are "custom-local:deepseek-v4.1-flash").
func idCandidates(modelID string) []string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return nil
	}
	out := []string{id}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		out = append(out, id[i+1:])
	}
	if i := strings.LastIndex(id, ":"); i >= 0 && i < len(id)-1 {
		out = append(out, id[i+1:])
	}
	return out
}

// choosePrice picks one price out of every entry published for a model.
//
// Preference order: the owning vendor's own entry (exact), then the most
// common price among at least two resellers (approximate). A single reseller
// entry is not enough — that is one gateway's markup, not a list price.
func choosePrice(modelID string, entries []pricedEntry) (ModelPrice, bool) {
	if len(entries) == 0 {
		return ModelPrice{}, false
	}
	if vendor := vendorOf(modelID); vendor != "" {
		for _, e := range entries {
			if e.provider == vendor {
				return ModelPrice{Input: e.input, Output: e.output}, true
			}
		}
	}
	if len(entries) < 2 {
		return ModelPrice{}, false
	}
	type pair struct{ in, out float64 }
	counts := make(map[pair]int, len(entries))
	for _, e := range entries {
		counts[pair{e.input, e.output}]++
	}
	best := pair{}
	bestCount := 0
	// Sort for determinism: two prices can tie on count, and map iteration
	// order must not decide what the user sees.
	keys := make([]pair, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].in != keys[j].in {
			return keys[i].in < keys[j].in
		}
		return keys[i].out < keys[j].out
	})
	for _, k := range keys {
		if counts[k] > bestCount {
			best, bestCount = k, counts[k]
		}
	}
	return ModelPrice{Input: best.in, Output: best.out, Approx: true}, true
}

// PricesForIDs prices a bare list of model ids, keyed by id.
//
// Used by the provider model picker, which deals in ids only — it has no
// display names, so only the exact-id path of LookupModelPrice can match. Ids
// with no trustworthy price are simply absent from the map, and the caller
// renders no tag for them.
func PricesForIDs(ids []string) map[string]ModelPrice {
	out := make(map[string]ModelPrice, len(ids))
	for _, id := range ids {
		if _, done := out[id]; done {
			continue
		}
		if p, ok := LookupModelPrice(id, ""); ok {
			out[id] = p
		}
	}
	return out
}

// WithPrices returns a copy of models with Price filled in from the catalog.
//
// It never mutates its input: callers hand it slices they do not own (the
// agent's stored CLI list, the ACP registry's available-model list), and writing
// a price into those would both corrupt shared state and persist a price that
// goes stale as soon as the catalog refreshes. An empty input is returned as-is
// — there is nothing to price and nothing to corrupt.
func WithPrices(models []AgentModel) []AgentModel {
	if len(models) == 0 {
		return models
	}
	out := make([]AgentModel, len(models))
	copy(out, models)
	for i := range out {
		if p, ok := LookupModelPrice(out[i].ID, out[i].Name); ok {
			price := p
			out[i].Price = &price
		}
	}
	return out
}

// WithoutPrices returns a copy safe to persist. Every DB write path funnels
// through it so a derived price can never outlive the snapshot it came from.
func WithoutPrices(models []AgentModel) []AgentModel {
	out := make([]AgentModel, len(models))
	for i, m := range models {
		m.Price = nil
		out[i] = m
	}
	return out
}

// currentPriceIndex returns the index for the live catalog snapshot, building
// it on first use and after every refresh.
func currentPriceIndex() *priceIndex {
	snapshot := CatalogSnapshot()
	if snapshot == nil {
		return nil
	}
	if cached := priceIndexCache.Load(); cached != nil && cached.source == snapshot {
		return cached.index
	}
	priceIndexMu.Lock()
	defer priceIndexMu.Unlock()
	// Re-check under the lock: a concurrent caller may have just built it.
	if cached := priceIndexCache.Load(); cached != nil && cached.source == snapshot {
		return cached.index
	}
	idx := buildPriceIndex(snapshot)
	priceIndexCache.Store(&cachedPriceIndex{source: snapshot, index: idx})
	return idx
}

// buildPriceIndex flattens every provider's priced models into id and name
// lookups. Entries without a usable price are dropped here, so the lookup
// paths never have to re-check.
func buildPriceIndex(cat *Catalog) *priceIndex {
	idx := &priceIndex{
		byID:   make(map[string][]pricedEntry),
		byName: make(map[string][]pricedEntry),
	}
	for providerID, provider := range cat.Providers {
		for _, m := range provider.Models {
			// 0/0 is how models.dev marks a subscription plan. Recording it
			// would render "$0 / $0" for a model that is anything but free.
			if m.InputPrice == 0 && m.OutputPrice == 0 {
				continue
			}
			entry := pricedEntry{provider: providerID, input: m.InputPrice, output: m.OutputPrice}
			// Keyed lowercased: the same model is spelled "GPT-5" by one agent
			// and "gpt-5" by the catalog, and a case mismatch would silently
			// drop it into the name path (where it would be marked approximate).
			idx.byID[strings.ToLower(m.ID)] = append(idx.byID[strings.ToLower(m.ID)], entry)
			if key := nameKey(m.Name); key != "" {
				idx.byName[key] = append(idx.byName[key], entry)
			}
		}
	}
	return idx
}
