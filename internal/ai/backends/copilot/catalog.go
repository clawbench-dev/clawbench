package copilot

import "clawbench/internal/model"

// CopilotCatalog is the known model set for GitHub Copilot CLI, which exposes
// no model-list command.
//
// A fallback list like this DOES go stale. When the CLI gains a real list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var CopilotCatalog = []model.AgentModel{
	{ID: "gpt-4.1", Name: "GPT-4.1"},
	{ID: "gpt-4o", Name: "GPT-4o"},
	{ID: "o3", Name: "o3"},
	{ID: "o4-mini", Name: "o4-mini"},
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
}
