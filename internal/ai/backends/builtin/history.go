package builtin

import (
	"context"
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

// historyFor returns the prior conversation to replay to the model.
//
// The built-in backend keeps no external session, so unlike every CLI backend
// there is nothing to resume: the model's context is rebuilt from ClawBench's
// own history each turn. That is a deliberate trade — the transcript and the
// model's context are the same data, so they cannot disagree.
//
// ForkContext is prepended as its own user message rather than concatenated
// into the prompt: the model then sees "here is earlier context" as a distinct
// block instead of an odd opening paragraph of the user's question.
func historyFor(req ai.ChatRequest) []llm.Message {
	var out []llm.Message
	if req.ForkContext != "" {
		out = append(out, llm.TextMessage(llm.RoleUser, req.ForkContext))
	}
	for _, m := range req.History {
		role := llm.Role(m.Role)
		switch role {
		case llm.RoleUser, llm.RoleAssistant:
			if m.Text == "" {
				continue
			}
			out = append(out, llm.TextMessage(role, m.Text))
		}
	}
	return out
}

// historyIDs returns req.History's stored ids, parallel to historyFor's output.
//
// It exists so a compaction can say WHICH stored turn it folded instead of "the
// newest one": the engine keeps the most recent turns verbatim, and a boundary
// that ignored that dropped them on the next turn. Synthetic entries (the fork
// context) get 0 — the engine then reports an index-only boundary and the caller
// writes no id at all, which replays rather than drops.
func historyIDs(req ai.ChatRequest) []int64 {
	out := make([]int64, 0, len(req.History)+1)
	if req.ForkContext != "" {
		out = append(out, 0)
	}
	for _, m := range req.History {
		role := llm.Role(m.Role)
		switch role {
		case llm.RoleUser, llm.RoleAssistant:
			if m.Text == "" {
				continue
			}
			out = append(out, m.ID)
		}
	}
	return out
}
