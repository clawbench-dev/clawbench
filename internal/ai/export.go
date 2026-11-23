package ai

// Exported seams for backend sub-packages.
//
// A backend that needs its own ACP tool/meta mapping implements it in its own
// package (see ACPBehavior) and needs the same primitives the generic path
// uses. Those primitives stay unexported inside this package and are re-exposed
// here as thin wrappers, so:
//
//   - the core keeps one implementation (no copy-paste drift), and
//   - sub-packages get a small, deliberate surface instead of reaching into
//     package internals.
//
// Add a wrapper only when a real backend needs it — every entry here is part of
// the contract those sub-packages compile against.

import (
	"context"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// streamChanSize is the buffer size of a stream event channel, so the
// consumer's expectations about buffering stay uniform across emit paths.
const streamChanSize = 512

// EmitStreamEvent sends one event to a stream channel, honoring the critical
// event policy (critical events wait briefly for room, deltas are dropped).
func EmitStreamEvent(ch chan<- StreamEvent, source string, event StreamEvent) {
	emitStreamEvent(ch, source, event)
}

// TryEmitStreamEvent is EmitStreamEvent with a delivery report, for callers
// that latch a one-shot signal and retry on the next frame.
func TryEmitStreamEvent(ch chan<- StreamEvent, source string, event StreamEvent) bool {
	return tryEmitStreamEvent(ch, source, event)
}

// EmitCriticalStreamEventReport is the delivery-reporting form of the critical
// send, used where a dropped one-shot signal must not be treated as reported.
func EmitCriticalStreamEventReport(ch chan<- StreamEvent, source string, event StreamEvent) bool {
	return emitCriticalStreamEventReport(ch, source, event)
}

// ForwardACPEvent emits an event produced on the ACP notification goroutine.
// It must be used (rather than EmitStreamEvent) by anything running on that
// goroutine: the shared SDK queue kills the connection on overflow, so that
// path may never block.
func ForwardACPEvent(ch chan<- StreamEvent, event StreamEvent) {
	forwardACPEvent(ch, event)
}

// MetaString / MetaInt / MetaFloat / MetaMaxInt read loosely-typed _meta
// values. Agents encode the same quantity as int, float64 or json.Number
// depending on the adapter, so every backend parser goes through these.
func MetaString(v any) string { return metaString(v) }

// MetaBool reads a truthy boolean field (a real bool, or a string "true").
func MetaBool(v any) bool     { return metaBool(v) }
func MetaInt(v any) int       { return metaInt(v) }
func MetaFloat(v any) float64 { return metaFloat(v) }
func MetaMaxInt(base int, vals ...int) int {
	return metaMaxInt(base, vals...)
}

// ExtractToolName resolves a canonical tool name from a title, kind, backend id
// and (optionally) the raw tool-call id.
func ExtractToolName(title string, kind acp.ToolKind, backendID string, toolCallID ...string) string {
	return extractToolName(title, kind, backendID, toolCallID...)
}

// ToolCallIDOf converts an ACP tool-call id to its string form.
func ToolCallIDOf(id acp.ToolCallId) string { return toolCallIDOf(id) }

// MapToolCallStatus copies an ACP status onto a ToolCall (Done / error text).
func MapToolCallStatus(status *acp.ToolCallStatus, tool *ToolCall) {
	mapToolCallStatus(status, tool)
}

// MapToolCallInput copies rawInput onto a ToolCall, applying the backend's
// field remaps.
func MapToolCallInput(tcu acp.SessionToolCallUpdate, tool *ToolCall, backendID string) {
	mapToolCallInput(tcu, tool, backendID)
}

// MapToolCallInputFromLocations fills a ToolCall's input from the ACP
// locations list, for agents that send no rawInput.
func MapToolCallInputFromLocations(tcu acp.SessionToolCallUpdate, tool *ToolCall) {
	mapToolCallInputFromLocations(tcu, tool)
}

// MapToolCallOutput copies rawOutput / content blocks onto a ToolCall.
func MapToolCallOutput(tcu acp.SessionToolCallUpdate, tool *ToolCall) {
	mapToolCallOutput(tcu, tool)
}

// ExtractInputFromLocationsAndTitle infers tool input from locations + title.
func ExtractInputFromLocationsAndTitle(locations []acp.ToolCallLocation, title string, kind acp.ToolKind, toolCallID string) map[string]any {
	return extractInputFromLocationsAndTitle(locations, title, kind, toolCallID)
}

// ResolveACPToolInput normalizes rawInput into a ToolCall's Input, applying
// remaps.
func ResolveACPToolInput(tc acp.SessionUpdateToolCall, tool *ToolCall, remaps map[string]string) {
	resolveACPToolInput(tc, tool, remaps)
}

// ACPRemapsForBackend returns the backend's ACP input remap table (falling back
// to the generic one).
func ACPRemapsForBackend(backendID string) map[string]string {
	return acpRemapsForBackend(backendID)
}

// ExtractMetaToolName reads the tool name from a nested _meta namespace.
func ExtractMetaToolName(meta map[string]any) string { return extractMetaToolName(meta) }

// ExtractMetaToolNameFlat reads the tool name from a flat _meta key.
func ExtractMetaToolNameFlat(meta map[string]any) string { return extractMetaToolNameFlat(meta) }

// ParseGenericACPToolCall is the default tool_call mapper. Backends that only
// tweak a few fields can call it and then adjust the result.
func ParseGenericACPToolCall(tc acp.SessionUpdateToolCall, backendID string) *ToolCall {
	return parseGenericACPToolCall(tc, backendID)
}

// ParseGenericACPToolCallUpdate is the default tool_call_update mapper.
func ParseGenericACPToolCallUpdate(tcu acp.SessionToolCallUpdate, backendID string) *ToolCall {
	return parseGenericACPToolCallUpdate(tcu, backendID)
}

// ACPUpdateMeta returns the _meta of any ACP session update variant.
func ACPUpdateMeta(update acp.SessionUpdate) map[string]any { return acpUpdateMeta(update) }

// MapToolCallName fills a ToolCall's name from an update's title/kind when the
// backend has no better source.
func MapToolCallName(tcu acp.SessionToolCallUpdate, tool *ToolCall, backendID string) {
	mapToolCallName(tcu, tool, backendID)
}

// ExtractMetaUsageForTest exposes the _meta dispatch for tests that live
// outside this package (they need the real backend set registered).
func ExtractMetaUsageForTest(backendID string, meta map[string]any) *MetaExtraction {
	return extractMetaUsage(backendID, meta)
}

// MapACPSessionUpdateWithConnForTest is MapACPSessionUpdateForTest with a
// connection, so per-backend behaviors are resolved (they key off the agent's
// backend id). Tests outside this package need it because the hooks are wired
// by internal/ai/backends.
func MapACPSessionUpdateWithConnForTest(ctx context.Context, update acp.SessionUpdate, ch chan<- StreamEvent, conn *ACPConn, deb *toolCallDebouncer) {
	mapACPSessionUpdate(update, ch, ctx, conn, deb)
}

// NewToolCallDebouncerForTest builds a debouncer for tests that exercise the
// ACP event loop from outside this package.
func NewToolCallDebouncerForTest(ch chan<- StreamEvent, conn *ACPConn) *toolCallDebouncer {
	return newToolCallDebouncer(ch, conn)
}

// NormalizeToolInput rewrites a tool call's raw JSON input field names to the
// canonical snake_case form, applying the caller's path mappings on top of the
// shared defaults.
func NormalizeToolInput(rawInput []byte, pathMappings map[string]string) ([]byte, error) {
	return normalizeToolInput(rawInput, pathMappings)
}

// TruncateToolOutput bounds a tool result's text for display.
func TruncateToolOutput(output string) string { return truncateToolOutput(output) }

// StreamErrorReason turns a backend's raw error text into a user-facing reason.
func StreamErrorReason(text string) string { return streamErrorReason(text) }

// CompactDetectedEvent builds the internal compaction signal event.
func CompactDetectedEvent(source string) StreamEvent { return compactDetectedEvent(source) }

// MaxToolOutputBytes is the shared display cap for a tool result's text.
const MaxToolOutputBytes = maxToolOutputBytes

// IsGrokCompactionCompleted reports whether a Grok event type marks the end of
// a context compaction.
func IsGrokCompactionCompleted(eventType string) bool { return isGrokCompactionCompleted(eventType) }

// ExecCommandJSON builds the canonical {"command":"..."} tool input.
func ExecCommandJSON(command string) string { return execCommandJSON(command) }

// AgentProcessEnv returns the environment overrides an agent CLI needs.
func AgentProcessEnv() []string { return agentProcessEnv() }

// CriticalSendTimeout is the bounded wait a critical event may spend waiting
// for room in a full stream channel.
var CriticalSendTimeout = criticalSendTimeout

// IsLimitErrorText reports whether an error text describes a quota/limit hit.
func IsLimitErrorText(text string) bool { return isLimitErrorText(text) }

// IsACPToolCallWrapperForTest exposes the wrapper-frame predicate for tests.
func IsACPToolCallWrapperForTest(title, backendID string) bool {
	return isACPToolCallWrapper(title, backendID)
}

// ACPModesMirrorThoughtLevelForTest exposes the modes/effort dedup flag.
func ACPModesMirrorThoughtLevelForTest(backendID string) bool {
	return acpModesMirrorThoughtLevel(backendID)
}

// ParseACPToolCallForTest exposes the tool_call dispatch for tests that live
// outside this package (they need the real backend behaviors registered).
func ParseACPToolCallForTest(backendID string, tc acp.SessionUpdateToolCall) *ToolCall {
	return parseACPToolCall(backendID, tc)
}

// ParseACPToolCallUpdateForTest exposes the update dispatch for tests outside
// this package.
func ParseACPToolCallUpdateForTest(backendID string, tcu acp.SessionToolCallUpdate) *ToolCall {
	return parseACPToolCallUpdate(backendID, tcu)
}

// LookupPluginCommandsFn resolves a backend's extra-command scanner.
var LookupPluginCommandsFn func(backendID string) func() []AvailableCommandInfo

// specForAgent returns the backend spec for an agent, or nil.
func specForAgent(agent *model.Agent) *model.BackendSpec {
	if agent == nil {
		return nil
	}
	return model.FindSpecByBackend(agent.Backend)
}

// scanOrNil calls an optional command scanner, tolerating nil.
func scanOrNil(scan func() []AvailableCommandInfo) []AvailableCommandInfo {
	if scan == nil {
		return nil
	}
	return scan()
}

// commandsReEmitDelay returns the backend's plugin-command settle time, or 0.
func commandsReEmitDelay(agent *model.Agent) time.Duration {
	if spec := specForAgent(agent); spec != nil {
		return spec.CommandsReEmitDelay
	}
	return 0
}

// hasPluginCommands reports whether the backend pre-scans its own plugin
// commands (so the ACP-provided list must be merged with them, not replaced).
func hasPluginCommands(agent *model.Agent) bool {
	if LookupPluginCommandsFn == nil || agent == nil {
		return false
	}
	return LookupPluginCommandsFn(agent.Backend) != nil
}

// ProbeCommandWindowForTest exposes the command-settle window calculation.
func ProbeCommandWindowForTest(agent *model.Agent) (maxWait, quiet time.Duration) {
	return probeCommandWindow(agent)
}
