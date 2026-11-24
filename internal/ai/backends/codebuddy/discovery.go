package codebuddy

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// maxCacheEntryBytes bounds a cache entry's on-disk size, and also caps how much
// decompressed data is read from it. Real entries measure well under 1 MiB, so
// this guards against a corrupt or hostile .info file (gzip bomb) without
// rejecting legitimate ones.
const maxCacheEntryBytes = 4 << 20 // 4 MiB

// codebuddyProductModel represents a model entry in codebuddy's product JSON.
type codebuddyProductModel struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	IsDefault bool     `json:"isDefault"`
	Tags      []string `json:"tags"`
	// Credits is the model's billing multiplier as the product config spells it
	// ("x0.06 credits", empty for the auto router). The CLI's own model picker
	// shows it, so the picker shows the same number (issue #657).
	Credits string `json:"credits"`
}

// codebuddyProduct represents the top-level structure of codebuddy's product JSON.
type codebuddyProduct struct {
	Models []codebuddyProductModel `json:"models"`
}

// Injectable seams so tests can run without touching the real CLI/home.
var (
	codebuddyResolveCLIPath = platform.ResolveCLIPath
	codebuddyUserHomeDir    = os.UserHomeDir
)

// discoverCodebuddyModels is the ModelSource entry point. The failure detail is
// returned to the caller rather than stashed in package state: discovery also
// runs from the background refresher, so a process-global "last failure" string
// could be overwritten by a concurrent refresh and misattribute the reason.
func discoverCodebuddyModels() ([]model.AgentModel, string) {
	realPath := codebuddyResolveCLIPath("codebuddy")
	home, _ := codebuddyUserHomeDir()
	return discoverCodebuddyModelsFrom(realPath, home)
}

// discoverCodebuddyModelsFrom is the testable core of DiscoverCodebuddyModels.
// It returns the discovered models (nil on failure) and a human-readable detail
// string describing what was attempted (empty on success).
//
// There is exactly ONE local source: the runtime cache the CLI writes under
// ~/.codebuddy/local_storage/. It is the config the CLI itself fetched from the
// server for THIS account's deployment (enterpriseId + deploymentType), so it is
// the only list that matches what the CLI can actually run.
//
// The two sources that used to sit beside it are deliberately gone:
//
//   - The product.*.json files shipped inside the npm package. They are generic
//     factory defaults whose models can be entirely disjoint from the account's
//     deployment (issue #539), so they did not merely rank lower — they were
//     actively wrong. Preferring them made the picker show retired models.
//   - The ACC_PRODUCT_CONFIG* environment overrides. They only apply to
//     enterprise deployments that set them by hand, and reading them from the
//     server process env silently produced a model list for whoever happened to
//     have the variable set.
//
// With no cache the answer is "nothing", which the UI renders as an empty picker
// with a refresh button — never a plausible-looking list that cannot run.
func discoverCodebuddyModelsFrom(realPath, home string) ([]model.AgentModel, string) {
	// Gated on the CLI being present so a stale cache left behind by an
	// uninstalled CLI is not reported.
	if realPath != "" {
		if models := modelsFromRuntimeCache(home); len(models) > 0 {
			slog.Debug("codebuddy model discovery: parsed runtime cache", "home", home, "models", len(models))
			return models, ""
		}
		return nil, "no CodeBuddy model list found (runtime cache " +
			filepath.Join(home, ".codebuddy", "local_storage") +
			" has no usable entry; run codebuddy once to sign in)"
	}
	return nil, "codebuddy CLI not found on PATH"
}

// parseProductModels parses product-config JSON bytes into models, applying the
// same filtering/default rules used previously. Returns nil if no usable models.
//
// Only "default" is filtered: it is a fallback marker, not a selectable model.
// "auto" is kept — CodeBuddy marks it isDefault:true and it is the deployment's
// real default (issue #539); dropping it silently changed the selected model.
func parseProductModels(data []byte) []model.AgentModel {
	var product codebuddyProduct
	if err := json.Unmarshal(data, &product); err != nil {
		slog.Debug("codebuddy model discovery: failed to parse product JSON", "error", err)
		return nil
	}
	if len(product.Models) == 0 {
		return nil
	}

	var models []model.AgentModel
	seen := make(map[string]bool, len(product.Models))
	defaultID := ""
	for _, m := range product.Models {
		if m.ID == "" || m.ID == "default" {
			continue
		}
		if isNonChatModel(m) {
			continue
		}
		// Drop duplicate IDs so the single-default invariant below holds even
		// if the config lists the same model twice.
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, model.AgentModel{ID: m.ID, Name: name, Credits: strings.TrimSpace(m.Credits)})
		if m.IsDefault && defaultID == "" {
			defaultID = m.ID
		}
	}

	if len(models) == 0 {
		return nil
	}

	// Emit exactly one default: the entry flagged isDefault wins (CodeBuddy
	// marks "auto" that way), otherwise the first surviving model. Previously
	// the first model was always forced default AND isDefault was honored,
	// producing two defaults; the downstream merge then picked the first row,
	// overriding the real default with a retired model.
	if defaultID == "" {
		defaultID = models[0].ID
	}
	for i := range models {
		models[i].Default = models[i].ID == defaultID
	}
	return models
}

// nonChatModelTags mark product-config entries that are generative-media models
// (image/video) rather than chat completions. They must never appear in the
// model picker.
var nonChatModelTags = map[string]bool{
	"text-to-image":  true,
	"image-to-image": true,
	"text-to-video":  true,
	"image-to-video": true,
}

// isNonChatModel reports whether a product-config entry is a generative-media
// model (image or video) rather than a chat model. It keys off the config's own
// tags rather than a hardcoded ID: the previous `m.ID == "hunyuan-image-v3.0"`
// check went stale the moment the model was renamed to "hunyuan-image-v3.0-art",
// leaking it into the picker (issue #539). The same hardcoding would also miss
// video models (e.g. kling-video-v3, tagged "text-to-video").
func isNonChatModel(m codebuddyProductModel) bool {
	for _, t := range m.Tags {
		if nonChatModelTags[t] {
			return true
		}
	}
	return false
}

// codebuddyCacheMeta is the subset of a cached product config used to rank
// entries. CodeBuddy's local_storage holds several populations at once: the
// base config plus per-plugin overlays (which carry extra internal completion
// models). We must prefer the base, most recent entry.
type codebuddyCacheMeta struct {
	PluginName     string `json:"pluginName"`
	DeploymentType string `json:"deploymentType"`
	Date           string `json:"date"`
}

// modelsFromRuntimeCache reads CodeBuddy's own runtime cache under
// ~/.codebuddy/local_storage/. The CLI stores product configs there as a JSON
// string wrapped in base64+gzip (older entries may be plain JSON). This is the
// reliable source for native installs, where no product JSON exists on disk.
//
// Entries are ranked before parsing: base configs (no pluginName) beat plugin
// overlays, then newest date wins. Without this, the result depends on the
// hash-based filename order and can silently surface a plugin's internal
// completion models instead of the selectable chat models.
func modelsFromRuntimeCache(home string) []model.AgentModel {
	if home == "" {
		return nil
	}
	dir := filepath.Join(home, ".codebuddy", "local_storage")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	type candidate struct {
		data []byte
		meta codebuddyCacheMeta
	}
	var candidates []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".info") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if info, err := e.Info(); err == nil && info.Size() > maxCacheEntryBytes {
			slog.Debug("codebuddy model discovery: skipping oversized cache entry", "path", path, "size", info.Size())
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		data, ok := decodeCacheEntry(raw)
		if !ok {
			continue
		}
		var meta codebuddyCacheMeta
		_ = json.Unmarshal(data, &meta)
		candidates = append(candidates, candidate{data: data, meta: meta})
	}

	// Stable rank: base configs first, then newest date.
	sort.SliceStable(candidates, func(i, j int) bool {
		pi := candidates[i].meta.PluginName != ""
		pj := candidates[j].meta.PluginName != ""
		if pi != pj {
			return !pi // base (no plugin) first
		}
		return candidates[i].meta.Date > candidates[j].meta.Date
	})

	for _, c := range candidates {
		if models := parseProductModels(c.data); len(models) > 0 {
			slog.Debug("codebuddy model discovery: parsed runtime cache",
				"plugin", c.meta.PluginName, "date", c.meta.Date, "models", len(models))
			return models
		}
	}
	return nil
}

// decodeCacheEntry decodes a CodeBuddy local_storage .info value. Entries are a
// JSON string containing base64(gzip(JSON)); some entries are plain JSON. It
// returns the inner JSON bytes and true on success. Output is capped at
// maxCacheEntryBytes; a payload that hits the cap is rejected rather than
// silently truncated.
func decodeCacheEntry(raw []byte) ([]byte, bool) {
	// Strip surrounding whitespace and optional JSON string quotes.
	s := bytes.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	// Try base64 → gzip → JSON.
	if decoded, err := base64.StdEncoding.DecodeString(string(s)); err == nil {
		if zr, err := gzip.NewReader(bytes.NewReader(decoded)); err == nil {
			defer func() { _ = zr.Close() }()
			if plain, err := io.ReadAll(io.LimitReader(zr, maxCacheEntryBytes)); err == nil {
				if len(plain) >= maxCacheEntryBytes {
					slog.Debug("codebuddy model discovery: cache entry exceeds size cap, skipping")
					return nil, false
				}
				return plain, true
			}
		}
	}

	// Fall back to plain JSON.
	if len(s) > 0 && s[0] == '{' {
		if len(s) > maxCacheEntryBytes {
			return nil, false
		}
		return s, true
	}
	return nil, false
}
