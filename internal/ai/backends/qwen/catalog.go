package qwen

import "clawbench/internal/model"

// QwenCatalog is the model set Qwen Code's own service offers.
//
// Qwen Code has no model-list command (`qwen --help` exposes `-m/--model` but no
// `models` subcommand), so this list is maintained by hand. The ids come from
// the model table shipped inside the CLI package (`chunks/chunk-RZHZV5B2.js`,
// the reasoning-capability manifest, read on 2026-11-24 for 0.28.4).
//
// It is only a fallback for the picker BEFORE a session exists. Once a session
// is created, Qwen reports its own models in session/new and those win — which
// matters most for a bound provider, where the only real model is whatever the
// endpoint serves (the provider's list is what the picker shows then).
//
// A fallback list like this DOES go stale. When Qwen Code gains a list command,
// migrate the backend to model.PluginSource and delete this file rather than
// editing the list.
var QwenCatalog = []model.AgentModel{
	{ID: "qwen3.8-max", Name: "Qwen3.8 Max", Default: true},
	{ID: "qwen3.8-flash", Name: "Qwen3.8 Flash"},
	{ID: "qwen3.7-max", Name: "Qwen3.7 Max"},
	{ID: "qwen3.7-plus", Name: "Qwen3.7 Plus"},
	{ID: "qwen3.6-plus", Name: "Qwen3.6 Plus"},
	{ID: "qwen3.6-flash", Name: "Qwen3.6 Flash"},
	{ID: "qwen3.5-plus", Name: "Qwen3.5 Plus"},
	{ID: "qwen3-coder-plus", Name: "Qwen3 Coder Plus"},
	{ID: "coder-model", Name: "Coder Model (Qwen OAuth alias)"},
}
