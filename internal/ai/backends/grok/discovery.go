package grok

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("grok", DiscoverGrokModels)
}

// grokDefaultModels lists known Grok models as a fallback when the binary is
// not found, the user is not authenticated, or `grok models` returns nothing.
var grokDefaultModels = []model.AgentModel{
	{ID: "grok-4.5", Name: "Grok 4.5"},
	{ID: "grok-build", Name: "Grok Build"},
}

// grokModelNames maps known model IDs to pretty display names, so discovered
// models are named consistently with the fallback list.
var grokModelNames = map[string]string{
	"grok":        "Grok",
	"grok-code":   "Grok Code",
	"grok-4.5":    "Grok 4.5",
	"grok-3":      "Grok 3",
	"grok-3-mini": "Grok 3 Mini",
	"grok-build":  "Grok Build",
}

// grokModelLineRe matches a model list line, allowing both "*" and "-" bullets:
//
//   - grok-4.5 (default)
//   - grok (default)
//   - grok-code
var grokModelLineRe = regexp.MustCompile(`^[\s*+-]*([A-Za-z0-9][A-Za-z0-9._-]*)(?:\s|\(|$)`)

// parseGrokModels parses `grok models` output.
// It captures an explicit "Default model: xxx" line, both "*" and "-" bullet
// markers, deduplicates model IDs, and skips section headers. At most one model
// is marked default: an explicit "(default)" suffix wins, then the "Default
// model:" value, then the first listed model.
func parseGrokModels(output string) []model.AgentModel {
	var models []model.AgentModel
	seen := make(map[string]bool)
	defaultMarked := false
	defaultID := ""

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Capture explicit "Default model: xxx"
		if strings.HasPrefix(strings.ToLower(trimmed), "default model:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				defaultID = strings.TrimSpace(parts[1])
			}
			continue
		}

		// Only consider list-looking lines.
		if !strings.Contains(trimmed, "-") && !strings.Contains(trimmed, "*") {
			continue
		}

		m := grokModelLineRe.FindStringSubmatch(trimmed)
		if len(m) < 2 {
			continue
		}
		id := m[1]
		// Skip section headers / non-model tokens.
		lower := strings.ToLower(id)
		if lower == "available" || lower == "models" || lower == "default" || lower == "model" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true

		isDefault := strings.Contains(strings.ToLower(trimmed), "(default)") && !defaultMarked
		if isDefault {
			defaultMarked = true
		}

		models = append(models, model.AgentModel{
			ID:      id,
			Name:    grokModelName(id),
			Default: isDefault,
		})
	}

	// Ensure exactly one default when possible.
	if len(models) > 0 {
		hasDefault := false
		for _, m := range models {
			if m.Default {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			// Prefer matching defaultID, else first entry.
			if defaultID != "" {
				for i := range models {
					if models[i].ID == defaultID {
						models[i].Default = true
						hasDefault = true
						break
					}
				}
			}
			if !hasDefault {
				models[0].Default = true
			}
		}
	}

	return models
}

// grokModelName returns the pretty display name for a model ID, falling back
// to the raw ID when the model is unknown.
func grokModelName(id string) string {
	if name, ok := grokModelNames[id]; ok {
		return name
	}
	return id
}

// DiscoverGrokModels discovers Grok model IDs by running `grok models`.
// Falls back to known defaults when the CLI is unavailable, unauthenticated,
// or returns no parseable model lines.
func DiscoverGrokModels() []model.AgentModel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stdout, _, err := model.RunCommandContext(ctx, "grok", "models")
	if err != nil {
		slog.Debug("grok model discovery: command failed", "error", err)
		return grokDefaults()
	}

	models := parseGrokModels(stdout)
	if len(models) == 0 {
		slog.Debug("grok model discovery: no models parsed, using defaults")
		return grokDefaults()
	}

	slog.Info("grok model discovery succeeded", "models", len(models))
	return models
}

// grokDefaults returns a copy of the default model list with the first marked
// as default.
func grokDefaults() []model.AgentModel {
	models := make([]model.AgentModel, len(grokDefaultModels))
	copy(models, grokDefaultModels)
	if len(models) > 0 {
		models[0].Default = true
	}
	return models
}
