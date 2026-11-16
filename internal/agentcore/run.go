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
	// ThinkingEffort is the reasoning level the user picked ("low", "medium",
	// "high"), or "" / "auto" for "let the model decide". It is advisory: a
	// model that does not support reasoning fails the call rather than silently
	// ignoring it, so the user sees which level was rejected.
	ThinkingEffort string
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
	// EventToolArgs carries a call's arguments once they are complete, just
	// before the tool runs. The start event cannot: arguments stream in after
	// it, so a tool that runs for minutes had no summary — no command, no file —
	// until its result arrived.
	EventToolArgs EventKind = "tool_args"
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
	// Limit is set on EventDone when a budget stopped the run: the cap that
	// was hit. It is NOT the same as Rounds/ToolCalls — the closing summary is
	// one model call past the cap, so reporting the totals as the limit would
	// name a number the user never configured.
	Limit int
	// Truncated is set on EventDone when the run ended while the model's reply
	// was still cut off at the output cap (the continuation budget ran out).
	// The answer is then a prefix and the UI should say so.
	Truncated bool
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
	// Truncated reports that the run ended while the model's reply was still
	// cut off at the output cap: Text is a prefix, not a finished answer. A
	// caller that produces an artifact (rather than a chat turn) must not treat
	// it as complete.
	Truncated bool
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
			res.Truncated = ev.Truncated
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
	// nudges counts how many times a model round produced neither text nor a
	// tool call and was answered with a continuation prompt. Capped so a
	// provider that keeps returning nothing cannot loop forever.
	nudges int
	// truncations counts how many times a reply was continued after the
	// endpoint cut it off at the output cap.
	truncations int
	// lastTruncated records that the most recent round was cut off at the
	// output cap, so the terminal event can say the answer may be incomplete.
	lastTruncated bool
	// terminated records that a done/error event was already sent. The panic
	// guard reads it so a panic after the terminal event cannot emit a second.
	terminated bool
}

// maxEmptyRoundNudges bounds how many times one run asks the model to continue
// after a round that produced nothing visible.
//
// One is enough for the case this exists for (a reasoning-only round, or a
// reply truncated before any content arrived). More would just burn model calls
// against an endpoint that is consistently returning empty responses.
const maxEmptyRoundNudges = 1

// maxTruncationContinuations bounds how many times one run continues a reply
// that the endpoint cut off at its output cap.
//
// Each continuation is a full model round (and costs a round of the budget), so
// an unbounded loop would turn a too-small max_tokens into a very expensive
// turn. Two continuations cover a long answer split into three parts; past that
// the run ends and says the reply may be incomplete.
const maxTruncationContinuations = 2

// truncationNudge asks the model to carry on from a cut-off reply.
//
// "Do not repeat" matters: without it the model usually restarts the answer
// from the beginning, which duplicates everything the user already read.
const truncationNudge = "Your previous reply was cut off by the output-token limit. " +
	"Continue from exactly where it stopped, without repeating anything you already wrote " +
	"and without starting over."

// emptyRoundNudge is sent when a round produced neither text nor a tool call.
//
// Such a round is a stall, not an answer: the model reasoned and then stopped,
// which used to end the turn with nothing visible. Naming both acceptable
// outcomes ("result" or "next tool call") keeps it from answering the nudge
// with another empty round.
const emptyRoundNudge = "Your previous reply contained no message to the user and no tool call. " +
	"Continue the user's task now: either take the next step with a tool call, " +
	"or, if the task is already finished, state the result in your reply."

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
		if runCtx.Err() != nil {
			r.finishCancelled()
			return
		}
		if r.rounds >= r.spec.Policy.Rounds() {
			r.finishWithClosing(runCtx, StopMaxRounds, system)
			return
		}
		r.rounds++

		out, err := r.runModelRound(runCtx, system, true)
		if err != nil {
			if runCtx.Err() != nil {
				r.finishCancelled()
				return
			}
			r.fail(err)
			return
		}

		r.lastTruncated = out.truncated
		r.recordAssistantTurn(out.texts, out.calls, out.thinking)

		// No tool calls means the model is answering. That is the normal end —
		// but only when it actually finished saying something: a reply cut off
		// at the output cap is a prefix, and a round with no text at all is a
		// stall.
		if len(out.calls) == 0 {
			if r.continueTruncated(out) {
				continue
			}
			if r.nudgeEmptyRound(out.texts) {
				continue
			}
			r.finish(StopComplete, 0)
			return
		}

		results := r.executeTools(runCtx, env, out.calls)
		if runCtx.Err() != nil {
			r.finishCancelled()
			return
		}
		r.recordToolResults(results)

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
			r.finishWithClosing(runCtx, StopMaxToolCalls, system)
			return
		}
	}
}

// continueTruncated asks the model to carry on after the endpoint cut its reply
// off at the output cap. It reports whether the run should keep going.
//
// The partial reply is already in the conversation (recordAssistantTurn ran), so
// the model sees where it stopped. The cap exists so a model whose output limit
// is far too small cannot burn the whole round budget on continuations.
func (r *runner) continueTruncated(out roundOutput) bool {
	if !out.truncated || r.truncations >= maxTruncationContinuations {
		return false
	}
	r.truncations++
	slog.Info("agentcore: reply hit the output cap, asking the model to continue",
		"round", r.rounds, "continuation", r.truncations)
	r.messages = append(r.messages, llm.TextMessage(llm.RoleUser, truncationNudge))
	return true
}

// recordAssistantTurn appends the model's reply to the conversation.
//
// Thinking goes FIRST: Anthropic requires the reasoning block to lead the
// assistant turn that carries the tool calls it justified.
//
// A round that produced neither text nor tool calls is NOT recorded: it carries
// no decision, and an assistant turn with nothing but reasoning is rejected by
// some providers once the continuation prompt is appended to the conversation.
func (r *runner) recordAssistantTurn(text []string, calls []llm.ToolCall, thinking *llm.Thinking) {
	if len(text) == 0 && len(calls) == 0 {
		return
	}
	parts := make([]llm.Part, 0, len(text)+len(calls)+1)
	if thinking != nil {
		parts = append(parts, llm.Part{Thinking: thinking})
	}
	for _, t := range text {
		parts = append(parts, llm.Part{Text: t})
	}
	for _, c := range calls {
		parts = append(parts, llm.Part{ToolCall: &llm.ToolCall{
			ID: c.ID, Name: c.Name, Arguments: c.Arguments,
		}})
	}
	if len(parts) > 0 {
		r.messages = append(r.messages, llm.Message{Role: llm.RoleAssistant, Parts: parts})
	}
}

// recordToolResults appends the tool outputs as one message; the clients split
// it per protocol (Anthropic merges blocks into a user message, OpenAI emits one
// tool message per result).
func (r *runner) recordToolResults(results []*llm.ToolResult) {
	parts := make([]llm.Part, 0, len(results))
	for _, res := range results {
		parts = append(parts, llm.Part{ToolResult: res})
	}
	r.messages = append(r.messages, llm.Message{Role: llm.RoleTool, Parts: parts})
}

// nudgeEmptyRound asks the model to continue after a round that produced
// neither text nor a tool call. It reports whether the run should keep going.
//
// Such a round is a stall, not an answer: the model reasoned and then stopped.
// Treating it as the answer is how a turn ends with nothing visible in it. The
// nudge is bounded so a provider that keeps returning nothing cannot loop.
func (r *runner) nudgeEmptyRound(text []string) bool {
	if hasVisibleText(text) || r.nudges >= maxEmptyRoundNudges {
		return false
	}
	r.nudges++
	slog.Info("agentcore: empty model round, asking the model to continue",
		"round", r.rounds)
	r.messages = append(r.messages, llm.TextMessage(llm.RoleUser, emptyRoundNudge))
	return true
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
//
// allowTools false withholds the tool declarations for this round: the closing
// summary runs that way, because a model that could still call tools would ask
// for work the budget cannot pay for. A provider that emits a tool call anyway
// is ignored rather than announced, so the UI never shows a card whose result
// will never arrive.
func (r *runner) runModelRound(ctx context.Context, system string, allowTools bool) (roundOutput, error) {
	stream, err := r.spec.Client.Stream(ctx, llm.Request{
		System:          system,
		Messages:        r.messages,
		Tools:           r.toolSpecs(allowTools),
		MaxOutputTokens: r.spec.MaxOutputTokens,
		ReasoningEffort: r.spec.ThinkingEffort,
	})
	if err != nil {
		return roundOutput{}, err
	}

	acc := newRoundAccumulator(allowTools)
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
			return roundOutput{}, ctx.Err()
		case ev, ok = <-stream:
			if !ok {
				return acc.result(r.rounds), nil
			}
		}
		if err := acc.apply(ev, r); err != nil {
			// Drain any remaining events so the producer can exit, then fail.
			go drainStream(stream)
			return roundOutput{}, err
		}
	}
}

// roundOutput is one model round's result.
type roundOutput struct {
	calls    []llm.ToolCall
	texts    []string
	thinking *llm.Thinking
	// truncated means the endpoint stopped at its output cap: the reply is a
	// prefix of what the model was writing.
	truncated bool
}

// toolSpecs declares the tools for one round, or none when the round must not
// call any.
func (r *runner) toolSpecs(allowTools bool) []llm.ToolSpec {
	if !allowTools {
		return nil
	}
	specs := make([]llm.ToolSpec, 0, len(r.toolList))
	for _, t := range r.toolList {
		s := t.Spec()
		specs = append(specs, llm.ToolSpec{Name: s.Name, Description: s.Description, Parameters: s.Parameters})
	}
	return specs
}

// roundAccumulator collects one model round's output as its events arrive.
//
// It is a separate type rather than locals in runModelRound so the event switch
// (one case per stream event, each with its own rule) does not live inside the
// read loop, where the two concerns made the function hard to follow and hard
// to keep under the complexity limit.
type roundAccumulator struct {
	// allowTools is false for the closing round: tool calls are dropped instead
	// of announced.
	allowTools bool
	texts      []string
	calls      []llm.ToolCall
	partial    map[int]*strings.Builder
	// index keeps the call order: deltas address their call by position, and
	// map iteration order is random.
	index     []int
	reasoning strings.Builder
	signature string
	// truncated records that the stream ended at the output cap, so the reply
	// is a prefix rather than the whole answer.
	truncated bool
}

func newRoundAccumulator(allowTools bool) *roundAccumulator {
	return &roundAccumulator{
		allowTools: allowTools,
		partial:    make(map[int]*strings.Builder),
	}
}

// apply consumes one stream event and reports a terminal stream error.
func (a *roundAccumulator) apply(ev llm.Event, r *runner) error {
	switch ev.Kind {
	case llm.KindText:
		a.texts = append(a.texts, ev.Text)
		r.emit(Event{Kind: EventText, Text: ev.Text})

	case llm.KindReasoning:
		a.reasoning.WriteString(ev.Reasoning)
		r.emit(Event{Kind: EventReasoning, Reasoning: ev.Reasoning})

	case llm.KindReasoningSignature:
		// Needed to replay the block when this turn also calls tools.
		a.signature = ev.Signature

	case llm.KindToolCallStart:
		if !a.allowTools {
			// Tools were withheld for this round; a call here would open a card
			// that can never complete.
			return nil
		}
		idx := len(a.index)
		a.index = append(a.index, idx)
		a.partial[idx] = &strings.Builder{}
		a.calls = append(a.calls, llm.ToolCall{ID: ev.ToolCall.ID, Name: ev.ToolCall.Name})
		// Announce immediately so the UI can open a card before the arguments
		// finish streaming.
		r.emit(Event{Kind: EventToolStart, ToolCallID: ev.ToolCall.ID, ToolName: ev.ToolCall.Name})

	case llm.KindToolCallDelta:
		if !a.allowTools || len(a.index) == 0 {
			return nil
		}
		last := a.index[len(a.index)-1]
		_, _ = a.partial[last].WriteString(ev.ToolCall.Arguments)

	case llm.KindUsage:
		if ev.Usage != nil {
			r.usage.InputTokens += ev.Usage.InputTokens
			r.usage.OutputTokens += ev.Usage.OutputTokens
			r.emit(Event{Kind: EventUsage, Usage: ev.Usage})
		}

	case llm.KindError:
		return ev.Err

	case llm.KindDone:
		// Terminal; the channel closes next. The truncation flag rides the
		// done event.
		a.truncated = ev.Truncated
	}
	return nil
}

// result assembles the round's output once the stream has closed.
func (a *roundAccumulator) result(round int) roundOutput {
	for i := range a.calls {
		if b, ok := a.partial[i]; ok {
			a.calls[i].Arguments = b.String()
		}
		if a.calls[i].ID == "" {
			// Some gateways omit the id. Minting a stable one here keeps the
			// result/call pairing intact downstream.
			a.calls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
		}
	}
	var thinking *llm.Thinking
	if a.reasoning.Len() > 0 {
		thinking = &llm.Thinking{Text: a.reasoning.String(), Signature: a.signature}
	}
	return roundOutput{calls: a.calls, texts: a.texts, thinking: thinking, truncated: a.truncated}
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

		// Arguments are complete now — tell the UI before running, so the tool
		// card can show WHAT is running (the command, the file) instead of a
		// bare tool name for the whole execution.
		r.emit(Event{Kind: EventToolArgs, ToolCallID: c.ID, ToolName: c.Name, ToolInput: c.Arguments})

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

// finishCancelled ends a run the user (or the turn timeout) stopped.
//
// A cancellation is an error rather than a done event so the caller can tell
// "the model finished" from "the user cut it short".
func (r *runner) finishCancelled() {
	r.emit(Event{Kind: EventError, Err: context.Canceled})
}

// closingPrompt tells the model its budget is spent and asks for a written
// hand-off.
//
// It names the reason because "you have no tools" alone reads as a glitch; the
// model then either apologizes vaguely or pretends the work is finished. The
// three questions are the three things a user needs from a turn that stopped
// early: what got done, what is left, what would come next.
func closingPrompt(reason string) string {
	limit := "step budget for this turn"
	if reason == StopMaxToolCalls {
		limit = "tool-call budget for this turn"
	}
	return "You have used up this turn's " + limit + ", so no further tools can run. " +
		"Reply to the user in a few sentences: what you have actually completed, " +
		"what is still missing, and what the next step would be. " +
		"Do not claim anything you did not actually do."
}

// finishWithClosing ends a budget-stopped run with one last model call that has
// no tools.
//
// Without it the transcript stops on a tool result — the user sees a wall of
// `sed`/`grep` output and no answer, which reads as the agent dying mid-task.
// The call is best-effort: if it fails, the run still ends with the same stop
// reason and whatever the model already produced.
func (r *runner) finishWithClosing(ctx context.Context, reason, system string) {
	limit := r.spec.Policy.Rounds()
	if reason == StopMaxToolCalls {
		limit = r.spec.Policy.ToolCalls()
	}
	if ctx.Err() != nil {
		r.finishCancelled()
		return
	}
	// Counted in Rounds because it really is a model call; the budget is
	// already spent, so this is deliberately the one call past the cap.
	r.rounds++
	r.messages = append(r.messages, llm.TextMessage(llm.RoleUser, closingPrompt(reason)))
	if out, err := r.runModelRound(ctx, system, false); err != nil {
		// Pressing stop during the closing round is still a cancellation, not
		// a completed turn that happened to run out of budget.
		if ctx.Err() != nil {
			r.finishCancelled()
			return
		}
		slog.Warn("agentcore: closing round failed, ending without a summary",
			"err", err, "stop_reason", reason)
	} else {
		r.lastTruncated = out.truncated
	}
	r.finish(reason, limit)
}

// finish sends the terminal success event with the run's totals.
func (r *runner) finish(reason string, limit int) {
	r.emit(Event{
		Kind:       EventDone,
		StopReason: reason,
		Limit:      limit,
		Truncated:  r.lastTruncated,
		Rounds:     r.rounds,
		ToolCalls:  r.toolCalls,
		TotalUsage: r.usage,
		Summary:    r.summary,
	})
}

// hasVisibleText reports whether a round produced anything the user can read.
//
// Whitespace-only output counts as nothing: some gateways emit a single newline
// before closing a stream, and that must not be mistaken for an answer.
func hasVisibleText(parts []string) bool {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return true
		}
	}
	return false
}
