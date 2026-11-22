package backends

import (
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

func init() {
	// Wire up the ACP lookup function variables in internal/ai so that
	// ACP event mapping code can query backend-specific data without
	// importing the backends package (avoiding import cycles).
	ai.LookupACPRemapsFn = LookupACPRemaps
	ai.LookupACPToolCallIDPrefixesFn = LookupACPToolCallIDPrefixes
	ai.LookupACPTitlePrefixesFn = LookupACPTitlePrefixes

	// Wire up the wrapper-frame filter so the ACP event loop can drop a
	// backend's "tool_call: id: X, args: {...}" envelope frames without
	// importing the backends package (openclaw emits one before every real
	// plugin/MCP tool frame).
	ai.LookupACPToolCallWrapperPrefixesFn = LookupACPToolCallWrapperPrefixes

	// Wire up the "modes duplicate thought_level" flag so capability
	// extraction can suppress the redundant mode surface (openclaw).
	ai.LookupACPModesMirrorThoughtLevelFn = LookupACPModesMirrorThoughtLevel

	// Wire up the model-switch method lookup so the ACP prompt path can send
	// session/set_model for agents that ignore a "model" config option (Hermes)
	// without importing the backends package.
	ai.LookupACPModelSwitchMethodFn = LookupACPModelSwitchMethod

	// Wire up the ACP behavior hooks so the event loop can ask a backend for
	// its own tool/meta mapping without importing this package.
	ai.LookupACPBehaviorFn = LookupACPBehavior

	// Wire up the extra-command scanner so the connection lifecycle can pre-scan
	// a backend's own plugin directory without knowing which backend it is.
	ai.LookupPluginCommandsFn = LookupPluginCommands

	// Wire up the mid-turn injection policy lookup so core flow can ask a
	// backend whether a mid-turn message can join the running turn (e.g.
	// CodeBuddy's session/steer) without importing the backends package.
	ai.LookupMidTurnInjectorFn = LookupMidTurnInjector

	// Wire up the BackendSpec loader so model/discovery.go can build
	// BackendRegistry dynamically from backend plugins.
	model.LoadBackendSpecs = AllSpecsSorted

	// Wire up the capability snapshot so model can answer "what can this
	// backend do" in one call; model's BackendSupportsMidTurn /
	// BackendMidTurnSplitsReply read this one.
	model.LookupBackendCapabilitiesFn = Capabilities
}
