package codex

import (
	"clawbench/internal/ai/steering"
)

// ---------------------------------------------------------------------------
// Codex mid-turn injection policy (_session/steering)
// ---------------------------------------------------------------------------
//
// codex-acp exposes the same ACP steering extension as claude-agent-acp. Two
// differences shape the options below:
//
//  1. It does NOT support the request-level `_meta.steering.idleBehavior`
//     option (its params parser reads only sessionId/prompt). So when no turn
//     is running it ALWAYS starts a new turn and returns "startedNewTurn".
//     The only safe guard is PromptInFlight, which the shared policy consults
//     before sending.
//  2. No user_message_chunk echo carrying our id, so there is no split
//     boundary — an injected message MERGES into the running reply. Registered
//     with MidTurnSplitsReply=false.
//
// Verified against codex-acp 2.2.2 by TestACPSteeringProbe
// (acp_steering_probe_integration_test.go).

var midTurn = steering.New(steering.Options{
	// No IdleBehavior: codex's params parser ignores _meta.
	StartedNewTurnReason: "stale",
})
