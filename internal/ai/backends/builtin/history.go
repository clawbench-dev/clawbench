package builtin

import (
	"context"
	"encoding/json"
	"errors"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
)

// isCancellation reports whether err means "the turn was stopped", not
// "something broke".
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// compile-time assertion that the backend satisfies the interface.
var _ ai.AIBackend = (*Backend)(nil)

// historyReplay is one turn's replayed messages plus the stored id each of them
// came from.
//
// The two lists must stay parallel: a compaction reports an INDEX into Messages
// and the caller records the id at that index. Building them in one pass is what
// makes a mismatch impossible — before, two separate loops had to agree about
// which turns were replayable, and an assistant turn with tool calls now expands
// into two messages.
type historyReplay struct {
	Messages []llm.Message
	IDs      []int64
}

// buildHistoryReplay renders the prior conversation for the model.
//
// The built-in backend keeps no external session, so unlike every CLI backend
// there is nothing to resume: the model's context is rebuilt from ClawBench's
// own history each turn. That is a deliberate trade — the transcript and the
// model's context are the same data, so they cannot disagree.
//
// Tool calls are replayed as REAL tool_use / tool_result blocks rather than
// prose. The difference matters: a text line that says "I ran the tests" is
// something the model can also invent, while a tool block is evidence it cannot
// fabricate. Without them the model saw a transcript of its own claims and
// continued the fiction (see the 2026-11-20 incident: a 14-second turn wrote a
// complete fake work log with zero tool calls).
//
// ForkContext is prepended as its own user message rather than concatenated
// into the prompt: the model then sees "here is earlier context" as a distinct
// block instead of an odd opening paragraph of the user's question.
func buildHistoryReplay(req ai.ChatRequest) historyReplay {
	var out historyReplay
	if req.ForkContext != "" {
		out.Messages = append(out.Messages, llm.TextMessage(llm.RoleUser, req.ForkContext))
		// Synthetic: no stored id, so a boundary that lands on it writes none.
		out.IDs = append(out.IDs, 0)
	}
	for _, m := range req.History {
		switch m.Role {
		case "user":
			if m.Text == "" {
				continue
			}
			out.Messages = append(out.Messages, llm.TextMessage(llm.RoleUser, m.Text))
			out.IDs = append(out.IDs, m.ID)

		case "assistant":
			parts := make([]llm.Part, 0, len(m.Tools)+1)
			if m.Text != "" {
				parts = append(parts, llm.Part{Text: m.Text})
			}
			for _, t := range m.Tools {
				parts = append(parts, llm.Part{ToolCall: &llm.ToolCall{
					ID:        t.ID,
					Name:      t.Name,
					Arguments: toolArguments(t.Input),
				}})
			}
			if len(parts) == 0 {
				continue
			}
			out.Messages = append(out.Messages, llm.Message{Role: llm.RoleAssistant, Parts: parts})
			out.IDs = append(out.IDs, m.ID)
			if len(m.Tools) == 0 {
				continue
			}
			// The results answer the calls in the same order. A separate message
			// is what both protocols expect (Anthropic merges it into a user
			// turn; OpenAI emits one role="tool" message per result).
			results := make([]llm.Part, 0, len(m.Tools))
			for _, t := range m.Tools {
				text := t.Output
				if text == "" {
					text = "(no output)"
				}
				results = append(results, llm.Part{ToolResult: &llm.ToolResult{
					CallID:  t.ID,
					Parts:   []llm.Part{{Text: text}},
					IsError: t.IsError,
				}})
			}
			out.Messages = append(out.Messages, llm.Message{Role: llm.RoleTool, Parts: results})
			// The result message belongs to the same stored turn, so it carries
			// the same id. Duplicating it keeps the index/id pairing intact.
			out.IDs = append(out.IDs, m.ID)
		}
	}
	return trimIncompleteTail(out)
}

// trimIncompleteTail drops a trailing tool round that never produced an answer.
//
// Such a turn only exists when the previous turn was cancelled after a tool
// call. It carries no answer the next turn needs, and replaying it makes the
// last assistant message a bare tool_use — which Anthropic rejects when
// extended thinking is on, because we cannot replay the thinking signature that
// must lead it (signatures are not stored).
func trimIncompleteTail(r historyReplay) historyReplay {
	n := len(r.Messages)
	if n < 2 || r.Messages[n-1].Role != llm.RoleTool {
		return r
	}
	prev := r.Messages[n-2]
	if prev.Role != llm.RoleAssistant {
		return r
	}
	for _, p := range prev.Parts {
		if p.Text != "" {
			return r
		}
	}
	r.Messages = r.Messages[:n-2]
	r.IDs = r.IDs[:n-2]
	return r
}

// toolArguments guarantees the object both protocols require.
//
// A malformed blob would make the Anthropic request invalid (its `input` is a
// JSON object, not a string). Falling back to {} keeps the call/result pairing
// and lets the model see that the call existed, which is the point of replay.
func toolArguments(raw string) string {
	if raw == "" || !json.Valid([]byte(raw)) {
		return "{}"
	}
	if raw[0] != '{' {
		return "{}"
	}
	return raw
}
