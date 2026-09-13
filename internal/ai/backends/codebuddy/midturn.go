package codebuddy

import (
	"context"
	"encoding/json"

	"clawbench/internal/ai"
)

// ---------------------------------------------------------------------------
// CodeBuddy mid-turn injection policy (session/steer)
// ---------------------------------------------------------------------------
//
// CodeBuddy exposes a private `session/steer` method: it injects a user message
// into the turn that is already running, so the model sees it before its next
// request, instead of the message waiting for the turn to end. Verified against
// `codebuddy --acp` (see docs/dev/codebuddy_acp_extensions.md §9.2):
//
//	request:  {sessionId, contentBlocks, clientUserMessageId?, expectedRequestId?, _meta?}
//	response: {steered: true, ownerRequestId} | {steered: false, reason: "idle"|"stale"}
//
// All CodeBuddy-specific knowledge about this feature lives here; the shared
// transport (internal/ai/acp_raw_rpc.go) and the shared seam
// (internal/ai/midturn.go) know nothing about steer.

// methodSessionSteer is CodeBuddy's private mid-turn injection method. It is
// deliberately NOT prefixed with "_", which is why the ACP SDK cannot send it
// and the raw channel exists.
const methodSessionSteer = "session/steer"

// steerContentBlock is one ACP content block in the steer request.
type steerContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// steerParams is the `session/steer` request payload.
type steerParams struct {
	SessionID           string              `json:"sessionId"`
	ContentBlocks       []steerContentBlock `json:"contentBlocks"`
	ClientUserMessageID string              `json:"clientUserMessageId,omitempty"`
}

// steerResponse is the `session/steer` reply.
type steerResponse struct {
	Steered        bool   `json:"steered"`
	OwnerRequestID string `json:"ownerRequestId"`
	Reason         string `json:"reason"`
}

// midTurnInjector implements ai.MidTurnInjector for CodeBuddy.
type midTurnInjector struct{}

// Inject attempts to place req.Content into CodeBuddy's running turn.
//
// It declines (returning Injected=false, no error) whenever injection is not
// clearly safe, so the caller falls back to its normal queueing path:
//
//   - attachments: the queue path already builds the file/directory prompt
//     prefixes; duplicating that here would drift. Declined.
//   - no ACP session yet: nothing is running.
//   - agent replied {steered:false}: the turn ended ("idle") or the client's
//     expectedRequestId no longer matches ("stale"). Both mean "queue it".
//
// A transport error is returned so the shared seam can log it; the caller still
// falls back to queueing either way.
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

	// Register the echo BEFORE sending: the agent may emit the receipt frame
	// before our CallRaw returns, and the boundary observer must already know
	// this id is ours (otherwise it would be dropped as a replay/broadcast).
	tracker, tracksEcho := transport.(ai.SteerEchoTracker)
	if tracksEcho {
		tracker.ExpectSteerEcho(req.ClientUserMessageID)
	}
	// forgetEcho drops the registration on any path where no injection landed,
	// so a declined steer cannot leave a stale entry for the connection's life.
	forgetEcho := func() {
		if tracksEcho {
			tracker.ForgetSteerEcho(req.ClientUserMessageID)
		}
	}

	raw, err := transport.CallRaw(ctx, methodSessionSteer, steerParams{
		SessionID:           sessionID,
		ContentBlocks:       []steerContentBlock{{Type: "text", Text: req.Content}},
		ClientUserMessageID: req.ClientUserMessageID,
		// expectedRequestId is intentionally omitted: tracking the in-flight
		// turn's request id would require reading _meta on every update, and
		// without it the agent simply reports reason:"idle" once the turn ends,
		// which we already handle.
	})
	if err != nil {
		forgetEcho()
		return ai.MidTurnInjectResult{Reason: "error"}, err
	}

	var res steerResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		forgetEcho()
		return ai.MidTurnInjectResult{Reason: "error"}, err
	}

	if !res.Steered {
		forgetEcho()
		switch res.Reason {
		case "idle", "stale":
			return ai.MidTurnInjectResult{Reason: res.Reason}, nil
		default:
			return ai.MidTurnInjectResult{Reason: "rejected"}, nil
		}
	}

	return ai.MidTurnInjectResult{
		Injected:       true,
		OwnerRequestID: res.OwnerRequestID,
	}, nil
}
