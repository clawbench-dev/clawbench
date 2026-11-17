package kimi

import "clawbench/internal/model"

// KimiCatalog is the known model set for Kimi CLI.
//
// A fallback list like this DOES go stale. When the CLI gains a real list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var KimiCatalog = []model.AgentModel{
	{ID: "kimi-k3", Name: "Kimi K3", Default: true},
	{ID: "kimi-k2-0711-chat", Name: "Kimi K2"},
	{ID: "kimi-for-coding", Name: "Kimi K2.7 Code"},
	{ID: "kimi-for-coding-highspeed", Name: "Kimi K2.7 Code Highspeed"},
	{ID: "moonshot-v1-128k", Name: "Moonshot v1 128K"},
	{ID: "moonshot-v1-32k", Name: "Moonshot v1 32K"},
	{ID: "moonshot-v1-8k", Name: "Moonshot v1 8K"},
	{ID: "kimi-latest", Name: "Kimi Latest"},
}
