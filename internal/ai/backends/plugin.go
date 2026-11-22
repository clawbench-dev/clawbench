package backends

import (
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// BackendPlugin is the complete self-describing registration unit for an AI backend.
// Each backend sub-package registers itself via ONE init() calling Register().
//
// Register() is the only entry point a backend needs: it forwards each non-nil
// field to the registry that consumes it (the in-process factory table in
// internal/ai, the model-source registry in internal/model, the on-disk session
// scanner table). Before that, every backend had to call three or four registration
// functions from three or four init() blocks, and forgetting one produced a
// backend that half worked (e.g. detectable but with no models).
type BackendPlugin struct {
	// ID is the unique backend identifier, e.g. "claude", "kimi".
	// Corresponds to Agent.Backend field.
	ID string

	// Spec describes the backend's auto-discovery configuration.
	// Collected into model.BackendRegistry at startup.
	Spec model.BackendSpec

	// Implementation is the in-process factory for a backend that runs inside
	// this process instead of spawning an agent (only the built-in agent).
	// nil for every process-spawning backend, which reach an agent over ACP.
	Implementation *InProcessPlugin

	// ACP is the ACP-mode mapping data. nil means no ACP support.
	// ACP event handling logic stays in internal/ai/ as shared infrastructure;
	// sub-packages only register mapping data (tool names, input field remaps).
	ACP *ACPPlugin

	// Behavior holds the backend's ACP event-mapping hooks (custom tool
	// parsing, private _meta payloads, plan/team bridges). Every field is
	// optional: nil falls back to the generic implementation in internal/ai.
	//
	// This is behavior, not mapping data, so it sits beside ACP rather than
	// inside it. Backends that speak plain ACP leave it nil.
	Behavior *ai.ACPBehavior

	// ModelSource is how this backend enumerates its models (static catalog,
	// command probe, or custom plugin). nil means the backend has no discovery
	// source of its own.
	//
	// This used to be a separate model.RegisterModelSource() call in a
	// discovery.go init(). Folding it in here means a backend cannot register
	// itself without also saying how its models are discovered — the two facts
	// always travel together.
	ModelSource model.ModelSource

	// ProviderEnv declares how this backend receives a BYOK provider: which
	// environment variables carry the endpoint and the credential, or that it
	// reads them from a config file. A non-nil entry with no protocols is the
	// deliberate "this backend cannot take a provider" decision — an absent
	// entry means "not classified", which a coverage test rejects.
	//
	// This used to be a 20-row table in model/provider_env.go, which meant every
	// new backend edited a file in another package. The fact is per-backend, so
	// it lives here.
	ProviderEnv *model.ProviderEnvSpec

	// PluginCommands scans backend-specific extra slash commands that the agent
	// itself does not advertise at NewSession (CodeBuddy's plugin cache).
	// nil means "the agent's own list is complete".
	PluginCommands func() []ai.AvailableCommandInfo

	// ListSessions is the on-disk ACP session scanner, used as a fallback when
	// the agent does not implement the session/list RPC. nil means "rely on the
	// RPC only".
	ListSessions ai.ListSessionsFromDiskFn

	// MidTurn is the backend's optional mid-turn injection policy: how to put a
	// user message into a turn that is already running, instead of queueing it
	// for the next one. nil means "always queue" (the default behavior).
	//
	// This is behavior, not mapping data, so it lives at the top level rather
	// than under ACP — a non-ACP backend could implement it too.
	MidTurn ai.MidTurnInjector

	// MidTurnSplitsReply reports whether an injected message SPLITS the running	// assistant reply into two (a "before" and "after" half) rather than merely
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

	// OrphanCmdline is this backend's command-line shape, used by
	// CleanupOrphans as a FALLBACK when the CLAWBENCH_CHILD=1 marker is
	// unreadable or absent (an agent spawned by an older build, or a /proc
	// entry whose environ we may not read).
	//
	// It is a fallback, not the primary signal: the marker is exact, while a
	// command line can in principle be reproduced by hand. Declaring nothing
	// means this backend's leftovers are only cleaned up when the marker is
	// readable — which is every child the current build spawns.
	OrphanCmdline ai.OrphanCmdlinePattern
}

// InProcessPlugin is the factory for a backend implemented in this process
// (the built-in agent). It is NOT a CLI transport: the backend never spawns a
// subprocess, it runs ClawBench's own tool loop.
type InProcessPlugin struct {
	// NewBackend returns a fresh backend instance for one run.
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

	// SpawnHooks customizes the ACP child's argv/env at spawn time.
	//
	// Backend-private spawn workarounds live here instead of in the shared
	// lifecycle: reading a backend's own MCP config file, pointing it at an
	// own-service endpoint, or setting a permission variable are that backend's
	// business, and a shared `if backend == "x"` is exactly the coupling this
	// registry exists to remove.
	SpawnHooks *ai.ACPSpawnHooks
}

// ACPModelSwitchSetModel is the ACP-standard model-switch method. Declare it as
// ACPPlugin.ModelSwitchMethod for agents that honor it instead of (or in
// addition to) a "model" config option.
const ACPModelSwitchSetModel = "session/set_model"
