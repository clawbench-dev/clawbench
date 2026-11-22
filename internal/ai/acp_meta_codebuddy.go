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

const (
	// metaKeyCodeBuddyIsCompactInternal marks every session update emitted while
	// CodeBuddy is performing a compaction (see IsCodeBuddyCompactionMeta).
	metaKeyCodeBuddyIsCompactInternal = "codebuddy.ai/isCompactInternal"
	// metaKeyCodeBuddyCompactType carries the compaction purpose
	// ("auto" / "manual") on those updates.
	metaKeyCodeBuddyCompactType = "codebuddy.ai/compactType"
	// metaKeyCodeBuddyCompactCancelled marks a compaction that was aborted; the
	// agent restores the original history, so the session must NOT be flagged.
	metaKeyCodeBuddyCompactCancelled = "codebuddy.ai/compact-cancelled"
	// metaKeyCodeBuddyCompactLimitReached marks a compaction that gave up after
	// hitting the token limit (same restore, same "not compacted" verdict).
	metaKeyCodeBuddyCompactLimitReached = "codebuddy.ai/compact-limit-reached"
	// metaKeyCodeBuddyRequestID is the turn's requestId, used by the replay
	// baseline comparison (CodeBuddy re-emits the previous turn's tail).
	metaKeyCodeBuddyRequestID = "codebuddy.ai/requestId"
	// metaKeyCodeBuddyTraceID is the trace/identity key a real frame carries
	// alongside requestId. Named here because the shared event loop's replay
	// diagnostics and the wire-shape tests both reference it.
	metaKeyCodeBuddyTraceID = "codebuddy.ai/traceId"
	// metaKeyCodeBuddyConversationRequestID is the turn-level request id, read by
	// the chunk de-duplication key (upstream prefers `conversationRequestId ??
	// requestId`).
	metaKeyCodeBuddyConversationRequestID = "codebuddy.ai/conversationRequestId"
	// metaKeyCodeBuddyErrorMessage carries the refusal payload on a
	// stopReason=refusal PromptResponse (a JSON {code,message,data} blob whose
	// data.details holds the real cause). Parsed by codebuddy/meta; the key is
	// named here because the refusal path lives in the shared prompt loop.
	metaKeyCodeBuddyErrorMessage = "codebuddy.ai/errorMessage"
	// metaKeyCodeBuddyErrorMessageBare is the same payload under an unprefixed
	// key, used on the out-of-band control-command failure path.
	metaKeyCodeBuddyErrorMessageBare = "errorMessage"
	// metaKeyCodeBuddyMessageID identifies the model call that produced a frame.
	// The mid-turn steer boundary matches it against the clientUserMessageId we
	// sent, which is how an injected message is recognized where it landed.
	metaKeyCodeBuddyMessageID = "codebuddy.ai/messageId"
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

// IsCodeBuddyCompactionMeta reports whether a CodeBuddy _meta payload marks the
// session as being compacted right now.
//
// CodeBuddy flags EVERY update emitted during a compaction (isCompactInternal)
// and restates the purpose in compactType. Either is enough: an update that
// carried only the type is still evidence of a compaction in progress.
func IsCodeBuddyCompactionMeta(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	if v, ok := m[metaKeyCodeBuddyIsCompactInternal]; ok && metaBool(v) {
		return true
	}
	return metaString(m[metaKeyCodeBuddyCompactType]) != ""
}

// IsCodeBuddyCompactionCancelledMeta reports whether a CodeBuddy _meta payload
// announces a compaction that did NOT complete (cancelled by the user, or the
// max-token limit was reached). The agent restores the original history in both
// cases, so the session must not be flagged as compacted.
func IsCodeBuddyCompactionCancelledMeta(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	_, cancelled := m[metaKeyCodeBuddyCompactCancelled]
	_, limitReached := m[metaKeyCodeBuddyCompactLimitReached]
	return cancelled || limitReached
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
