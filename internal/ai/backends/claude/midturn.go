package claude

import (
	"clawbench/internal/ai/steering"
)

// ---------------------------------------------------------------------------
// Claude mid-turn injection policy (_session/steering)
// ---------------------------------------------------------------------------
//
// claude-agent-acp exposes the ACP steering extension: a client injects a
// message into the turn that is already running so the model sees it before its
// next request, instead of the message waiting for the turn to end. Advertised
// via InitializeResponse._meta.steering.supported.
//
// Unlike CodeBuddy's `session/steer`, this adapter emits NO user_message_chunk
// echo carrying our id, so there is no split boundary: an injected message
// MERGES into the running reply rather than splitting it. That is why the
// backend is registered with MidTurnSplitsReply=false.
//
// Claude is the adapter that honors `_meta.steering.idleBehavior`, so it asks
// for "promptRequired" — a clean decline when no turn is running. The shared
// policy (internal/ai/steering) holds the rest.
//
// Verified against claude-agent-acp 0.89.1 by TestACPSteeringProbe
// (acp_steering_probe_integration_test.go).

var midTurn = steering.New(steering.Options{
	IdleBehavior:         "promptRequired",
	StartedNewTurnReason: "rejected",
})
