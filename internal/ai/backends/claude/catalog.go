package claude

import "clawbench/internal/model"

// ClaudeCatalog is the fallback for binary string scanning of the claude CLI,
// which has no --list-models command. The claude binary is commonly stripped,
// so this path is reached often.
//
// A fallback list like this DOES go stale. When the CLI gains a real list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var ClaudeCatalog = []model.AgentModel{
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "claude-haiku-3-5-20241022", Name: "Claude 3.5 Haiku"},
}

// ClaudeModelNames maps model family to display label.
var ClaudeModelNames = map[string]string{
	"sonnet": "Sonnet",
	"opus":   "Opus",
	"haiku":  "Haiku",
}

// ClaudeModelOrder is the preferred display order: sonnet (default), opus, haiku.
var ClaudeModelOrder = map[string]int{"sonnet": 0, "opus": 1, "haiku": 2}
