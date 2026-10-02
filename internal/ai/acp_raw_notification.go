package ai

import (
	"encoding/json"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Raw session/update notification bypass
// ---------------------------------------------------------------------------
//
// The ACP Go SDK's SessionUpdate union only knows the spec's ~13 variants. When
// an agent sends an extension variant (e.g. Claude's `subagent_spawned`, or any
// future `*_update`), UnmarshalJSON falls through to a set of discriminator-only
// fallback blocks; SessionInfoUpdate's block matches on the mere presence of
// `sessionUpdate`, so the unknown variant is NOT dropped — it is silently
// re-typed as SessionInfoUpdate with every payload field LOST (verified:
// TestSDKMisclassifiesUnknownSessionUpdateVariant).
//
// acpStdoutFilter therefore tees every `session/update` line to this sink, which
// reads the variant straight from the raw JSON before the SDK mangles it. The
// line still reaches the SDK (tee, not steal), so known variants are unaffected;
// this path only handles the ones the SDK misclassifies.
//
// This file is transport only: it decides WHICH variants the SDK lost and hands
// them, with their intact payload, to a registered handler. Interpreting a
// variant is a backend concern (see the team bridge), so the default is a no-op.

// jsonRPCMethodMarker identifies a JSON-RPC request/notification (both carry a
// method; responses carry only id+result/error). The filter uses a byte-substring
// guard so it does not unmarshal every line. The client then rejects requests
// (which have an id) and spec methods, keeping only custom notifications.
var jsonRPCMethodMarker = []byte(`"method"`)

// rawNotificationSink receives every `session/update` line the stdout filter
// parses. Implemented by *ClawBenchACPClient. Kept as an interface so the filter
// does not depend on the client (and so tests can substitute a recorder).
type rawNotificationSink interface {
	DispatchRawNotification(line []byte)
}

// ExtensionUpdate is a session/update variant the SDK's typed union does not
// model. Params is the intact `params` object, so the payload the SDK would have
// dropped is available to the handler.
type ExtensionUpdate struct {
	SessionID string
	Variant   string          // the raw sessionUpdate discriminator
	Params    json.RawMessage // the whole `params` object, unmodified
}

// sdkKnownSessionUpdateVariants is the set the SDK's SessionUpdate union
// classifies correctly. Anything else is misclassified (or dropped) by the SDK
// and must be read from the raw line instead.
var sdkKnownSessionUpdateVariants = map[string]bool{
	"user_message_chunk":        true,
	"agent_message_chunk":       true,
	"agent_thought_chunk":       true,
	"tool_call":                 true,
	"tool_call_update":          true,
	"plan":                      true,
	"plan_update":               true,
	"plan_removed":              true,
	"available_commands_update": true,
	"current_mode_update":       true,
	"config_option_update":      true,
	"session_info_update":       true,
	"usage_update":              true,
}

// sessionUpdateVariantMismatch reports whether the SDK misclassified an unknown
// discriminator into a known struct. The SDK's fallback blocks match on field
// presence rather than the discriminator, so e.g. an extension variant carrying
// `toolCallId`+`title` is parsed as a ToolCall while its SessionUpdate field
// still holds the original name. Downstream code branches on `update.ToolCall !=
// nil` and would act on the frame as if it were a real tool_call, emitting a
// wrong event. When this returns true the caller must ignore the typed update —
// the raw notification path (DispatchRawNotification) is what handles it.
func sessionUpdateVariantMismatch(u acp.SessionUpdate) bool {
	check := func(variant, got string) bool { return got != "" && got != variant }
	switch {
	case u.UserMessageChunk != nil:
		return check("user_message_chunk", u.UserMessageChunk.SessionUpdate)
	case u.AgentMessageChunk != nil:
		return check("agent_message_chunk", u.AgentMessageChunk.SessionUpdate)
	case u.AgentThoughtChunk != nil:
		return check("agent_thought_chunk", u.AgentThoughtChunk.SessionUpdate)
	case u.ToolCall != nil:
		return check("tool_call", u.ToolCall.SessionUpdate)
	case u.ToolCallUpdate != nil:
		return check("tool_call_update", u.ToolCallUpdate.SessionUpdate)
	case u.Plan != nil:
		return check("plan", u.Plan.SessionUpdate)
	case u.PlanUpdate != nil:
		return check("plan_update", u.PlanUpdate.SessionUpdate)
	case u.PlanRemoved != nil:
		return check("plan_removed", u.PlanRemoved.SessionUpdate)
	case u.AvailableCommandsUpdate != nil:
		return check("available_commands_update", u.AvailableCommandsUpdate.SessionUpdate)
	case u.CurrentModeUpdate != nil:
		return check("current_mode_update", u.CurrentModeUpdate.SessionUpdate)
	case u.ConfigOptionUpdate != nil:
		return check("config_option_update", u.ConfigOptionUpdate.SessionUpdate)
	case u.SessionInfoUpdate != nil:
		return check("session_info_update", u.SessionInfoUpdate.SessionUpdate)
	case u.UsageUpdate != nil:
		return check("usage_update", u.UsageUpdate.SessionUpdate)
	default:
		return false
	}
}

// SetExtensionUpdateHandler registers the callback for session/update variants
// the SDK misclassifies. Passing nil disables handling (the default). Call it
// once, after the connection is created.
func (c *ClawBenchACPClient) SetExtensionUpdateHandler(h func(ExtensionUpdate)) {
	c.mu.Lock()
	c.extensionUpdateHandler = h
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Raw notification methods (custom agent → client methods)
// ---------------------------------------------------------------------------
//
// The ACP SDK only dispatches the ~14 spec client methods; any other method name
// is logged and dropped (connection.go: "failed to handle notification"). Agents
// that expose extension notifications — CodeBuddy's `_codebuddy.ai/artifact`,
// `/checkpoint`, `/command`, `/authUrl`, … — therefore never reach ClawBench.
//
// The same stdout tee that recovers misclassified session/update variants also
// carries these lines. This part of the file classifies them: it filters out the
// spec methods (which the SDK handles), the session/update notifications (handled
// by the variant path above), and the responses to requests we issued (handled by
// acpRawRPC). Everything else is a custom agent→client notification and is handed
// to the registered handler. Default is a no-op: interpreting a method is a
// backend concern.

// sdkKnownClientMethods is the set the SDK's client dispatch handles. A
// notification whose method is in this set is left to the SDK.
var sdkKnownClientMethods = map[string]bool{
	"session/update":             true,
	"session/request_permission": true,
	"fs/read_text_file":          true,
	"fs/write_text_file":         true,
	"terminal/create":            true,
	"terminal/output":            true,
	"terminal/wait_for_exit":     true,
	"terminal/kill":              true,
	"terminal/release":           true,
	"elicitation/create":         true,
	"elicitation/complete":       true,
	"mcp/connect":                true,
	"mcp/message":                true,
	"mcp/disconnect":             true,
}

// SetExtensionNotificationHandler registers the callback for custom agent→client
// notification methods (any method the SDK does not dispatch). Passing nil
// disables handling (the default). Call it once, after the connection is created.
func (c *ClawBenchACPClient) SetExtensionNotificationHandler(h func(ExtensionNotification)) {
	c.mu.Lock()
	c.extensionNotificationHandler = h
	c.mu.Unlock()
}

// ExtensionNotification is an agent→client JSON-RPC notification whose method
// the SDK does not dispatch. Params is the intact `params` object.
type ExtensionNotification struct {
	Method string
	Params json.RawMessage
}

// DispatchRawNotification reads one raw agent line. It acts only on custom
// agent→client notifications; spec methods, session/update lines (handled by the
// variant path) and JSON-RPC responses are ignored. The SDK still receives every
// line via the tee.
//
// It must never block: it runs on the stdout pump goroutine, so it only takes
// leaf locks and calls the (non-blocking-by-contract) handler.
func (c *ClawBenchACPClient) DispatchRawNotification(line []byte) {
	// A notification has a method and no id. Cheap structural checks first so
	// responses and requests are rejected without a full unmarshal.
	var env struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	if env.Method == "" || len(env.ID) != 0 {
		return
	}

	if env.Method == "session/update" {
		c.dispatchSessionUpdateVariant(env.Params)
		return
	}
	if sdkKnownClientMethods[env.Method] {
		return
	}

	// A custom agent→client notification. Connection liveness, mirroring the
	// typed path — these are model-driven, so they also refresh the progress
	// clock. Lock-free (atomic), safe on the pump goroutine.
	if c.connRef != nil {
		c.connRef.TouchSessionUpdate()
		c.connRef.TouchModelProgress()
	}

	c.mu.Lock()
	handler := c.extensionNotificationHandler
	c.mu.Unlock()
	if handler == nil {
		slog.Debug("acp: extension notification with no handler", "method", env.Method)
		return
	}
	handler(ExtensionNotification{Method: env.Method, Params: env.Params})
}

// dispatchSessionUpdateVariant handles a raw session/update line: it forwards
// variants the SDK's typed union does not model, and ignores the rest (the SDK's
// typed callback handles known variants).
func (c *ClawBenchACPClient) dispatchSessionUpdateVariant(params json.RawMessage) {
	var probe struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &probe); err != nil {
		return
	}
	variant := probe.Update.SessionUpdate
	if variant == "" || sdkKnownSessionUpdateVariants[variant] {
		// Known variant: the SDK's typed callback handles it correctly.
		return
	}

	// Connection liveness, mirroring the typed path: every notification counts
	// as activity so an async workflow's connection is not swept. A variant the
	// SDK misclassified is model output, so it also refreshes the progress
	// clock. Both are lock-free (atomic) — safe on the pump goroutine.
	if c.connRef != nil {
		c.connRef.TouchSessionUpdate()
		c.connRef.TouchModelProgress()
	}

	// During a LoadSession replay, updates are buffered for the load handler.
	// The replay buffer is typed ([]acp.SessionNotification) and cannot carry an
	// unknown variant, so these are dropped here — the live path is what
	// surfaces extension variants. Logged so a future replay-aware consumer can
	// be justified by real data.
	if c.IsLoadSessionActive() {
		slog.Debug("acp: dropping extension session update during loadSession replay",
			"variant", variant, "session_id", probe.SessionID)
		return
	}

	c.mu.Lock()
	_, ok := c.sessionRoutes[probe.SessionID]
	handler := c.extensionUpdateHandler
	c.mu.Unlock()
	if !ok {
		// No active stream for this session — same drop rule as the typed path.
		return
	}
	if handler == nil {
		slog.Debug("acp: extension session update with no handler",
			"variant", variant, "session_id", probe.SessionID)
		return
	}
	handler(ExtensionUpdate{
		SessionID: probe.SessionID,
		Variant:   variant,
		Params:    params,
	})
}
