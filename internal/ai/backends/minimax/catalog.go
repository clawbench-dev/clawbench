package minimax

import "clawbench/internal/model"

// MiniMaxCatalog is the picker list the runtime advertised at session/new
// (mcode 0.7.9, 2026-11-24) for an unconfigured install.
//
// A fallback list like this DOES go stale. Its only job is the window BEFORE
// the first handshake: once session/new answers, its `model` config option is
// the authoritative list (ResolveModels then replaces membership, order and
// names with it, including any BYOK provider's models), so these entries stop
// showing as soon as the agent reports its own. Keep them in step with the
// shipped default rather than growing them into a frontier catalog.
var MiniMaxCatalog = []model.AgentModel{
	{ID: "MiniMax-M3.1-Flash", Name: "MiniMax M3.1 Flash"},
	{ID: "MiniMax-M3", Name: "MiniMax M3"},
}
