package ai

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// agy acp can return end_turn while run_command is still executing. The
// completion, further permission requests and the answer then arrive on the
// same session, without a second prompt response.
//
// Permission requests are the one follow-up this observer cannot see: they are
// `session/request_permission` REQUESTS, not `session/update` notifications.
// They reach the approval card only because the turn's stream route is kept
// registered until wait() returns (see Prompt), so do not expect a hook here.
type asyncCommandReply struct {
	mu       sync.Mutex
	commands map[acp.ToolCallId]bool
	hasText  bool
	awaiting bool
	readyAt  time.Time
	changed  chan struct{}
}

// Antigravity's own rawInput field names for an execute tool call, observed on
// `agy acp` 1.5.14. Nothing else in this repo emits them, so a shape that does
// not carry them means either a different tool or a changed schema — and in
// both cases this feature would silently stop engaging, which is exactly the
// bug it fixes. isFiniteCommand therefore logs the miss instead of shrugging.
const (
	agyRawInputCommandLine = "CommandLine"
	agyRawInputIsDaemon    = "IsDaemon"
)

func (c *ClawBenchACPClient) trackCommandReply(sessionID string) *asyncCommandReply {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.commandReplies == nil {
		return nil
	}
	reply := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
	c.commandReplies[sessionID] = reply
	return reply
}

// isFiniteCommand reports whether tc is a foreground command started by the
// current prompt — the only kind whose completion produces a follow-up. Daemons
// outlive the turn by design and must never hold it open.
func isFiniteCommand(tc *acp.SessionUpdateToolCall) bool {
	if tc.Kind != acp.ToolKindExecute {
		return false
	}
	if tc.Status == acp.ToolCallStatusCompleted || tc.Status == acp.ToolCallStatusFailed {
		return false
	}
	args, _ := rawJSONValue(tc.RawInput).(map[string]any)
	command, _ := args[agyRawInputCommandLine].(string)
	if command == "" {
		// Log key names only: rawInput of an execute call can carry a whole
		// script, and this line exists to diagnose a schema drift, not to
		// duplicate the command.
		slog.Debug("acp: execute tool call without Antigravity command fields",
			"tool_call_id", string(tc.ToolCallId), "raw_input_keys", mapKeys(args))
		return false
	}
	daemon, _ := args[agyRawInputIsDaemon].(bool)
	return !daemon
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
		if st, ok := tc.Status.Value(); ok && (st == acp.ToolCallStatusCompleted || st == acp.ToolCallStatusFailed) {
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

// wait blocks until the turn's follow-up has finished arriving. waited is false
// when the turn had nothing to wait for (no command was tracked and no waiting
// generation was observed), which is the case for every ordinary turn — the
// observer is created for the whole Antigravity session, not per background
// command.
func (r *asyncCommandReply) wait(ctx context.Context, disconnected <-chan struct{}) (waited bool, err error) {
	r.mu.Lock()
	if !r.awaiting && len(r.commands) == 0 {
		r.mu.Unlock()
		return false, nil
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
				return true, nil
			}
			settled = time.After(remaining)
		}
		select {
		case <-changed:
		case <-settled:
		case <-ctx.Done():
			return true, ctx.Err()
		case <-disconnected:
			return true, errors.New("ACP connection closed while awaiting background command result")
		}
	}
}

// mapKeys returns the sorted top-level key names of m, for logging a shape
// mismatch without echoing the values.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
