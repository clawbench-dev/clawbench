package codebuddy

import (
	"encoding/json"
	"fmt"

	"clawbench/internal/model"
)

// ModelsJSONContent renders CodeBuddy's models.json.
//
// Each model carries its own url and apiKey, and that url WINS over
// CODEBUDDY_BASE_URL (verified: a model with a url reaches that host even when
// CODEBUDDY_BASE_URL names a dead one). apiKey supports ${ENV}, so the secret is
// not written to disk.
//
// Registered as model.ProviderEnvSpec.ConfigDirRender, so the writer in
// internal/service no longer keeps a per-backend switch.
func ModelsJSONContent(p *model.Provider) ([]byte, error) {
	models := model.ProviderModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}
	declared := make([]map[string]any, 0, len(models))
	for _, m := range models {
		declared = append(declared, map[string]any{
			"id":                            m,
			model.ProviderConfigJSONNameKey: m,
			"provider":                      "openai",
			"url":                           p.BaseURL,
			"apiKey":                        "${" + model.ProviderSecretEnvName() + "}",
			"supportsToolCall":              true,
		})
	}
	return json.MarshalIndent(map[string]any{"models": declared}, "", "  ")
}
