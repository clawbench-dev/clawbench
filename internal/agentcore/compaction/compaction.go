// Package compaction decides when a conversation no longer fits a model's
// context window, and rewrites the older part of it into a summary.
//
// It exists because the built-in agent owns its own context: unlike an external
// CLI (which compacts on its own and merely tells us afterwards), nothing here
// notices the window filling up. Before this package the history builder simply
// dropped the oldest turns once a character budget was exceeded — the agent
// silently forgot what was said instead of summarizing it.
//
// The package is deliberately leaf-shaped: it depends on llm (for the message
// shape) and the standard library, so it can be unit-tested without a database,
// a model endpoint, or the HTTP server.
package compaction

import (
	"strings"

	"clawbench/internal/agentcore/llm"
)

// Tunables. They are variables rather than constants so a test can lower them
// without allocating megabyte-sized fixtures.
var (
	// TriggerRatio is the fraction of the model's context window at which a
	// conversation is compacted.
	//
	// 0.75 is deliberately below 1: the turn that triggers compaction still has
	// to run (a tool loop can add several rounds of output), and hitting the
	// limit mid-turn produces an API error the user cannot act on. Compacting
	// early costs one extra model call; compacting late costs a failed turn.
	TriggerRatio = 0.75

	// KeepRecentRatio is the fraction of the window reserved for turns kept
	// verbatim when compacting.
	//
	// The rest of the window is for the summary, the system prompt, the reply
	// and whatever tool results the next round adds. A fixed TURN COUNT cannot
	// work here: with long turns, "keep the last four" can be larger than the
	// whole window, so compaction would never get under it.
	KeepRecentRatio = 0.4

	// KeepRecentToolRounds is how many tool results inside the CURRENT exchange
	// are kept verbatim when a single turn overflows on its own.
	//
	// A turn that reads several large files exceeds any window without the
	// conversation ever ending, so waiting for the next exchange to compact
	// waits forever. Two rounds is enough to keep the reasoning that is
	// currently in play while discarding the file bodies already digested.
	KeepRecentToolRounds = 2

	// MaxSummaryTokens caps the summary's length.
	//
	// A summary that is allowed to grow to the size of the conversation it
	// replaced has not saved anything, so this is a hard cap rather than a
	// target.
	MaxSummaryTokens = 1200

	// SummaryPrompt is the instruction given to the model when compacting.
	SummaryPrompt = `You are compacting a conversation so it can continue in a smaller context window.

Write a summary of the conversation below that lets the conversation continue without the original messages.

Include, when present:
- What the user is trying to accomplish, in their own terms.
- Decisions already made and the reasoning behind them.
- Facts established about the codebase/files under discussion (paths, names, values).
- What was tried and did not work, so it is not retried.
- Anything still open or pending.

Omit:
- Greetings, acknowledgements, and other pleasantries.
- The full text of files that were read (say which file and what mattered in it).
- Your own reasoning process.

Write in the same language the user has been using. Be specific and factual: a later turn will rely on this summary as its only record of what happened.`
)

// EstimateTokens approximates the token count of a string.
//
// This is a heuristic, and it is biased UP on purpose:
//
//   - CJK text is roughly one token per character (some tokenizers are worse).
//   - Latin text is roughly one token per four characters.
//
// Over-estimating makes compaction fire slightly early, which is harmless.
// Under-estimating lets a conversation run past the window, which is a hard
// failure. Every caller uses this as a budget, so the bias must point that way.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	var cjk, other int
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	// 1 token per CJK rune, 1 token per 4 other runes, rounded up.
	return cjk + (other+3)/4
}

// isCJK reports whether r is a Han character or a CJK punctuation/fullwidth form.
//
// Mirrors the ranges the i18n guard uses, so "is this Chinese text" has one
// definition in the codebase.
func isCJK(r rune) bool {
	switch {
	case r >= 0x4e00 && r <= 0x9fff: // CJK Unified Ideographs
		return true
	case r >= 0x3000 && r <= 0x303f: // CJK punctuation
		return true
	case r >= 0xff00 && r <= 0xffef: // Fullwidth forms
		return true
	case r >= 0xfe30 && r <= 0xfe4f: // CJK compatibility forms
		return true
	}
	return false
}

// MessageTokens estimates the token cost of one message, including a small
// per-message overhead for the role and framing tokens every API adds.
func MessageTokens(m llm.Message) int {
	// 4 tokens of framing per message is the figure OpenAI publishes for chat
	// completions; Anthropic's is similar. It matters only for conversations
	// with hundreds of short messages.
	total := 4
	for _, p := range m.Parts {
		switch {
		case p.Text != "":
			total += EstimateTokens(p.Text)
		case p.ToolCall != nil:
			total += EstimateTokens(p.ToolCall.Name) + EstimateTokens(p.ToolCall.Arguments) + 4
		case p.ToolResult != nil:
			for _, rp := range p.ToolResult.Parts {
				total += EstimateTokens(rp.Text)
			}
			total += 4
		case p.Image != nil:
			// An image's token cost depends on its resolution, which is not
			// knowable here. A flat estimate keeps a conversation full of
			// screenshots from being treated as free.
			total += 1000
		}
	}
	return total
}

// ConversationTokens estimates the token cost of a whole conversation.
func ConversationTokens(system string, messages []llm.Message) int {
	total := EstimateTokens(system)
	for _, m := range messages {
		total += MessageTokens(m)
	}
	return total
}

// KeepBudget returns the token budget for turns kept verbatim.
func KeepBudget(windowTokens int) int {
	if windowTokens <= 0 {
		return 0
	}
	return int(float64(windowTokens) * KeepRecentRatio)
}

// ShouldCompact reports whether a conversation should be compacted before the
// next model call.
//
// windowTokens <= 0 means the model's window is unknown. That returns false:
// guessing would compact conversations that fit perfectly, and an unknown model
// is usually a self-hosted endpoint with a generous window rather than a tiny
// one.
func ShouldCompact(system string, messages []llm.Message, windowTokens int) bool {
	if windowTokens <= 0 || len(messages) == 0 {
		return false
	}
	return float64(ConversationTokens(system, messages)) >= float64(windowTokens)*TriggerRatio
}

// SplitPoint returns the index of the first message that is kept verbatim.
//
// It keeps the most recent exchanges that fit within keepBudgetTokens, and at
// least one. Walking backwards from the newest message (rather than keeping a
// fixed count of turns) is what makes this converge: with a fixed count, a
// conversation of long turns can keep four turns that are themselves larger
// than the window, so compaction runs forever without getting under it.
//
// The walk stops at an exchange BOUNDARY, so a tool result is never separated
// from the assistant turn that asked for it. Cutting between a tool call and its
// result produces a conversation the API rejects outright.
//
// A return of 0 means "nothing to compact" (everything fits the keep budget, or
// there is only one exchange).
func SplitPoint(messages []llm.Message, keepBudgetTokens int) int {
	if len(messages) == 0 || keepBudgetTokens <= 0 {
		return 0
	}

	// Collect exchange STARTS, newest first. An exchange begins at a user
	// message that is not merely carrying tool results (both protocols deliver
	// tool results inside a user-role message, so a naive count would treat one
	// tool round as several exchanges).
	var starts []int
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser && !isToolResultOnly(messages[i]) {
			starts = append(starts, i)
		}
	}
	if len(starts) < 2 {
		// One exchange (or none): there is no "older part" to summarize.
		return 0
	}

	// starts[0] is the newest exchange's start, starts[1] the one before it, and
	// so on. Walk newest → oldest accumulating each exchange's cost; stop when
	// adding the next older exchange would exceed the keep budget. The split is
	// then that exchange's start — everything older is summarized, and the
	// exchanges already accumulated stay verbatim.
	kept := 0
	for i, start := range starts {
		// The exchange occupying [start, end) where end is the NEXT (newer)
		// exchange's start, or the end of the conversation for the newest one.
		end := len(messages)
		if i > 0 {
			end = starts[i-1]
		}
		cost := messagesTokens(messages[start:end])
		if i > 0 && kept+cost > keepBudgetTokens {
			// This older exchange does not fit. The newest one that DID fit is
			// starts[i-1], so the split is there — not here, or this exchange
			// would be dropped even though the accumulated ones alone already
			// exceed nothing.
			return starts[i-1]
		}
		kept += cost
	}
	// Every exchange fits the keep budget, yet the caller asked to compact (the
	// whole conversation exceeds the window). Split at the oldest boundary so
	// progress is made instead of looping.
	return starts[len(starts)-1]
}

// messagesTokens sums the token estimate of a message range.
func messagesTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += MessageTokens(m)
	}
	return total
}

// ToolResultSplitPoint finds a cut inside the CURRENT exchange, for the case a
// turn overflows its window on its own.
//
// A single turn that reads several large files exceeds any window without the
// conversation ever ending, so waiting for the next exchange is waiting
// forever. The cut lands on a tool-result message (never between a call and its
// result), keeping the last KeepRecentToolRounds results verbatim.
//
// Returns 0 when there is nothing to fold: fewer results than we keep, or no
// tool results at all.
func ToolResultSplitPoint(messages []llm.Message) int {
	// Collect the indexes of tool-result-bearing messages, newest first.
	var resultIdx []int
	for i := len(messages) - 1; i >= 0; i-- {
		if isToolResultOnly(messages[i]) {
			resultIdx = append(resultIdx, i)
		}
	}
	if len(resultIdx) <= KeepRecentToolRounds {
		return 0
	}
	// The kept tail starts at the earliest KEPT result. Everything before that
	// index is folded — including the assistant turn that asked for those
	// results, which is correct: its request is part of the folded record.
	return resultIdx[KeepRecentToolRounds-1]
}

// FoldToolResults replaces the tool results before `upTo` with a single
// assistant message recording what they were.
//
// This keeps the call/result pairing valid: the assistant turns that REQUESTED
// the folded results stay in place (their tool_use blocks are what the API
// pairs against), and their results are replaced by one summary line per call.
// Dropping a result while keeping its call is what the API rejects, so the
// replacement is a result-shaped message, not a deletion.
func FoldToolResults(messages []llm.Message, upTo int) []llm.Message {
	if upTo <= 0 || upTo > len(messages) {
		return messages
	}
	out := make([]llm.Message, 0, len(messages))
	for i, m := range messages {
		if i >= upTo || !isToolResultOnly(m) {
			out = append(out, m)
			continue
		}
		// Replace each result with a short note, preserving its call id.
		var parts []llm.Part
		for _, p := range m.Parts {
			if p.ToolResult == nil {
				continue
			}
			parts = append(parts, llm.Part{ToolResult: &llm.ToolResult{
				CallID:  p.ToolResult.CallID,
				IsError: p.ToolResult.IsError,
				Parts:   []llm.Part{{Text: "[tool output omitted to fit the context window; it was folded into the summary above]"}},
			}})
		}
		out = append(out, llm.Message{Role: llm.RoleTool, Parts: parts})
	}
	return out
}

// isToolResultOnly reports whether a message carries only tool results.
//
// Both wire protocols deliver tool results inside a user-role message
// (Anthropic literally; OpenAI as a trailing user message for images), so a
// naive "user message = new exchange" count would treat one tool round as
// several exchanges.
func isToolResultOnly(m llm.Message) bool {
	sawResult := false
	for _, p := range m.Parts {
		if p.ToolResult != nil {
			sawResult = true
			continue
		}
		if p.Text != "" {
			return false
		}
	}
	return sawResult
}

// RenderForSummary renders the messages being dropped into a single text block
// for the summarizer.
//
// Tool calls are rendered by name and arguments (never by their full output):
// the point of the summary is to record what was DONE, and a file's whole
// contents would make the summary as large as the conversation it replaces.
func RenderForSummary(messages []llm.Message) string {
	var b strings.Builder
	for _, m := range messages {
		for _, p := range m.Parts {
			switch {
			case p.ToolCall != nil:
				b.WriteString("[tool call] ")
				b.WriteString(p.ToolCall.Name)
				if args := strings.TrimSpace(p.ToolCall.Arguments); args != "" {
					b.WriteString(" ")
					b.WriteString(truncate(args, 300))
				}
				b.WriteString("\n")
			case p.ToolResult != nil:
				b.WriteString("[tool result] ")
				text := toolResultText(p.ToolResult)
				if text == "" {
					b.WriteString("(empty)")
				} else {
					b.WriteString(truncate(text, 300))
				}
				b.WriteString("\n")
			case p.Image != nil:
				b.WriteString("[image]\n")
			case p.Text != "":
				b.WriteString(roleLabel(m.Role))
				b.WriteString(": ")
				b.WriteString(p.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// toolResultText concatenates a tool result's text parts.
func toolResultText(tr *llm.ToolResult) string {
	var b strings.Builder
	for _, p := range tr.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// roleLabel renders a role for the summarizer prompt.
func roleLabel(role llm.Role) string {
	switch role {
	case llm.RoleUser:
		return "User"
	case llm.RoleAssistant:
		return "Assistant"
	default:
		return string(role)
	}
}

// truncate shortens s to at most n runes, marking the cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// SummaryMessage renders a compaction summary as the message that replaces the
// dropped turns.
//
// It is a user message rather than a system message on purpose: a system
// message in the middle of a conversation is not expressible in every wire
// format (Anthropic folds it into a user turn anyway, OpenAI accepts it but
// some gateways reject it). A user message works everywhere, and the framing
// makes its role unmistakable to the model.
func SummaryMessage(summary string) llm.Message {
	return llm.TextMessage(llm.RoleUser,
		"[Summary of the earlier conversation, which was compacted to fit the context window. "+
			"Treat this as established context.]\n\n"+strings.TrimSpace(summary)+
			"\n\n[End of summary. The conversation continues below.]")
}
