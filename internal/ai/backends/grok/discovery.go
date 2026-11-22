package grok

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"clawbench/internal/model"
)

// discoverGrokModels lists models by running `grok models`.
//
// Listing is a CLI feature, not a chat transport: it works without signing in
// (the command still prints the catalog and marks its default), so it is what
// gives the picker a list before the first ACP handshake. The probe falls back
// to GrokCatalog when the binary is missing or the output cannot be parsed.
func discoverGrokModels() ([]model.AgentModel, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "grok", "models").Output()
	if err != nil {
		return GrokCatalog, fmt.Sprintf("grok: `grok models` failed: %v", err)
	}
	if models := parseGrokModels(string(out)); len(models) > 0 {
		return models, ""
	}
	return GrokCatalog, "grok: no models parsed from `grok models`"
}

// parseGrokModels parses `grok models` output. It captures an explicit
// "Default model: xxx" line, both "*" and "-" bullet markers, deduplicates
// model IDs, and skips section headers.
func parseGrokModels(output string) []model.AgentModel {
	return model.ParseBulletList(output, model.BulletOptions{
		Names: GrokModelNames,
	})
}
