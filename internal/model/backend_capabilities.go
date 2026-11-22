package model

// Provider delivery methods.
//
// These are the CHANNELS a bound provider can travel through to reach a
// spawned process — not a single-choice taxonomy. A backend may declare
// several: codebuddy uses env + config-dir + own-service, opencode uses
// env + inline-config.
//
// "Can this backend take a provider at all" is a separate question; see
// CanBindProvider on BackendCapabilities. Cursor, for example, declares only
// own-service and still accepts a binding.
const (
	// ProviderMethodEnv passes the endpoint and credential as environment
	// variables (ANTHROPIC_BASE_URL and friends). Requires at least one
	// variable NAME to be declared: the built-in agent lists the two protocols
	// with empty names because it calls the endpoint itself, and must not be
	// reported as an env backend.
	ProviderMethodEnv = "env"
	// ProviderMethodConfigDir writes a private config directory per agent
	// (claude's CLAUDE_CONFIG_DIR, kimi's KIMI_SHARE_DIR).
	ProviderMethodConfigDir = "config-dir"
	// ProviderMethodConfigFile edits the user's own config file (codex's
	// ~/.codex/config.toml). The only method that touches a file outside
	// ClawBench's data directory, so binding it needs explicit consent.
	ProviderMethodConfigFile = "config-file"
	// ProviderMethodInlineConfig passes config CONTENT through an environment
	// variable (opencode's OPENCODE_CONFIG_CONTENT).
	ProviderMethodInlineConfig = "inline-config"
	// ProviderMethodOwnService passes only a credential for the backend's own
	// official service, with no endpoint (the private protocol).
	ProviderMethodOwnService = "own-service"
	// ProviderMethodInProcess means ClawBench itself calls the endpoint, so
	// nothing is injected into a spawned process (the built-in agent).
	ProviderMethodInProcess = "in-process"
)

// BackendCapabilities is the complete static capability snapshot of one
// backend: everything a caller can know WITHOUT connecting to the agent.
//
// It exists because those facts used to be reachable only by knowing which
// field to inspect: "can join a running turn" is `plugin.MidTurn != nil`,
// "can be pointed at a provider" is a non-empty ProviderEnvSpec. Every consumer had to re-derive that mapping, and
// the provider "method" taxonomy had no representation at all.
//
// Runtime facts (available models, modes, thinking levels) are deliberately
// absent: they can only come from the agent over ACP, so a static snapshot
// must not pretend to know them.
type BackendCapabilities struct {
	// HasACP is true when the backend can be spawned over the ACP transport
	// (its spec names an AcpCommand). This is deliberately not "declares ACP
	// mapping data": backends that speak plain ACP (claude, cursor) leave the
	// mapping struct nil and use the shared generic remaps.
	HasACP bool
	// SupportsMidTurn is true when a user message can join an ALREADY RUNNING
	// turn instead of being queued. Drives the queued-bubble action label.
	SupportsMidTurn bool
	// MidTurnSplitsReply is true when such an injection SPLITS the running
	// reply rather than merely merging into it. Only meaningful when
	// SupportsMidTurn is true; merge-only is the default.
	MidTurnSplitsReply bool
	// ACPModelSwitchMethod is the JSON-RPC method this backend honors for a
	// model switch. Empty means the default config-option path.
	ACPModelSwitchMethod string
	// ACPModesMirrorThoughtLevel is true when the agent's `modes` block carries
	// the same value set as its thought-level option, so only one of the two
	// may become a UI control.
	ACPModesMirrorThoughtLevel bool
	// HasModelSource is true when the backend can enumerate its own models
	// (static catalog, CLI probe, or plugin).
	HasModelSource bool

	// CanBindProvider is true when a provider can be bound to this backend at
	// all. False means the settings picker is greyed out and the bind endpoint
	// answers 400 — NOT that the backend's own CLI cannot be configured with a
	// provider by hand (openclaw configures one on its Gateway).
	CanBindProvider bool
	// ProviderProtocols lists the wire protocols the backend reads. A key-only
	// backend still lists ProviderProtocolPrivate (the credential-only mode),
	// so an empty list means no binding path at all.
	ProviderProtocols []string
	// ProviderMethods lists the delivery channels declared by the backend's
	// spec, in a stable order. Empty means no provider can reach it.
	ProviderMethods []string
	// ProviderKeyOnly is true when the backend consumes only a credential and
	// has no endpoint slot: a provider bound to it must carry no base URL.
	ProviderKeyOnly bool
	// ProviderSecretInFile is true when the private config file has to hold the
	// API key in clear text (no ${ENV} reference syntax).
	ProviderSecretInFile bool
	// ProviderDeclaresModels is true when the private config file has to LIST
	// the models, because the CLI resolves an id against that declaration.
	ProviderDeclaresModels bool
}

// LookupBackendCapabilitiesFn is wired by the backends package at init time.
// It cannot be a direct call: model cannot import backends (backends imports
// model), so the lookup travels as a function value, like LoadBackendSpecs.
var LookupBackendCapabilitiesFn func(backendID string) (BackendCapabilities, bool)

// LookupBackendCapabilities returns the capability snapshot for a backend.
//
// An unknown backend, or an unwired lookup (isolated tests, a build without
// the backends package), returns the zero value — every capability false —
// which is the conservative answer every caller already assumes.
func LookupBackendCapabilities(backendID string) BackendCapabilities {
	if LookupBackendCapabilitiesFn == nil {
		return BackendCapabilities{}
	}
	caps, _ := LookupBackendCapabilitiesFn(backendID)
	return caps
}

// BackendSupportsMidTurn reports whether the backend can join a running turn.
// Falls back to false when the lookup is not wired, in which case callers use
// the "interrupt and send" affordance instead.
func BackendSupportsMidTurn(backendID string) bool {
	return LookupBackendCapabilities(backendID).SupportsMidTurn
}

// BackendMidTurnSplitsReply reports whether an injected message splits the
// running reply. Falls back to false (merge-only) when not wired or unknown —
// the conservative default, since merge-only is the case that needs the
// "reply merged into the previous one" hint.
func BackendMidTurnSplitsReply(backendID string) bool {
	return LookupBackendCapabilities(backendID).MidTurnSplitsReply
}
