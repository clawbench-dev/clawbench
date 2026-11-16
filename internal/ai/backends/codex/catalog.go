package codex

import "clawbench/internal/model"

// CodexCatalog is the last-resort fallback for Codex. Codex has no model-list
// command and ships a stripped Rust binary, so string extraction frequently
// fails; this list is updated manually from OpenAI's catalog.
//
// A fallback list like this DOES go stale — check it against the CLI before
// assuming a missing model is a discovery bug.
var CodexCatalog = []model.AgentModel{
	{ID: "gpt-5.5", Name: "GPT-5.5", Default: true},
	{ID: "gpt-5.4", Name: "GPT-5.4"},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini"},
}

// CodexModelOrder is the preferred display order for Codex models.
var CodexModelOrder = map[string]int{
	"gpt-5.5":      0,
	"gpt-5.4":      1,
	"gpt-5.4-mini": 2,
	"o3":           3,
	"o4-mini":      4,
}
