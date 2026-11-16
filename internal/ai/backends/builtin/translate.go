package builtin

import (
	"encoding/json"
	"fmt"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/ai"
)

// emitTimeout bounds how long one event may wait for the consumer.
//
// A chat turn's consumer is the session executor, which reads until the
// channel closes; it only stops early when the turn was cancelled. Waiting
// forever on a dead consumer would leak the goroutine and the model call, so
// the event is dropped after this and the loop keeps draining.
const emitTimeout = 30 * time.Second

// translator converts agentcore events into ai.StreamEvent values.
//
// It is stateful for one reason: the frontend's thinking block must be CLOSED
// before text or a tool call is appended. ai.AccumulateBlock closes a thinking
// block when a tool_use arrives, but a plain text delta after reasoning leaves
// the block open — which the UI renders as a spinner with no body, and which
// the periodic flush persists as in_progress, so a reload adopts it too.
type translator struct {
	reasoningOpen bool
}

// translate maps one agentcore event to the ai.StreamEvents to forward.
//
// It returns a slice because closing an open reasoning block is its own event,
// so one input event can produce two outputs.
func (t *translator) translate(ev agentcore.Event) []ai.StreamEvent {
	var out []ai.StreamEvent

	// Anything that appends visible content (or ends the turn) must close an
	// open reasoning block first.
	switch ev.Kind {
	case agentcore.EventText, agentcore.EventToolStart, agentcore.EventToolEnd,
		agentcore.EventDone, agentcore.EventError:
		if t.reasoningOpen {
			t.reasoningOpen = false
			out = append(out, ai.StreamEvent{Type: "thinking_done"})
		}
	}

	switch ev.Kind {
	case agentcore.EventText:
		out = append(out, ai.StreamEvent{Type: "content", Content: ev.Text})

	case agentcore.EventReasoning:
		t.reasoningOpen = true
		out = append(out, ai.StreamEvent{Type: "thinking", Content: ev.Reasoning})

	case agentcore.EventToolStart:
		out = append(out, ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{
				ID:   ev.ToolCallID,
				Name: ev.ToolName,
				// Input is deliberately empty here: the arguments are still
				// streaming. The completion event carries the full JSON.
				Done: false,
			},
		})

	case agentcore.EventToolEnd:
		input := ev.ToolInput
		if input != "" && !json.Valid([]byte(input)) {
			// A malformed argument blob would make AccumulateBlock drop the
			// input silently; sending it as a JSON string keeps the card
			// readable instead of showing an empty input.
			if raw, err := json.Marshal(input); err == nil {
				input = string(raw)
			} else {
				input = ""
			}
		}
		status := "success"
		if ev.ToolIsError {
			status = "error"
		}
		out = append(out, ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{
				ID:         ev.ToolCallID,
				Name:       ev.ToolName,
				Input:      input,
				Output:     ev.ToolOutput,
				Status:     status,
				Done:       true,
				DurationMs: int(ev.ToolDuration / time.Millisecond),
			},
		})

	case agentcore.EventUsage:
		if ev.Usage != nil {
			out = append(out, ai.StreamEvent{
				Type: "metadata",
				Meta: &ai.Metadata{
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
					Transport:    "builtin",
				},
			})
		}

	case agentcore.EventCompacted:
		// Two things ride this event: the summary (persisted by the executor so
		// the next turn starts compacted) and a user-visible notice.
		//
		// The notice is deliberately NOT the summary itself. A summary is
		// written for the model — dense, factual, and often long — so showing it
		// in the transcript would read as an unprompted essay. The user needs to
		// know THAT it happened and at what scale.
		out = append(out, ai.StreamEvent{
			Type:    "compacted",
			Summary: ev.Summary,
			SystemMessage: &ai.SystemMessageData{
				Content: compactionNotice(ev.Dropped, ev.SummaryTokens, ev.WindowTokens),
			},
		})

	case agentcore.EventDone:
		out = append(out, ai.StreamEvent{Type: "done", Meta: &ai.Metadata{
			Transport:    "builtin",
			StopReason:   ev.StopReason,
			InputTokens:  ev.TotalUsage.InputTokens,
			OutputTokens: ev.TotalUsage.OutputTokens,
		}})

	case agentcore.EventError:
		out = append(out, ai.StreamEvent{
			Type:   "error",
			Error:  errorText(ev.Err),
			Reason: reasonFor(ev.Err),
		})
	}
	return out
}

// compactionNotice renders the user-facing line shown when the conversation was
// compacted.
//
// It names the scale (how many messages were folded away and how full the
// context now is) because "it forgot" and "it summarized" look identical from
// the outside, and only one of them is a problem.
func compactionNotice(dropped, afterTokens, windowTokens int) string {
	if windowTokens <= 0 {
		return fmt.Sprintf("Earlier messages were summarized to fit the context window (%d messages folded).", dropped)
	}
	pct := afterTokens * 100 / windowTokens
	return fmt.Sprintf("Earlier messages were summarized to fit the context window "+
		"(%d messages folded; context is now about %d%% full).", dropped, pct)
}

// errorText renders a terminal error for the transcript.
func errorText(err error) string {
	if err == nil {
		return "the run failed without a message"
	}
	return err.Error()
}

// reasonFor classifies a terminal error into the structured reason codes the
// frontend localizes. A cancellation is not a failure: it must say so, or the
// user sees "something went wrong" after pressing stop.
func reasonFor(err error) string {
	if err == nil {
		return ai.ReasonRequestFailed
	}
	if isCancellation(err) {
		return ai.ReasonUserCancel
	}
	return ai.ReasonRequestFailed
}
