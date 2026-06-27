package pi

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
	model.RegisterDiscoverModelsFunc("pi", DiscoverPiModels)
}

// piModelLineRe matches lines from `pi --list-models` tabular output.
// Format: "provider        model                       context  max-out  thinking  images"
// We match any line with at least 2 whitespace-separated fields where the first
// doesn't look like a header.
var piModelLineRe = regexp.MustCompile(`^(\S+)\s+(\S+)`)

// ParsePiModels parses the output of `pi --list-models` into a list of AgentModel.
// Output format:
//
//	provider        model                       context  max-out  thinking  images
//	anthropic       claude-sonnet-4-6           1M       64K      yes       yes
//	openai          gpt-4o                      128K     4.1K     no        yes
//
// Models are prefixed with provider for disambiguation (e.g., "anthropic/claude-sonnet-4-6").
func ParsePiModels(output string) []model.AgentModel {
	var models []model.AgentModel
	for _, line := range strings.Split(output, "\n") {
		m := piModelLineRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		provider := m[1]
		modelID := m[2]
		// Skip header line
		if provider == "provider" || modelID == "model" {
			continue
		}
		fullID := provider + "/" + modelID
		models = append(models, model.AgentModel{
			ID:      fullID,
			Name:    fullID,
			Default: len(models) == 0,
		})
	}
	return models
}

// DiscoverPiModels discovers Pi model IDs by running `pi --list-models` and parsing the output.
// Pi outputs the model table to stderr (not stdout), so we must capture both streams.
func DiscoverPiModels() []model.AgentModel {
	// Try embedded binary first, fall back to PATH
	piCmd := "pi"
	if spec := model.FindSpecByBackend("pi"); spec != nil && spec.EmbeddedSubDir != "" {
		if p := model.EmbeddedBinaryPath(spec.EmbeddedSubDir); p != "" {
			piCmd = p
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, piCmd, "--list-models")
	// Pi outputs the model table to stderr; use CombinedOutput to capture both.
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("pi model discovery: command failed", "error", err)
		return nil
	}

	models := ParsePiModels(string(out))
	if len(models) == 0 {
		slog.Debug("pi model discovery: no models parsed")
		return nil
	}

	slog.Info("pi model discovery succeeded", "models", len(models))
	return models
}
