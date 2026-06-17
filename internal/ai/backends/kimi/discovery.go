package kimi

import (
	"log/slog"
	"os/exec"

	"clawbench/internal/model"
)

func init() {
	model.RegisterDiscoverModelsFunc("kimi", DiscoverKimiModels)
}

// kimiDefaultModels lists known models for Kimi CLI.
var kimiDefaultModels = []model.AgentModel{
	{ID: "kimi-k2-0711-chat", Name: "Kimi K2"},
	{ID: "moonshot-v1-128k", Name: "Moonshot v1 128K"},
	{ID: "moonshot-v1-32k", Name: "Moonshot v1 32K"},
	{ID: "moonshot-v1-8k", Name: "Moonshot v1 8K"},
	{ID: "kimi-latest", Name: "Kimi Latest"},
}

// DiscoverKimiModels discovers models for Kimi CLI.
func DiscoverKimiModels() []model.AgentModel {
	if _, err := exec.LookPath("kimi"); err != nil {
		return nil
	}
	models := make([]model.AgentModel, len(kimiDefaultModels))
	copy(models, kimiDefaultModels)
	slog.Info("kimi model discovery: using hardcoded defaults", "models", len(models))
	return models
}
