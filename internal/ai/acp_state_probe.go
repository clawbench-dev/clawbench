package ai

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACP state probe — "refresh all" for one agent
// ---------------------------------------------------------------------------

// ACPStateProbeResult is what a probe learned from the agent itself.
//
// The probe exists because the CLI-discovered model list is a GUESS: the
// authority for models, thinking levels, modes and slash commands is the agent's
// own session/new response (plus the available_commands_update it pushes right
// after). A manual "refresh" must therefore ask the agent, not a sidecar CLI.
type ACPStateProbeResult struct {
	Models         []model.AgentModel
	Modes          []ModeDef
	Efforts        []ThinkingEffortDef
	Commands       []AvailableCommandInfo
	ConfigState    *ConfigOptionState
	CurrentModelID string
	// CurrentModeID / CurrentEffortID are what the agent reports for itself.
	// They are NOT the user's selection (that lives in the session/DB); the
	// refresh endpoint ships them only so a client cache update does not blank
	// the ids it already holds.
	CurrentModeID   string
	CurrentEffortID string
	// SessionDeleted reports whether session/delete succeeded. Best-effort: a
	// failure is reported to the caller (so it can be logged) but never fails
	// the refresh — the state was already collected at that point.
	SessionDeleted bool
	DeleteError    string
}

// acpStateProbe is the implementation the refresh endpoint calls. Indirected
// through a variable so tests can answer without spawning a real agent process
// (a unit test that silently starts codebuddy would be slow, side-effecting and
// environment-dependent).
var acpStateProbe = ProbeACPState

// RunACPStateProbe is the entry point for the refresh endpoint. It goes through
// the replaceable hook so a handler test can exercise the whole refresh path
// without an agent binary (see SetACPStateProbeForTest).
func RunACPStateProbe(ctx context.Context, agent *model.Agent, cwd string) (*ACPStateProbeResult, error) {
	return acpStateProbe(ctx, agent, cwd)
}

// SetACPStateProbeForTest replaces the probe and returns a restore function.
// Production code must not call this.
func SetACPStateProbeForTest(fn func(ctx context.Context, agent *model.Agent, cwd string) (*ACPStateProbeResult, error)) func() {
	prev := acpStateProbe
	acpStateProbe = fn
	return func() { acpStateProbe = prev }
}

// ProbeACPState spawns the agent, creates ONE throwaway ACP session, collects
// what the agent reports about itself, writes it into the capability registry
// and then deletes the session again.
//
// The connection is deliberately NOT taken from the pool: a probe must never
// disturb a live conversation, and a pooled entry would also leak the probe's
// session-level current model into GetCurrentModelIDByAgentID (which would make
// the picker claim a model the user never chose).
//
// Registry writes use the merge-style Update* helpers rather than ForceUpdate:
// ForceUpdate replaces the whole capability struct, and the probe has no
// commands at handshake time (they arrive as notifications), so it would wipe
// the agent's slash commands.
func ProbeACPState(ctx context.Context, agent *model.Agent, cwd string) (*ACPStateProbeResult, error) {
	if agent == nil || !agent.SupportsACP() {
		return nil, fmt.Errorf("acp: state probe needs an agent with an acp command")
	}
	if cwd == "" {
		return nil, fmt.Errorf("acp: state probe needs a working directory (session/new requires an absolute cwd)")
	}

	// A key that cannot collide with a real ClawBench session id. The connection
	// is local to this call (never registered in the pool), the key only shows up
	// in logs and in getExternalSessionID (which returns "" for unknown ids, so
	// ensureAliveWithSession takes the NewSession branch).
	conn := newACPConn(agent, "__state_probe__:"+agent.ID)
	defer conn.close()

	start := time.Now()
	if _, err := conn.ensureAliveWithSession(ctx, cwd); err != nil {
		return nil, err
	}
	slog.Info("acp probe: session created",
		"agent", agent.ID, "acp_sid", conn.AcpSID(), "elapsed", time.Since(start))

	resp := conn.GetAndClearNewSessionResp()
	if resp == nil {
		return nil, fmt.Errorf("acp: state probe got no session/new response")
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return resp, nil
	})

	// Slash commands are pushed as notifications, not carried by session/new, so
	// they need a bounded settle window (see probeCommandWindow).
	maxWait, quiet := probeCommandWindow(agent)
	cmds := waitForProbeCommands(ctx, conn, maxWait, quiet)

	result := &ACPStateProbeResult{
		Models:          ext.models,
		Modes:           ext.modes,
		Efforts:         ext.efforts,
		Commands:        cmds,
		ConfigState:     ext.configState,
		CurrentModelID:  ext.modelCurrentID,
		CurrentModeID:   ext.modeCurrentID,
		CurrentEffortID: ext.effortCurrentID,
	}

	reg := GetAgentCapabilityRegistry()
	// A backend whose `modes` block duplicates its `thought_level` option must not
	// surface both — same rule as applyExtractedState, otherwise a refresh would
	// resurrect the duplicate picker that path deliberately suppresses.
	if len(ext.modes) > 0 && acpModesMirrorThoughtLevel(agent.Backend) {
		ext.modes = nil
		result.Modes = nil
	}
	if len(ext.modes) > 0 {
		reg.UpdateModes(agent.ID, ext.modes)
	}
	if len(ext.efforts) > 0 {
		reg.UpdateThinkingEfforts(agent.ID, ext.efforts)
	}
	if len(ext.models) > 0 {
		reg.UpdateModels(agent.ID, ext.models)
	}
	if ext.configState != nil {
		reg.UpdateConfigState(agent.ID, ext.configState)
	}
	// Empty means "the agent pushed nothing within the window" — keep the
	// previously known list rather than blanking the slash menu.
	if len(cmds) > 0 {
		reg.UpdateCommands(agent.ID, cmds)
	}

	// Delete the throwaway session on the agent side. Attempted even when the
	// agent did not advertise the capability: a probe session left behind is an
	// orphan in the agent's own store, and CodeBuddy does not advertise
	// sessionCapabilities at all while implementing session/delete.
	delCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.sendDeleteSession(delCtx); err != nil {
		result.DeleteError = err.Error()
		slog.Warn("acp probe: session/delete failed (best-effort)",
			"agent", agent.ID, "acp_sid", conn.AcpSID(), "error", err)
	} else {
		result.SessionDeleted = true
	}

	slog.Info("acp probe: collected agent state",
		"agent", agent.ID,
		"models", len(result.Models),
		"modes", len(result.Modes),
		"efforts", len(result.Efforts),
		"commands", len(result.Commands),
		"session_deleted", result.SessionDeleted,
		"elapsed", time.Since(start))
	return result, nil
}

// probeCommandWindow returns how long to wait for available_commands_update and
// how long the list must stay unchanged to count as settled.
//
// CodeBuddy sends its built-in commands immediately and the plugin/skill ones
// only after its PluginManager finishes loading (~4s, see
// codebuddyPluginLoadDelay). A short quiet window would therefore return the
// built-in list only — i.e. the refresh would silently drop exactly the commands
// that were missing in the first place.
func probeCommandWindow(agent *model.Agent) (maxWait, quiet time.Duration) {
	if isCodeBuddyBackend(agent) {
		return codebuddyPluginLoadDelay + time.Second, codebuddyPluginLoadDelay
	}
	return 1500 * time.Millisecond, 400 * time.Millisecond
}

// waitForProbeCommands blocks until the agent's pushed command list stops
// growing (or the window closes) and returns the cached list.
//
// Reads the client cache, not the registry: the client is where
// SessionUpdate merges available_commands_update with the pre-scanned plugin
// commands, and that merge is what the running sessions use.
func waitForProbeCommands(ctx context.Context, conn *ACPConn, maxWait, quiet time.Duration) []AvailableCommandInfo {
	client := conn.GetClient()
	if client == nil {
		return nil
	}
	deadline := time.Now().Add(maxWait)
	last := len(client.GetCommands())
	stableSince := time.Now()
	for {
		n := len(client.GetCommands())
		if n != last {
			last = n
			stableSince = time.Now()
		}
		if n > 0 && time.Since(stableSince) >= quiet {
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return client.GetCommandsAsInfo()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return client.GetCommandsAsInfo()
}
