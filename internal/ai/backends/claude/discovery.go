package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterModelSource(model.PluginSource("claude", discoverClaudeModels))
}

// claudeModelRe matches model IDs like "claude-sonnet-4-6". It requires exactly
// two version segments, excluding date-stamped snapshots
// ("claude-opus-4-20250514") and short aliases ("claude-sonnet-4").
var claudeModelRe = regexp.MustCompile(`^claude-(sonnet|opus|haiku)-\d+-\d+$`)

// claudeConfigDir is overridable in tests.
var claudeConfigDir = platform.ClaudeConfigDir

// discoverClaudeModels discovers Claude models by scanning the claude binary for
// printable strings. The claude CLI has no --list-models command and the binary
// is frequently stripped, so the catalog fallback is commonly reached.
func discoverClaudeModels() ([]model.AgentModel, string) {
	path := platform.ResolveCLIPath("claude")
	if path == "" {
		return model.ClaudeCatalog, "claude: CLI not found on PATH, using built-in catalog"
	}

	lines, err := platform.ExtractStrings(path, 4)
	if err != nil {
		return model.ClaudeCatalog, fmt.Sprintf("claude: cannot extract strings from %s: %v", path, err)
	}

	models := parseClaudeModels(lines)
	if len(models) == 0 {
		return model.ClaudeCatalog, fmt.Sprintf("claude: no model IDs found in %s (binary likely stripped)", path)
	}

	models = applyClaudeOverrides(models)
	sortClaudeModels(models)
	return models, ""
}

// parseClaudeModels extracts unique, non-date-stamped model IDs and gives each a
// readable name ("claude-sonnet-4-6" → "Claude Sonnet 4.6").
func parseClaudeModels(lines []string) []model.AgentModel {
	seen := make(map[string]bool)
	var models []model.AgentModel

	for _, line := range lines {
		if !claudeModelRe.MatchString(line) || seen[line] || isClaudeDateStamped(line) {
			continue
		}
		seen[line] = true

		name := line
		if parts := strings.SplitN(line, "-", 3); len(parts) == 3 {
			if family, ok := model.ClaudeModelNames[parts[1]]; ok {
				name = "Claude " + family + " " + strings.ReplaceAll(parts[2], "-", ".")
			}
		}
		models = append(models, model.AgentModel{ID: line, Name: name})
	}
	return models
}

// isClaudeDateStamped reports whether an ID contains an 8-digit date segment,
// marking it as a snapshot alias we do not offer.
func isClaudeDateStamped(modelID string) bool {
	for _, seg := range strings.Split(modelID, "-") {
		if len(seg) == 8 {
			return true
		}
	}
	return false
}

// applyClaudeOverrides replaces display names using ~/.claude/settings.json's
// modelOverrides, so a user who redirected a tier to another provider sees the
// real backing model. IDs are never changed — CLI invocation still uses the
// original Claude model ID.
//
// Overridden entries that collapse to the same display name are deduplicated,
// keeping the highest-priority (first) occurrence: otherwise a redirected setup
// shows the same real model several times under different tier names.
//
// The result is written back through a fresh slice: reusing the input's backing
// array via models[:0] would leave stale copies past the new length, and the
// caller (and tests) would see duplicated entries.
func applyClaudeOverrides(models []model.AgentModel) []model.AgentModel {
	overrides := loadClaudeModelOverrides()
	if len(overrides) == 0 {
		return models
	}
	seenNames := make(map[string]bool, len(models))
	kept := make([]model.AgentModel, 0, len(models))
	for i := range models {
		if name, ok := overrides[models[i].ID]; ok {
			models[i].Name = name
		}
		if seenNames[models[i].Name] {
			continue
		}
		seenNames[models[i].Name] = true
		kept = append(kept, models[i])
	}
	return kept
}

// sortClaudeModels orders by family (sonnet, opus, haiku) then newest ID first.
func sortClaudeModels(models []model.AgentModel) {
	sort.Slice(models, func(i, j int) bool {
		fi := strings.SplitN(models[i].ID, "-", 3)
		fj := strings.SplitN(models[j].ID, "-", 3)
		if len(fi) >= 2 && len(fj) >= 2 {
			oi, okI := model.ClaudeModelOrder[fi[1]]
			oj, okJ := model.ClaudeModelOrder[fj[1]]
			if okI && okJ && oi != oj {
				return oi < oj
			}
		}
		return models[i].ID > models[j].ID
	})
}

// loadClaudeModelOverrides reads ~/.claude/settings.json's modelOverrides map.
// Returns nil on any error — a missing or malformed settings file is normal.
func loadClaudeModelOverrides() map[string]string {
	data, err := os.ReadFile(filepath.Join(claudeConfigDir(), "settings.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		ModelOverrides map[string]string `json:"modelOverrides"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg.ModelOverrides
}
