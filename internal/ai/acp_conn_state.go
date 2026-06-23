package ai

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

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

	c.applyExtractedState(ext, false)
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

	c.applyExtractedState(ext, true)
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
		}
	}

	return ext
}

// applyExtractedState sets session-level current values and updates the agent-level registry.
// If preserveExisting is true, existing user selections are kept over the response defaults
// (used for ResumeSession where the user's config was re-applied after respawn).
func (c *ACPConn) applyExtractedState(ext sessionStateExtracted, preserveExisting bool) {
	modeCurrentID := ext.modeCurrentID
	effortCurrentID := ext.effortCurrentID
	modelCurrentID := ext.modelCurrentID
	configState := ext.configState

	// Preserve user's current selections over the resumed agent's defaults
	if preserveExisting {
		if existing := c.GetCurrentModeID(); existing != "" {
			modeCurrentID = existing
		}
		if configState != nil && c.GetCurrentModeID() != "" {
			configState.CurrentID = c.GetCurrentModeID()
		}
		if existing := c.GetCurrentThinkingEffortID(); existing != "" {
			effortCurrentID = existing
		}
		if existing := c.GetCurrentModelID(); existing != "" {
			modelCurrentID = existing
		}
	}

	// Set session-level current values on ACPConn
	c.SetCurrentModeID(modeCurrentID)
	c.SetCurrentThinkingEffortID(effortCurrentID)
	c.SetCurrentModelID(modelCurrentID)

	// Force-update agent-level registry (full overwrite, once per process instance)
	// Preserve loadSession/listSessions from spawnLocked's Initialize response.
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()
	loadSession := reg.GetLoadSession(agentID)
	listSessions := reg.GetListSessions(agentID)
	reg.ForceUpdateIfNeeded(agentID, ext.modes, ext.efforts, ext.models, nil, configState, loadSession, listSessions)
}

// EmitSessionStateEvents emits mode_update, thinking_effort_update, and model_list_update
// SSE events. Called on every stream start (new and resumed sessions) so the frontend
// always receives the current ACP state.
func (c *ACPConn) EmitSessionStateEvents(ch chan<- StreamEvent) {
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()

	if modeState := reg.GetModeState(agentID, c.GetCurrentModeID()); modeState != nil {
		slog.Info("acp: emitting mode_update for new session", "current_mode", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: modeState})
	}
	if effortState := reg.GetThinkingEffortState(agentID, c.GetCurrentThinkingEffortID()); effortState != nil {
		slog.Debug("acp: emitting thinking_effort_update for new session", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
		forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
	}
	if modelListState := reg.GetModelListState(agentID, c.GetCurrentModelID()); modelListState != nil {
		slog.Debug("acp: emitting model_list_update for new session", "current", modelListState.CurrentModelID, "available", len(modelListState.Models))
		forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelListState})
	}
}

// EmitCommandsUpdate re-emits cached slash commands as an SSE event.
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

// isACPPeerDisconnected checks whether the error is an ACP peer-disconnect error.
func isACPPeerDisconnected(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return isPeerDisconnectMsg(err.Error())
	}
	if reqErr.Code != -32603 {
		return false
	}
	if dataMap, ok := reqErr.Data.(map[string]any); ok {
		if errMsg, ok := dataMap["error"].(string); ok && isPeerDisconnectMsg(errMsg) {
			return true
		}
	}
	return isPeerDisconnectMsg(reqErr.Error())
}

// isPeerDisconnectMsg checks whether an error message indicates the peer
// process died or the connection pipe broke.
func isPeerDisconnectMsg(msg string) bool {
	return strings.Contains(msg, "peer disconnected") ||
		strings.Contains(msg, "broken pipe")
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

// IsACPResourceNotFound checks whether the error indicates the ACP agent
// could not find the requested resource.
func IsACPResourceNotFound(err error) bool {
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) {
		return strings.Contains(err.Error(), "Resource not found")
	}
	if reqErr.Code == -32002 && strings.Contains(reqErr.Message, "Resource not found") {
		return true
	}
	return strings.Contains(reqErr.Error(), "Resource not found")
}

// buildPromptBlocks constructs ACP ContentBlock list from the chat request.
// If a system prompt should be injected, it's prepended as the first text block.
func (b *ACPBackend) buildPromptBlocks(req ChatRequest) []acp.ContentBlock {
	prompt := req.Prompt

	// Prepend fork context (fork session first message) so the AI has
	// conversation history from the parent session.
	if req.ForkContext != "" {
		prompt = req.ForkContext + prompt
	}

	if req.ShouldInjectSystemPrompt() {
		prompt = fmt.Sprintf("[System Instructions: %s]\n\n%s", req.SystemPrompt, prompt)
	}

	return []acp.ContentBlock{acp.TextBlock(prompt)}
}
