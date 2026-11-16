package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// Auto-probe of an ACP agent's capabilities on its first session.
//
// An ACP agent's model list, thinking levels, modes and slash commands are not
// local knowledge: the agent reports them during its session handshake, and the
// AgentCapabilityRegistry persists them. A freshly installed agent therefore has
// NOTHING to show — the pickers are empty until the first message triggers a
// handshake, which is exactly the state a user reads as "this agent is broken".
//
// So when a session is created for an agent whose capability snapshot is still
// empty, we run the same probe the manual "refresh" button runs (see
// ai.ProbeACPState: one throwaway session, then delete it) in the background. The
// probe writes the registry, which persists on its own, and then we tell clients
// the agent list changed so every open picker fills in.
//
// Failures are silent by design: the probe spawns a real process and can fail
// for ordinary reasons (not signed in, no network, a slow cold start). The first
// real turn runs the same handshake and would populate everything anyway, so a
// failure here only means the lists stay empty a little longer. Logging it and
// moving on is better than an error toast for something the user did not ask for
// and cannot act on.

const (
	// autoProbeTimeout bounds one automatic probe. It matches the manual
	// refresh budget (acpProbeTimeout) — the work is identical; only the trigger
	// differs.
	autoProbeTimeout = acpProbeTimeout
)

// autoProbeInFlight tracks agents with a probe already running, so creating
// three sessions in a row does not spawn three processes for the same agent.
var (
	autoProbeMu       sync.Mutex
	autoProbeInFlight = map[string]bool{}
)

// maybeAutoProbeAgentState starts a background capability probe when the agent
// has never reported anything, and returns immediately.
//
// Deliberately called on session CREATION, not on every turn: the point is to
// fill the empty pickers before the user picks anything, and a turn already
// performs a handshake on its own.
func maybeAutoProbeAgentState(agent *model.Agent, projectPath string) {
	if agent == nil || projectPath == "" {
		return
	}
	// Only ACP agents have capabilities to report. A CLI agent's model list
	// comes from discovery, which already ran.
	if !agentUsesACP(agent) {
		return
	}
	// "Nothing reported yet" is the same predicate GET /api/agents uses to
	// decide whether to gate the CLI list (see resolveAgentModelLists). Using it
	// here keeps the two in step: an agent that shows the catalog fallback is
	// exactly an agent whose probe would still help.
	reg := ai.GetAgentCapabilityRegistry()
	if cap := reg.Get(agent.ID); cap != nil && cap.HasData() {
		return
	}

	autoProbeMu.Lock()
	if autoProbeInFlight[agent.ID] {
		autoProbeMu.Unlock()
		return
	}
	autoProbeInFlight[agent.ID] = true
	autoProbeMu.Unlock()

	// Snapshot: the probe runs a real process, and the shared pointer may be
	// replaced by a concurrent reload meanwhile (same reason the manual refresh
	// snapshots).
	snapshot := *agent

	go func() {
		defer func() {
			autoProbeMu.Lock()
			delete(autoProbeInFlight, snapshot.ID)
			autoProbeMu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), autoProbeTimeout)
		defer cancel()

		start := time.Now()
		res, err := ai.RunACPStateProbe(ctx, &snapshot, projectPath)
		if err != nil {
			slog.Info("auto probe of agent state failed; the first turn will populate it",
				"agent", snapshot.ID,
				"elapsed", time.Since(start),
				"error", err)
			return
		}
		slog.Info("auto probe of agent state done",
			"agent", snapshot.ID,
			"models", len(res.Models),
			"modes", len(res.Modes),
			"efforts", len(res.Efforts),
			"commands", len(res.Commands),
			"elapsed", time.Since(start))

		// Tell every client the agent list changed: loadAgents re-reads
		// /api/agents, which now carries the freshly reported lists. Same event
		// the background model discovery broadcasts, so no new client wiring.
		if mgr := ws.GetManager(); mgr != nil {
			mgr.BroadcastEvent(ws.ServerMessage{
				Type:  ws.MessageTypeEvent,
				ID:    ws.GenerateEventID(),
				Event: "agents_updated",
			})
		}
	}()
}
