package codex

import (
	"encoding/json"
	"os"
	"path/filepath"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// discoverCodexModels discovers Codex models from the only source that reflects
// reality: the catalog the CLI fetched from the API for this account.
//
// There used to be a second level that scraped model IDs out of the bundled
// Rust binary. It only worked on unstripped builds and, when it did work, could
// list models the account cannot run — so it was removed rather than kept as a
// middle tier. Codex has no model-list command, so there is no CLI path either.
func discoverCodexModels() ([]model.AgentModel, string) {
	if platform.ResolveCLIPath("codex") == "" {
		return nil, "codex: CLI not found on PATH"
	}

	if models := discoverCodexModelsFromCache(); len(models) > 0 {
		return models, ""
	}
	return CodexCatalog, "codex: no model catalog cached, using built-in catalog"
}

// discoverCodexModelsFromCache reads the catalog modern Codex versions fetch
// from the API. This reflects the authenticated account's entitlements.
func discoverCodexModelsFromCache() []model.AgentModel {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "models_cache.json"))
	if err != nil {
		return nil
	}

	var cache struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(cache.Models))
	var models []model.AgentModel
	for _, item := range cache.Models {
		if item.Slug == "" {
			continue
		}
		if _, dup := seen[item.Slug]; dup {
			continue
		}
		seen[item.Slug] = struct{}{}
		name := item.DisplayName
		if name == "" {
			name = item.Slug
		}
		models = append(models, model.AgentModel{ID: item.Slug, Name: name})
	}
	return models
}
