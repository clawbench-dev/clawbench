package grok

import "clawbench/internal/model"

// GrokCatalog is the fallback for `grok models` when the CLI is missing or its
// output cannot be parsed.
//
// A fallback list like this DOES go stale. When the CLI gains a reliable list
// command, migrate the backend to that probe and delete this file rather than
// editing the list.
var GrokCatalog = []model.AgentModel{
	{ID: "grok-4.6", Name: "Grok 4.6"},
	{ID: "grok-4.5", Name: "Grok 4.5"},
}

// GrokModelNames maps discovered IDs to display names.
var GrokModelNames = map[string]string{
	"grok":        "Grok",
	"grok-code":   "Grok Code",
	"grok-4.6":    "Grok 4.6",
	"grok-4.5":    "Grok 4.5",
	"grok-3":      "Grok 3",
	"grok-3-mini": "Grok 3 Mini",
	"grok-build":  "Grok Build",
}
