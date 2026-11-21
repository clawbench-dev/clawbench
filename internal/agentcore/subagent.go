package agentcore

import (
	"encoding/json"
	"fmt"
	"strings"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
)

const (
	// taskToolName is the tool the model calls to delegate work.
	taskToolName = "task"
	// maxSubagentDepth bounds recursion. 1 means the top-level turn may spawn
	// sub-agents, and a sub-agent may not spawn another. Raising it is a
	// one-line change; the budget and event attribution are already built for
	// more than one level.
	maxSubagentDepth = 1
	// MaxSubagentReportRunes bounds the report handed to the parent. A child
	// that pastes a whole file into its report would otherwise eat the parent's
	// context — the exact cost delegation is meant to avoid.
	MaxSubagentReportRunes = 32 * 1024

	// Argument names, shared by the schema and the decoder so the two cannot
	// drift.
	taskArgDescription = "description"
	taskArgPrompt      = "prompt"
)

// subagentRole is appended to the parent's system prompt for a child run.
//
// It is a section rather than prose because a model reads a tagged list as
// rules to follow. The four facts are the ones a child gets wrong without
// being told: there is no user to ask, ambiguity must not stall it, it cannot
// recurse, and the parent needs a report rather than a restatement of the task.
const subagentRole = `<role>
You are a sub-agent. Another agent spawned you to complete ONE task.
You have no user: do not ask questions and do not wait for confirmation.
If something is ambiguous, state the assumption and proceed.
You cannot delegate to another sub-agent.
When you finish, reply with a concise report: what you did, what you found (with file paths), and anything the parent must know. Do not restate the task.
</role>`

// taskTool delegates one self-contained task to a fresh sub-agent.
//
// It lives in this package rather than tools because it needs the parent's
// runner (client, policy, system prompt, shared budget, event channel) — the
// tools package is a leaf and must stay one.
type taskTool struct {
	r *runner
}

// Name reports the tool identifier the model calls.
func (t *taskTool) Name() string { return taskToolName }

// Snippet is the one-line "what is this for" in the system prompt's tool list.
func (t *taskTool) Snippet() string {
	return "delegate a self-contained subtask to a fresh sub-agent and get its report"
}

// Guidelines are the usage rules rendered into the prompt.
func (t *taskTool) Guidelines() []string {
	return []string{
		"Use task for independent work that would flood this conversation with intermediate output " +
			"(broad searches, multi-file surveys). Do not use it for a single read or grep you can do in one call.",
		"The sub-agent does not see this conversation. Put every fact it needs in the prompt.",
	}
}

// Spec declares the tool to the model.
func (t *taskTool) Spec() tools.Spec {
	return tools.Spec{
		Name: taskToolName,
		Description: "Delegate one self-contained task to a fresh sub-agent. " +
			"The sub-agent does not see this conversation, so the prompt must contain all the context it needs. " +
			"It returns a report; its intermediate work is not added to this conversation.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				taskArgDescription: map[string]any{
					"type":        "string",
					"description": "A short (3-6 word) label for the task, shown on the card.",
				},
				taskArgPrompt: map[string]any{
					"type":        "string",
					"description": "The complete brief: background, scope, and the expected output.",
				},
			},
			"required": []string{taskArgDescription, taskArgPrompt},
		},
	}
}

// Run executes one delegation.
func (t *taskTool) Run(env tools.Env, raw json.RawMessage) tools.Result {
	var args struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if len(raw) == 0 {
		return taskError("no arguments were provided")
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return taskError("arguments are not valid JSON for this tool: " + err.Error())
	}
	args.Prompt = strings.TrimSpace(args.Prompt)
	if args.Prompt == "" {
		return taskError("prompt is required: the sub-agent does not see this conversation, so it needs a complete brief")
	}

	limit := t.r.spec.Policy.Subagents()
	ordinal, ok := t.r.spec.Budget.TakeSubagent(limit)
	if !ok {
		return taskError(fmt.Sprintf("this turn has already used its %d sub-agents", limit))
	}

	child := t.childSpec(args.Prompt, ordinal)
	report, stopReason, runErr := t.runChild(env, child)
	report = truncateSubagentReport(strings.TrimSpace(report))

	if runErr != nil {
		text := "The sub-agent failed: " + runErr.Error()
		if report != "" {
			text += "\n\nPartial report:\n" + report
		}
		return tools.Result{Text: text, Display: args.Description, IsError: true}
	}
	if report == "" {
		text := "The sub-agent finished without a report."
		if stopReason == StopMaxRounds || stopReason == StopMaxToolCalls {
			text = "The sub-agent ran out of budget before producing a report."
		}
		return tools.Result{Text: text, Display: args.Description, IsError: true}
	}
	if stopReason == StopMaxRounds || stopReason == StopMaxToolCalls {
		report += "\n\n[The sub-agent ran out of budget before finishing; the report may be incomplete.]"
	}
	return tools.Result{Text: report, Display: args.Description}
}

// childSpec builds the run for one sub-agent.
//
// The parent's history, compaction summary and timeout are deliberately
// dropped: a child starts clean (isolation is the point) and is bounded by the
// parent's context. Its policy is copied, never widened.
func (t *taskTool) childSpec(prompt string, ordinal int) Spec {
	child := t.r.spec
	child.Prompt = prompt
	child.History = nil
	child.HistoryMessageIDs = nil
	child.PriorSummary = ""
	child.Depth = t.r.spec.Depth + 1
	child.Timeout = 0
	child.SystemPrompt = t.r.spec.SystemPrompt + "\n\n" + subagentRole
	if t.r.spec.SessionID != "" {
		// A distinct cache key: the child's conversation shares no prefix with
		// the parent's, so reusing the key would only pollute the provider's
		// prefix cache.
		child.SessionID = fmt.Sprintf("%s/sub-%d", t.r.spec.SessionID, ordinal)
	}
	return child
}

// runChild drives the child run and forwards its visible events.
//
// Terminal, usage and compaction events are swallowed: forwarding a child's
// done would end the parent's turn, and forwarding its compaction would write
// the child's boundary into the parent's stored summary.
func (t *taskTool) runChild(env tools.Env, child Spec) (string, string, error) {
	var (
		report     strings.Builder
		usage      llm.Usage
		stopReason = StopComplete
		runErr     error
	)

	for ev := range Run(env.Ctx, child) {
		switch ev.Kind {
		case EventText:
			// The report is what the child says after its LAST tool call;
			// earlier text is narration and is discarded when a tool ends.
			report.WriteString(ev.Text)
			t.forward(ev, env.CallID)

		case EventReasoning, EventToolStart, EventToolArgs:
			t.forward(ev, env.CallID)

		case EventToolEnd:
			report.Reset()
			t.forward(ev, env.CallID)

		case EventUsage:
			if ev.Usage != nil {
				usage.InputTokens += ev.Usage.InputTokens
				usage.OutputTokens += ev.Usage.OutputTokens
				usage.CacheReadTokens += ev.Usage.CacheReadTokens
				usage.CacheWriteTokens += ev.Usage.CacheWriteTokens
			}

		case EventCompacted:
			// A child's summary is temporary: the child's conversation is never
			// replayed, so its boundary has no meaning for the parent.

		case EventDone:
			stopReason = ev.StopReason

		case EventError:
			runErr = ev.Err
		}
	}

	// The child's spend belongs to the turn even when it failed.
	t.r.usage.InputTokens += usage.InputTokens
	t.r.usage.OutputTokens += usage.OutputTokens
	t.r.usage.CacheReadTokens += usage.CacheReadTokens
	t.r.usage.CacheWriteTokens += usage.CacheWriteTokens

	// The child's terminal event was swallowed, so close its reasoning block
	// explicitly; otherwise the UI keeps a spinner with no body.
	t.r.emit(Event{Kind: EventReasoningDone, ParentToolCallID: env.CallID})

	if runErr != nil {
		return "", stopReason, runErr
	}
	return strings.TrimSpace(report.String()), stopReason, nil
}

// forward sends one child event to the parent's channel, attributing it to the
// task call unless the child already attributed it to its own child (which can
// only happen once recursion is raised above one level).
func (t *taskTool) forward(ev Event, parent string) {
	if ev.ParentToolCallID == "" {
		ev.ParentToolCallID = parent
	}
	t.r.emit(ev)
}

// taskError is the result shape for a delegation that never started.
func taskError(msg string) tools.Result {
	return tools.Result{Text: msg, IsError: true}
}

// truncateSubagentReport caps the report so one child cannot spend the
// parent's whole context on its own output.
func truncateSubagentReport(s string) string {
	runes := []rune(s)
	if len(runes) <= MaxSubagentReportRunes {
		return s
	}
	return string(runes[:MaxSubagentReportRunes]) + "\n\n[The sub-agent's report was truncated.]"
}
