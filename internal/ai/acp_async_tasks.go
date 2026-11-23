package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var errBackgroundFollowup = errors.New("background follow-up")

const backgroundEventKey = "clawbench/backgroundEvent"

// The SDK drops extension fields. Carry these two lifecycle edges through its
// ordered notification queue, so an end cannot overtake the final text chunk
// and a spawn is processed before the response to session/prompt.
func normalizeAsyncTaskUpdate(line []byte) []byte {
	if !bytes.Contains(line, []byte(`"async_task_`)) {
		return line
	}
	var frame struct {
		Method string `json:"method"`
		Params struct {
			SID    string `json:"sessionId"`
			Update struct {
				Kind        string `json:"sessionUpdate"`
				ID          string `json:"asyncTaskId"`
				TaskType    string `json:"taskType"`
				State       string `json:"state"`
				Description string `json:"description"`
				Summary     string `json:"summary"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &frame) != nil || frame.Method != "session/update" || frame.Params.SID == "" {
		return line
	}
	u := frame.Params.Update
	if u.ID == "" {
		return line
	}
	switch u.Kind {
	case "async_task_spawned":
		u.State = "started"
	case "async_task_state_update":
		if u.State != "completed" && u.State != "failed" && u.State != "stopped" {
			return line
		}
	default:
		return line
	}
	event := map[string]any{
		"id": u.ID, "kind": u.TaskType, "state": u.State,
		"description": u.Description, "summary": u.Summary,
	}
	update := map[string]any{
		"sessionUpdate": "session_info_update",
		"_meta":         map[string]any{backgroundEventKey: event},
	}
	out, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": frame.Method,
		"params": map[string]any{"sessionId": frame.Params.SID, "update": update},
	})
	return out
}

// A prompt that hands work to the background keeps its original route and
// runner through the agent's follow-up. Each task has its own identity. A
// completed shell and the subsequent autonomous turn have DIFFERENT ids.
type promptBackgroundWork struct {
	expectFollowup bool
	commands       *asyncCommandReply // optional native agy command-completion signals
	mu             sync.Mutex
	active         map[string]string
	finished       map[string]bool
	changed        chan struct{}
	waiting        bool
	followup       bool
	turnErr        error
}

func newPromptBackgroundWork() *promptBackgroundWork {
	return &promptBackgroundWork{active: make(map[string]string), changed: make(chan struct{}, 1)}
}

func (w *promptBackgroundWork) observe(id, kind, state, description, summary string, ch chan<- StreamEvent, conn *ACPConn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	previous, known := w.active[id]
	if state == "started" {
		if known || w.finished[id] {
			return
		}
		w.active[id] = kind
		if conn != nil {
			conn.RecordTurnOutput()
		}
	} else {
		if !known {
			return
		}
		kind = previous
		delete(w.active, id)
		if w.finished == nil {
			w.finished = make(map[string]bool)
		}
		w.finished[id] = true
		if kind == "turn" {
			w.followup = false
			if state != "completed" {
				w.turnErr = fmt.Errorf("background turn %s: %s", state, summary)
			}
		} else if w.waiting && w.expectFollowup && state != "stopped" {
			w.followup = true
		}
	}
	if conn != nil {
		// Detached work can legitimately be silent for longer than the prompt
		// watchdog. Once only the model is outstanding its watchdog applies again.
		conn.TouchModelProgress()
		conn.TouchSessionUpdate()
		conn.backgroundTasksRunning.Store(w.hasDetachedTask())
	}
	if kind != "turn" {
		emitStreamEvent(ch, acpNotificationSource, backgroundTaskCard(id, state, description, summary))
	}
	select {
	case w.changed <- struct{}{}:
	default:
	}
}

// Called with w.mu held.
func (w *promptBackgroundWork) hasDetachedTask() bool {
	for _, kind := range w.active {
		if kind != "turn" {
			return true
		}
	}
	return false
}

func backgroundTaskCard(id, state, description, summary string) StreamEvent {
	tool := &ToolCall{ID: "async-task:" + id, Name: "BackgroundTask"}
	typ := "tool_use"
	if state == "started" {
		input, _ := json.Marshal(map[string]string{"description": description})
		tool.Input = string(input)
	} else {
		typ = "tool_result"
		tool.Done = true
		tool.Status = "success"
		if state != "completed" {
			tool.Status = "error"
		}
		tool.Output = state
		if summary != "" {
			tool.Output += ": " + summary
		}
	}
	return StreamEvent{Type: typ, Tool: tool}
}

func (w *promptBackgroundWork) wait(ctx context.Context, disconnected <-chan struct{}) error {
	if w.commands != nil {
		if _, err := w.commands.wait(ctx, disconnected); err != nil {
			return err
		}
	}
	for {
		w.mu.Lock()
		w.waiting = true
		done := len(w.active) == 0 && !w.followup
		err := w.turnErr
		w.mu.Unlock()
		if done {
			return err
		}
		select {
		case <-w.changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-disconnected:
			return errors.New("ACP connection closed during background work")
		}
	}
}

func (c *ClawBenchACPClient) onBackgroundEvent(sid string, value any) {
	e, _ := value.(map[string]any)
	id, _ := e["id"].(string)
	state, _ := e["state"].(string)
	if id == "" {
		return
	}
	switch state {
	case "started", "completed", "failed", "stopped":
	default:
		return
	}
	kind, _ := e["kind"].(string)
	description, _ := e["description"].(string)
	summary, _ := e["summary"].(string)
	c.mu.Lock()
	work := c.backgroundWork[sid]
	if work != nil {
		work.observe(id, kind, state, description, summary, c.sessionRoutes[sid], c.connRef)
		c.mu.Unlock()
		return
	}
	sameSession := sid == c.activeSessionID
	c.mu.Unlock()
	if !sameSession {
		return
	}
	if state == "started" {
		if kind == "turn" {
			c.adoptBackgroundTurn(sid, id)
		}
		return
	}
	// A scheduled wakeup can also start on a completely idle conversation.
	c.finishBackgroundTurn(sid, id, state, summary)
}
