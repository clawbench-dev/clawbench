// Package steering implements the shared `_session/steering` mid-turn injection
// policy used by the ACP adapters that expose it (claude-agent-acp, codex-acp).
//
// The wire shape is the same for both:
//
//	request:  {sessionId, prompt:[{type:"text",text}], _meta?:{steering:{idleBehavior?}}}
//	response: {outcome:"injected"|"promptRequired"|"startedNewTurn"|"failed", reason?}
//
// What differs is what the adapter does when no turn is running, and how the
// host should read the outcome — that is all Options carries. Everything else
// (the guard sequence, the request encoding, the reply decoding) is shared, so
// a new steering-capable backend is a few lines, not a copy of the policy.
package steering

import (
	"context"
	"encoding/json"

	"clawbench/internal/ai"
)

// methodSessionSteering is the ACP steering extension method. Its "_" prefix
// makes it a documented extension, so the call goes through CallExtension —
// which validates the prefix, so a rename that drops it fails locally instead
// of on the wire.
const methodSessionSteering = "_session/steering"

// Options tunes the shared policy for one adapter.
type Options struct {
	// IdleBehavior is sent as `_meta.steering.idleBehavior` when non-empty.
	//
	// claude-agent-acp supports "promptRequired": with no turn running it
	// reports {outcome:"promptRequired"} instead of starting one, which is a
	// clean decline. codex-acp's params parser reads only sessionId/prompt, so
	// it leaves this empty and relies on the PromptInFlight guard alone.
	IdleBehavior string

	// StartedNewTurnReason is the Reason reported when the adapter detached a
	// turn the host does not own. claude calls it "rejected"; codex calls it
	// "stale" (a turn settled between the PromptInFlight check and the call).
	// The value is log-only; the behavior is identical either way.
	StartedNewTurnReason string
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type steerParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
	Meta      *steerMeta     `json:"_meta,omitempty"`
}

type steerMeta struct {
	Steering steerMetaSteering `json:"steering"`
}

type steerMetaSteering struct {
	IdleBehavior string `json:"idleBehavior"`
}

type steerResponse struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// Injector implements ai.MidTurnInjector for a steering-capable adapter.
type Injector struct {
	opts Options
}

// New returns the mid-turn policy for one steering-capable adapter.
func New(opts Options) ai.MidTurnInjector {
	if opts.StartedNewTurnReason == "" {
		opts.StartedNewTurnReason = "rejected"
	}
	return &Injector{opts: opts}
}

// Inject attempts to place req.Content into the running turn.
//
// It declines (Injected=false, no error) whenever injection is not clearly
// safe, so the caller falls back to its normal queueing path:
//
//   - no ACP session yet: nothing is running.
//   - no prompt in flight: the adapter would START A NEW TURN the host does not
//     track (a "ghost turn": its output streams outside turn bookkeeping).
//     Codex has no idleBehavior option to ask for a clean decline, so this
//     check is the only guard there.
//   - outcome != "injected": the turn settled (promptRequired / startedNewTurn)
//     or the call failed.
//
// Attachments need no special case: req.Content already carries the
// "[User uploaded …]" / quote headers core flow built for it, exactly as a
// normal turn would.
func (i *Injector) Inject(
	ctx context.Context,
	transport ai.RawRPCTransport,
	req ai.MidTurnInjectRequest,
) (ai.MidTurnInjectResult, error) {
	sessionID := transport.AcpSessionID()
	if sessionID == "" {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}
	// A turn must actually be running. Without this, an idle session would make
	// the adapter detach a new turn the host does not track.
	if !transport.PromptInFlight() {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}

	params := steerParams{
		SessionID: sessionID,
		Prompt:    []contentBlock{{Type: "text", Text: req.Content}},
	}
	if i.opts.IdleBehavior != "" {
		// Defense in depth for adapters that honor it: PromptInFlight above
		// should already have caught the idle case, but if a turn settles in the
		// window between the check and the call, the adapter reports
		// promptRequired (a clean decline) rather than starting a rogue turn.
		params.Meta = &steerMeta{Steering: steerMetaSteering{IdleBehavior: i.opts.IdleBehavior}}
	}

	raw, err := transport.CallExtension(ctx, methodSessionSteering, params)
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
		// The adapter started a turn we do not own — never report success; the
		// caller queues the message and the drain loop delivers it.
		return ai.MidTurnInjectResult{Reason: i.opts.StartedNewTurnReason}, nil
	default: // "failed", "", or anything unknown
		return ai.MidTurnInjectResult{Reason: "rejected"}, nil
	}
}
