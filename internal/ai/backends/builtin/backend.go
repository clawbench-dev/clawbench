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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// BackendID is the backend/agent id. It is also the BackendSpec.ID, so a fresh
// install auto-inserts an agent with this id (see model.RefreshAgents).
const BackendID = "builtin"

func init() {
	backends.Register(&backends.BackendPlugin{
		ID:  BackendID,
		CLI: &backends.CLIPlugin{NewBackend: newBackend},
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
			// The reasoning levels this backend understands. They are static
			// (every model gets the same list) because the choice is translated
			// by our own client, not advertised per model by an external agent.
			ThinkingEffortLevels: []string{
				llm.EffortAuto, llm.EffortLow, llm.EffortMedium, llm.EffortHigh,
			},
		},
		ProviderEnv: &model.ProviderEnvSpec{
			// The two protocols are declared with EMPTY variable names on
			// purpose: this backend calls the endpoint itself, so no credential
			// variable has to exist and no auth mode can be "unsupported".
			// InProcess is what tells the rest of the stack that (see its doc).
			Anthropic: model.ProtoEnv("", "", ""),
			OpenAI:    model.ProtoEnv("", "", ""),
			InProcess: true,
		},
	})

	// Mode and thinking-level options live in the capability registry so every
	// reader (the chat drawer, the settings page, the PATCH validators) sees
	// them through the same path as an ACP agent's advertised options.
	ai.RegisterStaticCapabilities(BackendID, staticCapability())
}

// staticCapability is the option set this backend offers to every agent that
// runs on it. A copied agent (new id, same backend) gets the same list.
func staticCapability() *ai.AgentCapability {
	modes := model.BuiltinModes()
	modeDefs := make([]ai.ModeDef, 0, len(modes))
	for _, m := range modes {
		modeDefs = append(modeDefs, ai.ModeDef{ID: m.ID, Name: m.Name})
	}
	return &ai.AgentCapability{
		AvailableModes: modeDefs,
		AvailableThinkingEfforts: []ai.ThinkingEffortDef{
			{ID: llm.EffortAuto, Name: "Auto"},
			{ID: llm.EffortLow, Name: "Low"},
			{ID: llm.EffortMedium, Name: "Medium"},
			{ID: llm.EffortHigh, Name: "High"},
		},
	}
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
	spec, err := buildSpec(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan ai.StreamEvent, 64)
	go func() {
		defer close(ch)
		// One translator per turn: it tracks whether a reasoning block is
		// still open, so the closing event is emitted exactly once.
		tr := &translator{locale: model.Language}
		runEvents := agentcore.Run(ctx, spec)
		for ev := range runEvents {
			forward(ch, tr, ev)
		}
	}()
	return ch, nil
}

// buildSpec resolves everything a run needs from the agent and the request.
func buildSpec(req ai.ChatRequest) (agentcore.Spec, error) {
	agent := model.GetAgent(req.AgentID)
	if agent == nil {
		return agentcore.Spec{}, fmt.Errorf("builtin: agent %q not found", req.AgentID)
	}

	client, modelID, err := buildClient(agent, req.Model)
	if err != nil {
		return agentcore.Spec{}, err
	}

	// One pass builds the messages AND the parallel id list. An assistant turn
	// with tool calls expands into two messages, so two independent loops would
	// drift and make a compaction boundary cover the wrong stored turn.
	replay := buildHistoryReplay(req)

	return agentcore.Spec{
		Client:       client,
		SystemPrompt: req.SystemPrompt,
		Prompt:       req.Prompt,
		History:      replay.Messages,
		// Parallel to History: the boundary a compaction reports is an id from
		// here, so the caller records exactly which stored turn the summary
		// stands for.
		HistoryMessageIDs: replay.IDs,
		SessionID:         req.SessionID,
		WorkDir:           req.WorkDir,
		// The session mode narrows the agent's grant; it never widens it. An
		// empty mode (background task, older client) leaves the grant as-is.
		Policy:         agent.ToolPolicy.Normalized().ForMode(req.Mode),
		ThinkingEffort: req.ThinkingEffort,
		// A reply budget derived from the model, not the provider default: a
		// reasoning model spends it on thinking first, and an unset max_tokens
		// is where "the answer stops mid-sentence" came from.
		MaxOutputTokens: model.OutputTokenBudget(modelID),
		// The window comes from the model catalog. Zero means "unknown", which
		// disables compaction rather than guessing: compacting a conversation
		// that fits loses information for nothing.
		ContextWindow: model.ModelContextWindow(modelID),
		// A summary stored by an earlier turn replaces the turns it stood for,
		// so a long session compacts once instead of re-summarizing the same
		// history on every turn.
		PriorSummary: req.PriorSummary,
		Summarize:    summarizer(client),
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
//
// The model is the client's own: buildClient already resolved the turn's model
// into its Config, so there is no separate override to apply here.
func summarizer(client llm.Client) func(context.Context, string) (string, error) {
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
		return nil, "", newNoProviderError()
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

// errNoProvider identifies the "no provider" failure, so callers and tests can
// recognise it regardless of the language it is rendered in.
var errNoProvider = errors.New("built-in agent has no model provider")

// noProviderError is the user-facing "no provider" failure.
//
// It is its own type rather than fmt.Errorf("%w: %s", …) on purpose: a %w wrap
// makes the sentinel's own wording part of Error(), so users saw
// "built-in agent has no model provider: 内置智能体还没配模型提供商…" — English
// internal vocabulary in front of the sentence they need to read. Here Error()
// returns ONLY the localized text, and Is() carries the identity for errors.Is.
type noProviderError struct{ text string }

func (e *noProviderError) Error() string { return e.text }

func (e *noProviderError) Is(target error) bool { return target == errNoProvider }

// newNoProviderError renders the failure in the current UI language.
//
// Rendered per call rather than stored: the language can change while the
// server runs, and a package-level string would freeze whichever language was
// set at startup.
func newNoProviderError() error {
	locale := model.Language
	if locale == "" {
		locale = model.DefaultLanguage
	}
	return &noProviderError{text: i18n.T(i18n.LocalizerForLocale(locale), "BuiltinNoProvider")}
}

// forward translates one agentcore event into ai.StreamEvent.
//
// It never reports failure: a consumer that stops reading (the emit timeout)
// is logged and the remaining events are still delivered, because the producer
// must keep draining its own stream until it closes.
func forward(ch chan<- ai.StreamEvent, tr *translator, ev agentcore.Event) {
	for _, out := range tr.translate(ev) {
		select {
		case ch <- out:
		case <-time.After(emitTimeout):
			slog.Warn("builtin: consumer stopped reading, dropping event", "kind", ev.Kind)
			return
		}
	}
}
