package ai

import (
	"context"
	"encoding/json"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// 非标准 ACP 消息的唯一入口
// ---------------------------------------------------------------------------
//
// 这里处理「协议没建模、SDK 又用不了」的两类消息：
//
//  1. session/update 里 SDK 不认识的变体（claude 的 subagent_spawned 等）；
//  2. 自定义的 agent→client 方法（CodeBuddy 的 _codebuddy.ai/*）。
//
// 两者的判断都交给 SDK：它在「没有实现这个方法」或「载荷解不开」时回调
// ClientSideConnection.SetUnhandledHandler（副本里的 unhandled.go），把
// method + 原始 params 交出来。所以本文件不解析 JSON-RPC 信封，也不维护
// 「哪些方法/变体算标准」的白名单——SDK 以后支持了新变体，我们自动少接一类。
//
// 本文件只做路由，不解释内容：解释是后端的事（见 backends/codebuddy 等）。
// 目前生产代码没有注册任何 handler（两类消息都会走到「handler == nil」分支
// 被静默丢弃，与移除 stdout tee 之前的行为一致）；这里保留的是后端按需接线的
// 入口，不要据此以为某个变体已经在处理。
// 其余非标准处理的位置：_meta 挖掘在 acp_meta*.go，压缩检测的启发式在
// compact_detect.go，对端不合规的兜底在 acp_stdout_filter.go。

// ExtensionUpdate is a session/update variant the SDK's typed union does not
// model. Params is the intact `params` object, so the payload the SDK could not
// decode is still available to the handler.
type ExtensionUpdate struct {
	SessionID string
	Variant   string          // the raw sessionUpdate discriminator
	Params    json.RawMessage // the whole `params` object, unmodified
}

// ExtensionNotification is an agent→client JSON-RPC notification whose method
// the SDK does not dispatch. Params is the intact `params` object.
type ExtensionNotification struct {
	Method string
	Params json.RawMessage
}

// SetExtensionUpdateHandler registers the callback for session/update variants
// the SDK cannot decode. Passing nil disables handling (the default). Call it
// once, after the connection is created.
func (c *ClawBenchACPClient) SetExtensionUpdateHandler(h func(ExtensionUpdate)) {
	c.mu.Lock()
	c.extensionUpdateHandler = h
	c.mu.Unlock()
}

// SetExtensionNotificationHandler registers the callback for custom agent→client
// notification methods (any method the SDK does not dispatch). Passing nil
// disables handling (the default). Call it once, after the connection is created.
func (c *ClawBenchACPClient) SetExtensionNotificationHandler(h func(ExtensionNotification)) {
	c.mu.Lock()
	c.extensionNotificationHandler = h
	c.mu.Unlock()
}

// handleUnhandledNotification is the SDK's unhandled-message hook: it runs for
// every inbound notification the SDK answered -32601 (no such method) or -32602
// (payload did not decode) — exactly the two shapes this file exists for.
//
// It must never block: it runs on the SDK's inbound goroutine, so it only takes
// leaf locks and calls handlers that are non-blocking by contract.
func (c *ClawBenchACPClient) handleUnhandledNotification(_ context.Context, method string, params json.RawMessage) {
	if method == acp.ClientMethodSessionUpdate {
		c.dispatchSessionUpdateVariant(params)
		return
	}

	// A custom agent→client notification. Connection liveness, mirroring the
	// typed path — these are model-driven, so they also refresh the progress
	// clock. Lock-free (atomic), safe on the inbound goroutine.
	if c.connRef != nil {
		c.connRef.TouchSessionUpdate()
		c.connRef.TouchModelProgress()
	}

	c.mu.Lock()
	handler := c.extensionNotificationHandler
	c.mu.Unlock()
	if handler == nil {
		slog.Debug("acp: extension notification with no handler", "method", method)
		return
	}
	handler(ExtensionNotification{Method: method, Params: params})
}

// dispatchSessionUpdateVariant routes a session/update the SDK could not decode
// to the extension handler.
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
	if variant == "" {
		return
	}

	// Connection liveness, mirroring the typed path: every notification counts
	// as activity so an async workflow's connection is not swept. These frames
	// are model output, so they also refresh the progress clock. Both are
	// lock-free (atomic) — safe on the inbound goroutine.
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

// sessionUpdateVariantMismatch reports whether the SDK parsed an unknown variant
// into the WRONG known struct: the struct's preserved discriminator differs from
// its own name.
//
// Since schema 1.32 the union rejects a foreign discriminator at decode time, so
// this should never fire — it is the backstop for the typed path in case a future
// SDK release starts re-typing unknown frames again (v0.13.5 did exactly that,
// silently re-labeling every extension variant as SessionInfoUpdate).
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
