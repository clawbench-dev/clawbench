package ai

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ACPBackend implements the AIBackend interface using the Agent Client Protocol.
// It uses ACPConnectionPool for long-lived stdio connections and session reuse.
//
// With connection pooling:
//   - Each agent gets one long-lived subprocess (stdio)
//   - Multiple ClawBench sessions share the same connection (different ACP sessions)
//   - Multiple prompt turns within the same ClawBench session reuse the ACP session
//   - Cancel uses session/cancel (not process kill), session stays open
//   - Idle connections are killed after 5 minutes of inactivity
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
// It uses the connection pool to reuse long-lived connections and sessions.
//
// For the first message in a session: pool.GetOrCreate → new session → prompt
// For subsequent messages: pool.GetOrCreate → reuse session → prompt
// On cancel: session/cancel (session stays open for next prompt)
func (b *ACPBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) { //nolint:gocognit,gocyclo // complex ACP protocol handler, refactoring would reduce readability
	ch := make(chan StreamEvent, streamChanSize)

	go func() {
		defer close(ch)

		// Step 1: Get or create a long-lived connection from the pool
		pool := GetACPConnectionPool()
		entry, err := pool.GetOrCreate(ctx, b.agent)
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

		// Step 2: Get or create an ACP session for this ClawBench session
		// req.SessionID is the ClawBench session UUID on first call,
		// or the external_session_id (ACP session ID) on resume.
		// The pool entry tracks the mapping internally.
		acpSessionID, isNew, err := entry.GetOrCreateSession(ctx, req.SessionID, req.WorkDir)
		if err != nil {
			forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: session: %v", err), Reason: ReasonBackendExit})
			return
		}

		// Step 3: Emit session_capture for new sessions (handler persists ACP session ID)
		if isNew {
			forwardACPEvent(ch, StreamEvent{Type: "session_capture", Content: acpSessionID})

			// Extract and cache mode/config/thinking effort state from NewSessionResponse
			if sessResp := entry.GetAndClearSessionResp(); sessResp != nil {
				if modeState := extractACPModeState(sessResp); modeState != nil {
					entry.SetCachedModeState(modeState)
				}
				if configState := extractACPConfigOptions(sessResp); configState != nil {
					entry.SetCachedConfigState(configState)
				}
				if effortState := extractACPThinkingEffort(sessResp); effortState != nil {
					entry.SetCachedThinkingEffortState(effortState)
				}
				if modelList := extractACPModelList(sessResp); modelList != nil {
					entry.SetCachedModelListState(modelList)
				}
			}
		}

		// Always re-emit cached mode_update and config_update for every stream.
		// The frontend resets these states on page load / session switch, so
		// they need to be repopulated even for resumed ACP sessions.
		// These events are idempotent — re-emitting them is harmless.
		if modeState := entry.GetCachedModeState(); modeState != nil {
			slog.Info("acp: re-emitting cached mode_update", "current_mode", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
			forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: modeState})
		}
		if configState := entry.GetCachedConfigState(); configState != nil {
			slog.Info("acp: re-emitting cached config_update", "config_id", configState.ConfigID, "current", configState.CurrentID)
			forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
		}
		if effortState := entry.GetCachedThinkingEffortState(); effortState != nil {
			slog.Info("acp: re-emitting cached thinking_effort_update", "current", effortState.CurrentID, "available", len(effortState.AvailableLevels))
			forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
		}
		if modelListState := entry.GetCachedModelListState(); modelListState != nil {
			slog.Info("acp: re-emitting cached model_list_update", "current", modelListState.CurrentModelID, "available", len(modelListState.Models))
			forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelListState})
		}

		// Emit commands_update if cached from available_commands_update.
		// Also re-emitted for every stream to repopulate frontend state.
		if client := entry.GetClient(); client != nil {
			if cmds := client.GetCommands(); len(cmds) > 0 {
				infos := make([]AvailableCommandInfo, 0, len(cmds))
				for _, c := range cmds {
					info := AvailableCommandInfo{
						Name:        c.Name,
						Description: c.Description,
					}
					if c.Input != nil && c.Input.Unstructured != nil {
						info.InputHint = c.Input.Unstructured.Hint
					}
					infos = append(infos, info)
				}
				slog.Info("acp: re-emitting cached commands_update", "count", len(infos))
				forwardACPEvent(ch, StreamEvent{Type: "commands_update", Commands: infos})
			}
		}

		// Step 4: Send prompt via the pooled connection
		promptBlocks := b.buildPromptBlocks(req)
		err = entry.Prompt(ctx, acpSessionID, promptBlocks, ch, req)
		if err != nil {
			if ctx.Err() != nil {
				// User cancel — session stays open for next prompt
				slog.Info("acp: prompt cancelled", "session_id", req.SessionID, "acp_sid", acpSessionID)
				forwardACPEvent(ch, StreamEvent{Type: "done"})
				return
			}
			forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: prompt: %v", err), Reason: ReasonBackendExit})
			return
		}

		// Step 5: Prompt completed normally
		forwardACPEvent(ch, StreamEvent{Type: "done"})
	}()

	return ch, nil
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
