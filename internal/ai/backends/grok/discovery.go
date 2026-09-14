package grok

import (
	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.NewCLISource("grok", model.CLIOptions{
		Command:  "grok",
		Args:     []string{"models"},
		Parse:    parseGrokModels,
		Fallback: model.GrokCatalog,
	}))
}

// parseGrokModels parses `grok models` output. It captures an explicit
// "Default model: xxx" line, both "*" and "-" bullet markers, deduplicates
// model IDs, and skips section headers.
func parseGrokModels(output string) []model.AgentModel {
	return model.ParseBulletList(output, model.BulletOptions{
		Names: model.GrokModelNames,
	})
}
