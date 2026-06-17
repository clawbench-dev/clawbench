package claude

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterDiscoverModelsFunc("claude", DiscoverClaudeModels)
}

// claudeDefaultModels lists known Claude models as a fallback when binary
// scanning fails (e.g. claude CLI not found or ExtractStrings returns nothing).
var claudeDefaultModels = []model.AgentModel{
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "claude-haiku-3-5-20241022", Name: "Claude 3.5 Haiku"},
}

// claudeModelRe matches Claude model IDs like "claude-sonnet-4-6" or "claude-opus-4-5" from strings output.
// Requires exactly two version segments (major-minor), excludes:
// - date-stamped like "claude-opus-4-20250514" (8-digit date suffix)
// - short aliases like "claude-sonnet-4" (points to latest snapshot)
var claudeModelRe = regexp.MustCompile(`^claude-(sonnet|opus|haiku)-\d+-\d+$`)

// claudeModelOrder defines the preferred display order: sonnet first (default), then opus, then haiku.
var claudeModelOrder = map[string]int{"sonnet": 0, "opus": 1, "haiku": 2}

// claudeModelNames maps model ID prefixes to human-readable names.
var claudeModelNames = map[string]string{
	"sonnet": "Sonnet",
	"opus":   "Opus",
	"haiku":  "Haiku",
}

// claudeConfigDir returns the Claude config directory (~/.claude/).
// Overridable for testing (same pattern as DiscoverModels variable).
var claudeConfigDir = platform.ClaudeConfigDir

// LoadClaudeModelOverrides reads ~/.claude/settings.json and returns the
// modelOverrides map if present. Returns nil on any error (missing file,
// invalid JSON, no overrides key) — graceful degradation.
func LoadClaudeModelOverrides() map[string]string {
	path := filepath.Join(claudeConfigDir(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("claude model overrides: settings.json not found", "path", path, "error", err)
		return nil
	}
	var cfg struct {
		ModelOverrides map[string]string `json:"modelOverrides"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Debug("claude model overrides: invalid JSON", "path", path, "error", err)
		return nil
	}
	if len(cfg.ModelOverrides) == 0 {
		return nil
	}
	return cfg.ModelOverrides
}

// claudeIsDateStamped returns true if the model ID contains an 8-digit date segment
// like "claude-opus-4-20250514", which are snapshot aliases we want to skip.
func claudeIsDateStamped(modelID string) bool {
	for _, seg := range strings.Split(modelID, "-") {
		if len(seg) == 8 {
			return true
		}
	}
	return false
}

// DiscoverClaudeModels discovers Claude model IDs by scanning the claude binary
// with `strings`. Claude CLI does not have a --list-models command, so we extract
// model IDs from the binary which contains hardcoded model name patterns.
func DiscoverClaudeModels() []model.AgentModel { //nolint:gocyclo,gocognit // binary scanning model discovery
	// Resolve the real path for the claude binary, handling Windows .cmd wrappers
	path := platform.ResolveCLIPath("claude")
	if path == "" {
		// Claude binary not found — fall back to known defaults
		models := make([]model.AgentModel, len(claudeDefaultModels))
		copy(models, claudeDefaultModels)
		if len(models) > 0 {
			models[0].Default = true
		}
		slog.Info("claude model discovery: binary not found, using defaults", "models", len(models))
		return models
	}

	// Extract printable strings from the binary (cross-platform replacement for
	// the POSIX "strings" command, which does not exist on Windows)
	lines, err := platform.ExtractStrings(path, 4)
	if err != nil {
		slog.Debug("claude model discovery: extract strings failed", "error", err)
		return nil
	}

	// Extract unique model IDs matching the pattern
	seen := make(map[string]bool)
	var models []model.AgentModel
	for _, line := range lines {
		if !claudeModelRe.MatchString(line) || seen[line] {
			continue
		}
		// Skip date-stamped versions like claude-opus-4-20250514
		if claudeIsDateStamped(line) {
			continue
		}
		seen[line] = true

		// Generate human-readable name: claude-sonnet-4-6 → "Claude Sonnet 4.6"
		parts := strings.SplitN(line, "-", 3) // ["claude", "sonnet", "4-6"]
		name := line
		if len(parts) == 3 {
			if family, ok := claudeModelNames[parts[1]]; ok {
				version := strings.ReplaceAll(parts[2], "-", ".")
				name = "Claude " + family + " " + version
			}
		}

		models = append(models, model.AgentModel{
			ID:   line,
			Name: name,
		})
	}

	// Sort: sonnet first, then opus, then haiku; within each family, newest first
	sort.Slice(models, func(i, j int) bool {
		familyI := strings.SplitN(models[i].ID, "-", 3)
		familyJ := strings.SplitN(models[j].ID, "-", 3)
		if len(familyI) >= 2 && len(familyJ) >= 2 {
			orderI, okI := claudeModelOrder[familyI[1]]
			orderJ, okJ := claudeModelOrder[familyJ[1]]
			if okI && okJ && orderI != orderJ {
				return orderI < orderJ
			}
		}
		// Same family: sort by ID descending (newest first)
		return models[i].ID > models[j].ID
	})

	// Mark first model as default
	if len(models) > 0 {
		models[0].Default = true
	}

	// Apply model name overrides from ~/.claude/settings.json
	// When modelOverrides maps a Claude model ID to another name (e.g. "MiniMax-M2.7"),
	// we replace the display name so the user sees which underlying model is actually used.
	// The model ID is NOT changed — CLI invocation always uses the original Claude model ID.
	if overrides := LoadClaudeModelOverrides(); len(overrides) > 0 {
		for i := range models {
			if name, ok := overrides[models[i].ID]; ok {
				slog.Debug("claude model override applied", "id", models[i].ID, "name", name)
				models[i].Name = name
			}
		}
	}

	// If binary scanning found no models, fall back to known defaults
	if len(models) == 0 {
		models = make([]model.AgentModel, len(claudeDefaultModels))
		copy(models, claudeDefaultModels)
		if len(models) > 0 {
			models[0].Default = true
		}
		slog.Info("claude model discovery: binary scan found nothing, using defaults", "models", len(models))
		return models
	}

	return models
}
