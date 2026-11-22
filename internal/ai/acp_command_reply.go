package ai

import (
	"context"
	"errors"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// agy acp can return end_turn while run_command is still executing. The
// completion, further permission requests and the answer then arrive on the
// same session, without a second prompt response.
type asyncCommandReply struct {
	mu       sync.Mutex
	commands map[acp.ToolCallId]bool
	hasText  bool
	awaiting bool
	readyAt  time.Time
	changed  chan struct{}
}

func isFiniteCommand(tc *acp.SessionUpdateToolCall) bool {
	args, _ := tc.RawInput.(map[string]any)
	command, _ := args["CommandLine"].(string)
	daemon, _ := args["IsDaemon"].(bool)
	return tc.Kind == acp.ToolKindExecute && command != "" && !daemon &&
		tc.Status != acp.ToolCallStatusCompleted && tc.Status != acp.ToolCallStatusFailed
}

func (r *asyncCommandReply) observe(u acp.SessionUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case u.ToolCall != nil:
		tc := u.ToolCall
		if isFiniteCommand(tc) {
			r.commands[tc.ToolCallId] = true
		}
		r.hasText = false
	case u.ToolCallUpdate != nil:
		tc := u.ToolCallUpdate
		if tc.Status != nil && (*tc.Status == acp.ToolCallStatusCompleted || *tc.Status == acp.ToolCallStatusFailed) {
			delete(r.commands, tc.ToolCallId)
		}
		r.hasText = false
	case u.AgentThoughtChunk != nil:
		r.hasText = false
	case u.AgentMessageChunk != nil:
		r.hasText = true
	case u.UsageUpdate != nil:
		if !r.hasText {
			return
		}
		// Record the waiting generation even if the command finishes between
		// the SDK receiving the prompt response and Prompt inspecting it.
		if len(r.commands) != 0 {
			r.awaiting = true
		} else if r.awaiting {
			r.readyAt = time.Now()
		}
	default:
		return
	}
	if u.UsageUpdate == nil {
		r.readyAt = time.Time{}
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *asyncCommandReply) wait(ctx context.Context, disconnected <-chan struct{}) error {
	r.mu.Lock()
	if !r.awaiting && len(r.commands) == 0 {
		r.mu.Unlock()
		return nil
	}
	r.awaiting = true
	changed := make(chan struct{}, 1)
	r.changed = changed
	r.mu.Unlock()

	// Usage is emitted after every model generation. Give an immediately
	// following tool call time to invalidate an intermediate text/usage pair.
	const settle = 250 * time.Millisecond
	for {
		r.mu.Lock()
		ready := r.readyAt
		r.mu.Unlock()
		var settled <-chan time.Time
		if !ready.IsZero() {
			remaining := time.Until(ready.Add(settle))
			if remaining <= 0 {
				return nil
			}
			settled = time.After(remaining)
		}
		select {
		case <-changed:
		case <-settled:
		case <-ctx.Done():
			return ctx.Err()
		case <-disconnected:
			return errors.New("ACP connection closed while awaiting background command result")
		}
	}
}
