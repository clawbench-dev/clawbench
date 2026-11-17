package backends

import (
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// Capabilities returns the static capability snapshot for one registered
// backend. The bool is false when the backend is not registered.
//
// This is the ONE place that answers "what can this backend do". Before it,
// every caller had to know the mapping itself: a CLI factory means HasCLI, a
// non-nil MidTurn means mid-turn injection, a non-empty ProviderEnvSpec means
// provider binding. ProviderEnvSpec additionally had no vocabulary for the
// DELIVERY METHODS (env vs config-dir vs config-file …), so that taxonomy
// existed only in prose.
//
// The snapshot is pure: it reads the plugin and the registries, and never
// contacts an agent. Runtime facts (models, modes, thinking levels) are not
// here by design.
func Capabilities(backendID string) (model.BackendCapabilities, bool) {
	p := Lookup(backendID)
	if p == nil {
		return model.BackendCapabilities{}, false
	}
	caps := model.BackendCapabilities{
		HasCLI: ai.BackendSupportsCLI(backendID),
		// ACP support is the spawn command, not the mapping data: backends with
		// plain ACP (claude, cursor) leave ACP nil and use the generic remaps.
		HasACP:                 p.Spec.AcpCommand != "",
		SupportsMidTurn:        p.MidTurn != nil,
		MidTurnSplitsReply:     p.MidTurnSplitsReply,
		HasModelSource:         model.HasModelSource(backendID),
		CanBindProvider:        model.ProviderEnvSupported(backendID, ""),
		ProviderProtocols:      model.SupportedProtocols(backendID),
		ProviderMethods:        model.ProviderMethods(backendID),
		ProviderKeyOnly:        model.BackendIsKeyOnly(backendID),
		ProviderSecretInFile:   model.ProviderConfigSecretInFile(backendID),
		ProviderDeclaresModels: model.ProviderConfigDirDeclaresModels(backendID),
	}
	if p.ACP != nil {
		caps.ACPModelSwitchMethod = p.ACP.ModelSwitchMethod
		caps.ACPModesMirrorThoughtLevel = p.ACP.ModesMirrorThoughtLevel
	}
	return caps, true
}

// AllCapabilities returns the snapshot for every registered backend, keyed by
// backend ID. Iteration order is unspecified; callers that display the table
// sort the keys (or use AllSpecsSorted for display order).
func AllCapabilities() map[string]model.BackendCapabilities {
	plugins := All()
	out := make(map[string]model.BackendCapabilities, len(plugins))
	for _, p := range plugins {
		caps, ok := Capabilities(p.ID)
		if !ok {
			continue
		}
		out[p.ID] = caps
	}
	return out
}
