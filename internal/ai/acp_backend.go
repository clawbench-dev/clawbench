package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ACPBackend implements the AIBackend interface using the Agent Client Protocol.
// Each ClawBench session gets its own dedicated agent process (one-to-one model).
//
//   - Each ClawBench session = one agent subprocess (acp-stdio)
//   - Agent processes are never idle-reaped
//   - If the process dies, it is respawned and the session is recovered via ResumeSession
//   - Cancel marks the connection as dead; next prompt triggers respawn + ResumeSession
type ACPBackend struct {
	agent *model.Agent // resolved agent config

	// CLI fallback: used when the ACP connection fails (e.g., agent binary
	// doesn't support ACP mode). Lazily initialized on first fallback.
	cliFallback     AIBackend
	cliFallbackOnce sync.Once
}

// NewACPBackend creates a new ACPBackend for the given agent.
// The agent must have Transport set to "acp-stdio".
func NewACPBackend(agent *model.Agent) (*ACPBackend, error) {
	if agent.Transport != "acp-stdio" {
		return nil, fmt.Errorf("acp backend: agent %q has transport %q, expected acp-stdio", agent.ID, agent.Transport)
	}
	return &ACPBackend{agent: agent}, nil
}

// Name returns the backend identifier.
func (b *ACPBackend) Name() string {
	return b.agent.Backend
}

// ExecuteStream runs the ACP agent and returns a channel of streaming events.
//
// Flow: GetOrCreateConn → (ResumeSession or NewSession) → emit cached state → Prompt
// On peer disconnect during Prompt, automatically retries once after respawn + ResumeSession.
func (b *ACPBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) { //nolint:gocognit,gocyclo // complex ACP protocol handler, refactoring would reduce readability
	ch := make(chan StreamEvent, streamChanSize)

	go func() {
		defer close(ch)

		// Step 1: Get or create a dedicated connection for this session
		mgr := GetACPConnManager()
		connStart := time.Now()
		conn, isNew, err := mgr.GetOrCreateConn(ctx, b.agent, req.SessionID, req.WorkDir)
		slog.Info("acp: GetOrCreateConn done", "session_id", req.SessionID, "agent_id", b.agent.ID, "is_new", isNew, "elapsed", time.Since(connStart), "error", err)
		if err != nil {
			// ACP connection failed (e.g., agent binary doesn't support ACP mode).
			// Fall back to CLI backend so the user can still chat.
			slog.Warn("acp: connection failed, falling back to CLI backend", "agent_id", b.agent.ID, "error", err)
			b.cliFallbackOnce.Do(func() {
				cli, cliErr := NewBackend(b.agent.Backend)
				if cliErr != nil {
					slog.Error("acp: CLI fallback creation failed", "backend", b.agent.Backend, "error", cliErr)
					return
				}
				b.cliFallback = cli
			})
			if b.cliFallback == nil {
				forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: connection: %v", err), Reason: ReasonBackendExit})
				return
			}
			// Delegate to CLI backend and forward events
			fallbackCh, fallbackErr := b.cliFallback.ExecuteStream(ctx, req)
			if fallbackErr != nil {
				forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: connection: %v (CLI fallback also failed: %v)", err, fallbackErr), Reason: ReasonBackendExit})
				return
			}
			for event := range fallbackCh {
				forwardACPEvent(ch, event)
			}
			return
		}

		acpSessionID := conn.AcpSID()

		// Sync autoApprove from DB to ACPConn before prompt,
		// so RequestPermission callbacks use the correct state.
		conn.SetAutoApprove(getSessionAutoApprove(req.SessionID))

		// Step 2: Handle new vs recovered session
		b.emitSessionAndCacheState(conn, isNew, ch)

		// Step 3: Send prompt
		promptBlocks := b.buildPromptBlocks(req)
		err = conn.Prompt(ctx, promptBlocks, ch, req)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("acp: prompt cancelled", "session_id", req.SessionID, "acp_sid", acpSessionID)
				forwardACPEvent(ch, StreamEvent{Type: "done"})
				return
			}

			// If the error is a retryable disconnect (peer disconnect or config-killed
			// connection), retry once after respawn + ResumeSession.
			if isACPPeerDisconnected(err) || isConfigKilledConnection(err) {
				slog.Warn("acp: connection lost during prompt, retrying after respawn",
					"session_id", req.SessionID, "acp_sid", acpSessionID, "error", err)

				// If a config option killed the connection, skip that config on retry
				// to avoid crashing the respawned process with the same value.
				var configKilled *configKilledConnectionError
				if errors.As(err, &configKilled) {
					switch configKilled.ConfigID() {
					case "model":
						slog.Warn("acp: skipping model config on retry (caused previous crash)",
							"model", configKilled.Value(), "session_id", req.SessionID)
						req.Model = ""
					case "thinkingEffort":
						slog.Warn("acp: skipping thinking_effort config on retry (caused previous crash)",
							"thinking_effort", configKilled.Value(), "session_id", req.SessionID)
						req.ThinkingEffort = ""
					case "mode":
						slog.Warn("acp: skipping mode config on retry (caused previous crash)",
							"mode", configKilled.Value(), "session_id", req.SessionID)
						req.Mode = ""
					}
				}

				conn2, isNew2, retryErr := mgr.GetOrCreateConn(ctx, b.agent, req.SessionID, req.WorkDir)
				if retryErr != nil {
					forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: prompt: %v (retry respawn failed: %v)", err, retryErr), Reason: ReasonBackendExit})
					return
				}
				// Re-emit session/cache state for the respawned connection
				b.emitSessionAndCacheState(conn2, isNew2, ch)
				// Re-sync autoApprove for the respawned connection
				conn2.SetAutoApprove(getSessionAutoApprove(req.SessionID))
				promptBlocks2 := b.buildPromptBlocks(req)
				retryPromptErr := conn2.Prompt(ctx, promptBlocks2, ch, req)
				if retryPromptErr != nil {
					if ctx.Err() != nil {
						slog.Info("acp: prompt cancelled after retry", "session_id", req.SessionID)
						forwardACPEvent(ch, StreamEvent{Type: "done"})
						return
					}
					slog.Error("acp: retry also failed after respawn",
						"session_id", req.SessionID,
						"original_error", err.Error(),
						"retry_error", retryPromptErr.Error())
					forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: prompt: %v (retry also failed: %v)", err, retryPromptErr), Reason: ReasonBackendExit})
					return
				}
				// Retry succeeded
				forwardACPEvent(ch, StreamEvent{Type: "done"})
				return
			}

			forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: prompt: %v", err), Reason: ReasonBackendExit})
			return
		}

		// Step 4: Prompt completed normally
		forwardACPEvent(ch, StreamEvent{Type: "done"})
	}()

	return ch, nil
}

// emitSessionAndCacheState emits session_capture + cached ACP state events to the stream channel.
func (b *ACPBackend) emitSessionAndCacheState(conn *ACPConn, isNew bool, ch chan<- StreamEvent) {
	acpSessionID := conn.AcpSID()

	if isNew {
		// New session — emit session_capture for handler to persist ACP session ID
		forwardACPEvent(ch, StreamEvent{Type: "session_capture", Content: acpSessionID})
		b.cacheNewSessionState(conn)
	} else {
		b.mergeResumedSessionState(conn)
	}

	// Emit mode/thinking/model state on every stream start so the frontend
	// can populate chips regardless of whether the session is new or resumed.
	// Previously this only fired for isNew sessions, which meant resumed
	// sessions never received mode_update/thinking_effort_update events.
	b.emitSessionStateEvents(conn, ch)

	// config_update is still re-emitted every stream because the frontend
	// resets config state on session switch and config covers more than just mode.
	if configState := GetAgentCapabilityRegistry().GetConfigState(conn.AgentID()); configState != nil {
		slog.Debug("acp: re-emitting cached config_update", "config_id", configState.ConfigID, "current", configState.CurrentID)
		forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
	}

	// Emit commands_update if cached from available_commands_update.
	// Also re-emitted for every stream to repopulate frontend state.
	b.emitCommandsUpdate(conn, ch)

	// Re-emit cached plan state so the frontend populates the plan panel
	// on reconnect/respawn without waiting for a new plan_update event.
	if planState := conn.GetCachedPlanState(); planState != nil {
		forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: planState})
	}
}

// cacheNewSessionState extracts and caches mode/config/thinking/model state from
// a NewSessionResponse after creating a new ACP session.
// Session-level current values are stored on ACPConn; agent-level available lists
// are force-updated into AgentCapabilityRegistry (full overwrite, once per process).
func (b *ACPBackend) cacheNewSessionState(conn *ACPConn) {
	sessResp := conn.GetAndClearNewSessionResp()
	if sessResp == nil {
		slog.Warn("acp: cacheNewSessionState called with nil sessResp")
		return
	}
	slog.Info("acp: caching new session state",
		"has_modes", sessResp.Modes != nil,
		"config_options_count", len(sessResp.ConfigOptions),
	)

	// Extract all state from response
	var modes []ModeDef
	var modeCurrentID string
	if modeState := extractACPModeState(sessResp); modeState != nil {
		modes = modeState.AvailableModes
		modeCurrentID = modeState.CurrentModeID
		slog.Info("acp: extracted mode from v1 Modes field", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
	} else {
		slog.Info("acp: no mode from v1 Modes field, will rely on configOptions fallback")
	}
	configState := extractACPConfigOptions(sessResp)
	if configState != nil {
		slog.Info("acp: extracted config from configOptions", "config_id", configState.ConfigID, "current", configState.CurrentID, "options", len(configState.Options))
	} else {
		slog.Info("acp: no mode config from configOptions")
	}
	var efforts []ThinkingEffortDef
	var effortCurrentID string
	if effortState := extractACPThinkingEffort(sessResp); effortState != nil {
		efforts = effortState.AvailableLevels
		effortCurrentID = effortState.CurrentID
		slog.Info("acp: extracted thinking effort", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
	} else {
		slog.Info("acp: no thinking effort from configOptions")
	}
	var models []model.AgentModel
	var modelCurrentID string
	if modelList := extractACPModelList(sessResp); modelList != nil {
		models = modelList.Models
		modelCurrentID = modelList.CurrentModelID
		slog.Info("acp: extracted model list", "current", modelList.CurrentModelID, "available", len(modelList.Models))
	} else {
		slog.Info("acp: no model list from configOptions")
	}

	// Set session-level current values on ACPConn
	conn.SetCurrentModeID(modeCurrentID)
	conn.SetCurrentThinkingEffortID(effortCurrentID)
	conn.SetCurrentModelID(modelCurrentID)

	// Force-update agent-level registry (full overwrite, once per process instance)
	agentID := conn.AgentID()
	GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, modes, efforts, models, nil, configState)
}

// mergeResumedSessionState merges state from a ResumeSessionResponse, preserving
// the user's current selections (re-applied by ensureAliveWithSession) while
// updating available options lists from the resumed agent via the registry.
func (b *ACPBackend) mergeResumedSessionState(conn *ACPConn) {
	resumeResp := conn.GetAndClearResumeSessionResp()
	if resumeResp == nil {
		slog.Warn("acp: mergeResumedSessionState called with nil resumeResp")
		return
	}
	slog.Info("acp: merging resumed session state",
		"has_modes", resumeResp.Modes != nil,
		"config_options_count", len(resumeResp.ConfigOptions),
	)

	// Extract all state from response
	var modes []ModeDef
	var modeCurrentID string
	if modeState := extractACPModeStateFromResume(resumeResp); modeState != nil {
		modes = modeState.AvailableModes
		modeCurrentID = modeState.CurrentModeID
		slog.Info("acp: resumed mode state", "current", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
	} else {
		slog.Info("acp: no mode from resumed v1 Modes field")
	}
	configState := extractACPConfigOptionsFromResume(resumeResp)
	var efforts []ThinkingEffortDef
	var effortCurrentID string
	if effortState := extractACPThinkingEffortFromResume(resumeResp); effortState != nil {
		efforts = effortState.AvailableLevels
		effortCurrentID = effortState.CurrentID
	}
	var models []model.AgentModel
	var modelCurrentID string
	if modelList := extractACPModelListFromResume(resumeResp); modelList != nil {
		models = modelList.Models
		modelCurrentID = modelList.CurrentModelID
	}

	// Preserve user's current selections over the resumed agent's defaults
	if existing := conn.GetCurrentModeID(); existing != "" {
		modeCurrentID = existing
	}
	if configState != nil && conn.GetCurrentModeID() != "" {
		configState.CurrentID = conn.GetCurrentModeID()
	}
	if existing := conn.GetCurrentThinkingEffortID(); existing != "" {
		effortCurrentID = existing
	}
	if existing := conn.GetCurrentModelID(); existing != "" {
		modelCurrentID = existing
	}

	// Set session-level current values on ACPConn
	conn.SetCurrentModeID(modeCurrentID)
	conn.SetCurrentThinkingEffortID(effortCurrentID)
	conn.SetCurrentModelID(modelCurrentID)

	// Force-update agent-level registry (full overwrite, once per process instance)
	agentID := conn.AgentID()
	GetAgentCapabilityRegistry().ForceUpdateIfNeeded(agentID, modes, efforts, models, nil, configState)
}

// emitSessionStateEvents emits mode_update, thinking_effort_update, and model_list_update
// SSE events. Called on every stream start (new and resumed sessions) so the frontend
// always receives the current ACP state. Reads from AgentCapabilityRegistry + session current values.
func (b *ACPBackend) emitSessionStateEvents(conn *ACPConn, ch chan<- StreamEvent) {
	agentID := conn.AgentID()
	reg := GetAgentCapabilityRegistry()

	if modeState := reg.GetModeState(agentID, conn.GetCurrentModeID()); modeState != nil {
		slog.Info("acp: emitting mode_update for new session", "current_mode", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
		forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: modeState})
	}
	if effortState := reg.GetThinkingEffortState(agentID, conn.GetCurrentThinkingEffortID()); effortState != nil {
		slog.Debug("acp: emitting thinking_effort_update for new session", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
		forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
	}
	if modelListState := reg.GetModelListState(agentID, conn.GetCurrentModelID()); modelListState != nil {
		slog.Debug("acp: emitting model_list_update for new session", "current", modelListState.CurrentModelID, "available", len(modelListState.Models))
		forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelListState})
	}
}

// emitCommandsUpdate re-emits cached slash commands as an SSE event.
// Reads commands from the AgentCapabilityRegistry.
func (b *ACPBackend) emitCommandsUpdate(conn *ACPConn, ch chan<- StreamEvent) {
	agentID := conn.AgentID()
	cmds := GetAgentCapabilityRegistry().GetCommands(agentID)
	if len(cmds) == 0 {
		return
	}
	slog.Info("acp: re-emitting cached commands_update", "count", len(cmds))
	forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: cmds})
}

// isACPPeerDisconnected checks whether the error is an ACP peer-disconnect error
// (code -32603 with "peer disconnected" or "broken pipe" in the data). These errors
// are retryable because the agent process crashed and can be respawned + ResumeSession
// recovered.
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
// recognize a config option (e.g., CodeBuddy doesn't support "thinkingEffort").
// These errors have code -32603 with "Unknown config option" in the details.
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

// buildPromptBlocks constructs ACP ContentBlock list from the chat request.
// If a system prompt should be injected, it's prepended as the first text block.
func (b *ACPBackend) buildPromptBlocks(req ChatRequest) []acp.ContentBlock {
	prompt := req.Prompt

	// Inject system prompt if needed (same logic as CLI backends without --system-prompt flag)
	if req.ShouldInjectSystemPrompt() {
		prompt = fmt.Sprintf("[System Instructions: %s]\n\n%s", req.SystemPrompt, req.Prompt)
	}

	return []acp.ContentBlock{acp.TextBlock(prompt)}
}

// Ensure compile-time interface compliance
var _ AIBackend = (*ACPBackend)(nil)
