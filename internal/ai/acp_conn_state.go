package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACPConn state management — cache and emit session state
// Moved from ACPBackend (feature envy: these methods primarily operate on ACPConn data)
// ---------------------------------------------------------------------------

// sessionStateExtracted holds extracted state from an ACP session response.
// Used by CacheNewSessionState and MergeResumedSessionState to share extraction logic.
type sessionStateExtracted struct {
	modes           []ModeDef
	modeCurrentID   string
	configState     *ConfigOptionState
	efforts         []ThinkingEffortDef
	effortCurrentID string
	models          []model.AgentModel
	modelCurrentID  string
}

// CacheNewSessionState extracts and caches mode/config/thinking/model state from
// a NewSessionResponse after creating a new ACP session.
func (c *ACPConn) CacheNewSessionState() {
	sessResp := c.GetAndClearNewSessionResp()
	if sessResp == nil {
		slog.Warn("acp: CacheNewSessionState called with nil sessResp")
		return
	}
	slog.Info(
		"acp: caching new session state",
		"has_modes", sessResp.Modes != nil,
		"config_options_count", len(sessResp.ConfigOptions),
	)

	ext := c.extractSessionState(
		func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return sessResp, nil
		},
	)

	c.applyExtractedState(ext)
}

// MergeResumedSessionState merges state from a ResumeSessionResponse, preserving
// the user's current selections (re-applied by ensureAliveWithSession) while
// updating available options lists from the resumed agent via the registry.
func (c *ACPConn) MergeResumedSessionState() {
	resumeResp := c.GetAndClearResumeSessionResp()
	if resumeResp == nil {
		slog.Warn("acp: MergeResumedSessionState called with nil resumeResp")
		return
	}
	slog.Info(
		"acp: merging resumed session state",
		"has_modes", resumeResp.Modes != nil,
		"config_options_count", len(resumeResp.ConfigOptions),
	)

	ext := c.extractSessionState(
		func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return nil, resumeResp
		},
	)

	c.applyExtractedState(ext)
}

// extractSessionState extracts mode/config/thinking/model state from a session response.
// getResp returns either a NewSessionResponse or ResumeSessionResponse (one must be non-nil).
func (c *ACPConn) extractSessionState(getResp func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse)) sessionStateExtracted {
	newResp, resumeResp := getResp()
	var ext sessionStateExtracted

	// Extract mode state
	if newResp != nil {
		if modeState := extractACPModeState(newResp); modeState != nil {
			ext.modes = modeState.AvailableModes
			ext.modeCurrentID = modeState.CurrentModeID
			slog.Info("acp: extracted mode from v1 Modes field", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		} else {
			slog.Info("acp: no mode from v1 Modes field, will rely on configOptions fallback")
		}
		ext.configState = extractACPConfigOptions(newResp)
		if ext.configState != nil {
			slog.Info("acp: extracted config from configOptions", "config_id", ext.configState.ConfigID, "current", ext.configState.CurrentID, "options", len(ext.configState.Options))
		} else {
			slog.Info("acp: no mode config from configOptions")
		}
		if effortState := extractACPThinkingEffort(newResp); effortState != nil {
			ext.efforts = effortState.AvailableLevels
			ext.effortCurrentID = effortState.CurrentID
			slog.Info("acp: extracted thinking effort", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
		} else {
			slog.Info("acp: no thinking effort from configOptions")
		}
		if modelList := extractACPModelList(newResp); modelList != nil {
			ext.models = modelList.Models
			ext.modelCurrentID = modelList.CurrentModelID
			slog.Info("acp: extracted model list", "current", modelList.CurrentModelID, "available", len(modelList.Models))
		} else if c.stdoutFilter != nil {
			if cached := c.stdoutFilter.GetAndClearCachedModels(); cached != nil {
				ext.models = cached.Models
				ext.modelCurrentID = cached.CurrentModelID
				slog.Info("acp: extracted model list from SessionModelState extension", "current", cached.CurrentModelID, "available", len(cached.Models))
			} else {
				slog.Info("acp: no model list from configOptions or SessionModelState extension")
			}
		} else {
			slog.Info("acp: no model list from configOptions")
		}
	} else {
		if modeState := extractACPModeStateFromResume(resumeResp); modeState != nil {
			ext.modes = modeState.AvailableModes
			ext.modeCurrentID = modeState.CurrentModeID
			slog.Info("acp: resumed mode state", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		} else {
			slog.Info("acp: no mode from resumed v1 Modes field")
		}
		ext.configState = extractACPConfigOptionsFromResume(resumeResp)
		if effortState := extractACPThinkingEffortFromResume(resumeResp); effortState != nil {
			ext.efforts = effortState.AvailableLevels
			ext.effortCurrentID = effortState.CurrentID
		}
		if modelList := extractACPModelListFromResume(resumeResp); modelList != nil {
			ext.models = modelList.Models
			ext.modelCurrentID = modelList.CurrentModelID
		} else if c.stdoutFilter != nil {
			if cached := c.stdoutFilter.GetAndClearCachedModels(); cached != nil {
				ext.models = cached.Models
				ext.modelCurrentID = cached.CurrentModelID
				slog.Info("acp: extracted model list from resumed SessionModelState extension", "current", cached.CurrentModelID, "available", len(cached.Models))
			}
		}
	}

	return ext
}

// applyExtractedState sets session-level current values and updates the agent-level registry.
// Always preserves user's existing selections (from PreApply) over the agent's response defaults.
func (c *ACPConn) applyExtractedState(ext sessionStateExtracted) {
	currentIDs := map[string]*string{
		"mode":          &ext.modeCurrentID,
		"thought_level": &ext.effortCurrentID,
		"model":         &ext.modelCurrentID,
	}

	// Always preserve user's existing selections over the agent's defaults.
	// This is critical for new sessions: the PreApply step in ExecuteStream
	// sets currentModeID/currentThinkingEffortID from the user's request
	// BEFORE CacheNewSessionState runs. Without this preservation,
	// the agent's reported default would overwrite the user's choice.
	for category, idPtr := range currentIDs {
		if existing := c.GetCurrentSelection(category); existing != "" {
			*idPtr = existing
		}
	}

	// Special: also update configState.CurrentID to match preserved mode
	if ext.configState != nil && ext.modeCurrentID != "" {
		ext.configState.CurrentID = ext.modeCurrentID
	}

	// Set session-level current values on ACPConn
	c.SetCurrentModeID(ext.modeCurrentID)
	c.SetCurrentThinkingEffortID(ext.effortCurrentID)
	c.SetCurrentModelID(ext.modelCurrentID)

	// Force-update agent-level registry (full overwrite, once per process instance)
	// LoadSession comes from BackendSpec (authoritative), ListSessions from registry.
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()
	spec := model.FindSpecByBackend(c.agent.Backend)
	loadSession := spec != nil && spec.ACPLoadSession
	listSessions := reg.GetListSessions(agentID)
	reg.ForceUpdateIfNeeded(agentID, ext.modes, ext.efforts, ext.models, nil, ext.configState, loadSession, listSessions)
}

// EmitSessionStateEvents emits mode_update, thinking_effort_update, and model_list_update
// WS events. Called on every stream start (new and resumed sessions) so the frontend
// always receives the current ACP state.
func (c *ACPConn) EmitSessionStateEvents(ch chan<- StreamEvent) {
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()

	// Unified: iterate categories and build state via SelectState → domain type
	categories := []struct {
		category string
		emit     func(SelectState)
	}{
		{"mode", func(sel SelectState) {
			if ms := sel.ToModeState(); ms != nil {
				slog.Info("acp: emitting mode_update for new session", "current_mode", ms.CurrentModeID, "available", len(ms.AvailableModes))
				forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: ms})
			}
		}},
		{"thought_level", func(sel SelectState) {
			if tes := sel.ToThinkingEffortState(); tes != nil {
				slog.Debug("acp: emitting thinking_effort_update for new session", "current", tes.CurrentID, "available", len(tes.AvailableLevels))
				forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: tes})
			}
		}},
	}
	for _, cat := range categories {
		currentID := c.GetCurrentSelection(cat.category)
		if sel := reg.GetSelectState(agentID, cat.category, currentID); sel != nil && !sel.IsEmpty() {
			cat.emit(*sel)
		}
	}

	// Model list has a different structure (AgentModel with Default field), kept separate
	if modelListState := reg.GetModelListState(agentID, c.GetCurrentModelID()); modelListState != nil {
		slog.Debug("acp: emitting model_list_update for new session", "current", modelListState.CurrentModelID, "available", len(modelListState.Models))
		forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelListState})
	}
}

// EmitCommandsUpdate re-emits cached slash commands as a WS event.
func (c *ACPConn) EmitCommandsUpdate(ch chan<- StreamEvent) {
	agentID := c.AgentID()
	cmds := GetAgentCapabilityRegistry().GetCommands(agentID)
	if len(cmds) == 0 {
		if client := c.GetClient(); client != nil {
			clientCmds := client.GetCommandsAsInfo()
			if len(clientCmds) > 0 {
				cmds = clientCmds
				GetAgentCapabilityRegistry().UpdateCommands(agentID, cmds)
			}
		}
	}
	if len(cmds) == 0 {
		return
	}
	slog.Info("acp: re-emitting cached commands_update", "count", len(cmds), "source", func() string {
		if len(GetAgentCapabilityRegistry().GetCommands(agentID)) > 0 {
			return "registry"
		}
		return "client_fallback"
	}())
	forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: cmds})
}

// ScheduleCommandsReEmit starts a timer that re-emits the commands_update event
// after the given delay. This allows time for CodeBuddy's plugin system to load
// and send an updated AvailableCommandsUpdate via ACP (issue #383).
// Returns a stop function that cancels the timer.
func (c *ACPConn) ScheduleCommandsReEmit(ch chan<- StreamEvent, delay time.Duration) func() {
	timer := time.AfterFunc(delay, func() {
		agentID := c.AgentID()
		cmds := GetAgentCapabilityRegistry().GetCommands(agentID)
		if len(cmds) == 0 {
			return
		}
		slog.Info("acp: delayed re-emitting commands_update (plugin race fix)", "agent", agentID, "count", len(cmds))
		forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: cmds})
	})
	return func() { timer.Stop() }
}

// isACPPeerDisconnected checks whether the error is an ACP peer-disconnect error
// or a context deadline exceeded error from an ACP SDK timeout context.
// Deadline-exceeded errors from the ACP SDK are InternalError (-32603) with
// "context deadline exceeded" in the data — they indicate the agent process
// is unresponsive and should be treated the same as a disconnect for retry purposes.
func isACPPeerDisconnected(err error) bool {
	// Direct context.DeadlineExceeded (not wrapped in RequestError)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return isPeerDisconnectMsg(err.Error()) || isACPDeadlineMsg(err.Error())
	}
	if reqErr.Code != -32603 {
		return false
	}
	if dataMap, ok := reqErr.Data.(map[string]any); ok {
		if errMsg, ok := dataMap["error"].(string); ok && (isPeerDisconnectMsg(errMsg) || isACPDeadlineMsg(errMsg)) {
			return true
		}
	}
	return isPeerDisconnectMsg(reqErr.Error()) || isACPDeadlineMsg(reqErr.Error())
}

// isPeerDisconnectMsg checks whether an error message indicates the peer
// process died or the connection pipe broke.
func isPeerDisconnectMsg(msg string) bool {
	return strings.Contains(msg, "peer disconnected") ||
		strings.Contains(msg, "broken pipe")
}

// isACPDeadlineMsg checks whether an error message indicates a context
// deadline exceeded from an ACP SDK timeout context. This happens when
// Initialize, LoadSession, ResumeSession, SetSessionConfigOption, or other
// ACP RPCs time out — the ACP SDK converts context.DeadlineExceeded into
// InternalError (-32603) with "context deadline exceeded" in the data.
func isACPDeadlineMsg(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded")
}

// isUnknownConfigOption checks whether the error indicates the agent doesn't
// recognize a config option.
func isUnknownConfigOption(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return strings.Contains(err.Error(), "Unknown config option")
	}
	if dataMap, ok := reqErr.Data.(map[string]any); ok {
		if details, ok := dataMap["details"].(string); ok && strings.Contains(details, "Unknown config option") {
			return true
		}
	}
	return strings.Contains(reqErr.Error(), "Unknown config option")
}

// IsACPResourceNotFound checks whether the error indicates the ACP agent could
// not find the requested resource (specifically a session).
//
// ACP's "-32002 Resource not found" code is generic: it applies to any missing
// resource (a file, a tool, an MCP server), not just sessions. To avoid
// misreporting a load failure (e.g. a referenced file is missing) as "session
// gone", only the canonical session-scoped form is treated as a missing session:
//   - a RequestError with code -32002 whose message is "Resource not found", or
//   - a plain error whose text references the session resource directly.
func IsACPResourceNotFound(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		// Plain (non-JSON-RPC) error: only treat it as session-not-found when the
		// message explicitly refers to the requested session.
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "resource not found") && strings.Contains(msg, "session")
	}
	if reqErr.Code != -32002 {
		return false
	}
	// Canonical resource-not-found: message is exactly / contains "Resource not found".
	// Avoid matching internal errors (-32603) whose data happens to embed the phrase.
	return strings.Contains(strings.ToLower(reqErr.Message), "resource not found")
}

// buildPromptBlocks constructs ACP ContentBlock list from the chat request.
// If a system prompt should be injected, it's prepended as the first text block.
// Slash commands (e.g. /reload-plugins) are sent as-is — ACP agents detect
// commands by the leading "/" and will not recognize the command if it is
// prefixed with [System Instructions: ...] or other text.
func (b *ACPBackend) buildPromptBlocks(req ChatRequest) []acp.ContentBlock {
	prompt := req.Prompt

	// Prepend fork context (fork session first message) so the AI has
	// conversation history from the parent session.
	if req.ForkContext != "" {
		prompt = req.ForkContext + prompt
	}

	// Skip system prompt injection for slash commands — ACP agents
	// detect slash commands by the leading "/" and routing depends on it.
	if req.ShouldInjectSystemPrompt() && !IsACPSlashCommand(prompt) {
		prompt = fmt.Sprintf("[System Instructions: %s]\n\n%s", req.SystemPrompt, prompt)
	}

	return []acp.ContentBlock{acp.TextBlock(prompt)}
}

// IsACPSlashCommand checks if the text is an ACP slash command (e.g. /compact,
// /reload-plugins). ACP agents detect slash commands by the leading "/" and
// route them to CommandExecutor instead of the LLM. The regex matches the
// same pattern as CodeBuddy's isSlashCommand: /<letter>[<alphanumeric/hyphen>].
func IsACPSlashCommand(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '/' {
		return false
	}
	// Must start with /<letter> followed by alphanumeric or hyphen
	c := t[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
