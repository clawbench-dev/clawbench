// Package agentcore is the built-in agent's execution engine.
//
// It is a plain Go loop: call the model, run the tools it asks for, feed the
// results back, repeat until the model answers without tool calls or a budget
// runs out. It imports nothing from ClawBench except model (for the tool
// policy) and the standard library, so it can be driven from a chat turn, a
// background task, or a test without dragging in the ACP stack or the HTTP
// server.
//
// The engine emits its own Event type rather than ai.StreamEvent. That keeps
// this package a leaf; the chat adapter (internal/ai/backends/builtin) owns the
// translation, which is the only place that needs to know both shapes.
package agentcore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// Spec is one run: what to do, with what, and under which limits.
type Spec struct {
	// Client is the model endpoint.
	Client llm.Client
	// SystemPrompt is the agent's instructions, before the tool preamble.
	SystemPrompt string
	// Prompt is the user's message for this turn.
	Prompt string
	// History is the prior conversation, oldest first. Empty for a new session.
	History []llm.Message
	// WorkDir confines every file path the tools touch.
	WorkDir string
	// Policy decides which tools exist at all.
	Policy model.ToolPolicy
	// MaxOutputTokens caps one model reply. Zero uses the client's default.
	MaxOutputTokens int
	// Timeout bounds the whole run. Zero means no extra bound beyond ctx.
	Timeout time.Duration

	// ContextWindow is the model's context window in tokens. Zero means
	// unknown, which disables compaction entirely (guessing a small window
	// would compact conversations that fit).
	ContextWindow int
	// PriorSummary is a summary from an earlier compaction, carried in from the
	// stored session so it survives a turn boundary. When non-empty it replaces
	// the oldest history rather than being appended to it.
	PriorSummary string
	// Summarize produces a summary of the given conversation text. When nil,
	// compaction still runs but falls back to a deterministic digest that
	// records what happened without asking a model — worse prose, same facts.
	Summarize func(ctx context.Context, text string) (string, error)
}

// EventKind discriminates a run Event.
type EventKind string

const (
	// EventText is an incremental piece of the answer.
	EventText EventKind = "text"
	// EventReasoning is an incremental piece of the model's reasoning.
	EventReasoning EventKind = "reasoning"
	// EventToolStart announces a tool call before it runs.
	EventToolStart EventKind = "tool_start"
	// EventToolEnd reports a finished tool call.
	EventToolEnd EventKind = "tool_end"
	// EventUsage reports token accounting for one model round.
	EventUsage EventKind = "usage"
	// EventCompacted reports that the conversation was rewritten into a summary.
	// It is user-visible: silently forgetting earlier turns reads as the agent
	// breaking, so the UI is told what happened and how much was folded away.
	EventCompacted EventKind = "compacted"
	// EventDone is the terminal success event.
	EventDone EventKind = "done"
	// EventError is the terminal failure event.
	EventError EventKind = "error"
)

// Event is one update from a run.
type Event struct {
	Kind EventKind

	Text      string
	Reasoning string

	// Tool fields, set on EventToolStart / EventToolEnd.
	ToolCallID   string
	ToolName     string
	ToolInput    string
	ToolOutput   string
	ToolIsError  bool
	ToolDuration time.Duration

	Usage *llm.Usage
	Err   error

	// Summary carries the compaction summary. It is set on EventCompacted (the
	// text that just replaced the dropped turns) and on EventDone (the latest
	// summary, so the caller can persist it across the turn boundary and the
	// next turn starts from the compacted state instead of re-summarizing the
	// same history).
	Summary string
	// Dropped is set on EventCompacted: how many messages the summary replaced.
	Dropped int
	// SummaryTokens and WindowTokens are set on EventCompacted: the estimated
	// size after compaction and the model's window, so the UI can say how full
	// the context is.
	SummaryTokens int
	WindowTokens  int

	// StopReason is set on EventDone: why the run ended (see the Stop*
	// constants). "complete" is the normal case.
	StopReason string
	// Rounds and ToolCalls are set on EventDone: the run's totals.
	Rounds    int
	ToolCalls int
	// TotalUsage is set on EventDone: summed across every round, not just the
	// last one. EventUsage carries the per-round figure.
	TotalUsage llm.Usage
}

// RunResult summarises a completed run. It is what an internal caller (a
// background task that has no user to stream to) gets back.
type RunResult struct {
	// Text is the model's final answer.
	Text string
	// Rounds is how many model calls the run made.
	Rounds int
	// ToolCalls is how many tools ran.
	ToolCalls int
	// Usage is the summed token usage across rounds.
	Usage llm.Usage
	// StopReason explains why the run ended: "complete" when the model
	// answered on its own, otherwise "max_rounds" / "max_tool_calls" /
	// "timeout" / "cancelled" / "error".
	StopReason string
	// Summary is the latest compaction summary, or "" when the run never
	// compacted. A caller persists it so the next turn starts from the
	// compacted state instead of re-summarizing the same history.
	Summary string
}

// Stop reasons.
const (
	StopComplete     = "complete"
	StopMaxRounds    = "max_rounds"
	StopMaxToolCalls = "max_tool_calls"
	StopTimeout      = "timeout"
	StopCancelled    = "cancelled"
	StopError        = "error"
)

// Errors surfaced as terminal events.
var (
	// ErrNoToolsConfigured means the policy granted no capabilities, so the
	// model can only talk. That is a valid configuration, not a failure.
	ErrNoToolsConfigured = errors.New("no tools are enabled")
)

// Run executes one turn, streaming events on the returned channel.
//
// The channel always closes. Exactly one terminal event (EventDone or
// EventError) is sent before it closes, so a consumer can distinguish "the run
// finished" from "the producer died".
//
// A caller that stops reading MUST keep draining the channel: the producer
// blocks on it, so abandoning it leaks the goroutine.
func Run(ctx context.Context, spec Spec) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		r := &runner{spec: spec, ch: ch}
		// A panic anywhere below — a tool, a malformed provider response, a
		// bug in this package — must not take the server down. This agent runs
		// inside the ClawBench process, so an unrecovered panic is an outage
		// for every other session and feature; an ACP backend would only lose
		// its own child process. Turn it into the run's terminal error instead.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("agentcore: panic during run",
					"panic", rec, "stack", string(debug.Stack()))
				// A panic after the terminal event (e.g. in a deferred cleanup)
				// must not emit a second one.
				if !r.terminated {
					r.emit(Event{Kind: EventError, Err: fmt.Errorf("internal error: %v", rec)})
				}
			}
		}()
		r.run(ctx)
	}()
	return ch
}

// RunSync executes one turn and returns the result, discarding intermediate
// events. This is the entry point for background tasks, which have no UI.
//
// onEvent, when non-nil, still receives every event — that is how a task can
// log progress without being a chat turn.
func RunSync(ctx context.Context, spec Spec, onEvent func(Event)) (RunResult, error) {
	var (
		text   strings.Builder
		res    = RunResult{StopReason: StopComplete}
		runErr error
	)
	for ev := range Run(ctx, spec) {
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev.Kind {
		case EventText:
			text.WriteString(ev.Text)
		case EventDone:
			res.StopReason = ev.StopReason
			res.Rounds = ev.Rounds
			res.ToolCalls = ev.ToolCalls
			res.Usage = ev.TotalUsage
			res.Summary = ev.Summary
		case EventError:
			res.StopReason = StopError
			runErr = ev.Err
		}
	}
	res.Text = text.String()
	return res, runErr
}

// runner holds the mutable state of one run.
type runner struct {
	spec Spec
	ch   chan<- Event

	messages  []llm.Message
	toolList  []tools.Tool
	toolIndex map[string]tools.Tool
	rounds    int
	toolCalls int
	usage     llm.Usage
	finalText strings.Builder
	// summary is the most recent compaction summary, reported on EventDone so
	// the caller can persist it across the turn boundary.
	summary string
	// terminated records that a done/error event was already sent. The panic
	// guard reads it so a panic after the terminal event cannot emit a second.
	terminated bool
}

func (r *runner) run(ctx context.Context) {
	if r.spec.Client == nil {
		r.fail(fmt.Errorf("no model endpoint is configured for this agent"))
		return
	}

	runCtx := ctx
	if r.spec.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, r.spec.Timeout)
		defer cancel()
	}

	r.buildTools()

	// The conversation starts from the stored history plus this turn's prompt.
	r.messages = append([]llm.Message{}, r.spec.History...)
	// A summary from an earlier compaction replaces the turns it stood for.
	// It goes FIRST: everything after it is either recent (kept verbatim) or
	// this turn.
	if r.spec.PriorSummary != "" {
		r.messages = append([]llm.Message{compaction.SummaryMessage(r.spec.PriorSummary)}, r.messages...)
	}
	r.messages = append(r.messages, llm.TextMessage(llm.RoleUser, r.spec.Prompt))

	system := buildSystemPrompt(r.spec.SystemPrompt, r.spec.WorkDir, r.toolList, r.spec.Policy)

	env := tools.Env{
		Root:              r.spec.WorkDir,
		CommandTimeoutSec: r.spec.Policy.CommandTimeout(),
		Ctx:               runCtx,
	}

	// Compact once up front if the replayed history already overflows. Doing it
	// here (rather than only inside the loop) means the FIRST model call of the
	// turn is already sized correctly, instead of failing and being retried.
	r.compactIfNeeded(runCtx, system)

	for {
		if err := runCtx.Err(); err != nil {
			r.finishStopped(runCtx, StopCancelled)
			return
		}
		if r.rounds >= r.spec.Policy.Rounds() {
			r.finishStopped(runCtx, StopMaxRounds)
			return
		}
		r.rounds++

		calls, text, err := r.runModelRound(runCtx, system)
		if err != nil {
			if runCtx.Err() != nil {
				r.finishStopped(runCtx, StopCancelled)
				return
			}
			r.fail(err)
			return
		}

		// Record the assistant turn so the next round sees what it decided.
		assistantParts := make([]llm.Part, 0, len(text)+len(calls))
		for _, t := range text {
			assistantParts = append(assistantParts, llm.Part{Text: t})
		}
		for _, c := range calls {
			assistantParts = append(assistantParts, llm.Part{ToolCall: &llm.ToolCall{
				ID: c.ID, Name: c.Name, Arguments: c.Arguments,
			}})
		}
		if len(assistantParts) > 0 {
			r.messages = append(r.messages, llm.Message{Role: llm.RoleAssistant, Parts: assistantParts})
		}

		// No tool calls means the model is answering. That is the normal end.
		if len(calls) == 0 {
			r.emit(Event{
				Kind:       EventDone,
				StopReason: StopComplete,
				Rounds:     r.rounds,
				ToolCalls:  r.toolCalls,
				TotalUsage: r.usage,
				Summary:    r.summary,
			})
			return
		}

		results := r.executeTools(runCtx, env, calls)
		if runCtx.Err() != nil {
			r.finishStopped(runCtx, StopCancelled)
			return
		}
		// Tool results go back as one message; the clients split it per
		// protocol (Anthropic merges blocks into a user message, OpenAI emits
		// one tool message per result).
		var parts []llm.Part
		for _, res := range results {
			parts = append(parts, llm.Part{ToolResult: res})
		}
		r.messages = append(r.messages, llm.Message{Role: llm.RoleTool, Parts: parts})

		// A single turn can overflow the window on its own: reading a large file
		// or running a verbose command adds tens of thousands of tokens in one
		// step, and a few rounds of that exceed any window. Compacting here —
		// between rounds, never mid-exchange — is what keeps a long tool loop
		// from dying at round 3 of a task that would have finished at round 5.
		r.compactIfNeeded(runCtx, system)

		if r.toolCalls >= r.spec.Policy.ToolCalls() {
			// The budget is spent. Do one last model round WITHOUT tools so the
			// model has a chance to summarize what it found instead of the turn
			// ending with a bare tool result the user never sees.
			r.finishStopped(runCtx, StopMaxToolCalls)
			return
		}
	}
}

// buildTools materialises the tool list from the policy.
//
// A capability the policy does not grant produces no tool at all — the model
// never learns it exists. That is the whole safety model: it is not a filter
// applied at call time (which a prompt injection can argue with), it is an
// absence.
func (r *runner) buildTools() {
	r.toolIndex = make(map[string]tools.Tool)
	if r.spec.Client == nil {
		return
	}
	policy := r.spec.Policy
	r.toolList = tools.All(policy.Has(model.CapFSRead), policy.Has(model.CapFSWrite), policy.Has(model.CapExec))

	// An image returned by read is only useful if the model can see it.
	// Filtering here (rather than at read time) keeps the tool list stable
	// while still telling the model the truth about its own abilities.
	if !r.spec.Client.SupportsVision() {
		r.toolList = filterVisionlessRead(r.toolList)
	}
	for _, t := range r.toolList {
		r.toolIndex[t.Name()] = t
	}
}

// filterVisionlessRead swaps read for a variant that refuses images, so a
// text-only model gets a clear message instead of a corrupt picture.
func filterVisionlessRead(list []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, 0, len(list))
	for _, t := range list {
		if t.Name() == "read" {
			out = append(out, tools.ReadToolNoVision{})
			continue
		}
		out = append(out, t)
	}
	return out
}

// runModelRound performs one streaming model call and collects its tool calls.
func (r *runner) runModelRound(ctx context.Context, system string) ([]llm.ToolCall, []string, error) {
	specs := make([]llm.ToolSpec, 0, len(r.toolList))
	for _, t := range r.toolList {
		s := t.Spec()
		specs = append(specs, llm.ToolSpec{Name: s.Name, Description: s.Description, Parameters: s.Parameters})
	}

	stream, err := r.spec.Client.Stream(ctx, llm.Request{
		System:          system,
		Messages:        r.messages,
		Tools:           specs,
		MaxOutputTokens: r.spec.MaxOutputTokens,
	})
	if err != nil {
		return nil, nil, err
	}

	var (
		texts   []string
		calls   []llm.ToolCall
		partial map[int]*strings.Builder
		index   []int // ordered indexes, so call order is preserved
	)
	partial = make(map[int]*strings.Builder)

	for {
		// The channel is drained with an explicit select rather than a range:
		// a client whose Stream implementation ignores its context (a stalled
		// HTTP body, a stub, a future protocol) must not be able to wedge the
		// turn after the user pressed stop.
		var (
			ev llm.Event
			ok bool
		)
		select {
		case <-ctx.Done():
			go drainStream(stream)
			return nil, nil, ctx.Err()
		case ev, ok = <-stream:
			if !ok {
				goto drained
			}
		}

		switch ev.Kind {
		case llm.KindText:
			texts = append(texts, ev.Text)
			r.emit(Event{Kind: EventText, Text: ev.Text})

		case llm.KindReasoning:
			r.emit(Event{Kind: EventReasoning, Reasoning: ev.Reasoning})

		case llm.KindToolCallStart:
			idx := len(index)
			index = append(index, idx)
			partial[idx] = &strings.Builder{}
			calls = append(calls, llm.ToolCall{ID: ev.ToolCall.ID, Name: ev.ToolCall.Name})
			// Announce immediately so the UI can open a card before the
			// arguments finish streaming.
			r.emit(Event{Kind: EventToolStart, ToolCallID: ev.ToolCall.ID, ToolName: ev.ToolCall.Name})

		case llm.KindToolCallDelta:
			if len(index) == 0 {
				continue
			}
			last := index[len(index)-1]
			partial[last].WriteString(ev.ToolCall.Arguments)

		case llm.KindUsage:
			if ev.Usage != nil {
				r.usage.InputTokens += ev.Usage.InputTokens
				r.usage.OutputTokens += ev.Usage.OutputTokens
				r.emit(Event{Kind: EventUsage, Usage: ev.Usage})
			}

		case llm.KindError:
			// Drain any remaining events so the producer can exit, then fail.
			go drainStream(stream)
			return nil, nil, ev.Err

		case llm.KindDone:
			// Terminal; the channel closes next.
		}
	}

drained:
	for i := range calls {
		if b, ok := partial[i]; ok {
			calls[i].Arguments = b.String()
		}
		if calls[i].ID == "" {
			// Some gateways omit the id. Minting a stable one here keeps the
			// result/call pairing intact downstream.
			calls[i].ID = fmt.Sprintf("call_%d_%d", r.rounds, i)
		}
	}
	return calls, texts, nil
}

// drainStream consumes a stream to completion in the background.
//
// Used on the paths that stop reading early (cancellation, a terminal error
// event): the producer blocks on the channel, so abandoning it would leak both
// the goroutine and the in-flight HTTP response.
func drainStream(stream <-chan llm.Event) {
	for range stream {
	}
}

// compactIfNeeded rewrites the older part of the conversation into a summary
// when it no longer fits the model's window.
//
// It is called once before the first model round and again after each tool
// round. The two call sites cover the two ways a conversation grows: across
// turns (replayed history) and within one turn (tool results).
//
// It loops: one pass may not be enough when the kept tail is itself oversized
// (a small window with several long recent turns). Each pass folds strictly more
// than the last, so the loop terminates; the cap is a belt-and-braces guard
// against a pathological estimate.
//
// Failure is non-fatal: a conversation that could not be compacted continues
// as-is and the model call will either succeed or fail with the provider's own
// error. Refusing to continue because a SUMMARY failed would turn a degraded
// turn into no turn at all.
func (r *runner) compactIfNeeded(ctx context.Context, system string) {
	if r.spec.ContextWindow <= 0 {
		return
	}

	const maxPasses = 4
	for pass := 0; pass < maxPasses; pass++ {
		if !compaction.ShouldCompact(system, r.messages, r.spec.ContextWindow) {
			return
		}
		if !r.compactOnce(ctx, system) {
			// Nothing left to fold. The next model call may still overflow (one
			// enormous single message), but folding further would discard the
			// very thing the user just asked about.
			return
		}
	}
}

// compactOnce performs one compaction pass. It reports whether anything was
// folded.
func (r *runner) compactOnce(ctx context.Context, system string) bool {
	before := compaction.ConversationTokens(system, r.messages)
	split := compaction.SplitPoint(r.messages, compaction.KeepBudget(r.spec.ContextWindow))
	var (
		older  []llm.Message
		recent []llm.Message
		folded bool
	)
	if split > 0 {
		older, recent = r.messages[:split], r.messages[split:]
	} else {
		// No old exchange to fold: this single turn overflowed on its own. Cut
		// inside it by folding the earlier TOOL RESULTS, keeping the calls that
		// asked for them (a call without its result is what the API rejects).
		cut := compaction.ToolResultSplitPoint(r.messages)
		if cut <= 0 {
			return false
		}
		folded = true
		// The folded messages are the results before the cut. What is KEPT is
		// the whole conversation with those results replaced by short notes —
		// the calls stay, so the pairing the API validates stays intact.
		older = r.messages[:cut]
		prefix := compaction.FoldToolResults(r.messages[:cut], cut)
		recent = append(prefix, r.messages[cut:]...)
	}

	summary, err := r.summarize(ctx, older)
	if err != nil {
		slog.Warn("agentcore: compaction failed, continuing without it",
			"err", err, "dropped", len(older))
		return false
	}
	if strings.TrimSpace(summary) == "" {
		slog.Warn("agentcore: compaction produced an empty summary, continuing without it",
			"dropped", len(older))
		return false
	}

	// A second pass must not lose the first pass's summary: it is the only
	// record of everything folded before this call. Folding it into the new
	// summary keeps the record complete instead of summarizing only the tail of
	// what was already summarized.
	if r.summary != "" && r.summary != summary {
		summary = mergeSummaries(r.summary, summary)
	}

	r.messages = append([]llm.Message{compaction.SummaryMessage(summary)}, recent...)
	r.summary = summary

	after := compaction.ConversationTokens(system, r.messages)
	slog.Info("agentcore: compacted conversation",
		"dropped", len(older), "kept", len(recent),
		"tokens_before", before, "tokens_after", after,
		"window", r.spec.ContextWindow, "tool_results_folded", folded)

	r.emit(Event{
		Kind:          EventCompacted,
		Summary:       summary,
		Dropped:       len(older),
		SummaryTokens: after,
		WindowTokens:  r.spec.ContextWindow,
	})
	return true
}

// summarize turns the dropped messages into a summary.
//
// A caller-supplied Summarize is preferred (it can use the same endpoint with a
// dedicated prompt). Without one the deterministic digest below is used: it is
// worse prose but it is not worse INFORMATION, and a background task that never
// configured a summarizer still gets compaction instead of a hard failure.
func (r *runner) summarize(ctx context.Context, messages []llm.Message) (string, error) {
	text := compaction.RenderForSummary(messages)
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	if r.spec.Summarize != nil {
		return r.spec.Summarize(ctx, text)
	}
	return compaction.DigestSummary(text), nil
}

// mergeSummaries combines a previous compaction summary with a new one.
//
// Used when one compaction pass is not enough and a second pass folds more
// turns: the first summary is the only record of everything folded earlier, so
// dropping it would forget the beginning of the conversation entirely.
//
// It is a plain concatenation rather than a model call: the two texts are
// already summaries, and asking a model to summarize a summary (of a summary)
// degrades the facts with every pass. Concatenating keeps both verbatim.
//
// The result is CAPPED. Unbounded concatenation would eventually make the
// summary itself overflow the window — the compaction would then be the thing
// that does not fit. Past the cap the OLDER half is dropped with a note, which
// loses the distant past while keeping the recent past and the boundary honest.
func mergeSummaries(previous, next string) string {
	combined := strings.TrimSpace(previous) + "\n\n" + strings.TrimSpace(next)
	limit := compaction.MaxSummaryTokens * 4 // rough runes-per-token, biased high
	if len([]rune(combined)) <= limit {
		return combined
	}
	note := "[The earliest part of this summary was dropped to stay within the context window.]\n\n"
	keep := limit - len([]rune(note))
	if keep < 0 {
		return note
	}
	// Keep the TAIL: the most recent part of the summary is what the next reply
	// depends on, exactly like the kept turns.
	runes := []rune(combined)
	return note + string(runes[len(runes)-keep:])
}

// executeTools runs every call in order and returns the results.
func (r *runner) executeTools(ctx context.Context, env tools.Env, calls []llm.ToolCall) []*llm.ToolResult {
	results := make([]*llm.ToolResult, 0, len(calls))
	for _, c := range calls {
		if r.toolCalls >= r.spec.Policy.ToolCalls() {
			results = append(results, &llm.ToolResult{
				CallID:  c.ID,
				IsError: true,
				Parts:   []llm.Part{{Text: "Tool call budget for this turn is exhausted; no further tools will run."}},
			})
			continue
		}
		r.toolCalls++

		start := time.Now()
		tool, ok := r.toolIndex[c.Name]
		var res tools.Result
		if !ok {
			// A hallucinated tool name. Telling the model which tools exist
			// lets it correct itself instead of retrying the same name.
			res = tools.Result{
				Text:    fmt.Sprintf("There is no tool named %q. Available tools: %s.", c.Name, r.availableToolNames()),
				IsError: true,
			}
		} else {
			res = tool.Run(env, []byte(c.Arguments))
		}
		elapsed := time.Since(start)

		r.emit(Event{
			Kind:         EventToolEnd,
			ToolCallID:   c.ID,
			ToolName:     c.Name,
			ToolInput:    c.Arguments,
			ToolOutput:   res.Text,
			ToolIsError:  res.IsError,
			ToolDuration: elapsed,
		})

		parts := []llm.Part{{Text: res.Text}}
		// An image only travels to a model that can see it. A text-only model
		// already got a note from ReadToolNoVision, so reaching here with an
		// image means the client supports vision.
		if res.Image != nil {
			parts = append(parts, llm.Part{Image: &llm.Image{MediaType: res.Image.MediaType, Data: res.Image.Data}})
		}
		results = append(results, &llm.ToolResult{CallID: c.ID, Parts: parts, IsError: res.IsError})
	}
	return results
}

func (r *runner) availableToolNames() string {
	names := make([]string, 0, len(r.toolList))
	for _, t := range r.toolList {
		names = append(names, t.Name())
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// emit sends an event, giving up if the consumer went away.
func (r *runner) emit(ev Event) {
	if ev.Kind == EventDone || ev.Kind == EventError {
		r.terminated = true
	}
	select {
	case r.ch <- ev:
	case <-time.After(emitGrace):
		// A consumer that has stopped reading for this long is gone. Dropping
		// the event is better than blocking the whole run forever, and the
		// terminal event below still closes the loop.
		slog.Warn("agentcore: consumer stopped reading, dropping event", "kind", ev.Kind)
	}
}

// emitGrace bounds how long one event may wait for a consumer.
const emitGrace = 30 * time.Second

// fail sends the terminal error event.
func (r *runner) fail(err error) {
	r.emit(Event{Kind: EventError, Err: err})
}

// finishStopped ends the run after a budget or cancellation stop.
//
// A budget stop is not a failure: the user sees whatever the model produced so
// far plus a note saying why it ended, which is far more useful than a generic
// error. A cancellation is reported as cancellation so the caller can tell it
// apart from "the model finished".
func (r *runner) finishStopped(ctx context.Context, reason string) {
	if reason == StopCancelled || ctx.Err() != nil {
		r.emit(Event{Kind: EventError, Err: context.Canceled})
		return
	}
	r.emit(Event{
		Kind:       EventDone,
		StopReason: reason,
		Rounds:     r.rounds,
		ToolCalls:  r.toolCalls,
		TotalUsage: r.usage,
		Summary:    r.summary,
	})
}
