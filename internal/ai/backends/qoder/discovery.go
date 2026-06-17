package qoder

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("qoder", DiscoverQoderModels)
}

// qoderSkipModels are model IDs in the dynamic-texts.json that are tier-based
// selectors or routing aliases, not actual models.
var qoderSkipModels = map[string]bool{
	"auto":        true,
	"ultimate":    true,
	"performance": true,
	"efficient":   true,
	"lite":        true,
}

// qoderModelKeyRe matches keys like "modelSelector.item.qmodel" in the dynamic-texts JSON.
var qoderModelKeyRe = regexp.MustCompile(`^modelSelector\.item\.(.+)$`)

// DiscoverQoderModels discovers Qoder model IDs by reading the cached model catalog
// from ~/.qoder/.auth/dynamic-texts.json.
func DiscoverQoderModels() []model.AgentModel { //nolint:gocyclo // JSON-based model discovery
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("qoder model discovery: cannot determine home directory", "error", err)
		return nil
	}

	jsonPath := filepath.Join(homeDir, ".qoder", ".auth", "dynamic-texts.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		slog.Debug("qoder model discovery: dynamic-texts.json not found", "path", jsonPath, "error", err)
		return nil
	}

	var raw struct {
		Texts map[string]interface{} `json:"texts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Debug("qoder model discovery: failed to parse JSON", "error", err)
		return nil
	}

	if len(raw.Texts) == 0 {
		slog.Debug("qoder model discovery: empty texts in JSON")
		return nil
	}

	type modelInfo struct {
		id   string
		name string
	}
	var modelEntries []modelInfo

	for key, val := range raw.Texts {
		m := qoderModelKeyRe.FindStringSubmatch(key)
		if len(m) < 2 {
			continue
		}
		modelID := m[1]

		// Skip description/markdown suffixes
		if strings.HasSuffix(modelID, ".description") || strings.HasSuffix(modelID, ".markdownDescription") {
			continue
		}

		// Skip known tier/alias IDs
		if qoderSkipModels[modelID] {
			continue
		}

		// Skip experts-* entries
		if strings.HasPrefix(modelID, "experts-") {
			continue
		}

		// Skip quest-* entries
		if strings.HasPrefix(modelID, "quest-") {
			continue
		}

		// Skip internal preview/dogfooding models
		if strings.HasSuffix(modelID, "_preview") {
			continue
		}

		// Skip keys with dots in the remaining part (metadata like "lite.description.quest")
		if strings.Contains(modelID, ".") {
			continue
		}

		name := modelID
		if strVal, ok := val.(string); ok && strVal != "" {
			name = strVal
		}

		modelEntries = append(modelEntries, modelInfo{id: modelID, name: name})
	}

	if len(modelEntries) == 0 {
		return nil
	}

	var models []model.AgentModel
	for i, e := range modelEntries {
		models = append(models, model.AgentModel{
			ID:      e.id,
			Name:    e.name,
			Default: i == 0,
		})
	}

	slog.Info("qoder model discovery succeeded", "models", len(models))
	return models
}
