package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// BuiltinAgentBackendID mirrors builtin.BackendID.
//
// Duplicated as a constant rather than imported: this package must not depend
// on the backend plugin (which imports ai and model). A drift test pins it.
const builtinAgentBackendID = "builtin"

// BuiltinAgentID is the agent id the built-in backend auto-creates.
//
// The id is the spec id (see model.RefreshAgents), so this is also the id the
// rows in the agents table carry.
const BuiltinAgentID = builtinAgentBackendID

// builtinAgentAvailable reports whether the built-in agent can be used for a
// background task right now.
//
// Three independent things must hold, and each has its own user-facing
// consequence when it does not:
//   - the agent row exists (it is auto-created on startup, so this is "the
//     install is old enough" or "the user deleted it"),
//   - it is not disabled (the user turned it off on purpose),
//   - it has a provider bound (otherwise there is no endpoint to call).
func builtinAgentAvailable() (*model.Agent, error) {
	agent := model.GetAgent(BuiltinAgentID)
	if agent == nil {
		return nil, fmt.Errorf("the built-in agent is not installed")
	}
	if agent.Disabled {
		return nil, fmt.Errorf("the built-in agent is disabled")
	}
	if agent.Provider() == nil {
		return nil, fmt.Errorf("the built-in agent has no model provider bound")
	}
	return agent, nil
}

// BuiltinAgentAvailable reports whether the built-in agent can run a task.
func BuiltinAgentAvailable() bool {
	_, err := builtinAgentAvailable()
	return err == nil
}

// BuiltinAgentTask describes one background task for the built-in agent.
type BuiltinAgentTask struct {
	// Prompt is the user-side instruction (what to produce).
	Prompt string
	// SystemPrompt is the role instruction, before the tool preamble.
	SystemPrompt string
	// WorkDir confines every file the agent may read. Empty means "no file
	// tools", which is the safe default for a task that only needs the text it
	// was handed.
	WorkDir string
	// Policy decides which tools exist. Zero value = no tools (text only).
	Policy model.ToolPolicy
	// MaxOutputTokens caps the reply. Zero follows the model.
	MaxOutputTokens int
	// Timeout bounds the whole run. Zero means no extra bound.
	Timeout time.Duration
}

// RunBuiltinAgentTask runs one background task on the built-in agent and returns
// its final answer.
//
// This is the entry point for internal work (meeting minutes, proofreading,
// skill extraction). It deliberately does NOT go through the chat pipeline: no
// session row, no WS events, no queue. A background task has no user watching
// its intermediate steps, so streaming would be noise — the caller wants the
// answer and the failure reason.
//
// The task is bounded on three axes (rounds, tool calls, wall clock) because
// nobody is there to press stop. See model.ToolPolicy and Task.Timeout.
func RunBuiltinAgentTask(ctx context.Context, task BuiltinAgentTask) (string, error) {
	agent, err := builtinAgentAvailable()
	if err != nil {
		return "", err
	}

	client, modelID, err := builtinAgentClient(agent)
	if err != nil {
		return "", err
	}

	// A caller that sets no budget must not fall through to the provider's
	// default: that default is often smaller than one reasoning round, and the
	// endpoint then cuts the answer off. Same rule as the chat path.
	maxOut := task.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = model.OutputTokenBudget(modelID)
	}

	spec := agentcore.Spec{
		Client:          client,
		SystemPrompt:    task.SystemPrompt,
		Prompt:          task.Prompt,
		WorkDir:         task.WorkDir,
		Policy:          task.Policy.Normalized(),
		MaxOutputTokens: maxOut,
		Timeout:         task.Timeout,
		ContextWindow:   model.ModelContextWindow(modelID),
	}

	res, err := agentcore.RunSync(ctx, spec, nil)
	if err != nil {
		return "", err
	}
	// A run that stopped on a budget is not a failure, but it IS an incomplete
	// answer. Returning it silently would hand the caller half a document that
	// looks whole, so a background task treats every non-complete stop as an
	// error — a chat turn can say "reply continue", a generated artifact
	// cannot. The closing round (agentcore.finishWithClosing) usually leaves a
	// note saying what is missing; that note is the error detail rather than
	// being thrown away.
	if res.StopReason != agentcore.StopComplete {
		detail := strings.TrimSpace(res.Text)
		if detail == "" {
			return "", fmt.Errorf("the built-in agent stopped before answering (%s)", res.StopReason)
		}
		return "", fmt.Errorf("the built-in agent stopped before finishing (%s): %s", res.StopReason, detail)
	}
	// A reply still cut off at the output cap is a prefix. The chat path tells
	// the user and offers "continue"; an artifact has nobody to ask, so it is a
	// failure like any other incomplete run.
	if res.Truncated {
		return "", fmt.Errorf("the built-in agent's answer was cut off at the model's output limit")
	}
	return strings.TrimSpace(res.Text), nil
}

// builtinAgentClient resolves the model endpoint for the built-in agent.
//
// The agent's own provider binding is the only source: a background task has no
// per-request override, and silently falling back to the shared summary model
// would produce answers the user cannot account for ("why is this minutes
// document written by a model I never chose?").
func builtinAgentClient(agent *model.Agent) (llm.Client, string, error) {
	provider := agent.Provider()
	if provider == nil {
		return nil, "", fmt.Errorf("the built-in agent has no model provider bound")
	}
	modelID := agent.DefaultModelID()
	if modelID == "" {
		return nil, "", fmt.Errorf("the built-in agent has no model selected")
	}
	client, err := llm.New(llm.Config{
		Protocol:       provider.Protocol,
		BaseURL:        provider.BaseURL,
		APIKey:         provider.APIKey,
		AuthMode:       provider.AuthMode,
		Model:          modelID,
		SupportsVision: model.ModelSupportsVision(provider.Protocol, modelID),
	})
	if err != nil {
		return nil, "", err
	}
	return client, modelID, nil
}
