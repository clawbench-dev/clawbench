package copilot

import (
	"log/slog"
	"os/exec"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("copilot", DiscoverCopilotModels)
}

// copilotDefaultModels lists known models for GitHub Copilot CLI.
var copilotDefaultModels = []model.AgentModel{
	{ID: "gpt-4.1", Name: "GPT-4.1"},
	{ID: "gpt-4o", Name: "GPT-4o"},
	{ID: "o3", Name: "o3"},
	{ID: "o4-mini", Name: "o4-mini"},
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
}

// DiscoverCopilotModels discovers models for GitHub Copilot CLI.
func DiscoverCopilotModels() []model.AgentModel {
	if _, err := exec.LookPath("copilot"); err != nil {
		return nil
	}
	models := make([]model.AgentModel, len(copilotDefaultModels))
	copy(models, copilotDefaultModels)
	slog.Info("copilot model discovery: using hardcoded defaults", "models", len(models))
	return models
}
