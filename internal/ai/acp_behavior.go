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

	// RefusalDetail reads the agent's own explanation out of a
	// stopReason=refusal PromptResponse._meta.
	//
	// A refusal carries no content, and the JSON-RPC code is usually a generic
	// placeholder (-32603 Internal error) that explains nothing. Backends that
	// stash the real cause somewhere in _meta return it here; the shared code
	// then puts it in the warning event instead of the useless code. nil (or an
	// empty return) falls back to the generic refusal copy.
	RefusalDetail func(meta map[string]any) string

	// CostFieldCarriesCredit reports that this backend repurposes the
	// ACP-standard usage_update.cost field to carry a CREDIT amount instead of
	// a monetary cost.
	//
	// The amount is still real billing (it is kept as
	// MetaExtraction.CostCredit), but persisting it as chat_metadata.cost_usd
	// would inflate the cost statistics with a unit-less number. CodeBuddy is
	// the only backend that does this today; every other agent reports money.
	//
	// A field rather than a backend-id comparison in the core: the fact belongs
	// to the backend that ships the shape, and the core should not have to learn
	// a new id to interpret its own wire format.
	CostFieldCarriesCredit bool

	// IsReplayedTurnMeta reports whether a notification's _meta belongs to the
	// previous, already-completed turn rather than the current one.
	//
	// CodeBuddy re-emits the tail of the previous turn when an ACP session is
	// reused across prompts (stale agent_message_chunk text and stale
	// usage_update trace identity), so the core needs a way to recognise that.
	// Declaring the hook (rather than comparing ids in the core) keeps "which
	// backends can be compared across turns" a fact of the backend: other
	// backends' requestIds are not comparable, and their genuine chunks could
	// share an id with the baseline.
	IsReplayedTurnMeta func(conn *ACPConn, meta map[string]any) bool
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
// predicate win; the default recognizes OpenCode v2's child-session tag.
func isChildSessionUpdate(backendID string, update acp.SessionUpdate) bool {
	if b := acpBehavior(backendID); b != nil && b.IsChildSessionUpdate != nil {
		return b.IsChildSessionUpdate(acpUpdateMeta(update))
	}
	return isOpenCodeChildSessionUpdate(update)
}
