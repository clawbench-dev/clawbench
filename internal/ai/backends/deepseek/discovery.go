package deepseek

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
	model.RegisterDiscoverModelsFunc("deepseek", DiscoverDeepSeekModels)
}

// deepseekModelLineRe matches lines like "  deepseek-v4-flash (deepseek)" or "* deepseek-v4-pro (deepseek)".
var deepseekModelLineRe = regexp.MustCompile(`^(\*?)\s*(\S+)\s+\((\S+)\)`)

// deepseekDefaultRe extracts the default model from the header line.
var deepseekDefaultRe = regexp.MustCompile(`Available models \(default:\s*(\S+)\)`)

// parseDeepSeekModels parses deepseek models output.
// Output format:
//
//	Available models (default: deepseek-v4-pro)
//	  deepseek-v4-flash (deepseek)
//	* deepseek-v4-pro (deepseek)
//
// The Name field includes the provider prefix for disambiguation (e.g., "deepseek/deepseek-v4-pro"),
// consistent with Pi and OpenCode model naming.
func parseDeepSeekModels(output string) []model.AgentModel {
	// Extract default model name from header
	var defaultModel string
	if m := deepseekDefaultRe.FindStringSubmatch(output); len(m) >= 2 {
		defaultModel = m[1]
	}

	var models []model.AgentModel
	for _, line := range strings.Split(output, "\n") {
		m := deepseekModelLineRe.FindStringSubmatch(line)
		if len(m) < 4 {
			continue
		}
		isDefault := m[1] == "*" || m[2] == defaultModel || (defaultModel == "" && len(models) == 0)
		modelID := m[2]
		provider := m[3]

		// Only include the native deepseek provider
		if !strings.EqualFold(provider, "deepseek") {
			continue
		}

		fullID := provider + "/" + modelID
		models = append(models, model.AgentModel{
			ID:      fullID,
			Name:    fullID,
			Default: isDefault,
		})
	}
	return models
}

// DiscoverDeepSeekModels discovers DeepSeek/CodeWhale model IDs by running the CLI
// and parsing the output. Tries "codewhale models" first, falls back to "deepseek models".
func DiscoverDeepSeekModels() []model.AgentModel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try primary command first
	cmd := exec.CommandContext(ctx, "codewhale", "models")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to legacy command
		cmd = exec.CommandContext(ctx, "deepseek", "models")
		out, err = cmd.CombinedOutput()
		if err != nil {
			slog.Debug("deepseek model discovery: command failed", "error", err)
			return nil
		}
	}

	models := parseDeepSeekModels(string(out))
	if len(models) == 0 {
		slog.Debug("deepseek model discovery: no models parsed")
		return nil
	}

	slog.Info("deepseek model discovery succeeded", "models", len(models))
	return models
}
