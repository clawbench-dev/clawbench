package ai

import (
	"context"
	"fmt"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ACPBackend implements the AIBackend interface using the Agent Client Protocol.
// It uses ACPConnectionPool for long-lived connections and session reuse.
//
// With connection pooling:
//   - Each agent gets one long-lived process (stdio) or HTTP transport
//   - Multiple ClawBench sessions share the same connection (different ACP sessions)
//   - Multiple prompt turns within the same ClawBench session reuse the ACP session
//   - Cancel uses session/cancel (not process kill), session stays open
//   - Idle connections are killed after 5 minutes of inactivity
type ACPBackend struct {
	agent *model.Agent // resolved agent config
}

// NewACPBackend creates a new ACPBackend for the given agent.
// The agent must have Transport set to "acp-stdio" or "acp-http".
func NewACPBackend(agent *model.Agent) (*ACPBackend, error) {
	if agent.Transport != "acp-stdio" && agent.Transport != "acp-http" {
		return nil, fmt.Errorf("acp backend: agent %q has transport %q, expected acp-stdio or acp-http", agent.ID, agent.Transport)
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
func (b *ACPBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, streamChanSize)

	go func() {
		defer close(ch)

		// Step 1: Get or create a long-lived connection from the pool
		pool := GetACPConnectionPool()
		entry, err := pool.GetOrCreate(ctx, b.agent)
		if err != nil {
			forwardACPEvent(ch, StreamEvent{Type: "error", Error: fmt.Sprintf("acp: connection: %v", err), Reason: ReasonBackendExit})
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

			// Emit mode_update and config_update for new sessions
			// stdio transport: extract from NewSessionResponse
			if sessResp := entry.GetAndClearSessionResp(); sessResp != nil {
				if modeState := extractACPModeState(sessResp); modeState != nil {
					slog.Info("acp: emitting mode_update", "current_mode", modeState.CurrentModeID, "available", len(modeState.AvailableModes))
					forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: modeState})
				}
				if configState := extractACPConfigOptions(sessResp); configState != nil {
					slog.Info("acp: emitting config_update", "config_id", configState.ConfigID, "current", configState.CurrentID)
					forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
				}
			}
			// HTTP transport: extract from parsed mode/config state
			if ms, cs := entry.GetAndClearModeStates(); ms != nil || cs != nil {
				if ms != nil {
					slog.Info("acp: emitting mode_update (HTTP)", "current_mode", ms.CurrentModeID, "available", len(ms.AvailableModes))
					forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: ms})
				}
				if cs != nil {
					slog.Info("acp: emitting config_update (HTTP)", "config_id", cs.ConfigID, "current", cs.CurrentID)
					forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: cs})
				}
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
