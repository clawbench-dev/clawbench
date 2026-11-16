package mimo

import "clawbench/internal/model"

// MimoCatalog is the known model set for MiMo-Code CLI.
//
// A fallback list like this DOES go stale. When the CLI gains a real list
// command, migrate the backend to model.NewCLISource and delete this file
// rather than editing the list.
var MimoCatalog = []model.AgentModel{
	{ID: "mimo/mimo-auto", Name: "MiMo Auto", Default: true},
	{ID: "xiaomi/mimo-v2.5-pro-ultraspeed", Name: "MiMo V2.5 Pro Ultraspeed"},
	{ID: "xiaomi/mimo-v2.5-pro", Name: "MiMo V2.5 Pro"},
	{ID: "xiaomi/mimo-v2.5", Name: "MiMo V2.5"},
	{ID: "xiaomi/mimo-v2-pro", Name: "MiMo V2 Pro"},
	{ID: "xiaomi/mimo-v2-omni", Name: "MiMo V2 Omni"},
	{ID: "xiaomi/mimo-v2-flash", Name: "MiMo V2 Flash"},
}
