package grok

import (
	"context"
	"log/slog"
	"os/exec"
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

// parseGrokModels parses `grok models` output.
// Format (each available model on its own line, default marked):
//
//	Default model: grok-4.5
//
//	Available models:
//	  * grok-4.5 (default)
//	  * grok-build
//
// Only lines beginning with "* " are parsed; the "(default)" suffix marks the
// default model. The first model is used as fallback default when the suffix
// is absent.
func parseGrokModels(output string) []model.AgentModel {
	var models []model.AgentModel
	defaultSeen := false

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "* ") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(line, "* "))
		if id == "" {
			continue
		}
		isDefault := false
		if strings.HasSuffix(id, " (default)") {
			isDefault = true
			id = strings.TrimSuffix(id, " (default)")
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, model.AgentModel{
			ID:      id,
			Name:    id,
			Default: isDefault,
		})
		if isDefault {
			defaultSeen = true
		}
	}

	// If no model carried "(default)", mark the first as default.
	if !defaultSeen && len(models) > 0 {
		models[0].Default = true
	}
	return models
}

// DiscoverGrokModels discovers Grok model IDs by running `grok models`.
// Falls back to known defaults when the CLI is unavailable, unauthenticated,
// or returns no parseable model lines.
func DiscoverGrokModels() []model.AgentModel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grok", "models")
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("grok model discovery: command failed", "error", err)
		return grokDefaults()
	}

	models := parseGrokModels(string(out))
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
