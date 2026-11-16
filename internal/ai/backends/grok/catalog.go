package grok

import "clawbench/internal/model"

// GrokCatalog is the fallback for `grok models`.
//
// A fallback list like this DOES go stale. When the CLI gains a reliable list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var GrokCatalog = []model.AgentModel{
	{ID: "grok-4.5", Name: "Grok 4.5"},
	{ID: "grok-build", Name: "Grok Build"},
}

// GrokModelNames maps discovered IDs to display names.
var GrokModelNames = map[string]string{
	"grok":        "Grok",
	"grok-code":   "Grok Code",
	"grok-4.5":    "Grok 4.5",
	"grok-3":      "Grok 3",
	"grok-3-mini": "Grok 3 Mini",
	"grok-build":  "Grok Build",
}
