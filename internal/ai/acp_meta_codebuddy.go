package ai

// ---------------------------------------------------------------------------
// CodeBuddy ACP _meta adapter
// ---------------------------------------------------------------------------
//
// CodeBuddy (`codebuddy --acp`) packs rich metadata into _meta:
//
//   - `_meta.usage` — OpenAI-style token usage:
//     { prompt_tokens, completion_tokens, completion_thinking_tokens,
//       prompt_cache_hit_tokens, prompt_cache_miss_tokens,
//       prompt_cache_write_tokens, cache_read_input_tokens,
//       cache_creation_input_tokens, completion_tokens_details, credit }
//   - `_meta.codebuddy.ai/usageByCategory` — context-window breakdown:
//     { conversation, tools, systemPrompt, skills, mcp, version }
//   - `_meta.codebuddy.ai/*` — trace/identity:
//     { requestId, traceId, traceparent, messageId, messageRequestId,
//       requestModelId, requestModelName, responseModelId,
//       finishReason, outcome, agentPhase, progress }
//
// These appear on both session/update notifications (on the per-variant
// update._meta) and the PromptResponse._meta.

// The CodeBuddy _meta PARSER now lives in
// internal/ai/backends/codebuddy/meta: the key names, the shapes and the quirks
// (a usage block per model call, a re-reported credit total, a refusal whose
// cause hides in a JSON envelope) are that backend's own business. The shared
// layer reaches it through ACPBehavior.ExtractMeta / RefusalDetail.
//
// What stays in this package is the set of CodeBuddy keys the SHARED EVENT LOOP
// has to read while routing frames — the compaction flags and the replay
// identity — because those decisions are made per-notification by code that
// cannot be moved out of the loop. Each is a narrow, documented read of one key.

// CodeBuddy keys the SHARED event loop still reads directly.
//
// Everything else CodeBuddy-specific moved to the backend's own sub-package
// (internal/ai/backends/codebuddy) and reaches the loop through an ACPBehavior
// hook. What remains here is the set of keys the loop must read PER FRAME while
// routing it, where the decision itself is shared:
//
//   - the replay-identity keys (requestId / messageId / traceId): the loop's
//     log lines and the connection's chunk-dedup key name them, and a hook
//     cannot cover a key the loop drops a frame on;
//   - the refusal keys: read only by the refusal-detail hook's callers-in-test.
//
// Each is a narrow, documented read of one key — not a behaviour branch on a
// backend id.
const (
	//nolint:gosec // G101: a _meta key name, not a credential
	metaKeyCodeBuddyRequestCredits = "codebuddy.ai/requestCredits"
	// metaKeyCodeBuddyRequestID is the per-generation request id. The shared
	// event loop logs it when it drops a replayed chunk; the replay hook reads
	// it to compare against the completed-turn baseline.
	metaKeyCodeBuddyRequestID = "codebuddy.ai/requestId"
	// metaKeyCodeBuddyConversationRequestID is the turn-level request id,
	// preferred over RequestID in the chunk de-duplication key (matching
	// upstream's `conversationRequestId ?? requestId`).
	metaKeyCodeBuddyConversationRequestID = "codebuddy.ai/conversationRequestId"
	metaKeyCodeBuddyTraceID               = "codebuddy.ai/traceId"
	metaKeyCodeBuddyMessageID             = "codebuddy.ai/messageId"
	// metaKeyCodeBuddyErrorMessage carries the refusal payload on a
	// stopReason=refusal PromptResponse (a JSON-encoded {code,message,data}).
	metaKeyCodeBuddyErrorMessage = "codebuddy.ai/errorMessage"
)

// costFieldCarriesCredit reports whether a backend repurposes the
// ACP-standard usage_update.cost field to carry a CREDIT amount rather than a
// monetary cost.
//
// The fact is read from the backend's registered ACPBehavior
// (CostFieldCarriesCredit), not hard-coded here. The amount is still real
// billing: the cost-only paths (generation tools, team-member turns) carry no
// usage block, so it is captured as MetaExtraction.CostCredit instead of being
// discarded, and never let into chat_metadata.cost_usd.
func costFieldCarriesCredit(backendID string) bool {
	if b := acpBehavior(backendID); b != nil {
		return b.CostFieldCarriesCredit
	}
	return false
}

// ACPConnIsCompletedTurnRequestID reports whether rid belongs to a turn that has
// already completed on this connection (and is not the currently adopted
// agent-initiated turn).
//
// It exists for backends whose own replay hook needs the same baseline
// comparison this package's event loop uses: the requestId set lives on
// ACPConn, whose fields are private to this package. Backends that declare
// ACPBehavior.IsReplayedTurnMeta call this to answer their own question; it
// carries no backend knowledge itself.
func ACPConnIsCompletedTurnRequestID(conn *ACPConn, rid string) bool {
	if conn == nil {
		return false
	}
	return conn.isCompletedTurnRequestID(rid)
}
