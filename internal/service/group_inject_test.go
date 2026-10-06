package service

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

func TestBuildMemberInjection(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 1, Role: "user", Content: `{"blocks":[{"type":"text","text":"问题Q"}]}`},
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: `{"blocks":[{"type":"text","text":"A1"}]}`},
		{ID: 3, Role: "assistant", AgentID: "row-b", Content: `{"blocks":[{"type":"text","text":"B1"}]}`},
	}
	names := map[string]string{"row-a": "A", "row-b": "B"}
	got := buildInjectionText(msgs, 1, "row-b" /*self*/, names, nil, nil, "请回应 A")
	if !strings.Contains(got, "A: A1") {
		t.Fatalf("missing A's speech: %q", got)
	}
	if strings.Contains(got, "B: B1") {
		t.Fatalf("must exclude self speech: %q", got)
	}
	if !strings.Contains(got, "请回应 A") {
		t.Fatalf("missing host instruction: %q", got)
	}
}

// A member that has left keeps its speech on the timeline but must be visibly
// marked, so a reader does not treat it as an active participant (decision #39).
func TestBuildMemberInjection_LeftMemberMarked(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-gone", Content: `{"blocks":[{"type":"text","text":"legacy"}]}`},
	}
	names := map[string]string{"row-gone": "Gone"}
	got := buildInjectionText(msgs, 0, "row-self", names, map[string]bool{"row-gone": true}, nil, "")
	if !strings.Contains(got, "Gone（已离场）: legacy") {
		t.Fatalf("left member's speech must be marked 已离场: %q", got)
	}
}

// The roster header lists active participants with their specialties so a
// member knows who it is talking to (decision #41). Members without a
// specialty must not render empty parentheses.
func TestBuildMemberInjection_ParticipantRoster(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 1, Role: "user", Content: `{"blocks":[{"type":"text","text":"Q"}]}`},
	}
	roster := []ParticipantInfo{
		{Name: "Claude", Specialty: "代码编写与推理"},
		{Name: "Plain"},
	}
	got := buildInjectionText(msgs, 0, "row-self", map[string]string{}, nil, roster, "")
	if !strings.Contains(got, "参与者：") {
		t.Fatalf("must render a participant roster header: %q", got)
	}
	if !strings.Contains(got, "Claude（代码编写与推理）") {
		t.Fatalf("roster must include specialties: %q", got)
	}
	if !strings.Contains(got, "Plain") {
		t.Fatalf("roster must include members without a specialty: %q", got)
	}
	if strings.Contains(got, "Plain（") {
		t.Fatalf("member without specialty must not render empty parentheses: %q", got)
	}
}

// An all-empty roster (nobody has a specialty) must be omitted rather than
// rendered as a bare "参与者：" line — it would be pure noise.
func TestBuildMemberInjection_RosterOmittedWhenNoSpecialty(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 1, Role: "user", Content: `{"blocks":[{"type":"text","text":"Q"}]}`},
	}
	roster := []ParticipantInfo{{Name: "A"}, {Name: "B"}}
	got := buildInjectionText(msgs, 0, "row-self", map[string]string{}, nil, roster, "")
	if strings.Contains(got, "参与者：") {
		t.Fatalf("roster with no specialties must be omitted entirely: %q", got)
	}
}

// Same agent added twice must still let each member see the other: identity is
// the member ROW id, not the agent id.
func TestBuildMemberInjection_SameAgentTwoMembers(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 10, Role: "assistant", AgentID: "row-1", Content: `{"blocks":[{"type":"text","text":"from one"}]}`},
		{ID: 11, Role: "assistant", AgentID: "row-2", Content: `{"blocks":[{"type":"text","text":"from two"}]}`},
	}
	names := map[string]string{"row-1": "One", "row-2": "Two"}
	got := buildInjectionText(msgs, 0, "row-1", names, nil, nil, "")
	if !strings.Contains(got, "Two: from two") {
		t.Fatalf("member row-1 must see row-2's speech: %q", got)
	}
	if strings.Contains(got, "One: from one") {
		t.Fatalf("member row-1 must not replay its own speech: %q", got)
	}
}

func TestBuildMemberInjection_CursorExcludesOlder(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 5, Role: "user", Content: `{"blocks":[{"type":"text","text":"old"}]}`},
		{ID: 6, Role: "user", Content: `{"blocks":[{"type":"text","text":"new"}]}`},
	}
	got := buildInjectionText(msgs, 5, "self", nil, nil, nil, "")
	if strings.Contains(got, "old") {
		t.Fatalf("cursor must exclude id<=cursor: %q", got)
	}
	if !strings.Contains(got, "new") {
		t.Fatalf("cursor must include id>cursor: %q", got)
	}
}

// participantMetadata must exclude the speaker itself and classify left members
// separately, so the header never tells a member about itself.
func TestParticipantMetadata(t *testing.T) {
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"agent-claude": {ID: "agent-claude", Specialty: "代码编写与推理"},
	}
	defer func() { model.Agents = origAgents }()

	members := []GroupMember{
		{ID: "row-self", AgentID: "agent-claude", Name: "Self"},
		{ID: "row-other", AgentID: "agent-claude", Name: "Other"},
		{ID: "row-gone", AgentID: "agent-claude", Name: "Gone", Left: true},
	}
	leftIDs, roster := participantMetadata(members, "row-self")
	if !leftIDs["row-gone"] {
		t.Fatalf("left member must be in leftIDs: %v", leftIDs)
	}
	if leftIDs["row-self"] || leftIDs["row-other"] {
		t.Fatalf("active members must not be in leftIDs: %v", leftIDs)
	}
	if len(roster) != 1 || roster[0].Name != "Other" {
		t.Fatalf("roster must exclude self and left members, got %+v", roster)
	}
	if roster[0].Specialty != "代码编写与推理" {
		t.Fatalf("roster entry must carry the specialty, got %q", roster[0].Specialty)
	}
}

// A member's failed turn leaves a warning block on the timeline (role=assistant,
// that member's agent_id). It is operational information for the USER, not
// discussion content: injecting it would feed raw errors like "create backend:
// ..." to the other members as if it were speech (decision #70). The failure
// stays on the timeline (decision #69) — it is only excluded from injection.
//
// NOTE: this already holds by construction — ExtractPlainText goes through
// extractTextsFromArray (chat.go:590), which skips every block whose type is
// not "text" (thinking/tool_use/warning). This test PINS that invariant so a
// future loosening of that filter cannot silently start leaking failures.
func TestBuildMemberInjection_ExcludesWarningBlocks(t *testing.T) {
	warningContent := `{"blocks":[{"type":"warning","text":"create backend: exit status 1"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: warningContent},
		{ID: 3, Role: "assistant", AgentID: "row-a", Content: `{"blocks":[{"type":"text","text":"real speech"}]}`},
	}
	names := map[string]string{"row-a": "A"}

	// A different member must not see A's failure.
	other := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "")
	if strings.Contains(other, "create backend") {
		t.Fatalf("warning text must not be injected to another member: %q", other)
	}
	if !strings.Contains(other, "real speech") {
		t.Fatalf("real speech must still be injected: %q", other)
	}

	// A's own warning is filtered by the author rule anyway, but assert the
	// warning text is absent regardless.
	self := buildInjectionText(msgs, 0, "row-a", names, nil, nil, "")
	if strings.Contains(self, "create backend") {
		t.Fatalf("warning text must not be injected to the member itself: %q", self)
	}
}

// A message that mixes real content with a warning block must still be injected
// (only PURE-warning messages are excluded) — otherwise a backend that appends
// a warning to a real reply would lose that reply.
func TestBuildMemberInjection_KeepsMixedContent(t *testing.T) {
	mixed := `{"blocks":[{"type":"text","text":"my answer"},{"type":"warning","text":"truncated"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: mixed},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-a": "A"}, nil, nil, "")
	if !strings.Contains(got, "my answer") {
		t.Fatalf("a mixed message's real content must be injected: %q", got)
	}
}
