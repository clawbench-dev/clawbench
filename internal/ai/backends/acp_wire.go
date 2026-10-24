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

	// Wire up the mid-turn injection policy lookup so core flow can ask a
	// backend whether a mid-turn message can join the running turn (e.g.
	// CodeBuddy's session/steer) without importing the backends package.
	ai.LookupMidTurnInjectorFn = LookupMidTurnInjector

	// Wire up the BackendSpec loader so model/discovery.go can build
	// BackendRegistry dynamically from backend plugins.
	model.LoadBackendSpecs = AllSpecsSorted

	// Wire up the CLI capability reporter so model can tell whether a backend
	// has a CLI implementation (grok and other ACP-only backends return false).
	model.BackendSupportsCLIFn = ai.BackendSupportsCLI

	// Wire up the mid-turn capability reporter so model can tell whether a
	// backend can inject a message into the running turn (CodeBuddy's
	// session/steer). Drives the queued-bubble action label: "insert into the
	// current reply" when true, "interrupt and send" when false.
	model.BackendSupportsMidTurnFn = func(backendID string) bool {
		return LookupMidTurnInjector(backendID) != nil
	}

	// Wire up the "does an injection split the reply?" reporter so core flow can
	// decide whether an injected user message needs the merge-only hint
	// ("回复已并入上一轮") on its bubble. CodeBuddy splits (true); claude/codex
	// merge (false) because their steering emits no correlation echo.
	model.BackendMidTurnSplitsReplyFn = LookupMidTurnSplitsReply
}
