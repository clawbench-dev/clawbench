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
// Detection uses two kinds of evidence, in this order of preference:
//
//  1. A signal the AGENT emits specifically for the compaction (a _meta flag, a
//     tool call it would not otherwise make, a protocol event). Precise, no
//     false positives.
//  2. A collapse in the reported context occupancy (IsCompactionUsageDrop).
//     This is NOT a text heuristic — it is a numeric transition on the
//     protocol's own usage channel — but it is weaker: see that function for
//     its guards and its accepted false-positive surfaces.
//
// Text heuristics over the model's own prose remain excluded: the assistant
// saying "I compacted the conversation" is not evidence, and a false positive
// permanently changes the session's injection behavior.
//
// Per-backend signals (all re-verified against real agents on 2026-10-18):
//
//   - Claude agent-acp (ACP): a TOOL CALL — title "Compact conversation",
//     kind "think", `_meta.claudeCode.toolName` = "compact", and a terminal
//     rawOutput carrying {trigger, preTokens, postTokens, durationMs}.
//     Older adapters instead emitted an agent_message_chunk reading
//     "Compacting completed."; that path is kept for them.
//     NOTE: the boundary is also accompanied by a usage_update with used=0.
//     That is NOT used as a signal — a genuine empty context reports the same
//     shape (which is why IsCompactionUsageDrop excludes used=0).
//
//   - Codex agent-acp (ACP): the same tool call shape but WITHOUT any _meta,
//     so only the title identifies it.
//
//   - OpenCode (ACP): NO signal at all — no tool call, no _meta, no text
//     notice. Only the usage collapse is visible. This is the backend
//     IsCompactionUsageDrop exists for.
//
//   - CodeBuddy (ACP + CLI): historically flagged every update with
//     `_meta["codebuddy.ai/isCompactInternal"]` / `compactType`. Re-verified
//     2026-10-18 against CodeBuddy 2.168.2: the flag is GONE — a 1.2 MB wire
//     dump contained no compaction key at all, and the summary streams as
//     ordinary agent_message_chunks. The _meta detection is kept for older
//     builds. CodeBuddy's own summary leaves the occupancy unchanged, so the
//     drop heuristic does not cover it either: it is currently undetectable
//     through any protocol signal.
//
//   - Claude CLI: `system` + subtype `compact_boundary` (verified).
//
//   - Grok CLI: `compact_completed` / `auto_compact_completed` events.
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
//
// NOTE (verified 2026-10-18, claude-agent-acp against Claude Code 2.1.302):
// this text is no longer what the adapter sends. It now reports a compaction as
// a TOOL CALL (see IsCompactionToolCall). The text check is kept because older
// adapters still emit it, and the cost of keeping it is zero.
func IsClaudeCompactionText(text string) bool {
	return strings.Contains(text, claudeACPCompactCompletedText)
}

// compactionToolNames are the tool names agents use to report a compaction as a
// tool call rather than as a _meta flag or a text notice.
//
// Verified against real agents (2026-10-18):
//   - claude-agent-acp: title "Compact conversation", kind "think",
//     _meta.claudeCode.toolName = "compact"
//   - codex-acp: title "Compact conversation", kind "think", NO _meta
//
// Both are matched case-insensitively on the tool name so a casing change in a
// future adapter does not silently disable detection.
var compactionToolNames = map[string]bool{
	"compact": true,
}

// IsCompactionToolCall reports whether a tool call is the agent reporting a
// context compaction.
//
// Either signal is sufficient — name OR title. Using OR rather than "name wins"
// matters because the two adapters disagree on which one they send, and a
// future version of either could change: claude stamps
// _meta.claudeCode.toolName="compact" (title also present), codex-acp sends
// ONLY the title. A "name is authoritative, title is a fallback" rule would go
// silently blind the day an adapter wraps compaction in a differently-named
// tool while keeping the title.
//
// Both matches are narrow (exact name in compactionToolNames, exact
// case-insensitive title), so OR does not widen the false-positive surface:
// "Write compact.md" matches neither.
func IsCompactionToolCall(name, title string) bool {
	if compactionToolNames[strings.ToLower(strings.TrimSpace(name))] {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(title), compactionToolCallTitle)
}

// compactionToolCallTitle is the title codex-acp (and claude-agent-acp) use for
// the compaction pseudo-tool. Kept as a named constant so the narrowness of the
// match is visible at the definition rather than buried in a comparison.
const compactionToolCallTitle = "Compact conversation"

// IsCompactionUsageDrop reports whether a usage transition looks like a
// compaction: the context occupancy fell sharply while the window size stayed
// the same.
//
// WHY THIS EXISTS — some agents compact with NO protocol signal at all.
// Measured 2026-10-18 against real agents:
//
//	claude    (ACP)  49744 -> 2258   with a compact tool call
//	codex     (ACP)  19898 -> 4891   with a compact tool call
//	opencode  (ACP)  20663 -> 1893   with NOTHING — no tool call, no _meta,
//	                                 no text notice; only the usage drop
//	codebuddy (ACP)  46509 -> 46509  (its summary streams as ordinary text)
//
// OpenCode therefore cannot be detected any other way. The package comment
// above explains why heuristics over user-visible text are excluded; this is a
// NUMERIC signal from the protocol's own usage channel, not a text heuristic,
// and it is the only evidence some agents ever produce.
//
// The guards that keep it from firing on ordinary turns:
//   - both snapshots must report the SAME window size (a size change means the
//     agent switched model or reconnected, not that it compacted);
//   - the drop must be at least compactUsageDropRatio of the previous used
//     count. A turn's context grows; it does not lose 60% of itself. Ordinary
//     turns end slightly above where they started.
//   - a floor on both sides: below compactUsageMinTokens the ratio is noise (a
//     10-token context dropping to 4 is a 60% "drop" with no meaning), and an
//     occupancy of ZERO is excluded outright — see the nextUsed guard below.
//
// KNOWN false-positive surfaces, accepted deliberately:
//   - same-size model switch (sonnet -> opus are both 200k): if the agent
//     rebuilds its context, used collapses without a compaction. Cost is one
//     extra system-prompt re-injection, not incorrect content.
//   - a rewind/reset that keeps the same window size. Same cost.
//
// Both are cheap and self-correcting; missing a real compaction is not (the
// system prompt stays absent for the rest of the session).
func IsCompactionUsageDrop(prev, next *UsageState) bool {
	if prev == nil || next == nil {
		return false
	}
	// Same window required: a different size means a different model/connection.
	if prev.Size <= 0 || next.Size != prev.Size {
		return false
	}
	if prev.Used < compactUsageMinTokens {
		return false
	}
	// An EMPTY context is not evidence of a compaction. `used:0` with a real
	// window is a legal snapshot meaning "nothing in context" (see the package
	// comment: claude's compact_boundary is itself accompanied by used=0, which
	// is exactly why that signal is not used). Without this guard the drop
	// ratio is 1.0 every time and EVERY such notification would be reported as
	// a compaction — silently re-injecting the system prompt every turn.
	// All three observed real compactions leave a non-zero summary (2258 /
	// 4891 / 1893), so this costs nothing.
	if next.Used <= 0 {
		return false
	}
	// A rise (or no change) is never a compaction.
	if next.Used >= prev.Used {
		return false
	}
	drop := float64(prev.Used-next.Used) / float64(prev.Used)
	return drop >= compactUsageDropRatio
}

// IsCompactionUsageDropFromWire is IsCompactionUsageDrop for a LIVE notification
// rather than two merged snapshots.
//
// It differs in one way that matters: the incoming notification must carry its
// OWN window size. MergeUsageState keeps `size` sticky, so a "naked" update
// (used=3000, size=0) merged against a 200k snapshot produces a merged Size of
// 200000 — the size-equality guard would then pass while comparing a REAL
// snapshot against a PARTIAL one. Requiring the wire size makes "two genuine
// snapshots" an actual precondition, which is what the guard is for.
func IsCompactionUsageDropFromWire(prev *UsageState, incoming *UsageState) bool {
	if incoming == nil || incoming.Size <= 0 {
		return false
	}
	return IsCompactionUsageDrop(prev, incoming)
}

const (
	// compactUsageDropRatio is the fraction of the previous occupancy that must
	// disappear before the transition is treated as a compaction. 0.6 sits well
	// clear of a normal turn (which grows) and well below the observed real
	// compactions (0.95, 0.75, 0.91).
	compactUsageDropRatio = 0.6
	// compactUsageMinTokens is the floor below which the ratio is meaningless.
	compactUsageMinTokens = 1000
)

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
