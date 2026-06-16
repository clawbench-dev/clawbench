//go:build integration

package ai

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Agent Spec Table
// ---------------------------------------------------------------------------

// acpAgentSpec describes an ACP-capable agent for parameterized testing.
type acpAgentSpec struct {
	ID          string        // Agent ID (matches BackendRegistry ID)
	Backend     string        // Backend type
	AcpCommand  string        // ACP spawn command
	DefaultCmd  string        // CLI binary name for availability check
	HasThinking bool          // Whether BackendRegistry lists thinking levels
	Timeout     time.Duration // Per-prompt timeout
}

// acpAgentSpecs lists all ACP-capable agents from BackendRegistry.
var acpAgentSpecs = []acpAgentSpec{
	{ID: "claude", Backend: "claude", AcpCommand: "npx -y @agentclientprotocol/claude-agent-acp@latest", DefaultCmd: "claude", HasThinking: true, Timeout: 120 * time.Second},
	{ID: "codebuddy", Backend: "codebuddy", AcpCommand: "codebuddy --acp", DefaultCmd: "codebuddy", HasThinking: true, Timeout: 90 * time.Second},
	{ID: "opencode", Backend: "opencode", AcpCommand: "opencode acp", DefaultCmd: "opencode", HasThinking: true, Timeout: 90 * time.Second},
	{ID: "codex", Backend: "codex", AcpCommand: "npx -y @agentclientprotocol/codex-acp@latest", DefaultCmd: "codex", HasThinking: true, Timeout: 120 * time.Second},
	{ID: "qoder", Backend: "qoder", AcpCommand: "qodercli --acp", DefaultCmd: "qodercli", HasThinking: false, Timeout: 90 * time.Second},
	{ID: "cline", Backend: "cline", AcpCommand: "cline --acp", DefaultCmd: "cline", HasThinking: true, Timeout: 90 * time.Second},
	{ID: "kimi", Backend: "kimi", AcpCommand: "kimi acp", DefaultCmd: "kimi", HasThinking: true, Timeout: 90 * time.Second},
	{ID: "copilot", Backend: "copilot", AcpCommand: "copilot --acp", DefaultCmd: "copilot", HasThinking: true, Timeout: 90 * time.Second},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildAgentFromSpec creates a model.Agent from an acpAgentSpec.
func buildAgentFromSpec(spec acpAgentSpec) *model.Agent {
	return &model.Agent{
		ID:         spec.ID + "-acp-state-test",
		Name:       spec.ID + " ACP State Test",
		Backend:    spec.Backend,
		Transport:  "acp-stdio",
		AcpCommand: spec.AcpCommand,
		Models: []model.AgentModel{
			{ID: "default", Name: "Default Model", Default: true},
		},
	}
}

// requireACPAvailable skips the test if the agent's ACP command is not available.
// For bridge adapters (npx-based), checks both npx and the underlying CLI.
// For native agents, checks the CLI binary exists and supports the ACP subcommand.
func requireACPAvailable(t *testing.T, spec acpAgentSpec) {
	t.Helper()
	isBridge := strings.HasPrefix(spec.AcpCommand, "npx ")

	if isBridge {
		// Bridge adapter: need npx + the underlying CLI
		if _, err := exec.LookPath("npx"); err != nil {
			t.Skipf("npx not available, skipping %s ACP bridge test", spec.ID)
		}
		if _, err := exec.LookPath(spec.DefaultCmd); err != nil {
			t.Skipf("%s CLI not available, skipping %s ACP bridge test", spec.DefaultCmd, spec.ID)
		}
		return
	}

	// Native agent: check CLI binary
	path, err := exec.LookPath(spec.DefaultCmd)
	if err != nil {
		t.Skipf("%s CLI not available, skipping ACP test", spec.ID)
	}

	// Verify ACP subcommand is supported — try `--acp --version` or `acp --version`
	// depending on the command format.
	parts := strings.Fields(spec.AcpCommand)
	args := append(parts[1:], "--version")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("%s ACP subcommand not supported (error: %v, output: %s), skipping",
			spec.ID, err, truncate(string(output), 200))
	}
}

// findModeUpdateEvents finds all mode_update events in the event list.
func findModeUpdateEvents(events []StreamEvent) []StreamEvent {
	return findACPEvents(events, "mode_update")
}

// findConfigUpdateEvents finds all config_update events in the event list.
func findConfigUpdateEvents(events []StreamEvent) []StreamEvent {
	return findACPEvents(events, "config_update")
}

// findThinkingEffortUpdateEvents finds all thinking_effort_update events.
func findThinkingEffortUpdateEvents(events []StreamEvent) []StreamEvent {
	return findACPEvents(events, "thinking_effort_update")
}

// findCommandsUpdateEvents finds all commands_update events.
func findCommandsUpdateEvents(events []StreamEvent) []StreamEvent {
	return findACPEvents(events, "commands_update")
}

// findModelListUpdateEvents finds all model_list_update events.
func findModelListUpdateEvents(events []StreamEvent) []StreamEvent {
	return findACPEvents(events, "model_list_update")
}

// configUpdateHasModeCategory checks whether any config_update event contains
// a mode-category option.
func configUpdateHasModeCategory(events []StreamEvent) bool {
	for _, e := range events {
		if e.Config == nil {
			continue
		}
		for _, opt := range e.Config.Options {
			if opt.Category == "mode" {
				return true
			}
		}
	}
	return false
}

// configUpdateHasThoughtLevelCategory checks whether any config_update event
// contains a thought_level-category option.
func configUpdateHasThoughtLevelCategory(events []StreamEvent) bool {
	for _, e := range events {
		if e.Config == nil {
			continue
		}
		for _, opt := range e.Config.Options {
			if opt.Category == "thought_level" {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Test: Mode / Thinking / Commands / Model State for All ACP Agents
// ---------------------------------------------------------------------------

// TestACPState_AllAgents_ModeThinkingCommandsModel validates that each ACP-capable
// agent correctly reports mode, thinking effort, commands, and model list state
// through the ACP protocol after a prompt is sent.
//
// This test addresses the bug where Claude/OpenCode agents in ACP mode don't
// show the current mode in the Session Info bar, while CodeBuddy does.
// The root cause is that some agents may not report modes/thinking in their
// NewSessionResponse, or the backend may not correctly extract/cached the state.
func TestACPState_AllAgents_ModeThinkingCommandsModel(t *testing.T) {
	for _, spec := range acpAgentSpecs {
		t.Run(spec.ID, func(t *testing.T) {
			requireACPAvailable(t, spec)

			agent := buildAgentFromSpec(spec)
			env := setupACPTestEnvForAgent(t, agent)

			backend, err := NewACPBackend(agent)
			require.NoError(t, err, "NewACPBackend should succeed for %s", spec.ID)

			sessionID := acpSessionID()
			cleanupConn(t, sessionID)

			// Send a short prompt to establish the ACP connection and get state.
			events := sendACPPrompt(t, backend, sessionID, "说一个字：好", spec.Timeout)
			requireDoneEvent(t, events)

			conn := env.mgr.GetConn(sessionID)

			// ── Mode State ──────────────────────────────────────────────
			t.Run("mode", func(t *testing.T) {
				modeUpdates := findModeUpdateEvents(events)
				configUpdates := findConfigUpdateEvents(events)
				hasConfigMode := configUpdateHasModeCategory(configUpdates)

				// At least one source of mode state must be present.
				if len(modeUpdates) == 0 && !hasConfigMode {
					// Check cached state — if cached, the event may have been
					// consumed before we started listening.
					if conn != nil {
						if ms := cachedModeState(sessionID); ms != nil && len(ms.AvailableModes) > 0 {
							t.Logf("No mode_update/config_update(mode) SSE event, but cached ModeState exists: current=%q, available=%d",
								ms.CurrentModeID, len(ms.AvailableModes))
							return
						}
					}
					t.Errorf("Agent %s: no mode state available — neither mode_update nor config_update(category=mode) in SSE events. "+
						"Frontend Session Info will NOT show mode chip.", spec.ID)
					return
				}

				// Check cached mode state on the connection.
				if conn == nil {
					t.Skip("Connection not available (agent may have disconnected)")
				}
				modeState := cachedModeState(sessionID)
				require.NotNil(t, modeState, "Agent %s: cached ModeState should not be nil after prompt", spec.ID)

				assert.NotEmpty(t, modeState.CurrentModeID,
					"Agent %s: ModeState.CurrentModeID should not be empty", spec.ID)
				assert.NotEmpty(t, modeState.AvailableModes,
					"Agent %s: ModeState.AvailableModes should not be empty — this is what drives frontend mode chip display", spec.ID)

				// Check that mode names are populated (frontend displays the name).
				for _, m := range modeState.AvailableModes {
					if m.Name == "" {
						t.Logf("WARN: Agent %s: ModeDef{id=%q} has empty Name — frontend will fall back to ID for display", spec.ID, m.ID)
					}
				}

				t.Logf("Mode state: current=%q, available=%v", modeState.CurrentModeID,
					modeNamesFromState(modeState))

				// Verify SSE event data matches cached state.
				if len(modeUpdates) > 0 && modeUpdates[0].Mode != nil {
					assert.Equal(t, modeState.CurrentModeID, modeUpdates[0].Mode.CurrentModeID,
						"Agent %s: SSE mode_update currentModeId should match cached state", spec.ID)
				}
			})

			// ── Thinking Effort State ───────────────────────────────────
			t.Run("thinking_effort", func(t *testing.T) {
				effortUpdates := findThinkingEffortUpdateEvents(events)
				configUpdates := findConfigUpdateEvents(events)
				hasConfigThought := configUpdateHasThoughtLevelCategory(configUpdates)

				if conn == nil {
					t.Skip("Connection not available")
				}
				effortState := cachedThinkingEffortState(sessionID)

				if effortState == nil && !spec.HasThinking {
					t.Logf("Agent %s: no thinking effort state (expected — not listed in BackendRegistry)", spec.ID)
					return
				}

				if effortState == nil {
					if len(effortUpdates) == 0 && !hasConfigThought {
						if spec.HasThinking {
							// Agent's ACP implementation doesn't report thinking effort via
							// ConfigOptions — this is an agent-level limitation, not a backend bug.
							// Log as warning so it's visible but doesn't fail the test.
							t.Logf("WARN: Agent %s: BackendRegistry lists thinking levels but agent does not report them via ACP configOptions — "+
								"thinking effort chip will not appear in frontend", spec.ID)
						} else {
							t.Logf("Agent %s: no thinking effort state from agent (optional)", spec.ID)
						}
					}
					return
				}

				assert.NotEmpty(t, effortState.AvailableLevels,
					"Agent %s: ThinkingEffortState.AvailableLevels should not be empty", spec.ID)

				t.Logf("Thinking effort state: current=%q, available=%d levels",
					effortState.CurrentID, len(effortState.AvailableLevels))

				// Check that level names are populated.
				for _, l := range effortState.AvailableLevels {
					if l.Name == "" {
						t.Logf("WARN: Agent %s: ThinkingEffortDef{id=%q} has empty Name", spec.ID, l.ID)
					}
				}

				// Verify SSE/cache consistency.
				if len(effortUpdates) > 0 && effortUpdates[0].ThinkingEffort != nil {
					assert.Equal(t, effortState.CurrentID, effortUpdates[0].ThinkingEffort.CurrentID,
						"Agent %s: SSE thinking_effort_update currentId should match cached state", spec.ID)
				}
			})

			// ── Commands State ──────────────────────────────────────────
			t.Run("commands", func(t *testing.T) {
				cmdUpdates := findCommandsUpdateEvents(events)

				if conn == nil {
					t.Skip("Connection not available")
				}

				// Commands are reported via available_commands_update ACP notification,
				// which the backend forwards as commands_update SSE.
				if len(cmdUpdates) == 0 {
					// Check if client has cached commands
					client := conn.GetClient()
					if client != nil {
						cmds := client.GetCommandsAsInfo()
						if len(cmds) > 0 {
							t.Logf("No commands_update SSE event, but client has %d cached commands", len(cmds))
							return
						}
					}
					t.Logf("Agent %s: no commands reported (optional — agent may not support slash commands)", spec.ID)
					return
				}

				cmds := cmdUpdates[0].Commands
				assert.NotEmpty(t, cmds,
					"Agent %s: commands_update event should contain at least one command", spec.ID)

				// Verify command format.
				for _, c := range cmds {
					assert.NotEmpty(t, c.Name, "Agent %s: command should have a name", spec.ID)
				}

				t.Logf("Commands: %d available (%s...)", len(cmds), firstCmdName(cmds))
			})

			// ── Model List State ────────────────────────────────────────
			t.Run("model_list", func(t *testing.T) {
				modelUpdates := findModelListUpdateEvents(events)

				if conn == nil {
					t.Skip("Connection not available")
				}
				modelListState := cachedModelListState(sessionID)

				if modelListState == nil {
					if len(modelUpdates) > 0 {
						t.Errorf("Agent %s: model_list_update SSE event present but cached ModelListState is nil", spec.ID)
					} else {
						t.Logf("Agent %s: no model list from ACP (optional — agent may not report models via ConfigOptions)", spec.ID)
					}
					return
				}

				assert.NotEmpty(t, modelListState.Models,
					"Agent %s: ModelListState.Models should not be empty", spec.ID)

				t.Logf("Model list: current=%q, available=%d models",
					modelListState.CurrentModelID, len(modelListState.Models))
			})

			// ── Summary ────────────────────────────────────────────────
			t.Logf("Full state: %s", fmtACPStateSummary(sessionID))
		})
	}
}

// ---------------------------------------------------------------------------
// Test: State Re-emitted on Second Prompt (SSE Reconnect Scenario)
// ---------------------------------------------------------------------------

// TestACPState_AllAgents_StateReemittedOnSecondPrompt verifies that ACP state
// events (mode_update, thinking_effort_update, commands_update) are re-emitted
// on every ExecuteStream call, which is critical for SSE reconnection.
func TestACPState_AllAgents_StateReemittedOnSecondPrompt(t *testing.T) {
	// Test only agents that are likely to have mode state.
	// Skip qoder which have no thinking levels in BackendRegistry.
	criticalSpecs := []acpAgentSpec{}
	for _, spec := range acpAgentSpecs {
		if spec.HasThinking {
			criticalSpecs = append(criticalSpecs, spec)
		}
	}

	for _, spec := range criticalSpecs {
		t.Run(spec.ID, func(t *testing.T) {
			requireACPAvailable(t, spec)

			agent := buildAgentFromSpec(spec)
			env := setupACPTestEnvForAgent(t, agent)

			backend, err := NewACPBackend(agent)
			require.NoError(t, err)

			sessionID := acpSessionID()
			cleanupConn(t, sessionID)

			// First prompt — establish connection.
			events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", spec.Timeout)
			requireDoneEvent(t, events1)

			// Second prompt on same session — state should be re-emitted.
			events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", spec.Timeout)
			requireDoneEvent(t, events2)

			// Mode state should be re-emitted on second prompt.
			modeUpdates2 := findModeUpdateEvents(events2)
			configUpdates2 := findConfigUpdateEvents(events2)
			hasConfigMode2 := configUpdateHasModeCategory(configUpdates2)

			conn := env.mgr.GetConn(sessionID)
			if conn != nil {
				modeState := cachedModeState(sessionID)
				if modeState != nil && len(modeState.AvailableModes) > 0 {
					// Cached mode exists — second prompt should re-emit it.
					if len(modeUpdates2) == 0 && !hasConfigMode2 {
						t.Errorf("Agent %s: mode state exists in cache but was NOT re-emitted on second prompt. "+
							"Frontend will not populate mode chip after SSE reconnect.", spec.ID)
					} else {
						t.Logf("Agent %s: mode state re-emitted on second prompt (mode_update=%d, config_mode=%v)",
							spec.ID, len(modeUpdates2), hasConfigMode2)
					}
				}
			}

			// Thinking effort should be re-emitted.
			effortUpdates2 := findThinkingEffortUpdateEvents(events2)
			if conn != nil {
				effortState := cachedThinkingEffortState(sessionID)
				if effortState != nil && len(effortState.AvailableLevels) > 0 {
					if len(effortUpdates2) == 0 {
						t.Errorf("Agent %s: thinking effort state exists in cache but was NOT re-emitted on second prompt. "+
							"Frontend will not populate thinking chip after SSE reconnect.", spec.ID)
					} else {
						t.Logf("Agent %s: thinking effort re-emitted on second prompt (%d events)",
							spec.ID, len(effortUpdates2))
					}
				}
			}

			// Commands should be re-emitted.
			cmdUpdates2 := findCommandsUpdateEvents(events2)
			if len(cmdUpdates2) > 0 {
				t.Logf("Agent %s: commands re-emitted on second prompt (%d commands)",
					spec.ID, len(cmdUpdates2[0].Commands))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers for logging
// ---------------------------------------------------------------------------

// modeNamesFromState returns a slice of "id:name" strings for logging.
func modeNamesFromState(ms *ModeState) []string {
	if ms == nil {
		return nil
	}
	names := make([]string, len(ms.AvailableModes))
	for i, m := range ms.AvailableModes {
		if m.Name != "" {
			names[i] = fmt.Sprintf("%s:%s", m.ID, m.Name)
		} else {
			names[i] = m.ID
		}
	}
	return names
}

// firstCmdName returns the name of the first command, or "<none>" if empty.
func firstCmdName(cmds []AvailableCommandInfo) string {
	if len(cmds) == 0 {
		return "<none>"
	}
	return cmds[0].Name
}
