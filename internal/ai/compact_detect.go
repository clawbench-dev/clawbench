package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Compaction detection
// ---------------------------------------------------------------------------
//
// When an agent compacts a session's context, the conversation is rewritten into
// a summary. Whatever the injected system prompt contributed to the context
// (tool-use rules, the user-interaction contract, media rules) may no longer be
// present afterwards, so the next turn must re-inject it — independently of the
// periodic chat.system_prompt_interval setting, which defaults to "never".
//
// Detection is deliberately restricted to signals the AGENT itself emits for the
// compaction event. Heuristics over user-visible text (e.g. "the context usage
// dropped") are excluded on purpose: a false positive permanently changes the
// injection behavior of a session, and a false negative merely preserves the
// status quo.
//
// Per-backend signals (all verified against the shipped bundles / adapters):
//
//   - CodeBuddy (ACP + CLI): every session update emitted while the session is
//     compacting carries `_meta["codebuddy.ai/isCompactInternal"] = true` and,
//     when the purpose is known, `_meta["codebuddy.ai/compactType"]` ∈
//     {user-command, pre-message-auto, emergency-auto}. The ACP broadcast
//     service additionally re-sends the compaction summary as an
//     agent_message_chunk carrying the same flag.
//
//   - Claude agent-acp (ACP): on `compact_boundary` it emits an
//     agent_message_chunk whose text is "Compacting completed." (and
//     "Compacting..." while running). The boundary is also accompanied by a
//     usage_update with used=0, which is NOT used here — a genuine empty
//     context reports the same shape.
//
//   - Claude CLI: the same boundary arrives as a `system` message with
//     subtype `compact_boundary`.
//
// Negative signals — compaction that was cancelled or hit its limit — are
// explicitly NOT treated as compaction: the agent restores the original history
// in those cases, so the context was never rewritten.

const (
	// metaKeyCodeBuddyIsCompactInternal marks every session update emitted while
	// CodeBuddy is compacting the session context.
	metaKeyCodeBuddyIsCompactInternal = "codebuddy.ai/isCompactInternal"
	// metaKeyCodeBuddyCompactType carries the compaction purpose
	// (user-command | pre-message-auto | emergency-auto).
	metaKeyCodeBuddyCompactType = "codebuddy.ai/compactType"
	// metaKeyCodeBuddyCompactCancelled marks a compaction that was aborted; the
	// original history is restored, so the session is NOT considered compacted.
	metaKeyCodeBuddyCompactCancelled = "codebuddy.ai/compact-cancelled"
	// metaKeyCodeBuddyCompactLimitReached marks a compaction that gave up after
	// repeated max-token failures; likewise not a completed compaction.
	metaKeyCodeBuddyCompactLimitReached = "codebuddy.ai/compact-limit-reached"
)

// claudeACPCompactCompletedText is the exact text the claude-agent-acp adapter
// emits on a `compact_boundary` system message. Matched as a substring so
// surrounding whitespace/newlines in the chunk do not matter.
const claudeACPCompactCompletedText = "Compacting completed."

// IsCodeBuddyCompactionMeta reports whether a CodeBuddy _meta payload marks the
// session as compacting. The boolean flag is authoritative; compactType is
// accepted as an alternative carrier because the broadcast service attaches
// both, and older/newer builds may omit one.
func IsCodeBuddyCompactionMeta(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	if v, ok := meta[metaKeyCodeBuddyIsCompactInternal]; ok && metaBool(v) {
		return true
	}
	return metaString(meta[metaKeyCodeBuddyCompactType]) != ""
}

// IsCodeBuddyCompactionCancelledMeta reports whether a CodeBuddy _meta payload
// announces a compaction that did NOT complete (cancelled by the user, or the
// max-token limit was reached). The agent restores the original history in both
// cases, so the session must not be flagged as compacted.
func IsCodeBuddyCompactionCancelledMeta(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	_, cancelled := meta[metaKeyCodeBuddyCompactCancelled]
	_, limitReached := meta[metaKeyCodeBuddyCompactLimitReached]
	return cancelled || limitReached
}

// IsCompactCommand reports whether the user's message is the /compact slash
// command (with or without arguments, e.g. "/compact" or "/compact keep the
// API notes"). Used to flag the session immediately when the user asks for a
// compaction, instead of waiting for the agent to report it back.
//
// The command must be the FIRST token: "/compactfoo" is a different command,
// and a message merely mentioning /compact mid-sentence must not trigger it.
func IsCompactCommand(text string) bool {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return false
	}
	body := strings.TrimPrefix(t, "/")
	if i := strings.IndexAny(body, " \t\r\n"); i >= 0 {
		body = body[:i]
	}
	return body == "compact"
}

// IsClaudeCompactionText reports whether a streamed text chunk is the
// claude-agent-acp adapter's post-compaction notice ("Compacting completed.").
func IsClaudeCompactionText(text string) bool {
	return strings.Contains(text, claudeACPCompactCompletedText)
}

// isGrokCompactionMessage reports whether a grok stream event type announces a
// FINISHED compaction. The binary also emits auto_compact_started / *_failed /
// compact_cancelled — none of those mean the context was rewritten, so they are
// deliberately excluded.
func isGrokCompactionCompleted(eventType string) bool {
	switch eventType {
	case "compact_completed", "auto_compact_completed":
		return true
	}
	return false
}

// isCLICompactionMessage reports whether a parsed CLI stream-json message
// signals that the session context was compacted. Covers the shapes the Claude
// and CodeBuddy CLIs use (see the package comment for the per-backend list):
//
//   - Claude CLI: `system` with subtype `compact_boundary`.
//   - CodeBuddy CLI: `system` with subtype `status` and status `compacting`,
//     or any message whose providerData/_meta carries the compaction flag.
//
// Negative signals (a cancelled or limit-reached compaction) are not produced
// in the CLI stream-json shape, so nothing is excluded here.
func isCLICompactionMessage(msg *ClaudeStreamMessage) bool {
	if msg == nil {
		return false
	}
	if msg.Type == "system" {
		if msg.Subtype == "compact_boundary" {
			return true
		}
		if msg.Subtype == "status" && msg.Status == "compacting" {
			return true
		}
	}
	if msg.ProviderData != nil && (msg.ProviderData.IsCompactInternal || msg.ProviderData.CompactType != "") {
		return true
	}
	return IsCodeBuddyCompactionMeta(msg.Meta)
}

// metaBool reads a boolean field that may be a real bool or a JSON-ish
// string/number (the bundles serialize some flags as 1/"true").
func metaBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	}
	return false
}

// compactDetectedEvent builds the internal signal consumed by the service layer
// to flag a session as compacted. It is never forwarded to clients: the service
// layer intercepts it (like session_capture) and persists the flag instead.
func compactDetectedEvent(source string) StreamEvent {
	return StreamEvent{Type: "compact_detected", Content: source}
}

// acpUpdateMeta returns the per-variant _meta of an ACP session update, or nil
// when the variant carries none. ACP attaches _meta to each variant rather than
// to the SessionUpdate union, so detection has to look at the active branch.
func acpUpdateMeta(update acp.SessionUpdate) map[string]any {
	switch {
	case update.AgentMessageChunk != nil:
		return update.AgentMessageChunk.Meta
	case update.AgentThoughtChunk != nil:
		return update.AgentThoughtChunk.Meta
	case update.UserMessageChunk != nil:
		return update.UserMessageChunk.Meta
	case update.ToolCall != nil:
		return update.ToolCall.Meta
	case update.ToolCallUpdate != nil:
		return update.ToolCallUpdate.Meta
	case update.UsageUpdate != nil:
		return update.UsageUpdate.Meta
	case update.SessionInfoUpdate != nil:
		return update.SessionInfoUpdate.Meta
	case update.CurrentModeUpdate != nil:
		return update.CurrentModeUpdate.Meta
	case update.ConfigOptionUpdate != nil:
		return update.ConfigOptionUpdate.Meta
	}
	return nil
}
