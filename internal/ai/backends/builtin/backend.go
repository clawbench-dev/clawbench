// Package builtin registers the built-in agent backend.
//
// Unlike every other backend, the tools of this one live in ClawBench itself
// (internal/agentcore) rather than in an external CLI. That difference drives
// every choice here:
//
//   - no spawn command, no install command, no login; MCP is attached by
//     ClawBench's own client (internal/mcp), not injected into a CLI;
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
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/mcptools"
	"clawbench/internal/agentcore/tools"
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/i18n"
	"clawbench/internal/mcp"
	"clawbench/internal/model"
)

// BackendID is the backend/agent id. It is also the BackendSpec.ID, so a fresh
// install auto-inserts an agent with this id (see model.RefreshAgents).
const BackendID = "builtin"

func init() {
	backends.Register(&backends.BackendPlugin{
		ID:             BackendID,
		Implementation: &backends.InProcessPlugin{NewBackend: newBackend},
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
			// No AcpCommand: this backend never spawns a process. Its transport
			// is "in-process", and SupportsACP() stays false because
			// acp_command is empty.
			//
			// AutoLoadsNativeSkills must be false: this agent has no skill
			// directory of its own, so ClawBench must inject the shared table
			// (or nothing, depending on config) rather than assume the backend
			// loads it. Getting this wrong in the true direction would leave
			// the agent with no skills at all.
			AutoLoadsNativeSkills: false,
			// Legacy SSE is not implemented by our client: it needs a
			// server-to-client event stream and a separate message endpoint.
			// Declaring it here disables the option in the settings page instead
			// of letting a user pick a transport that will fail at connect time.
			MCPRejectsSSE: true,
			SortOrder:     0,
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
		// The turn loop is ours, so a message can join it at the next model
		// request and we know exactly where it entered — the reply splits, just
		// like CodeBuddy's session/steer.
		MidTurnInProcess:   midTurn{},
		MidTurnSplitsReply: true,
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

// modelListStateForAgent returns the model list this agent can run, in the same
// shape an ACP agent reports when it answers session/new.
//
// The built-in agent has NO handshake, so this is the only model list anyone
// can get for it: every consumer (the chat drawer, GET /api/agents, the ACP
// gate in internal/handler via ai.HasConfirmedACPModels, the selftest) reads it
// through ai.AgentCapabilityRegistry.GetModelListState, which asks the backend
// this question.
//
// The source is the bound provider's curated list — the ids the endpoint is
// known to serve. An unbound agent (or a provider with no curated models) has
// nothing trustworthy to offer, because whatever it runs is inherited from the
// user's endpoint configuration rather than chosen. CurrentModelID is the model
// a turn will actually run (model.Agent.DefaultModelID answers exactly as the
// turn does), so the picker marks the right row and the client does not fall
// back to "Default".
func modelListStateForAgent(agent *model.Agent) *ai.ModelListState {
	if agent == nil {
		return nil
	}
	current := scopedCurrentModel(agent)
	models := model.WithPrices(agent.SelectableProviderModels())
	if len(models) == 0 {
		// No provider qualifies (unbound, or nothing curated) — the old
		// single-provider list is deliberately NOT a fallback: a provider that
		// did not qualify here is one this backend cannot call, so listing its
		// models would offer choices that fail on the first turn.
		//
		// What is left is the "custom…" escape hatch: a provider with no curated
		// list can still have an explicit pick. Echo that single model back — it
		// is what the next turn sends, so the chip must name it instead of
		// "Default", and withholding the list would leave the drawer empty for
		// the one model the user deliberately chose.
		if current == "" || agent.Provider() == nil {
			return nil
		}
		models = model.WithPrices([]model.AgentModel{{
			ID:      current,
			Name:    model.ProviderModelName(current),
			Default: true,
		}})
	}
	out := make([]model.AgentModel, len(models))
	copy(out, models)
	for i := range out {
		out[i].Default = out[i].ID == current
	}
	if current != "" && !hasModelID(out, current) {
		// A bare pick (an agent default written before the picker scoped ids, or
		// a hand-typed one) may name the same model as a scoped entry. Adopt that
		// entry instead of prepending a duplicate the turn cannot run — a bare id
		// resolves to the agent's binding, which may not even exist.
		//
		// Only an UNAMBIGUOUS match counts: two providers serving the same name
		// means the bare pick cannot say which one was meant, and guessing would
		// silently switch providers. That case keeps the old prepend.
		bare := model.ProviderModelName(current)
		adoptedIdx := -1
		ambiguous := false
		for i := range out {
			if model.ProviderModelName(out[i].ID) == bare {
				if adoptedIdx >= 0 {
					ambiguous = true
					break
				}
				adoptedIdx = i
			}
		}
		if adoptedIdx >= 0 && !ambiguous {
			current = out[adoptedIdx].ID
			for i := range out {
				out[i].Default = out[i].ID == current
			}
		} else {
			out = append([]model.AgentModel{{
				ID: current, Name: model.ProviderModelName(current), Default: true,
			}}, out...)
		}
	}
	return &ai.ModelListState{CurrentModelID: current, Models: out}
}

// scopedCurrentModel renders the model a turn would run in the same form the
// picker entries use (provider-scoped). Without this the "default" flag never
// matched: DefaultModelID answers with the bound provider's bare id while every
// row now carries its provider prefix.
func scopedCurrentModel(agent *model.Agent) string {
	current := agent.DefaultModelID()
	if current == "" {
		return ""
	}
	if _, _, ok := model.ParseProviderScopedModelID(current); ok {
		return current
	}
	if p := agent.Provider(); p != nil && p.ID > 0 {
		return model.ProviderScopedModelID(p.ID, current)
	}
	return current
}

func hasModelID(models []model.AgentModel, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Backend implements ai.AIBackend by driving agentcore in-process.
//
// It is stateless between turns on purpose: the conversation lives in
// chat_history, which the service layer reads and hands over as History. A
// cached transcript here would be a second source of truth that drifts the
// moment the user edits or deletes a message.
type Backend struct{}

// ModelListState reports the model list of one built-in agent.
//
// The backend is created per run (see ai.ModelListProvider) and is stateless,
// and several built-in agents (the auto-inserted one plus any copies) share it,
// so the agent has to be named. An id this backend does not serve yields nil
// rather than some other agent's list.
func (b *Backend) ModelListState(agentID string) *ai.ModelListState {
	agent := model.GetAgent(agentID)
	if agent == nil || !agent.BuiltinBackend || agent.Backend != BackendID {
		return nil
	}
	state := modelListStateForAgent(agent)
	if state == nil {
		return nil
	}
	state.AgentID = agent.ID
	return state
}

// newBackend constructs the in-process backend.
func newBackend() ai.AIBackend {
	return &Backend{}
}

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

	// Register the join target BEFORE the turn starts, so a message queued while
	// the previous turn was still running can be inserted the moment this one is
	// alive. The mailbox is sealed by the engine on its way out; cleanup here
	// only removes the registry entry.
	//
	// Keyed by the ClawBench session id, NOT req.SessionID: on a resumed turn of
	// a self-hosted backend SessionID is blanked (there is no external id to
	// resume), while the inject endpoint addresses the turn by the id the user's
	// conversation actually has. Registering under SessionID left the registry
	// empty from the second turn on, so every insertion declined as "idle".
	var releaseInjections func()
	if req.ClawbenchSessionID != "" {
		box := agentcore.NewInjectionMailbox()
		spec.Injections = box
		releaseInjections = registerInjectionBox(req.ClawbenchSessionID, box)
	}

	ch := make(chan ai.StreamEvent, 64)
	go func() {
		defer close(ch)
		if releaseInjections != nil {
			defer releaseInjections()
		}
		// One translator per turn: it tracks whether a reasoning block is
		// still open, so the closing event is emitted exactly once.
		tr := &translator{locale: model.Language}
		// MCP tools are attached per turn. The pool keeps the connections, so
		// after the first turn this is a policy lookup rather than a process
		// start, and a policy change lands on the next turn.
		toolList, notes, release := attachMCP(ctx, req, spec.Client)
		if release != nil {
			defer release()
		}
		spec.ExtraTools = toolList
		spec.ToolNotes = append(spec.ToolNotes, notes...)

		runEvents := agentcore.Run(ctx, spec)
		for ev := range runEvents {
			forward(ch, tr, ev)
		}
	}()
	return ch, nil
}

// attachMCP resolves this agent's MCP policy and returns the tools it may use.
//
// It never fails the turn: a server that cannot start becomes a note the model
// can relay. Failing the whole turn because one of five servers is
// misconfigured would make an unrelated typo look like the agent is broken.
//
// Only http is advertised as a capability. Our client does not implement legacy
// SSE, and ResolveMCPServers drops transports the backend cannot take — the
// same gate ACP backends use for servers they cannot accept.
func attachMCP(ctx context.Context, req ai.ChatRequest, client llm.Client) ([]tools.Tool, []string, func()) {
	servers := model.ResolveMCPServers(req.AgentID, model.MCPCapabilities{HTTP: true})
	if len(servers) == 0 {
		return nil, nil, nil
	}

	// The working directory is part of the key: a stdio server is started in
	// it, so two projects must not share one process. Cleaning it keeps a
	// trailing slash from creating a second key for the same directory.
	workDir := req.WorkDir
	if workDir != "" {
		workDir = filepath.Clean(workDir)
	}
	key := req.AgentID + "\x00" + workDir
	bundle := mcp.DefaultPool().Acquire(ctx, key, servers, mcp.DialOptions{WorkDir: workDir})

	toolList, notes := mcptools.Build(mcptools.FromBundle(bundle), client.SupportsVision())
	notes = append(append([]string(nil), bundle.Notes...), notes...)
	return toolList, notes, bundle.Release
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

	// This turn's pictures. The PREFERRED form is a pointer, not the bytes: the
	// agent's read tool returns an image, so the model opens the file when it
	// actually needs to look, and the path stays in the transcript as the handle
	// for later turns. Bytes are sent only when the agent cannot open the file
	// itself (no read capability, or a path outside the confined working
	// directory) — a path nobody can open is a picture nobody sees.
	//
	// History never carries images either way: re-sending every earlier turn's
	// picture made each turn pay again for what the model had already described.
	prompt := req.Prompt
	var promptParts []llm.Part
	policy := agent.ToolPolicy.Normalized().ForMode(req.Mode)
	if !client.SupportsVision() && len(req.Files) > 0 {
		// Worth a line: the user attached a picture and this model will only see
		// its name. Without it "the agent ignored my screenshot" has no trace at
		// all, because nothing fails — the turn just answers about a path.
		slog.Info("builtin: model cannot read images, attachments are only named",
			"agent", req.AgentID, "model", modelID, "attachments", len(req.Files))
	}
	if client.SupportsVision() {
		hints, fallback := readableImageHints(req.Files, req.WorkDir, policy.Has(model.CapFSRead), policy.ConfinesToWorkDir())
		for _, h := range hints {
			prompt += "\n" + imageHint(h)
		}
		if len(fallback) > 0 {
			parts, notes := turnImageParts(fallback)
			promptParts = parts
			for _, n := range notes {
				prompt += "\n" + n
			}
		}
	}

	return agentcore.Spec{
		Client:       client,
		SystemPrompt: req.SystemPrompt,
		Prompt:       prompt,
		PromptParts:  promptParts,
		History:      replay.Messages,
		// Parallel to History: the boundary a compaction reports is an id from
		// here, so the caller records exactly which stored turn the summary
		// stands for.
		HistoryMessageIDs: replay.IDs,
		SessionID:         req.SessionID,
		WorkDir:           req.WorkDir,
		// The session mode narrows the agent's grant; it never widens it. An
		// empty mode (background task, older client) leaves the grant as-is.
		Policy:         policy,
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
// The agent's own provider binding wins, UNLESS the model id names a provider
// (the picker offers models from every configured provider, see
// model.SelectableProviderModels). A scoped id is the user's explicit pick, so
// the turn follows it: the endpoint, credential and wire protocol all come from
// that provider rather than from the agent's binding.
//
// When the agent has no binding, the shared summary model is used and the UI is
// told which model is running — a silent fallback to "some model" is how a user
// ends up debugging answers produced by an endpoint they never configured.
func buildClient(agent *model.Agent, modelOverride string) (llm.Client, string, error) {
	modelID := modelOverride
	if modelID == "" {
		modelID = agent.DefaultModelID()
	}

	// A scoped id that names a provider which no longer exists falls back to the
	// binding: the row was deleted between the pick and the turn, and running
	// the bound provider's default is better than refusing the whole turn.
	provider, modelID := agent.ResolveProviderForModel(modelID)
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
// recognize it regardless of the language it is rendered in.
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
