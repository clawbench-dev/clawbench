package grok

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("grok", DiscoverGrokModels)
}

// grokDefaultModels is used when `grok models` is unavailable or unparseable.
var grokDefaultModels = []model.AgentModel{
	{ID: "grok", Name: "Grok", Default: true},
	{ID: "grok-4.5", Name: "Grok 4.5"},
}

// grokModelLineRe matches lines like:
//
//   - grok-4.5
//   - grok (default)
//   - grok-code
var grokModelLineRe = regexp.MustCompile(`^[\s*+-]*([A-Za-z0-9][A-Za-z0-9._-]*)(?:\s|\(|$)`)

// parseGrokModels parses `grok models` human-readable output.
func parseGrokModels(output string) []model.AgentModel {
	var models []model.AgentModel
	seen := make(map[string]bool)
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

		// Only consider list-looking lines under "Available models" section.
		if !strings.Contains(line, "-") && !strings.Contains(line, "*") {
			continue
		}

		m := grokModelLineRe.FindStringSubmatch(trimmed)
		if len(m) < 2 {
			continue
		}
		id := m[1]
		// Skip section headers / non-model tokens
		lower := strings.ToLower(id)
		if lower == "available" || lower == "models" || lower == "default" || lower == "model" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true

		isDefault := strings.Contains(strings.ToLower(trimmed), "(default)") ||
			strings.HasPrefix(strings.TrimSpace(line), "*") ||
			(defaultID != "" && id == defaultID)

		models = append(models, model.AgentModel{
			ID:      id,
			Name:    id,
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

// DiscoverGrokModels discovers models via `grok models`, falling back to defaults
// when the CLI is present but output cannot be parsed.
func DiscoverGrokModels() []model.AgentModel {
	if _, err := exec.LookPath("grok"); err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grok", "models")
	out, err := cmd.Output()
	if err == nil {
		if models := parseGrokModels(string(out)); len(models) > 0 {
			slog.Info("grok model discovery succeeded", "models", len(models))
			return models
		}
		slog.Debug("grok model discovery: no models parsed, using defaults")
	} else {
		slog.Debug("grok model discovery: command failed, using defaults", "error", err)
	}

	models := make([]model.AgentModel, len(grokDefaultModels))
	copy(models, grokDefaultModels)
	slog.Info("grok model discovery: using hardcoded defaults", "models", len(models))
	return models
}
