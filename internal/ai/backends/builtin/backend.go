// Package builtin registers the built-in agent backend.
//
// Unlike every other backend, the tools of this one live in ClawBench itself
// (internal/agentcore) rather than in an external CLI. That difference drives
// every choice here:
//
//   - no spawn command, no install command, no login, no MCP injection;
//   - no external session: the conversation is rebuilt from chat_history each
//     turn, so there is no session_capture and nothing to resume;
//   - tools are granted per agent (model.ToolPolicy) instead of being whatever
//     the CLI happens to ship.
package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// BackendID is the backend/agent id. It is also the BackendSpec.ID, so a fresh
// install auto-inserts an agent with this id (see model.RefreshAgents).
const BackendID = "builtin"

func init() {
	ai.RegisterBackend(BackendID, newBackend)

	backends.Register(&backends.BackendPlugin{
		ID: BackendID,
		Spec: model.BackendSpec{
			ID:      BackendID,
			Backend: BackendID,
			// NoCLI makes the backend "always present" without a PATH probe:
			// there is no binary to find, the implementation is this process.
			NoCLI:   true,
			Builtin: true,
			// The product name, not a description of the mechanism: this agent
			// IS ClawBench running its own tool loop, so it is named after the
			// product rather than after the fact that it is "built in" (which
			// describes how it is implemented, not what it is).
			Name:      "ClawBench Agent",
			Specialty: "内置智能体：文件读写与命令执行，工具权限可精细控制", // i18n-exempt: backend spec specialty, localized by the client
			// No AcpCommand: this backend never spawns a process. Transport is
			// always "cli" in the sense of "the in-process path", and
			// SupportsACP() stays false because acp_command is empty.
			//
			// AutoLoadsNativeSkills must be false: this agent has no skill
			// directory of its own, so ClawBench must inject the shared table
			// (or nothing, depending on config) rather than assume the backend
			// loads it. Getting this wrong in the true direction would leave
			// the agent with no skills at all.
			AutoLoadsNativeSkills: false,
			SortOrder:             0,
		},
	})
}

// newBackend constructs the in-process backend.
func newBackend() ai.AIBackend {
	return &Backend{}
}

// Backend implements ai.AIBackend by driving agentcore in-process.
//
// It is stateless between turns on purpose: the conversation lives in
// chat_history, which the service layer reads and hands over as History. A
// cached transcript here would be a second source of truth that drifts the
// moment the user edits or deletes a message.
type Backend struct{}

// Name reports the backend id.
func (b *Backend) Name() string { return BackendID }

// ExecuteStream runs one turn and adapts agentcore's events to ai.StreamEvent.
//
// The returned channel is always closed and always ends with exactly one
// terminal event, matching the AIBackend contract.
func (b *Backend) ExecuteStream(ctx context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	spec, err := buildSpec(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan ai.StreamEvent, 64)
	go func() {
		defer close(ch)
		// One translator per turn: it tracks whether a reasoning block is
		// still open, so the closing event is emitted exactly once.
		tr := &translator{}
		runEvents := agentcore.Run(ctx, spec)
		for ev := range runEvents {
			if !forward(ch, tr, ev) {
				// The consumer is gone; keep draining so the producer can exit.
				for range runEvents {
				}
				return
			}
		}
	}()
	return ch, nil
}

// buildSpec resolves everything a run needs from the agent and the request.
func buildSpec(ctx context.Context, req ai.ChatRequest) (agentcore.Spec, error) {
	agent := model.GetAgent(req.AgentID)
	if agent == nil {
		return agentcore.Spec{}, fmt.Errorf("builtin: agent %q not found", req.AgentID)
	}

	client, modelID, err := buildClient(agent, req.Model)
	if err != nil {
		return agentcore.Spec{}, err
	}

	return agentcore.Spec{
		Client:       client,
		SystemPrompt: req.SystemPrompt,
		Prompt:       req.Prompt,
		History:      historyFor(req),
		WorkDir:      req.WorkDir,
		Policy:       agent.ToolPolicy.Normalized(),
		// The window comes from the model catalog. Zero means "unknown", which
		// disables compaction rather than guessing: compacting a conversation
		// that fits loses information for nothing.
		ContextWindow: model.ModelContextWindow(modelID),
		// A summary stored by an earlier turn replaces the turns it stood for,
		// so a long session compacts once instead of re-summarizing the same
		// history on every turn.
		PriorSummary: req.PriorSummary,
		Summarize:    summarizer(client, req.Model, modelID),
	}, nil
}

// summarizer builds the function that compacts older turns.
//
// It reuses the SAME client and model as the turn itself. Using a different
// endpoint would need a second set of credentials for a task that is inherently
// about the conversation being summarized, and would fail exactly when the user
// has only one provider configured — which is the common case.
//
// The system prompt is fixed and separate from the agent's own: compaction is a
// mechanical transcription task, and handing it the agent's persona would make
// it answer the conversation instead of summarizing it.
func summarizer(client llm.Client, modelOverride, modelID string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, text string) (string, error) {
		stream, err := client.Stream(ctx, llm.Request{
			System:          compaction.SummaryPrompt,
			Messages:        []llm.Message{llm.TextMessage(llm.RoleUser, text)},
			MaxOutputTokens: compaction.MaxSummaryTokens,
			// Summarizing is transcription, not composition: a low temperature
			// keeps it from embellishing facts a later turn will rely on.
			Temperature: 0.1,
		})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for ev := range stream {
			switch ev.Kind {
			case llm.KindText:
				b.WriteString(ev.Text)
			case llm.KindError:
				return "", ev.Err
			}
		}
		return b.String(), nil
	}
}

// buildClient resolves the model endpoint for one turn.
//
// The agent's own provider binding wins. When it has none, the shared summary
// model is used and the UI is told which model is running — a silent fallback
// to "some model" is how a user ends up debugging answers produced by an
// endpoint they never configured.
func buildClient(agent *model.Agent, modelOverride string) (llm.Client, string, error) {
	modelID := modelOverride
	if modelID == "" {
		modelID = agent.DefaultModelID()
	}

	provider := agent.Provider()
	if provider == nil {
		return nil, "", ErrNoProvider
	}

	client, err := llm.New(llm.Config{
		Protocol: provider.Protocol,
		BaseURL:  provider.BaseURL,
		APIKey:   provider.APIKey,
		AuthMode: provider.AuthMode,
		Model:    modelID,
		// Vision support comes from the model catalog when it knows the id.
		SupportsVision: model.ModelSupportsVision(provider.Protocol, modelID),
	})
	if err != nil {
		return nil, "", err
	}
	return client, modelID, nil
}

// ErrNoProvider is returned when an agent has no model endpoint bound.
//
// It is a hard error rather than a silent fallback: an agent that quietly ran
// on some other endpoint would produce answers the user cannot account for.
var ErrNoProvider = fmt.Errorf("this agent has no model provider bound; " +
	"open its settings and choose a provider, or configure the shared summary model")

// forward translates one agentcore event into ai.StreamEvent.
//
// It reports false when the event could not be delivered, which is the signal
// to stop reading.
func forward(ch chan<- ai.StreamEvent, tr *translator, ev agentcore.Event) bool {
	for _, out := range tr.translate(ev) {
		select {
		case ch <- out:
		case <-time.After(emitTimeout):
			slog.Warn("builtin: consumer stopped reading, dropping event", "kind", ev.Kind)
			return true
		}
	}
	return true
}
