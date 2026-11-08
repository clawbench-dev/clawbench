package model

// Tests for the picker price lookup (issue #643).
//
// The behaviour worth pinning is the CHOICE of price, not the arithmetic: the
// same model id is resold by dozens of gateways at different prices, and the
// picker must show the owning vendor's number — or nothing — rather than a
// random reseller's markup.

import (
	"testing"
)

// priceFixture builds a catalog with the given per-provider prices.
// key: provider id → model id → {input, output}.
func priceFixture(t *testing.T, entries map[string]map[string][2]float64) *Catalog {
	t.Helper()
	cat := &Catalog{Providers: make(map[string]CatalogProvider, len(entries))}
	for providerID, models := range entries {
		p := CatalogProvider{ID: providerID, Protocol: ProviderProtocolOpenAI}
		for id, price := range models {
			p.Models = append(p.Models, CatalogModel{
				ID: id, Name: id, InputPrice: price[0], OutputPrice: price[1],
			})
		}
		cat.Providers[providerID] = p
	}
	return cat
}

func withPriceCatalog(t *testing.T, cat *Catalog) {
	t.Helper()
	ReplaceCatalog(cat)
	t.Cleanup(func() { ReplaceCatalog(nil) })
}

// The owning vendor's price must beat every reseller's, even when the reseller
// is listed first and is far cheaper. This is the case that produced a wrong
// number before the family rule existed (GLM-5.2 ranged $0.34–$2.02).
func TestLookupModelPrice_PrefersOwningVendor(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"cheap-gateway": {"glm-5.2": {0.34, 2.3}},
		"zai":           {"glm-5.2": {1.4, 4.4}},
	}))

	got, ok := LookupModelPrice("glm-5.2", "GLM-5.2")
	if !ok {
		t.Fatal("expected a price for glm-5.2")
	}
	if got.Input != 1.4 || got.Output != 4.4 {
		t.Fatalf("want the vendor price 1.4/4.4, got %v/%v", got.Input, got.Output)
	}
	if got.Approx {
		t.Fatal("a price from the owning vendor must not be marked approximate")
	}
}

// When the owning vendor has no entry, a consensus across at least two
// resellers is acceptable — but only flagged approximate, so the picker shows
// "~" and the user knows it is not the agent's own quote.
func TestLookupModelPrice_ConsensusIsApproximate(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"gateway-a": {"qwen3.7-plus": {0.4, 1.6}},
		"gateway-b": {"qwen3.7-plus": {0.4, 1.6}},
		"gateway-c": {"qwen3.7-plus": {9.9, 9.9}},
	}))

	got, ok := LookupModelPrice("qwen3.7-plus", "Qwen3.7 Plus")
	if !ok {
		t.Fatal("expected a consensus price")
	}
	if got.Input != 0.4 || got.Output != 1.6 {
		t.Fatalf("want the most common price 0.4/1.6, got %v/%v", got.Input, got.Output)
	}
	if !got.Approx {
		t.Fatal("a reseller consensus must be marked approximate")
	}
}

// One reseller is not a consensus: that is a single gateway's markup, and
// showing it as the model's price would be a fabrication.
func TestLookupModelPrice_SingleResellerIsNotEnough(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"lonely-gateway": {"unknown-model-x": {7, 21}},
	}))

	if got, ok := LookupModelPrice("unknown-model-x", "Unknown Model X"); ok {
		t.Fatalf("a single reseller entry must not produce a price, got %+v", got)
	}
}

// models.dev marks subscription plans as 0/0. Rendering "$0 / $0" would tell
// the user the model is free.
func TestLookupModelPrice_ZeroPriceIsNotAPrice(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"zai":           {"glm-5.2": {0, 0}},
		"another-gw":    {"glm-5.2": {0, 0}},
		"third-gateway": {"glm-5.2": {0, 0}},
	}))

	if got, ok := LookupModelPrice("glm-5.2", "GLM-5.2"); ok {
		t.Fatalf("0/0 is a subscription plan, not a price; got %+v", got)
	}
}

// "auto" names a routing slot. A gateway publishes a model literally called
// "Auto", and matching it would put that gateway's number on CodeBuddy's
// router, which is a different product entirely.
func TestLookupModelPrice_GenericIDsAreSkipped(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"morph":  {"auto": {0.85, 1.55}},
		"other":  {"auto": {0.85, 1.55}},
		"third":  {"auto": {0.85, 1.55}},
		"fourth": {"auto": {0.85, 1.55}},
	}))

	if got, ok := LookupModelPrice("auto", "Auto"); ok {
		t.Fatalf("generic routing ids must never be priced; got %+v", got)
	}
}

// Agents namespace their ids ("deepseek/deepseek-v4-pro", and CodeBuddy's
// "custom-local:hy3"). The lookup must see through both separators.
func TestLookupModelPrice_NamespacedIDs(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"deepseek": {"deepseek-v4-pro": {0.66, 1.98}},
	}))

	for _, id := range []string{"deepseek-v4-pro", "deepseek/deepseek-v4-pro", "custom-local:deepseek-v4-pro"} {
		got, ok := LookupModelPrice(id, "")
		if !ok {
			t.Fatalf("%q: expected the vendor price", id)
		}
		if got.Input != 0.66 || got.Output != 1.98 {
			t.Fatalf("%q: want 0.66/1.98, got %v/%v", id, got.Input, got.Output)
		}
	}
}

// Qoder reports opaque ids (qmodel) with a readable name. Name matching is the
// only way in, and the result must be marked approximate.
func TestLookupModelPrice_FallsBackToDisplayName(t *testing.T) {
	cat := &Catalog{Providers: map[string]CatalogProvider{
		"gateway-a": {ID: "gateway-a", Models: []CatalogModel{
			{ID: "qwen/qwen3.7-plus", Name: "Qwen3.7 Plus", InputPrice: 0.4, OutputPrice: 1.6},
		}},
		"gateway-b": {ID: "gateway-b", Models: []CatalogModel{
			{ID: "qwen3.7-plus-2026", Name: "Qwen3.7-Plus", InputPrice: 0.4, OutputPrice: 1.6},
		}},
	}}
	withPriceCatalog(t, cat)

	got, ok := LookupModelPrice("qmodel", "Qwen3.7-Plus")
	if !ok {
		t.Fatal("expected a name-matched price")
	}
	if !got.Approx {
		t.Fatal("a name match must be marked approximate")
	}
	if got.Input != 0.4 || got.Output != 1.6 {
		t.Fatalf("want 0.4/1.6, got %v/%v", got.Input, got.Output)
	}
}

// The regression that made an opaque-id agent show a bare "$3 / $15": the
// display name starts with a vendor prefix, so choosePrice found the owning
// vendor's entry and reported it as exact. But the ID never matched — Qoder
// says "smodel", not "claude-sonnet-4-5" — so the number is a reference price
// and MUST carry the tilde.
func TestLookupModelPrice_NameMatchOnOwningVendorIsStillApproximate(t *testing.T) {
	cat := &Catalog{Providers: map[string]CatalogProvider{
		"anthropic": {ID: "anthropic", Models: []CatalogModel{
			{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", InputPrice: 3, OutputPrice: 15},
		}},
	}}
	withPriceCatalog(t, cat)

	got, ok := LookupModelPrice("smodel", "Claude Sonnet 4.5")
	if !ok {
		t.Fatal("expected a name-matched price")
	}
	if !got.Approx {
		t.Fatal("a name match must be marked approximate even when it lands on the owning vendor")
	}
	if got.Input != 3 || got.Output != 15 {
		t.Fatalf("want 3/15, got %v/%v", got.Input, got.Output)
	}
}

// Model ids are spelled inconsistently across agents and the catalog
// ("GPT-5" vs "gpt-5"). A case mismatch must not silently drop the id match
// and demote an exact price to an approximate one.
func TestLookupModelPrice_IDMatchIsCaseInsensitive(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"openai": {"gpt-5": {2, 8}},
	}))

	got, ok := LookupModelPrice("GPT-5", "GPT-5")
	if !ok {
		t.Fatal("expected the vendor price")
	}
	if got.Approx {
		t.Fatal("a case-different id is still an exact id match")
	}
}

// WithPrices must not write into the caller's slice: it is handed the agent's
// stored list and the ACP registry's list, both of which are shared state.
func TestWithPrices_DoesNotMutateInput(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"zai": {"glm-5.2": {1.4, 4.4}},
	}))

	input := []AgentModel{{ID: "glm-5.2", Name: "GLM-5.2"}}
	out := WithPrices(input)

	if input[0].Price != nil {
		t.Fatal("WithPrices must not write into the input slice")
	}
	if out[0].Price == nil {
		t.Fatal("the returned copy must carry the price")
	}
	if out[0].Price.Input != 1.4 {
		t.Fatalf("want 1.4, got %v", out[0].Price.Input)
	}
}

// WithoutPrices is the persistence guard: a derived price must never reach the
// agents table, where it would outlive the catalog snapshot.
func TestWithoutPrices_StripsPrice(t *testing.T) {
	priced := []AgentModel{{ID: "m", Name: "M", Price: &ModelPrice{Input: 1, Output: 2}}}
	out := WithoutPrices(priced)

	if out[0].Price != nil {
		t.Fatal("WithoutPrices must clear the price")
	}
	if priced[0].Price == nil {
		t.Fatal("WithoutPrices must not mutate its input")
	}
}

// A missing catalog must degrade to "no prices", never to a panic or a guess.
func TestLookupModelPrice_NoCatalog(t *testing.T) {
	ReplaceCatalog(nil)
	t.Cleanup(func() { ReplaceCatalog(nil) })

	if _, ok := LookupModelPrice("glm-5.2", "GLM-5.2"); ok {
		t.Fatal("with no catalog there is nothing to look up")
	}
}

// The index must be rebuilt when the snapshot is replaced, otherwise a price
// change would only take effect after a restart.
func TestPriceIndex_RebuildsOnCatalogSwap(t *testing.T) {
	withPriceCatalog(t, priceFixture(t, map[string]map[string][2]float64{
		"zai": {"glm-5.2": {1.4, 4.4}},
	}))
	if got, _ := LookupModelPrice("glm-5.2", "GLM-5.2"); got.Input != 1.4 {
		t.Fatalf("want 1.4 before the swap, got %v", got.Input)
	}

	ReplaceCatalog(priceFixture(t, map[string]map[string][2]float64{
		"zai": {"glm-5.2": {2.8, 8.8}},
	}))
	if got, _ := LookupModelPrice("glm-5.2", "GLM-5.2"); got.Input != 2.8 {
		t.Fatalf("want the new 2.8 after the swap, got %v", got.Input)
	}
}
