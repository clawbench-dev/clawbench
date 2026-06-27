package opencode

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
	model.RegisterDiscoverModelsFunc("opencode", DiscoverOpenCodeModels)
}

// opencodeModelLineRe matches lines like "minimax/MiniMax-M2.5" or "opencode/minimax-m2.5-free"
var opencodeModelLineRe = regexp.MustCompile(`^(\S+)/(\S+)$`)

// parseOpenCodeModels parses opencode models output.
// Output format: one "provider/model" per line, e.g.:
//
//	opencode/minimax-m2.5-free
//	minimax/MiniMax-M2.5
//	anthropic/claude-sonnet-4-6
//
// The Name field includes the provider prefix for disambiguation,
// since different providers may offer models with identical names.
// The first model is marked as default.
func parseOpenCodeModels(output string) []model.AgentModel {
	var models []model.AgentModel
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := opencodeModelLineRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}

		models = append(models, model.AgentModel{
			ID:      line,              // full "provider/model" as ID (opencode uses this format)
			Name:    m[1] + "/" + m[2], // include provider in display name for disambiguation
			Default: len(models) == 0,
		})
	}
	return models
}

// DiscoverOpenCodeModels discovers OpenCode model IDs by running `opencode models`
// and parsing the output. Uses embedded binary if available, falls back to PATH.
func DiscoverOpenCodeModels() []model.AgentModel {
	// Try embedded binary first, fall back to PATH
	opencodeCmd := "opencode"
	if spec := model.FindSpecByBackend("opencode"); spec != nil && spec.EmbeddedSubDir != "" {
		if p := model.EmbeddedBinaryPath(spec.EmbeddedSubDir); p != "" {
			opencodeCmd = p
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, opencodeCmd, "models")
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("opencode model discovery: command failed", "error", err)
		return nil
	}

	models := parseOpenCodeModels(string(out))
	if len(models) == 0 {
		slog.Debug("opencode model discovery: no models parsed")
		return nil
	}

	slog.Info("opencode model discovery succeeded", "models", len(models))
	return models
}
