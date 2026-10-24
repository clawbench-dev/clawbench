package backends

import (
	"os/exec"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// BackendPlugin is the complete self-describing registration unit for an AI backend.
// Each backend sub-package registers itself via init() calling Register().
//
// Currently, CLI-mode registration uses ai.RegisterBackend() in factory.go,
// while ACP mapping data is registered via backends.Register() in acp_register.go.
// Eventually, ai.RegisterBackend should be deprecated in favor of a unified
// backends.Register() that handles both CLI and ACP data in one place.
type BackendPlugin struct {
	// ID is the unique backend identifier, e.g. "claude", "kimi".
	// Corresponds to Agent.Backend field.
	ID string

	// Spec describes the backend's auto-discovery configuration.
	// Collected into model.BackendRegistry at startup.
	Spec model.BackendSpec

	// CLI is the CLI-mode configuration. nil means no CLI support.
	// Used by backends that ride the CLIBackend skeleton (most backends).
	CLI *CLIPlugin

	// Custom is the custom backend factory. nil means use standard CLI or ACP path.
	// Used by backends that directly implement AIBackend or wrap CLIBackend (e.g. Codex, VeCLI).
	// When Custom is non-nil, Factory uses Custom.NewBackend and ignores CLI.
	Custom *CustomPlugin

	// ACP is the ACP-mode mapping data. nil means no ACP support.
	// ACP event handling logic stays in internal/ai/ as shared infrastructure;
	// sub-packages only register mapping data (tool names, input field remaps).
	ACP *ACPPlugin

	// MidTurn is the backend's optional mid-turn injection policy: how to put a
	// user message into a turn that is already running, instead of queueing it
	// for the next one. nil means "always queue" (the default behavior).
	//
	// This is behavior, not mapping data, so it lives at the top level rather
	// than under ACP — a non-ACP backend could implement it too.
	MidTurn ai.MidTurnInjector

	// MidTurnSplitsReply reports whether an injected message SPLITS the running
	// assistant reply into two (a "before" and "after" half) rather than merely
	// merging into it. Only meaningful when MidTurn is non-nil.
	//
	// CodeBuddy echoes the injected message back on the wire with a
	// correlation id, so the host can cut the reply exactly at the injection
	// point — the split. claude/codex's `_session/steering` emits no such echo,
	// so their injected message MERGES: the user bubble lands after the
	// still-streaming reply with nothing following it, which reads as "sent but
	// ignored". The frontend renders a hint on the injected bubble ONLY for the
	// merge-only case, so this bit is the switch that turns it on.
	MidTurnSplitsReply bool
}

// CLIPlugin provides CLI-mode configuration for backends using the CLIBackend skeleton.
type CLIPlugin struct {
	// NewBackend returns a CLIBackend instance (configured with buildArgs/newParser/filterLine/preStart).
	NewBackend func() *ai.CLIBackend

	// ToolNameMap is the backend's complete tool name normalization map.
	// key: backend raw tool name → value: canonical name (e.g. "read_file" → "Read")
	ToolNameMap map[string]string

	// InputRemaps is the backend's CLI-mode tool input field remapping map.
	// key: original field name → value: target field name (e.g. "filePath" → "file_path")
	InputRemaps map[string]string

	// PreExecHook is called before the CLI subprocess starts, for backend-specific env injection.
	// e.g. Pi's API key injection logic. nil means no extra injection needed.
	PreExecHook func(cmd *exec.Cmd, req ai.ChatRequest)
}

// CustomPlugin provides a custom backend factory for backends not using the CLIBackend skeleton.
type CustomPlugin struct {
	// NewBackend returns a custom AIBackend instance.
	NewBackend func() ai.AIBackend
}

// ACPPlugin provides ACP-mode mapping data.
// ACP event handling stays in internal/ai/ as shared infrastructure.
// Sub-packages only register backend-specific mapping tables;
// shared event processing code queries these tables at runtime.
type ACPPlugin struct {
	// ToolCallIDPrefixes maps ACP toolCallID prefixes to canonical names for this backend.
	// e.g. Kimi: "read_file" → "Read"
	ToolCallIDPrefixes map[string]string

	// ToolTitlePrefixes maps ACP tool-call TITLE prefixes to canonical names for this
	// backend. Use this when the agent encodes the tool name at the START of the title
	// (e.g. Hermes: "read_file: foo.go" → "Read", "delegate_task: ..." → "Agent") instead
	// of in the toolCallID. Matching is by longest prefix first, so a more specific entry
	// ("browser_navigate") wins over a shorter one ("browser").
	ToolTitlePrefixes map[string]string

	// InputRemaps is the backend's ACP-mode tool input field remapping map.
	InputRemaps map[string]string

	// ModelSwitchMethod names the JSON-RPC method this backend honors for a
	// model switch. Empty means the default: send the selection as a config
	// option (session/set_config_option, configId "model").
	//
	// Some agents accept that config option but never act on it — Hermes stores
	// it in its session state and ignores it, so a switch silently keeps the old
	// model. Those agents implement the standard ACP session/set_model instead
	// (see ACPModelSwitchSetModel), which the Go SDK does not expose, so it is
	// sent over the raw JSON-RPC side channel.
	ModelSwitchMethod string

	// ToolCallWrapperTitlePrefixes lists title prefixes that mark a tool_call
	// frame as a WRAPPER around the real call rather than a tool of its own.
	// A wrapper frame is dropped (no tool card) and only remembered, so the
	// frame that follows can be rendered on its own.
	//
	// openclaw's bridge emits two frames for every plugin/MCP tool: first
	// `tool_call: id: web_fetch, args: {"url": ...}` (kind=other, toolCallId
	// call_xxx), then `web_fetch: url: ...` (kind=fetch, a DIFFERENT toolCallId
	// of the form tool_call:<parent>:web_fetch:1). Forwarding both draws a
	// second card literally named "tool_call"; and because the ids differ, no
	// id-based dedup can collapse them — the title is the only signal.
	ToolCallWrapperTitlePrefixes []string

	// ModesMirrorThoughtLevel tells the host that the agent's `modes` block
	// carries the SAME value set as its `thought_level` config option, so the
	// two must not both become UI controls.
	//
	// openclaw's bridge advertises modes=[off,minimal,low,medium,high,adaptive]
	// and a thought_level config option with the identical values. Only the
	// config option is writable: session/set_config_option(configId="mode")
	// answers -32603 "ACP bridge mode does not support session config option
	// \"mode\"", so a mode picker would be dead AND duplicate the effort picker.
	// With this set, the host suppresses the mode surface for the backend.
	ModesMirrorThoughtLevel bool
}

// ACPModelSwitchSetModel is the ACP-standard model-switch method. Declare it as
// ACPPlugin.ModelSwitchMethod for agents that honor it instead of (or in
// addition to) a "model" config option.
const ACPModelSwitchSetModel = "session/set_model"
