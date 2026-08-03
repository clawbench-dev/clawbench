package antigravity

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("antigravity", DiscoverAntigravityModels)
}

// antigravityDefaultModels lists known Antigravity models as a fallback when
// the binary is not found, the user is not authenticated, or `agy models`
// returns nothing parseable.
var antigravityDefaultModels = []model.AgentModel{
	{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
	{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
	{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
}

// antigravityModelNames maps known model IDs to pretty display names, so
// discovered models are named consistently with the fallback list.
var antigravityModelNames = map[string]string{
	"gemini-3.1-pro":   "Gemini 3.1 Pro",
	"gemini-3.1-flash": "Gemini 3.1 Flash",
	"gemini-3.5-flash": "Gemini 3.5 Flash",
	"gemini-3-pro":     "Gemini 3 Pro",
	"gemini-3-flash":   "Gemini 3 Flash",
	"gemini-2.5-pro":   "Gemini 2.5 Pro",
	"gemini-2.5-flash": "Gemini 2.5 Flash",
}

// agyLogPrefixRE matches Go log prefix style lines, e.g.
// "I0428 10:00:00.000000   1234 message" — mirrors the agy-acp bridge's
// /^[IWEF]\d{4}\s/ status-line filter.
var agyLogPrefixRE = regexp.MustCompile(`^[IWEF]\d{4}\s`)

// isAgyStatusLine reports whether a `agy models` line is diagnostic output
// rather than a model entry. Mirrors the agy-acp bridge's status filtering
// (logging prefixes, auth/error messages) so parsing stays in sync.
func isAgyStatusLine(line string) bool {
	switch {
	case line == "Fetching available models...":
		return true
	case line == "You are not logged into Antigravity":
		return true
	case strings.HasPrefix(line, "error "):
		return true
	case strings.Contains(line, "Failed to"):
		return true
	case agyLogPrefixRE.MatchString(line):
		return true
	}
	return false
}

// parseAgyModels parses `agy models` output. Each non-status line is a model
// ID (or a "Gemini 3.5 Flash" style display name). Duplicate lines collapse
// to a single entry, and the first entry is marked as the default.
func parseAgyModels(output string) []model.AgentModel {
	var ids []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isAgyStatusLine(line) {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		ids = append(ids, line)
	}

	models := make([]model.AgentModel, 0, len(ids))
	for i, id := range ids {
		models = append(models, model.AgentModel{
			ID:      id,
			Name:    antigravityModelName(id),
			Default: i == 0,
		})
	}
	return models
}

// antigravityModelName returns the pretty display name for a model ID,
// falling back to the raw ID when the model is unknown.
func antigravityModelName(id string) string {
	if name, ok := antigravityModelNames[id]; ok {
		return name
	}
	return id
}

// DiscoverAntigravityModels discovers Antigravity model IDs by running
// `agy models`. Falls back to known defaults when the CLI is unavailable,
// unauthenticated, or returns no parseable model lines.
func DiscoverAntigravityModels() []model.AgentModel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stdout, _, err := model.RunCommandContext(ctx, "agy", "models")
	if err != nil {
		slog.Debug("antigravity model discovery: command failed", "error", err)
		return antigravityDefaults()
	}

	models := parseAgyModels(stdout)
	if len(models) == 0 {
		slog.Debug("antigravity model discovery: no models parsed, using defaults")
		return antigravityDefaults()
	}

	slog.Info("antigravity model discovery succeeded", "models", len(models))
	return models
}

// antigravityDefaults returns a copy of the default model list with the first
// marked as default.
func antigravityDefaults() []model.AgentModel {
	models := make([]model.AgentModel, len(antigravityDefaultModels))
	copy(models, antigravityDefaultModels)
	if len(models) > 0 {
		models[0].Default = true
	}
	return models
}
