package ai

import acp "github.com/coder/acp-go-sdk"

// ACPBehavior is a backend's set of ACP event-mapping hooks.
//
// Everything here is OPTIONAL: a nil field falls back to the generic
// implementation in this package (parseGenericACPToolCall, extractGenericMeta,
// the best-effort parent-link decoder, …). A backend that speaks plain ACP
// declares nothing and still works; a backend with a private wire shape
// implements only the hooks it needs, in its own sub-package.
//
// Why function fields instead of an interface: the hooks are independent and
// most backends need none of them. An interface would force every backend to
// embed a no-op base struct just to satisfy the type, and adding a hook would
// break every implementation. Fields are additive.
//
// The type lives here rather than in internal/ai/backends because every
// signature mentions types owned by this package (ToolCall, MetaExtraction,
// ACPConn, StreamEvent); putting it in backends would force those types out
// through a second layer for no benefit.
type ACPBehavior struct {
	// ParseToolCall maps an opening tool_call frame to a ToolCall.
	ParseToolCall func(tc acp.SessionUpdateToolCall) *ToolCall

	// ParseToolCallUpdate maps a tool_call_update frame to a ToolCall.
	ParseToolCallUpdate func(tcu acp.SessionToolCallUpdate) *ToolCall

	// ExtractMeta reads the backend's private _meta usage/quota/trace payload.
	ExtractMeta func(meta map[string]any) *MetaExtraction

	// ParentToolCallID returns the id of the tool call that spawned the
	// sub-agent which produced this frame, or "" for top-level content.
	ParentToolCallID func(meta map[string]any) string

	// OnToolCall runs for every non-wrapper tool_call frame, before the
	// thinking_done event is emitted. Used to record state that later frames
	// need (e.g. CodeBuddy's team member → spawn-call mapping).
	OnToolCall func(conn *ACPConn, tc *acp.SessionUpdateToolCall)

	// OnToolCallUpdate runs for every tool_call_update frame, before the
	// debouncer. Used to bridge a private checklist format into plan_update
	// (CodeBuddy task tools, OpenCode todowrite).
	OnToolCallUpdate func(conn *ACPConn, ch chan<- StreamEvent, tcu acp.SessionToolCallUpdate)

	// OnSessionInfoUpdate runs for every session_info_update frame. CodeBuddy
	// carries its Agent Teams state in this standard notification's _meta.
	OnSessionInfoUpdate func(conn *ACPConn, ch chan<- StreamEvent, meta map[string]any)

	// IsChildSessionUpdate reports whether a frame was projected from a
	// sub-agent's own session into the parent's stream (OpenCode v2). Such
	// frames carry no parent link and are dropped unless one was resolved.
	IsChildSessionUpdate func(meta map[string]any) bool
}

// LookupACPBehaviorFn resolves a backend's behavior hooks. Wired by
// internal/ai/backends to backends.LookupACPBehavior to avoid an import cycle.
var LookupACPBehaviorFn func(backendID string) *ACPBehavior

// acpBehavior returns the backend's hooks, or nil when it declares none.
func acpBehavior(backendID string) *ACPBehavior {
	if LookupACPBehaviorFn == nil || backendID == "" {
		return nil
	}
	return LookupACPBehaviorFn(backendID)
}

// isChildSessionUpdate reports whether an update was projected from a
// sub-agent's own session into the parent's stream. Backends that declare a
// predicate win; the default recognises OpenCode v2's child-session tag.
func isChildSessionUpdate(backendID string, update acp.SessionUpdate) bool {
	if b := acpBehavior(backendID); b != nil && b.IsChildSessionUpdate != nil {
		return b.IsChildSessionUpdate(acpUpdateMeta(update))
	}
	return isOpenCodeChildSessionUpdate(update)
}
