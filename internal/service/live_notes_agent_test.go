package service

import (
	"context"
	"strings"
	"testing"

	"clawbench/internal/model"
)

// withAgentMap installs a fresh agent map for the duration of a test and
// restores the previous one.
//
// The global map is nil in a bare test binary (nothing loaded agents), so
// writing to it panics — the failure mode is "assignment to entry in nil map",
// which reads like a product bug rather than a fixture gap.
func withAgentMap(t *testing.T) {
	t.Helper()
	prevMap := model.Agents
	prevList := model.AgentList
	model.Agents = map[string]*model.Agent{}
	model.AgentList = nil
	t.Cleanup(func() {
		model.Agents = prevMap
		model.AgentList = prevList
	})
}

// TestBuiltinAgentAvailable_ReportsWhyNot pins the three independent reasons the
// built-in agent can be unusable.
//
// The caller shows a different remedy for each ("install it", "enable it", "bind
// a provider"), so collapsing them into one error would leave the user guessing.
func TestBuiltinAgentAvailable_ReportsWhyNot(t *testing.T) {
	tests := []struct {
		name string
		// setup mutates the global agent map; nil means "leave it absent".
		agent *model.Agent
		want  string
	}{
		{name: "absent", agent: nil, want: "not installed"},
		{name: "disabled", agent: &model.Agent{ID: BuiltinAgentID, Disabled: true}, want: "disabled"},
		{name: "no provider", agent: &model.Agent{ID: BuiltinAgentID}, want: "no model provider bound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withAgentMap(t)
			if tc.agent == nil {
				delete(model.Agents, BuiltinAgentID)
			} else {
				model.Agents[BuiltinAgentID] = tc.agent
			}

			_, err := builtinAgentAvailable()
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q in %q", tc.want, err.Error())
			}
			if BuiltinAgentAvailable() {
				t.Fatal("BuiltinAgentAvailable must agree with builtinAgentAvailable")
			}
		})
	}
}

// TestRunBuiltinAgentTask_FailsWithoutAgent pins that a background task reports
// a clear reason instead of calling a nil endpoint.
func TestRunBuiltinAgentTask_FailsWithoutAgent(t *testing.T) {
	withAgentMap(t)

	_, err := RunBuiltinAgentTask(context.Background(), BuiltinAgentTask{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error when the agent is not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected an install hint, got %q", err.Error())
	}
}

// TestAgentMinutesPolicy_IsReadOnly pins the tool grant for minutes generation.
//
// Producing minutes is transcription and lookup. Granting write would let a
// background job silently edit the project while nobody is watching, and there
// is nothing in the task that needs it — the caller writes the artifact.
func TestAgentMinutesPolicy_IsReadOnly(t *testing.T) {
	p := agentMinutesPolicy(0).Normalized()
	if !p.Has(model.CapFSRead) {
		t.Fatal("minutes generation must be able to read project files")
	}
	if p.Has(model.CapFSWrite) {
		t.Fatal("minutes generation must not write files")
	}
	if p.Has(model.CapExec) {
		t.Fatal("minutes generation must not run commands")
	}
}

// TestAgentMinutesPolicy_HonoursRoundBudget pins that the config knob reaches
// the policy.
func TestAgentMinutesPolicy_HonoursRoundBudget(t *testing.T) {
	if got := agentMinutesPolicy(7).MaxRounds; got != 7 {
		t.Fatalf("expected the configured budget, got %d", got)
	}
	// 0 means unlimited; the policy expresses it by leaving MaxRounds zero.
	if got := agentMinutesPolicy(0).Rounds(); got != 0 {
		t.Fatalf("expected no round ceiling, got %d", got)
	}
}

// TestAgentMinutesFallBackWhenUnavailable pins that a meeting never fails just
// because the built-in agent is unusable.
//
// The user is waiting on minutes for a meeting that already happened. The
// summary path produces a usable document, so falling back is strictly better
// than surfacing "the built-in agent is disabled" as the reason there are no
// minutes.
func TestAgentMinutesFallBackWhenUnavailable(t *testing.T) {
	withAgentMap(t)
	prevCfg := model.ConfigInstance.Meeting.AgentMinutes
	t.Cleanup(func() { model.ConfigInstance.Meeting.AgentMinutes = prevCfg })

	// Agent mode on, agent absent: the summary path runs and fails on its own
	// precondition (no summary model), NOT on the agent.
	model.ConfigInstance.Meeting.AgentMinutes = true

	_, err := GenerateLiveNotesMinutesWithOptions(
		context.Background(), "t", "someone said something", "", LiveNotesSpeaker{},
		LiveNotesMinutesOptions{WorkDir: "/tmp"},
	)
	if err == nil {
		t.Fatal("expected the summary path's own precondition error")
	}
	if strings.Contains(err.Error(), "built-in agent") {
		t.Fatalf("an unavailable agent must fall back silently, got %q", err.Error())
	}
}

// TestAgentMinutesSkipsAgentWithoutWorkDir pins the other half of the fallback:
// an agent with no working directory can only see the text it was handed, which
// is exactly what the summary path already does. Running it would cost
// tool-less round-trips for an identical result.
func TestAgentMinutesSkipsAgentWithoutWorkDir(t *testing.T) {
	prevCfg := model.ConfigInstance.Meeting.AgentMinutes
	t.Cleanup(func() { model.ConfigInstance.Meeting.AgentMinutes = prevCfg })

	model.ConfigInstance.Meeting.AgentMinutes = true

	_, err := GenerateLiveNotesMinutesWithOptions(
		context.Background(), "t", "someone said something", "", LiveNotesSpeaker{},
		LiveNotesMinutesOptions{}, // WorkDir deliberately empty
	)
	if err == nil {
		t.Fatal("expected the summary path's own precondition error")
	}
	if strings.Contains(err.Error(), "agent") {
		t.Fatalf("an empty workdir must not attempt an agent run, got %q", err.Error())
	}
}
