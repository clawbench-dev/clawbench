package mimo

import (
	"log/slog"
	"os/exec"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("mimo", DiscoverMimoModels)
}

// mimoDefaultModels lists known models for MiMo-Code CLI.
var mimoDefaultModels = []model.AgentModel{
	{ID: "mimo/mimo-auto", Name: "MiMo Auto", Default: true},
	{ID: "xiaomi/mimo-v2.5-pro-ultraspeed", Name: "MiMo V2.5 Pro Ultraspeed"},
	{ID: "xiaomi/mimo-v2.5-pro", Name: "MiMo V2.5 Pro"},
	{ID: "xiaomi/mimo-v2.5", Name: "MiMo V2.5"},
	{ID: "xiaomi/mimo-v2-pro", Name: "MiMo V2 Pro"},
	{ID: "xiaomi/mimo-v2-omni", Name: "MiMo V2 Omni"},
	{ID: "xiaomi/mimo-v2-flash", Name: "MiMo V2 Flash"},
}

// DiscoverMimoModels discovers models for MiMo-Code CLI.
func DiscoverMimoModels() []model.AgentModel {
	if _, err := exec.LookPath("mimo"); err != nil {
		return nil
	}
	models := make([]model.AgentModel, len(mimoDefaultModels))
	copy(models, mimoDefaultModels)
	slog.Info("mimo model discovery: using hardcoded defaults", "models", len(models))
	return models
}
