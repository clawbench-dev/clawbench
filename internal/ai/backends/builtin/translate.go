package builtin

import (
	"encoding/json"
	"fmt"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/ai"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// emitTimeout bounds how long one event may wait for the consumer.
//
// A chat turn's consumer is the session executor, which reads until the
// channel closes; it only stops early when the turn was cancelled. Waiting
// forever on a dead consumer would leak the goroutine and the model call, so
// the event is dropped after this and the loop keeps draining.
// It is a variable only so a test can shorten it; nothing in production
// assigns to it.
var emitTimeout = 30 * time.Second

// translator converts agentcore events into ai.StreamEvent values.
//
// It is stateful for one reason: the frontend's thinking block must be CLOSED
// before text or a tool call is appended. ai.AccumulateBlock closes a thinking
// block when a tool_use arrives, but a plain text delta after reasoning leaves
// the block open — which the UI renders as a spinner with no body, and which
// the periodic flush persists as in_progress, so a reload adopts it too.
type translator struct {
	reasoningOpen bool
	// locale is the UI language the budget notice is written in. Empty falls
	// back to the product default rather than emitting a raw message id.
	locale string
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

	case agentcore.EventToolArgs:
		// Arguments are final but the tool has not run yet. Carrying them here
		// is what lets the card show the command / file while it executes —
		// the start event had none, and the end event arrives minutes later.
		out = append(out, ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{
				ID:    ev.ToolCallID,
				Name:  ev.ToolName,
				Input: toolInputJSON(ev.ToolInput),
				Done:  false,
			},
		})

	case agentcore.EventToolEnd:
		status := "success"
		if ev.ToolIsError {
			status = "error"
		}
		out = append(out, ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{
				ID:         ev.ToolCallID,
				Name:       ev.ToolName,
				Input:      toolInputJSON(ev.ToolInput),
				Output:     ev.ToolOutput,
				Status:     status,
				Done:       true,
				DurationMs: int(ev.ToolDuration / time.Millisecond),
			},
		})

	case agentcore.EventUsage:
		// Deliberately NOT forwarded as a `metadata` frame.
		//
		// One metadata frame per model round is a REPLACEMENT in the executor
		// (e.responseMetadata = event.Meta), and the terminal `done` frame only
		// fills fields that are still empty. So a turn with tool calls — several
		// rounds — recorded the LAST round's tokens, and the `done` total was
		// ignored: usage and cost were understated on every multi-round turn.
		// The per-round figure has no consumer here (the UI shows the turn's
		// total, which the `done` frame below carries), and a single-round turn
		// is unaffected — done reports the same numbers this frame used to.
		return nil

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
			// The boundary the executor records. Without it the executor had to
			// write its own newest message id, which marked the turns the engine
			// kept verbatim as "already summarized".
			CompactedThroughHistory:   ev.CompactedThroughHistory,
			CompactedThroughMessageID: ev.CompactedThroughMessageID,
			SystemMessage: &ai.SystemMessageData{
				Content: compactionNotice(ev.Dropped, ev.SummaryTokens, ev.WindowTokens),
			},
		})

	case agentcore.EventDone:
		out = append(out, t.doneEvents(ev)...)

	case agentcore.EventError:
		out = append(out, ai.StreamEvent{
			Type:   "error",
			Error:  errorText(ev.Err),
			Reason: reasonFor(ev.Err),
		})
	}
	return out
}

// toolInputJSON normalizes a tool call's argument blob for the frontend.
//
// A malformed blob would make AccumulateBlock drop the input silently; sending
// it as a JSON string keeps the card readable instead of showing an empty input.
// Shared by the args and end events so both carry the same shape.
func toolInputJSON(raw string) string {
	if raw == "" || json.Valid([]byte(raw)) {
		return raw
	}
	// Marshaling a string cannot fail, so the error is not a branch: a malformed
	// blob always reaches the frontend as a quoted string.
	quoted, _ := json.Marshal(raw)
	return string(quoted)
}

// doneEvents renders the terminal frame.
//
// A budget stop ends the turn the same way a finished one does, so without a
// note the user cannot tell "it gave up" from "it answered". The note is a
// system timeline row (same channel as the compaction notice) because it is not
// part of the model's answer. The done event still carries the stop reason and
// the totals so the ledger records why the turn ended.
func (t *translator) doneEvents(ev agentcore.Event) []ai.StreamEvent {
	var out []ai.StreamEvent
	for _, notice := range t.doneNotices(ev) {
		out = append(out, ai.StreamEvent{
			Type:          "system_message",
			SystemMessage: &ai.SystemMessageData{Content: notice},
		})
	}
	// Context usage for the bottom bar's panel. The engine reports the size of
	// the conversation it just finished and the model's window; the frontend's
	// usage chip keys on size > 0, so without this a built-in agent had no
	// context panel at all (ACP agents report their own).
	//
	// It goes BEFORE the done event: the turn's terminal frame is what makes the
	// UI finalize the reply, and a usage frame arriving after it would land on a
	// turn the client already closed.
	if ev.WindowTokens > 0 {
		out = append(out, ai.StreamEvent{
			Type: "usage_update",
			Usage: &ai.UsageState{
				Used:         ev.ContextTokens,
				Size:         ev.WindowTokens,
				InputTokens:  ev.TotalUsage.InputTokens,
				OutputTokens: ev.TotalUsage.OutputTokens,
				TotalTokens:  ev.TotalUsage.InputTokens + ev.TotalUsage.OutputTokens,
			},
		})
	}
	return append(out, ai.StreamEvent{Type: "done", Meta: &ai.Metadata{
		Transport:    "builtin",
		StopReason:   ev.StopReason,
		InputTokens:  ev.TotalUsage.InputTokens,
		OutputTokens: ev.TotalUsage.OutputTokens,
		TotalTokens:  ev.TotalUsage.InputTokens + ev.TotalUsage.OutputTokens,
		// The cache split rides the message metadata so the ledger records what
		// the prompt cache actually saved. Without it every built-in turn looked
		// like a total miss (all four columns zero) even when the provider
		// reported a hit.
		CachedReadTokens:  ev.TotalUsage.CacheReadTokens,
		CachedWriteTokens: ev.TotalUsage.CacheWriteTokens,
		CacheHitTokens:    ev.TotalUsage.CacheReadTokens,
	}})
}

// doneNotices renders the system lines a turn's ending needs.
//
// Two different facts can be true at once — the step budget ran out, and the
// last reply was cut off by the output cap — so this returns a list rather than
// one message. A normal completion returns none.
func (t *translator) doneNotices(ev agentcore.Event) []string {
	var out []string
	if ev.Truncated {
		out = append(out, t.localize("BuiltinTruncatedNotice"))
	}
	switch ev.StopReason {
	case agentcore.StopMaxRounds:
		out = append(out, t.localize("BuiltinMaxRoundsNotice", map[string]any{"Rounds": ev.Limit}))
	case agentcore.StopMaxToolCalls:
		out = append(out, t.localize("BuiltinMaxToolCallsNotice", map[string]any{"ToolCalls": ev.Limit}))
	}
	return out
}

// localize renders one message id in the UI language.
func (t *translator) localize(id string, data ...map[string]any) string {
	locale := t.locale
	if locale == "" {
		locale = model.DefaultLanguage
	}
	return i18n.T(i18n.LocalizerForLocale(locale), id, data...)
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
