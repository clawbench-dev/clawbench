package service

import (
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// chat_request.go holds the ONE implementation of "turn a user message into an
// ai.ChatRequest", plus the fork-context builder it depends on.
//
// There used to be two: one in the HTTP handler (used by direct sends) and one
// here (used by the queue and push paths). They drifted in five ways, and every
// drift made a queued message behave differently from an identical direct send:
//
//  1. resume guard — the handler refused to resume a session whose only
//     assistant message was an empty interrupted placeholder; the queue path
//     resumed it anyway, handing the CLI an ID it had never seen (context
//     amnesia).
//  2. persisted model — the handler honored the session's saved model; the
//     queue path ignored it and fell back to the agent default.
//  3. persisted transport — same split for the session's CLI/ACP override.
//  4. fork envelope — the handler wrapped injected history in an explanatory
//     envelope with "User:"/"Assistant:" roles; the queue path emitted bare
//     "user:"/"assistant:" lines with no envelope.
//  5. plain-text history — the handler recovered non-block content via
//     ExtractPlainText; the queue path skipped it, silently dropping history.
//
// Keeping one implementation is what makes those five fixes stick: there is no
// second copy left to forget.

// BuildChatRequest constructs an ai.ChatRequest for one user turn.
//
// The override arguments come from the request (a frontend model/mode pick);
// when empty, the session's persisted choice is used before falling back to the
// agent's default. That ordering is why a queued message now honors the model
// the user selected in the session, instead of silently reverting to the
// agent default.
func BuildChatRequest(prompt, sessionID, projectPath, backendName, agentID, modelOverride, thinkingEffortOverride, modeOverride, transportOverride, fileDir string, hasAttachments bool) ai.ChatRequest {
	agentID = ResolveAgentID(sessionID, agentID)

	// An explicit per-request pick always describes the model being sent right
	// now, so it carries no model binding. Only an INHERITED pick (read from
	// context_state) remembers which model it was made under.
	explicitThinkingEffort := thinkingEffortOverride != ""

	modelOverride, transportOverride, thinkingEffortOverride, modeOverride = applySessionOverrides(sessionID, modelOverride, transportOverride, thinkingEffortOverride, modeOverride)

	thinkingEffortModel := ""
	if !explicitThinkingEffort && thinkingEffortOverride != "" {
		if _, m, ok := UserThinkingEffortPick(sessionID); ok {
			thinkingEffortModel = m
		}
	}

	effectiveThinkingEffort := thinkingEffortOverride // Explicit pick > session > agent default
	effectiveMode := modeOverride                     // Explicit pick > session > agent default

	// Resolved before resolveAgentConfig because the model fallback depends on
	// whether this turn goes over ACP (see resolveAgentConfig). applySessionOverrides
	// above has already finalized transportOverride, so the effective transport is
	// known here.
	isACP := resolveIsACP(agentID, transportOverride)

	systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode := resolveAgentConfig(agentID, projectPath, modelOverride, effectiveThinkingEffort, thinkingEffortModel, effectiveMode, isACP)

	// Resolve effective session ID for CLI.
	// All backends store their CLI-identifiable session ID in external_session_id:
	//   - codebuddy/claude/qoder: ClawBench UUID (same as session id)
	//   - opencode/codex/deepseek/pi: CLI-assigned ID (captured from stream events)
	// When resuming, we always use external_session_id so the CLI can find its session context.
	//
	// EXCEPTION: ACP-backed agents manage their own session mapping internally
	// via ACPConnectionPool (clawbench UUID → ACP session ID). For ACP agents,
	// always use the ClawBench UUID as the session ID — the pool handles the rest.
	resumeStart := time.Now()
	resume := SessionHasAssistant(sessionID)
	slog.Info("acp perf: buildChatRequest.SessionHasAssistant", "session_id", sessionID, "resume", resume, "elapsed", time.Since(resumeStart))

	// Resolve external_session_id for fork detection (used by both CLI resume and fork context injection).
	var resolvedExtID string
	if resume {
		extStart := time.Now()
		resolvedExtID = GetExternalSessionID(sessionID)
		slog.Info("acp perf: buildChatRequest.GetExternalSessionID", "session_id", sessionID, "ext_id", resolvedExtID, "elapsed", time.Since(extStart))
	}

	effectiveSessionID, resume := resolveResumeSessionID(sessionID, backendName, agentID, resume, isACP, resolvedExtID)

	// Detect fork session first message: resume=true (has copied assistant messages)
	// but no external_session_id (AI side has no context). This happens after
	// ForkSession which copies messages in DB but doesn't inherit the AI-side session.
	// Inject formatted history so the AI can continue with context.
	//
	// Note: This branch only fires when resume is still true after the above
	// checks. If the first message was interrupted (resume set to false above),
	// this fork detection is skipped — correct, because a phantom-cancel session
	// is not a fork.
	//
	// Guard against re-injection on subsequent messages:
	// After the first AI response, session_capture persists external_session_id,
	// so resolvedExtID != "" and the above resume branch uses it directly,
	// bypassing this fork detection.
	forkContext, forkResume := resolveForkInjection(sessionID, backendName, agentID, resume, isACP, resolvedExtID)
	if forkContext != "" {
		resume = forkResume
	}

	// Inject media handling rules only when the user message carries attachments.
	// These rules are omitted for text-only messages to save tokens.
	if hasAttachments {
		systemPrompt = appendMediaPrompt(systemPrompt)
	}

	// Compaction re-injection: a session whose context was just compacted (by the
	// agent, or by the user sending /compact) must get the system prompt back on
	// its next turn — the summary may have dropped it, and the periodic interval
	// defaults to "never".
	//
	// The flag is CONSUMED (read-and-clear) so it affects exactly one turn, and
	// it is deliberately NOT consumed by the /compact command turn itself: that
	// turn is the command, not a model call, and the compaction it triggers
	// happens after it. Consuming it there would leave the following real turn
	// without the re-injection.
	compacted := false
	if !ai.IsCompactCommand(prompt) {
		compacted = ConsumeSessionCompacted(sessionID)
	}

	// HasConversationHistory: conservative on error (true = has history) so a
	// DB hiccup can never silently reset the session via amnesia prevention.
	hasConversationHistory := true
	if count, err := GetChatMessageCount(sessionID); err == nil {
		hasConversationHistory = count > 0
	} else {
		slog.Warn("BuildChatRequest: GetChatMessageCount failed, assuming conversation history", "session_id", sessionID, "err", err)
	}

	// Auto-cruise: when the session has the switch on, prepend the cruise
	// addendum to the PROMPT. It rides the prompt (not the system prompt)
	// because a CLI resume does not necessarily re-send the system prompt (see
	// ChatRequest.ShouldInjectSystemPrompt) — the prompt is what reaches the
	// model on every turn. Prompt is transient (the persisted user row is
	// req.Message), so this never leaks into the user bubble or history.
	//
	// Slash commands are EXEMPT: ACP agents detect their commands by the leading
	// "/" (see ai.IsACPSlashCommand / acp_conn_state.go), so prepending anything
	// would send non-slash text and the agent would route it to the LLM instead
	// of its CommandExecutor — the command silently does nothing. The addendum
	// reaches the NEXT ordinary turn anyway (cruise is session-scoped), so
	// skipping it for the command turn loses nothing.
	if GetSessionCruise(sessionID) && !ai.IsACPSlashCommand(prompt) {
		prompt = BuildCruiseSingleAddendum() + "\n\n" + prompt
	}

	return ai.ChatRequest{
		Prompt:                 prompt,
		SessionID:              effectiveSessionID,
		WorkDir:                fileDir,
		SystemPrompt:           systemPrompt,
		Model:                  agentModel,
		Command:                agentCommand,
		AgentID:                agentID,
		ThinkingEffort:         effectiveThinkingEffort,
		Mode:                   effectiveMode,
		Resume:                 resume,
		HasAttachments:         hasAttachments,
		AssistantMessageCount:  GetAssistantMessageCount(sessionID),
		HasConversationHistory: hasConversationHistory,
		ForkContext:            forkContext,
		Compacted:              compacted,
	}
}

// applySessionOverrides fills each empty override from the session's persisted
// choice, so every caller (direct send, queue drain, push, scheduler) behaves
// identically. The queue path historically passed empty overrides and lost the
// user's selection.
//
// Sources differ by field:
//   - model / transport have their own chat_sessions columns;
//   - mode / thinking effort have NO column — they live in
//     chat_sessions.context_state (written by PATCH /api/ai/session/update).
//     Without reading context_state a drained turn would silently revert to the
//     agent default, and for mode there is no agent-level session memory at all
//     (agent.EffectiveModeID() returns only PreferredMode, not the session's
//     current mode).
//
// Thinking effort is special (issue #552): context_state also receives the
// value the AGENT reports for itself, which must NOT be replayed as if the user
// chose it. Only an explicit pick (UserThinkingEffortPick) is inherited here;
// the agent-reported value stays display-only and the per-model/global default
// decides the next turn.
//
// Explicit overrides always win; this only fills the blanks.
func applySessionOverrides(sessionID, modelOverride, transportOverride, thinkingEffortOverride, modeOverride string) (string, string, string, string) {
	if modelOverride == "" {
		modelOverride = GetSessionModel(sessionID)
	}
	if transportOverride == "" {
		transportOverride = GetSessionTransport(sessionID)
	}
	if thinkingEffortOverride == "" {
		// The pick carries the model it was made under; resolveAgentConfig
		// discards it when the turn runs a different model (the pick then
		// belongs to another model and its level may not even be offered).
		thinkingEffortOverride, _, _ = UserThinkingEffortPick(sessionID)
	}
	if modeOverride == "" {
		if cs := GetContextState(sessionID); cs != nil && cs.Mode != nil {
			modeOverride = cs.Mode.CurrentModeID
		}
	}
	return modelOverride, transportOverride, thinkingEffortOverride, modeOverride
}

// resolveAgentConfig looks up the agent and derives the prompt, model, command,
// thinking effort and mode from it. Overrides win over the agent's defaults;
// an unknown agent id yields empty fields rather than an error, so a stale
// agent reference degrades to a plain turn instead of failing the send.
//
// isACP suppresses ONLY the agent-default model fallback. ACP has no concept of
// a client-chosen default: the agent reports its own currentValue at session/new
// and the client should leave it alone until it knows which models the agent can
// run. Before that first handshake, agent.Models holds the CLI-discovered list,
// which is a guess — sending one as session/set_config_option can select a model
// the agent cannot run. An EXPLICIT choice (modelOverride) is always honored:
// that is the user's intent, not a guess.
//
// thinkingEffortModel is the model the inherited effort pick was made under
// (empty for an explicit per-request override, which is always about the model
// being sent right now). When it names a DIFFERENT model, the pick is dropped:
// it belongs to the previous model and its level may not even be offered by the
// one now running (OpenCode rejects it with -32602).
func resolveAgentConfig(agentID, projectPath, modelOverride, thinkingEffort, thinkingEffortModel, mode string, isACP bool) (systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode string) {
	effectiveThinkingEffort = thinkingEffort
	effectiveMode = mode

	agent := model.GetAgent(agentID)
	if agent == nil {
		return "", "", "", effectiveThinkingEffort, effectiveMode
	}

	systemPrompt = agent.RuntimeSystemPrompt
	// Append the cross-agent skill table. It is added here rather than baked
	// into RuntimeSystemPrompt (composed once at load time) so a skill installed
	// after startup — or a completed git sync — shows up on the next turn.
	systemPrompt = AppendSkillsSection(systemPrompt, agentID, projectPath)
	// Replace {{PROJECT_PATH}} per-request with the actual project path from cookie
	if projectPath != "" {
		systemPrompt = strings.ReplaceAll(systemPrompt, "{{PROJECT_PATH}}", projectPath)
	}
	if modelOverride != "" {
		agentModel = modelOverride
	} else if !isACP || ai.HasConfirmedACPModels(agentID) {
		// No explicit choice. Fall back to the agent default — unless this is an
		// ACP turn whose agent has not reported its models yet, in which case
		// the default would come from the CLI list and is a guess. Leaving the
		// model empty makes applyModelSelection skip its RPC entirely, so the
		// agent keeps its own default.
		if defaultID := agent.DefaultModelID(); defaultID != "" {
			agentModel = defaultID
		}
	}
	if agent.Command != "" {
		agentCommand = agent.Command
	}
	// A pick made under a different model does not apply to this turn.
	if effectiveThinkingEffort != "" && thinkingEffortModel != "" && thinkingEffortModel != agentModel {
		effectiveThinkingEffort = ""
	}
	// Fall back to the effort remembered for THIS model (issue #552), then to
	// the agent's model-independent default. Previously only the latter was
	// consulted, so a user's per-model choice was forgotten on every new
	// session and the agent's own "low" default won.
	//
	// The GLOBAL default is checked against the levels this agent/model is known
	// to offer. A per-model pick is trusted as-is: it was validated when the
	// user made it. The global one is not model-aware — a user whose Settings
	// default is "low" (valid for glm-5.3) who switches to a model that only
	// offers high/max/default would otherwise have "low" re-sent on EVERY turn,
	// rejected with -32602 each time while the chip kept showing it.
	if effectiveThinkingEffort == "" {
		effectiveThinkingEffort = EffectiveThinkingEffortForModel(agent, agentModel, isACP)
	}
	// Fall back to agent's preferred mode when nothing was specified
	if effectiveMode == "" && agent.EffectiveModeID() != "" {
		effectiveMode = agent.EffectiveModeID()
	}
	return systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode
}

// EffectiveThinkingEffortForModel resolves the effort a turn should run, for the
// model and transport it will actually use.
//
// Priority (issue #552):
//  1. the user's pick for THIS model — trusted as-is (it was validated when the
//     user made it, against this model's levels);
//  2. the model-independent default from Settings, but ONLY if the level is one
//     this agent/model is known to offer;
//  3. empty — let the agent keep its own default.
//
// Step 2's filter is the point of this function. The global default is not
// model-aware: a user whose Settings default is "low" (valid for glm-5.3) who
// switches to a model that only offers high/max/default would otherwise have
// "low" re-sent on EVERY turn, rejected with -32602 each time while the UI kept
// showing it — the issue's symptom, one layer down.
//
// An UNKNOWN level list counts as "offered": not having been told must never
// discard the user's configured default. For ACP the list arrives at session/new
// and is refreshed from the set_config_option response (which carries the NEW
// model's levels), so by the time a switch has happened the list is the right
// one.
func EffectiveThinkingEffortForModel(agent *model.Agent, modelID string, isACP bool) string {
	if agent == nil {
		return ""
	}
	if modelID != "" {
		if v := agent.ThinkingEffortByModel[modelID]; v != "" {
			return v
		}
	}
	global := agent.EffectiveThinkingEffort()
	if global == "" {
		return ""
	}
	if !ThinkingEffortIsOffered(agent, isACP, global) {
		slog.Info("configured thinking effort not offered by this agent/model, letting the agent decide",
			"agent_id", agent.ID, "model", modelID, "level", global)
		return ""
	}
	return global
}

// ThinkingEffortIsOffered reports whether level is known to be runnable by this
// agent over the given transport. An unknown list returns true.
func ThinkingEffortIsOffered(agent *model.Agent, isACP bool, level string) bool {
	if level == "" {
		return false
	}
	if isACP {
		if es := ai.GetAgentCapabilityRegistry().GetThinkingEffortState(agent.ID, ""); es != nil && len(es.AvailableLevels) > 0 {
			for _, l := range es.AvailableLevels {
				if l.ID == level {
					return true
				}
			}
			return false
		}
		return true
	}
	if len(agent.ThinkingEffortLevels) == 0 {
		return true
	}
	for _, l := range agent.ThinkingEffortLevels {
		if l == level {
			return true
		}
	}
	return false
}

// resolveIsACP decides whether this turn goes over ACP. An explicit transport
// override is authoritative; otherwise the agent's configured transport wins.
func resolveIsACP(agentID, transportOverride string) bool {
	if transportOverride != "" {
		return transportOverride == transportACPStdio
	}
	agent := model.GetAgent(agentID)
	if agent == nil {
		return false
	}
	return agent.Transport == transportACPStdio
}

// resolveResumeSessionID picks the session id to hand the CLI and the final
// resume flag.
//
// A returned session id of "" means the CLI cannot be given one. The resume flag
// distinguishes the two reasons, and the difference is deliberate:
//
//   - The first assistant message was an empty cancel/warning placeholder, so no
//     CLI-side session was ever created: resume becomes false, because the turn
//     must start a completely fresh session (no --resume, proper system prompt
//     injection).
//   - The session does have real content but the CLI has no external session id:
//     the id is cleared so the CLI does not receive one it never saw, but resume
//     stays TRUE so the fork-context path below still injects the history
//     (context amnesia is mitigated, not accepted).
//
// ACP turns always use the ClawBench UUID — the ACP pool owns the mapping — and
// keep resume as-is.
func resolveResumeSessionID(sessionID, backendName, agentID string, resume, isACP bool, resolvedExtID string) (string, bool) {
	if !resume {
		slog.Info("session: new conversation (no resume)",
			slog.String("session", sessionID),
			slog.String("backend", backendName),
			slog.String("agent", agentID))
		return sessionID, false
	}

	if isACP {
		return sessionID, true
	}

	if resolvedExtID != "" {
		slog.Info("session resume: resolved external_session_id",
			slog.String("session", sessionID),
			slog.String("external_session_id", resolvedExtID),
			slog.String("backend", backendName),
			slog.String("agent", agentID),
			slog.Bool("ext_id_is_clawbench_uuid", resolvedExtID == sessionID))
		return resolvedExtID, true
	}

	if !SessionHasRealAssistantContent(sessionID) {
		slog.Info("session resume: external_session_id is empty and no real AI content (first message interrupted), starting fresh",
			slog.String("session", sessionID),
			slog.String("backend", backendName),
			slog.String("agent", agentID))
		return "", false
	}

	// No external session ID available — the CLI cannot resume a session it has
	// never seen. Clear the id so the backend does not pass an invalid one to
	// --resume; resume stays true so the fork-context injection still runs. Log a
	// warning for diagnosis.
	slog.Warn("session resume: external_session_id is empty, CLI will start a new session (context amnesia)",
		slog.String("session", sessionID),
		slog.String("backend", backendName),
		slog.String("agent", agentID))
	return "", true
}

// resolveForkInjection decides whether this turn must carry injected history, and
// what the resume flag becomes.
//
// Two independent sources:
//
//  1. The LEGACY copied-rows fork: resume is still true (the copied assistant
//     messages are in the DB) but there is no external_session_id, so the CLI has
//     no AI-side context. Its history is rendered on the fly from the copied rows.
//  2. A stored BACKGROUND: a fork that copied no rows (any fork touching a group)
//     left its source timeline in context_state.fork_context. That text is read
//     until the session produces its first successful turn.
//
// The returned resume replaces the caller's value when history was injected. For
// ACP it becomes false: external_session_id is empty, so the pool has no existing
// connection — NewSession (not ResumeSession with an invalid id) is correct, and
// the injected text supplies the context.
func resolveForkInjection(sessionID, backendName, agentID string, resume, isACP bool, resolvedExtID string) (text string, newResume bool) {
	if resume && resolvedExtID == "" {
		if ctx := BuildForkContext(sessionID); ctx != "" {
			slog.Info("fork session: injecting context history",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("is_acp", isACP))
			if isACP {
				resume = false
			}
			return ctx, resume
		}
	}
	if pending, ok := storedForkBackground(sessionID); ok {
		if isACP {
			resume = false
		}
		return pending, resume
	}
	return "", resume
}

// storedForkBackground returns the pending fork background for a session.
//
// The gate is "this session has not yet produced a successful turn". It must NOT
// be an assistant-count check: a failed turn writes an assistant WARNING row
// (persistTurnWarning), so counting would expire the background after a single
// failure. The counterpart is ClearForkContext, called after a clean turn — that
// write is what actually marks the background as consumed.
//
// The (usually absent) json_extract runs FIRST so an ordinary session pays one
// cheap lookup instead of a chat_history scan on every turn.
func storedForkBackground(sessionID string) (string, bool) {
	pending := GetForkContext(sessionID)
	if pending == "" || SessionHasRealAssistantContent(sessionID) {
		return "", false
	}
	return pending, true
}

// BuildForkContext reads a session's history and formats it as a text block that
// can be prepended to the user's prompt, giving the AI context from the parent
// session when a forked session sends its first message.
//
// This is the queued-message / task-engine path, so it must render history the
// same way a direct send does: the explicit envelope tells the model what it is
// looking at, and the capitalised role names are what the model expects. The
// rendering, priority selection and budget enforcement live in fork_context.go
// so this path cannot drift from the handler path again.
func BuildForkContext(sessionID string) string {
	return BuildForkContextWithOptions(sessionID, ForkContextOptions{
		Header:            "[Below is the conversation history from before this session. Continue based on this context.]\n\n",
		Footer:            "[End of conversation history. Now answer the user's new question.]\n\n",
		CapitalizeRoles:   true,
		PlainTextFallback: true,
		BudgetChars:       model.ChatForkContextBudget,
	})
}

// extractMessageParts renders a message's blocks for fork-context injection:
// text verbatim, tool_use as structured JSON, and thinking/warning/error skipped.
// Empty text blocks are dropped so a message with no renderable content does not
// emit a bare role label.
func extractMessageParts(blocks []model.ContentBlock, toolCallMap map[string]*ToolCallRecord) []string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case contentKeyText:
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case eventTypeToolUse:
			tcJSON := FormatToolUseBlock(b, toolCallMap)
			if tcJSON != "" {
				parts = append(parts, tcJSON)
			}
			// thinking, warning, error: skipped
		}
	}
	return parts
}

// appendMediaPrompt appends the media handling rules to a system prompt when
// there is one, or returns them alone when there is not.
func appendMediaPrompt(systemPrompt string) string {
	mediaPrompt := model.BuildMediaPrompt()
	if mediaPrompt == "" {
		return systemPrompt
	}
	if systemPrompt != "" {
		return systemPrompt + "\n\n" + mediaPrompt
	}
	return mediaPrompt
}
