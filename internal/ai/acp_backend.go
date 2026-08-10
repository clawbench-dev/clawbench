package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}

// NewACPBackend creates a new ACPBackend for the given agent.
// The agent must have AcpCommand set (indicating ACP support).
func NewACPBackend(agent *model.Agent) (*ACPBackend, error) {
	if !agent.SupportsACP() {
		return nil, fmt.Errorf("acp backend: agent %q does not support acp-stdio transport (no acp_command)", agent.ID)
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

		streamStart := time.Now()

		// Step 1: Get or create a dedicated connection for this session
		mgr := GetACPConnManager()
		connStart := time.Now()
		if req.WorkDir == "" {
			slog.Warn("acp: WorkDir is EMPTY in ChatRequest, ACP process will inherit server CWD",
				slog.String("session_id", req.SessionID),
				slog.String("agent_id", b.agent.ID))
		}
		conn, isNew, err := mgr.GetOrCreateConn(ctx, b.agent, req.SessionID, req.WorkDir)
		slog.Info("acp: GetOrCreateConn done", "session_id", req.SessionID, "agent_id", b.agent.ID, "is_new", isNew, "elapsed", time.Since(connStart), "error", err)
		if err != nil {
			// ACP connection failed — surface the error directly.
			// Do NOT fall back to CLI backend: the user chose ACP transport
			// and silent fallback hides real problems (e.g., NewSession timeout).
			slog.Error("acp: connection failed", "agent_id", b.agent.ID, "error", err)
			forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: connection: %v", err), Reason: ReasonBackendExit})
			return
		}

		acpSessionID := conn.AcpSID()

		// Sync autoApprove from DB to ACPConn before prompt,
		// so RequestPermission callbacks use the correct state.
		conn.SetAutoApprove(getSessionAutoApprove(req.SessionID))

		// Pre-apply user's mode/thinkingEffort selection to the cached state BEFORE
		// emitting WS events, so the frontend sees the user's choice (e.g. "plan")
		// instead of the agent's default (e.g. "bypassPermissions") during streaming.
		// The actual RPC to set the config is still done inside Prompt().
		if req.Mode != "" {
			conn.UpdateCachedCurrent("mode", req.Mode)
			conn.PreApplyConfigCurrentID("mode", req.Mode)
		}
		if req.ThinkingEffort != "" {
			conn.UpdateCachedCurrent("thought_level", req.ThinkingEffort)
			conn.PreApplyConfigCurrentID("thinkingEffort", req.ThinkingEffort)
		}

		// Step 2: Handle new vs recovered session
		slog.Info("acp perf: ExecuteStream.step2_emitSession_start", "session_id", req.SessionID, "is_new", isNew, "after_GetOrCreateConn", time.Since(streamStart))
		b.emitSessionAndCacheState(conn, isNew, ch)

		// Step 3: Send prompt
		slog.Info("acp perf: ExecuteStream.step3_Prompt_start", "session_id", req.SessionID, "after_emitSession", time.Since(streamStart))
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

			// Non-fatal prompt error (e.g., API returned malformed response,
			// agent cancelled the turn). The agent process is still alive and
			// the connection is usable — surface as an amber warning card so
			// the user can retry without losing the session.
			//
			// Extract human-readable error message from ACP RequestError.
			// RequestError.Error() returns the full JSON blob which is not
			// useful to the end user — we want the code and just the .Message
			// field (e.g. "Internal error: Upstream request failed: [invalid_request_error] Insufficient Balance").
			forwardACPEvent(ch, StreamEvent{Type: "warning", Content: acpErrorText(err), Reason: ReasonRequestFailed})
			forwardACPEvent(ch, StreamEvent{Type: "done"})
			return
		}

		// Step 4: Prompt completed normally
		slog.Info("acp perf: ExecuteStream.step4_done", "session_id", req.SessionID, "total_elapsed", time.Since(streamStart))
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
		conn.CacheNewSessionState()
	} else {
		conn.MergeResumedSessionState()
	}

	// Emit mode/thinking/model state on every stream start so the frontend
	// can populate chips regardless of whether the session is new or resumed.
	conn.EmitSessionStateEvents(ch)

	// config_update is still re-emitted every stream because the frontend
	// resets config state on session switch and config covers more than just mode.
	if configState := GetAgentCapabilityRegistry().GetConfigState(conn.AgentID()); configState != nil {
		slog.Debug("acp: re-emitting cached config_update", "config_id", configState.ConfigID, "current", configState.CurrentID)
		forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
	}

	// Emit commands_update if cached from available_commands_update.
	conn.EmitCommandsUpdate(ch)

	// Re-emit cached plan state so the frontend populates the plan panel
	// on reconnect/respawn without waiting for a new plan_update event.
	if planState := conn.GetCachedPlanState(); planState != nil {
		forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: planState})
	}
}

// acpErrorText formats an ACP error for the UI warning banner, preserving both
// the JSON-RPC error code and the human-readable message. For a RequestError we
// emit "ACP error <code>: <message>" instead of err.Error()'s raw JSON blob
// (which is noisy and unhelpful to the end user). Non-RequestError errors fall
// through to err.Error() unchanged.
func acpErrorText(err error) string {
	var reqErr *acp.RequestError
	if errors.As(err, &reqErr) {
		return fmt.Sprintf("ACP error %d: %s", reqErr.Code, reqErr.Message)
	}
	return err.Error()
}

// Ensure compile-time interface compliance
var _ AIBackend = (*ACPBackend)(nil)
