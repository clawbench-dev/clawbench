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
	effectiveThinkingEffort := thinkingEffortOverride // Explicit pick takes priority
	effectiveMode := modeOverride                     // Explicit pick takes priority

	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}

	// Session-persisted choices fill in when the caller did not specify one.
	// Read once here so every caller (direct send, queue drain, push) behaves
	// identically — the queue path previously passed empty overrides and lost
	// the user's model/transport selection.
	sessionModel := GetSessionModel(sessionID)
	sessionTransport := GetSessionTransport(sessionID)

	if modelOverride == "" {
		modelOverride = sessionModel
	}
	if transportOverride == "" {
		transportOverride = sessionTransport
	}

	systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode := resolveAgentConfig(agentID, projectPath, modelOverride, effectiveThinkingEffort, effectiveMode)

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

	isACP := resolveIsACP(agentID, transportOverride)

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
	var forkContext string
	if resume && resolvedExtID == "" {
		forkContext = BuildForkContext(sessionID)
		if forkContext != "" {
			slog.Info("fork session: injecting context history",
				slog.String("session", sessionID),
				slog.String("backend", backendName),
				slog.String("agent", agentID),
				slog.Bool("is_acp", isACP))

			// For ACP sessions: external_session_id is empty, so the ACP pool
			// has no existing connection for this session. Setting Resume=false
			// ensures the ACP backend calls NewSession (not ResumeSession with
			// an invalid ID). The fork context in the prompt provides the
			// necessary history, so a new session is the correct approach.
			if isACP {
				resume = false
			}
		}
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

// resolveAgentConfig looks up the agent and derives the prompt, model, command,
// thinking effort and mode from it. Overrides win over the agent's defaults;
// an unknown agent id yields empty fields rather than an error, so a stale
// agent reference degrades to a plain turn instead of failing the send.
func resolveAgentConfig(agentID, projectPath, modelOverride, thinkingEffort, mode string) (systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode string) {
	effectiveThinkingEffort = thinkingEffort
	effectiveMode = mode

	agent, ok := model.Agents[agentID]
	if !ok {
		return "", "", "", effectiveThinkingEffort, effectiveMode
	}

	systemPrompt = agent.RuntimeSystemPrompt
	// Replace {{PROJECT_PATH}} per-request with the actual project path from cookie
	if projectPath != "" {
		systemPrompt = strings.ReplaceAll(systemPrompt, "{{PROJECT_PATH}}", projectPath)
	}
	if modelOverride != "" {
		agentModel = modelOverride
	} else if defaultID := agent.DefaultModelID(); defaultID != "" {
		agentModel = defaultID
	}
	if agent.Command != "" {
		agentCommand = agent.Command
	}
	// Fall back to agent's effective thinking effort when nothing was specified
	if effectiveThinkingEffort == "" && agent.EffectiveThinkingEffort() != "" {
		effectiveThinkingEffort = agent.EffectiveThinkingEffort()
	}
	// Fall back to agent's preferred mode when nothing was specified
	if effectiveMode == "" && agent.EffectiveModeID() != "" {
		effectiveMode = agent.EffectiveModeID()
	}
	return systemPrompt, agentModel, agentCommand, effectiveThinkingEffort, effectiveMode
}

// resolveIsACP decides whether this turn goes over ACP. An explicit transport
// override is authoritative; otherwise the agent's configured transport wins.
func resolveIsACP(agentID, transportOverride string) bool {
	if transportOverride != "" {
		return transportOverride == transportACPStdio
	}
	agent, ok := model.Agents[agentID]
	if !ok {
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
