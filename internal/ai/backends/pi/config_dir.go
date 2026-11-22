package pi

import (
	"encoding/json"
	"fmt"

	"clawbench/internal/model"
)

// ModelsJSONContent renders pi's models.json.
//
// pi resolves a model as "<provider>/<model>", so the provider key is ours and
// the picker hands out the prefixed form (see ProviderModelIDFor).
//
// Registered as model.ProviderEnvSpec.ConfigDirRender.
func ModelsJSONContent(p *model.Provider) ([]byte, error) {
	id := model.ProviderConfigProviderID("pi")
	api := "openai-completions"
	if p.Protocol == model.ProviderProtocolAnthropic {
		api = "anthropic-messages"
	}
	models := model.ProviderModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}
	declared := make([]map[string]any, 0, len(models))
	for _, m := range models {
		declared = append(declared, map[string]any{"id": m, model.ProviderConfigJSONNameKey: m})
	}
	doc := map[string]any{
		"providers": map[string]any{
			id: map[string]any{
				model.ProviderConfigJSONNameKey: "ClawBench " + p.Name,
				"baseUrl":                       p.BaseURL,
				// pi expands $ENV in apiKey, so the secret stays in ClawBench's
				// database and is handed to the process through the environment.
				"apiKey": "$" + model.ProviderSecretEnvName(),
				"api":    api,
				"models": declared,
			},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}
