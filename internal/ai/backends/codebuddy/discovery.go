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
	"regexp"
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

func init() {
	model.RegisterModelSource(model.PluginSource("codebuddy", discoverCodebuddyModels))
}

// codebuddyProductFiles are the product-config filenames CodeBuddy ships.
// The npm package and native binary both reference several variants; only one
// is present for a given distribution channel. Order is preference order:
// the cloud-hosted config is the public/selectable model list, and the
// internal/IOA/self-hosted variants carry deployment-specific extras.
// product.json is the base document the variants are patched from, so it is
// the last resort.
var codebuddyProductFiles = []string{
	"product.cloudhosted.json",
	"product.internal.json",
	"product.ioa.json",
	"product.selfhosted.json",
	"product.json",
}

// codebuddyProductEnvVars are the official environment variables that override
// the product config. ACC_PRODUCT_CONFIG_PATH points at a file; the others may
// carry either a file path or inline JSON.
var codebuddyProductEnvVars = []string{
	"ACC_PRODUCT_CONFIG_PATH",
	"ACC_PRODUCT_CONFIG_V3",
	"ACC_PRODUCT_CONFIG_V2",
	"ACC_PRODUCT_CONFIG",
}

// codebuddyProductModel represents a model entry in codebuddy's product JSON.
type codebuddyProductModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// codebuddyProduct represents the top-level structure of codebuddy's product JSON.
type codebuddyProduct struct {
	Models []codebuddyProductModel `json:"models"`
}

// Injectable seams so tests can run without touching the real CLI/home.
var (
	codebuddyResolveCLIPath = platform.ResolveCLIPath
	codebuddyUserHomeDir    = os.UserHomeDir
	codebuddyGetenv         = os.Getenv
)

// DiscoverCodebuddyModels discovers CodeBuddy models from, in order:
//  1. an explicit ACC_PRODUCT_CONFIG* override,
//  2. a product.*.json file next to the resolved CLI (npm or native layout),
//  3. CodeBuddy's runtime cache under ~/.codebuddy/local_storage/.
//
// Returns nil if none of the sources yields a model list.
func DiscoverCodebuddyModels() []model.AgentModel {
	models, _ := discoverCodebuddyModels()
	return models
}

// discoverCodebuddyModels is the ModelSource entry point. The failure detail is
// returned to the caller rather than stashed in package state: discovery also
// runs from the background refresher, so a process-global "last failure" string
// could be overwritten by a concurrent refresh and misattribute the reason.
func discoverCodebuddyModels() ([]model.AgentModel, string) {
	realPath := codebuddyResolveCLIPath("codebuddy")
	home, _ := codebuddyUserHomeDir()
	return discoverCodebuddyModelsFrom(realPath, home, codebuddyGetenv)
}

// discoverCodebuddyModelsFrom is the testable core of DiscoverCodebuddyModels.
// It returns the discovered models (nil on failure) and a human-readable detail
// string describing what was attempted (empty on success).
func discoverCodebuddyModelsFrom(realPath, home string, getenv func(string) string) ([]model.AgentModel, string) {
	// 1. Explicit env override wins — but only when the CLI is actually
	// present. These are CodeBuddy's own variables, read from the ClawBench
	// server process env; without this gate a stray value would make discovery
	// report models for a CLI that isn't installed.
	if realPath != "" {
		if models := modelsFromEnvOverride(getenv); len(models) > 0 {
			return models, ""
		}
	}

	// 2. Product JSON on disk, probing every candidate directory × filename.
	var tried []string
	for _, dir := range codebuddyProductDirs(realPath, home) {
		for _, name := range codebuddyProductFiles {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			tried = append(tried, path)
			if models := parseProductModels(data); len(models) > 0 {
				slog.Debug("codebuddy model discovery: parsed product JSON", "path", path, "models", len(models))
				return models, ""
			}
		}
	}

	// 3. Runtime cache written by the CLI itself. This is the only source that
	// works for native/standalone installs, where the product config is
	// compiled into the binary and never written to disk. Gated on the CLI
	// being present so a stale cache left behind by an uninstalled CLI is not
	// reported.
	cacheNote := "skipped (CLI not found)"
	if realPath != "" {
		if models := modelsFromRuntimeCache(home); len(models) > 0 {
			slog.Debug("codebuddy model discovery: parsed runtime cache", "home", home, "models", len(models))
			return models, ""
		}
		cacheNote = "no usable entry"
	}

	// Build a detail that names every source actually attempted, so a native
	// install (which has no product JSON on disk by design) is not misled into
	// looking for a file that will never exist.
	var parts []string
	if len(tried) > 0 {
		const maxListed = 6
		listed := tried
		if len(listed) > maxListed {
			listed = append(append([]string{}, listed[:maxListed]...), "…")
		}
		parts = append(parts, "product config: "+strings.Join(listed, ", "))
	} else if realPath == "" {
		parts = append(parts, "codebuddy CLI not found on PATH")
	} else {
		parts = append(parts, "product config: none found next to the CLI")
	}
	parts = append(parts, "runtime cache: "+cacheNote)

	return nil, "no CodeBuddy model list found (" + strings.Join(parts, "; ") + ")"
}

// codebuddyProductDirs returns candidate directories that may hold a product
// config, ordered by likelihood. It covers both the npm layout
// (.../node_modules/@tencent-ai/codebuddy-code/) and the native layout
// (~/.local/share/codebuddy/versions/<ver>/), plus the CLI's config dir.
func codebuddyProductDirs(realPath, home string) []string {
	seen := make(map[string]bool)
	var dirs []string
	add := func(dir string) {
		if dir == "" || dir == "." || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if realPath != "" {
		add(filepath.Dir(realPath))               // alongside the binary
		add(filepath.Dir(filepath.Dir(realPath))) // npm package root
	}

	if home != "" {
		// Native install keeps versioned binaries under this tree; a future
		// distribution may place the product config there too. Unix-only
		// convention — on Windows this directory simply does not exist.
		versionsDir := filepath.Join(home, ".local", "share", "codebuddy", "versions")
		add(versionsDir)
		if entries, err := os.ReadDir(versionsDir); err == nil {
			// Prefer the newest version dir (names are semver, so a lexical
			// descending sort puts the active install first).
			var names []string
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(names)))
			for _, name := range names {
				add(filepath.Join(versionsDir, name))
			}
		}
		add(filepath.Join(home, ".local", "share", "codebuddy"))
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, ".codebuddy"))
	}

	return dirs
}

// modelsFromEnvOverride reads the ACC_PRODUCT_CONFIG* environment variables.
// Each value may be a path to a product JSON file or inline JSON content.
func modelsFromEnvOverride(getenv func(string) string) []model.AgentModel {
	if getenv == nil {
		return nil
	}
	for _, key := range codebuddyProductEnvVars {
		val := strings.TrimSpace(getenv(key))
		if val == "" {
			continue
		}
		// Inline JSON
		if strings.HasPrefix(val, "{") {
			if models := parseProductModels([]byte(val)); len(models) > 0 {
				slog.Warn("codebuddy model discovery: using product JSON from env override", "var", key)
				return models
			}
			continue
		}
		// File path
		if data, err := os.ReadFile(val); err == nil {
			if models := parseProductModels(data); len(models) > 0 {
				slog.Warn("codebuddy model discovery: using product JSON from env override", "var", key, "path", val)
				return models
			}
		}
	}
	return nil
}

// parseProductModels parses product-config JSON bytes into models, applying the
// same filtering/default rules used previously. Returns nil if no usable models.
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
	for _, m := range product.Models {
		if m.ID == "default" || m.ID == "auto" {
			continue
		}
		if m.ID == "hunyuan-image-v3.0" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, model.AgentModel{
			ID:      m.ID,
			Name:    name,
			Default: m.IsDefault || (len(models) == 0 && m.ID != "default" && m.ID != "auto"),
		})
	}

	if len(models) == 0 {
		return nil
	}
	return models
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

// codebuddyModelRe extracts model IDs from codebuddy --help output (legacy, kept for ParseCodebuddyModels).
var codebuddyModelRe = regexp.MustCompile(`Currently supported: \(([^)]+)\)`)

// ParseCodebuddyModels parses codebuddy --help output to extract model IDs.
//
// Deprecated: codebuddy --help launches a TUI that hangs without a TTY; use DiscoverCodebuddyModels instead.
func ParseCodebuddyModels(output string) []model.AgentModel {
	matches := codebuddyModelRe.FindStringSubmatch(output)
	if len(matches) < 2 {
		return nil
	}

	parts := strings.Split(matches[1], ",")
	var models []model.AgentModel
	for i, p := range parts {
		id := strings.TrimSpace(p)
		if id == "" {
			continue
		}
		models = append(models, model.AgentModel{
			ID:      id,
			Name:    id,
			Default: i == 0,
		})
	}
	return models
}
