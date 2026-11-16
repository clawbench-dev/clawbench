package antigravity

import "clawbench/internal/model"

// AntigravityCatalog is the fallback for `agy models` when the CLI is missing,
// unauthenticated, or returns nothing parseable.
//
// A fallback list like this DOES go stale. When the CLI gains a real list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var AntigravityCatalog = []model.AgentModel{
	{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
	{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
	{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
}

// AntigravityModelNames maps discovered IDs to the same display names the
// catalog uses, so a discovered model and a fallback entry render identically.
var AntigravityModelNames = map[string]string{
	"gemini-3.1-pro":   "Gemini 3.1 Pro",
	"gemini-3.1-flash": "Gemini 3.1 Flash",
	"gemini-3.5-flash": "Gemini 3.5 Flash",
	"gemini-3-pro":     "Gemini 3 Pro",
	"gemini-3-flash":   "Gemini 3 Flash",
	"gemini-2.5-pro":   "Gemini 2.5 Pro",
	"gemini-2.5-flash": "Gemini 2.5 Flash",
}
