package codex

import (
	"context"
	"encoding/json"

	"clawbench/internal/ai"
)

// ---------------------------------------------------------------------------
// Codex mid-turn injection policy (_session/steering)
// ---------------------------------------------------------------------------
//
// codex-acp exposes the same ACP steering extension as claude-agent-acp:
//
//	request:  {sessionId, prompt:[{type:"text",text}]}
//	response: {outcome:"injected"|"startedNewTurn"|"failed"}
//
// Two important differences from claude:
//
//  1. It does NOT support the request-level `_meta.steering.idleBehavior`
//     option (its params parser reads only sessionId/prompt). So when no turn
//     is running it ALWAYS starts a new turn and returns "startedNewTurn".
//     The only safe guard is PromptInFlight: we decline before sending rather
//     than let the adapter detach a turn ClawBench does not own.
//  2. No user_message_chunk echo carrying our id, so there is no split
//     boundary — an injected message MERGES into the running reply. Registered
//     with MidTurnSplitsReply=false.
//
// Verified against codex-acp 2.2.2 by TestACPSteeringProbe
// (acp_steering_probe_integration_test.go).

// methodSessionSteering is the ACP steering extension method (see the claude
// policy for why it rides the raw channel rather than the SDK).
const methodSessionSteering = "_session/steering"

// steerContentBlock is one ACP content block in the steering request.
type steerContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// steerParams is the `_session/steering` request payload. Codex reads only
// sessionId and prompt; no _meta is sent.
type steerParams struct {
	SessionID string              `json:"sessionId"`
	Prompt    []steerContentBlock `json:"prompt"`
}

// steerResponse is the `_session/steering` reply.
type steerResponse struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// midTurnInjector implements ai.MidTurnInjector for Codex.
type midTurnInjector struct{}

// Inject attempts to place req.Content into Codex's running turn.
//
// It declines (Injected=false, no error) whenever injection is not clearly
// safe, so the caller falls back to its normal queueing path:
//
//   - attachments: the queue path already builds the file/directory prompt
//     prefixes; duplicating that here would drift. Declined.
//   - no ACP session yet: nothing is running.
//   - no prompt in flight: Codex would START A NEW TURN ("startedNewTurn") that
//     ClawBench does not track. This is the critical guard — Codex has no
//     idleBehavior option to ask for a clean decline.
//   - outcome != "injected": startedNewTurn (a race settled the turn between the
//     check and the call) or failed.
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
	// Critical: without a live turn Codex detaches a new one. Decline instead.
	if !transport.PromptInFlight() {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}

	raw, err := transport.CallRaw(ctx, methodSessionSteering, steerParams{
		SessionID: sessionID,
		Prompt:    []steerContentBlock{{Type: "text", Text: req.Content}},
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
	case "startedNewTurn":
		// A turn settled between the PromptInFlight check and the call. The
		// adapter started a turn we do not own — do NOT report success; the
		// caller will queue the message and the drain loop delivers it.
		return ai.MidTurnInjectResult{Reason: "stale"}, nil
	default: // "failed", "", or anything unknown
		return ai.MidTurnInjectResult{Reason: "rejected"}, nil
	}
}
