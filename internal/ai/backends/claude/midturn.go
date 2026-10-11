package claude

import (
	"context"
	"encoding/json"

	"clawbench/internal/ai"
)

// ---------------------------------------------------------------------------
// Claude mid-turn injection policy (_session/steering)
// ---------------------------------------------------------------------------
//
// claude-agent-acp exposes the ACP steering extension: a client injects a
// message into the turn that is already running so the model sees it before
// its next request, instead of the message waiting for the turn to end.
// Advertised via InitializeResponse._meta.steering.supported.
//
//	request:  {sessionId, prompt:[{type:"text",text}], _meta:{steering:{idleBehavior?}}}
//	response: {outcome:"injected"|"promptRequired"|"startedNewTurn"|"failed", reason?}
//
// Unlike CodeBuddy's `session/steer`, this adapter emits NO user_message_chunk
// echo carrying our id, so there is no split boundary: an injected message
// MERGES into the running reply rather than splitting it. That is why the
// backend is registered with MidTurnSplitsReply=false.
//
// Verified against claude-agent-acp 0.89.1 by
// TestACPSteeringProbe (acp_steering_probe_integration_test.go).

// methodSessionSteering is the ACP steering extension method. It carries a "_"
// prefix, so the SDK's CallExtension could send it — but the call deliberately
// goes through the shared raw channel (RawRPCTransport.CallRaw) instead, which
// writes through the same lockedWriter as the SDK and thus cannot interleave.
const methodSessionSteering = "_session/steering"

// steerContentBlock is one ACP content block in the steering request.
type steerContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// steerParams is the `_session/steering` request payload.
type steerParams struct {
	SessionID string              `json:"sessionId"`
	Prompt    []steerContentBlock `json:"prompt"`
	Meta      *steerMeta          `json:"_meta,omitempty"`
}

// steerMeta carries the request-level steering options. idleBehavior is
// claude-only; setting it to "promptRequired" makes the adapter report
// {outcome:"promptRequired"} instead of silently starting a new turn when no
// turn is running — the host then owns the continuation.
type steerMeta struct {
	Steering steerMetaSteering `json:"steering"`
}

type steerMetaSteering struct {
	IdleBehavior string `json:"idleBehavior"`
}

// steerResponse is the `_session/steering` reply.
type steerResponse struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// midTurnInjector implements ai.MidTurnInjector for Claude.
type midTurnInjector struct{}

// Inject attempts to place req.Content into Claude's running turn.
//
// It declines (Injected=false, no error) whenever injection is not clearly
// safe, so the caller falls back to its normal queueing path:
//
//   - attachments: the queue path already builds the file/directory prompt
//     prefixes; duplicating that here would drift. Declined.
//   - no ACP session yet: nothing is running.
//   - no prompt in flight: nothing to join. Declined rather than let the
//     adapter start a turn we do not own (see PromptInFlight).
//   - outcome != "injected": promptRequired (idle), startedNewTurn, or failed.
func (m *midTurnInjector) Inject(
	ctx context.Context,
	transport ai.RawRPCTransport,
	req ai.MidTurnInjectRequest,
) (ai.MidTurnInjectResult, error) {
	if len(req.Files) > 0 {
		return ai.MidTurnInjectResult{Reason: "unsupported"}, nil
	}

	sessionID := transport.AcpSessionID()
	if sessionID == "" {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}
	// A turn must actually be running. Without this, an idle session would make
	// the adapter detach a new turn the host does not track.
	if !transport.PromptInFlight() {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}

	raw, err := transport.CallRaw(ctx, methodSessionSteering, steerParams{
		SessionID: sessionID,
		Prompt:    []steerContentBlock{{Type: "text", Text: req.Content}},
		// idleBehavior=promptRequired is defense in depth: PromptInFlight above
		// should already have caught the idle case, but if a turn settles in the
		// window between the check and the call, the adapter reports
		// promptRequired (a clean decline) rather than starting a rogue turn.
		Meta: &steerMeta{Steering: steerMetaSteering{IdleBehavior: "promptRequired"}},
	})
	if err != nil {
		return ai.MidTurnInjectResult{Reason: "error"}, err
	}

	var res steerResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return ai.MidTurnInjectResult{Reason: "error"}, err
	}

	switch res.Outcome {
	case "injected":
		return ai.MidTurnInjectResult{Injected: true}, nil
	case "promptRequired":
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	case "startedNewTurn":
		// Should not happen with idleBehavior=promptRequired, but never report a
		// detached turn as a successful injection.
		return ai.MidTurnInjectResult{Reason: "rejected"}, nil
	default: // "failed", "", or anything unknown
		return ai.MidTurnInjectResult{Reason: "rejected"}, nil
	}
}
