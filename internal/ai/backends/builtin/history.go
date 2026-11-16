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
