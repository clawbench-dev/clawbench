package service

import "testing"

func TestTurnSpecEffectiveTimelineDefaultsToSessionID(t *testing.T) {
	spec := TurnSpec{SessionID: "sess-1"}
	if got := spec.effectiveTimelineSessionID(); got != "sess-1" {
		t.Fatalf("got %q, want sess-1", got)
	}
}

func TestTurnSpecEffectiveTimelineUsesOverride(t *testing.T) {
	spec := TurnSpec{SessionID: "member-1", TimelineSessionID: "group-1"}
	if got := spec.effectiveTimelineSessionID(); got != "group-1" {
		t.Fatalf("got %q, want group-1", got)
	}
}

func TestRunConfigEffectiveTimeline(t *testing.T) {
	c := RunConfig{SessionID: "m", TimelineSessionID: "g"}
	if got := c.effectiveTimelineSessionID(); got != "g" {
		t.Fatalf("got %q, want g", got)
	}
	c2 := RunConfig{SessionID: "s"}
	if got := c2.effectiveTimelineSessionID(); got != "s" {
		t.Fatalf("got %q, want s", got)
	}
}

// SpeakerID and AgentID are distinct fields: they must be independently
// settable (the group orchestrator puts the member row id in SpeakerID while
// AgentID stays the real agent id used for backend resolution).
func TestTurnSpecSpeakerIDIsSeparateFromAgentID(t *testing.T) {
	spec := TurnSpec{SessionID: "member-1", AgentID: "agent-real", SpeakerID: "member-1"}
	if spec.SpeakerID != "member-1" || spec.AgentID != "agent-real" {
		t.Fatalf("SpeakerID=%q AgentID=%q", spec.SpeakerID, spec.AgentID)
	}
}
