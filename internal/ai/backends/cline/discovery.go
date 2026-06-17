package cline

import (
	"log/slog"
	"os/exec"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("cline", DiscoverClineModels)
}

// clineDefaultModels lists known models for Cline CLI.
var clineDefaultModels = []model.AgentModel{
	{ID: "anthropic/claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "anthropic/claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "openai/gpt-4.1", Name: "GPT-4.1"},
	{ID: "openai/gpt-4o", Name: "GPT-4o"},
	{ID: "openai/o3", Name: "o3"},
	{ID: "openai/o4-mini", Name: "o4-mini"},
	{ID: "minimax/MiniMax-M1", Name: "MiniMax-M1"},
	{ID: "minimax/MiniMax-M2.7", Name: "MiniMax-M2.7"},
}

// DiscoverClineModels discovers models for Cline CLI.
func DiscoverClineModels() []model.AgentModel {
	if _, err := exec.LookPath("cline"); err != nil {
		return nil
	}
	models := make([]model.AgentModel, len(clineDefaultModels))
	copy(models, clineDefaultModels)
	slog.Info("cline model discovery: using hardcoded defaults", "models", len(models))
	return models
}
