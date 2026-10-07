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
	got := buildInjectionText(msgs, 1, "row-b" /*self*/, names, nil, nil, "请回应 A", "")
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
	got := buildInjectionText(msgs, 0, "row-self", names, map[string]bool{"row-gone": true}, nil, "", "")
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
	got := buildInjectionText(msgs, 0, "row-self", map[string]string{}, nil, roster, "", "")
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
	got := buildInjectionText(msgs, 0, "row-self", map[string]string{}, nil, roster, "", "")
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
	got := buildInjectionText(msgs, 0, "row-1", names, nil, nil, "", "")
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
	got := buildInjectionText(msgs, 5, "self", nil, nil, nil, "", "")
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
	other := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "")
	if strings.Contains(other, "create backend") {
		t.Fatalf("warning text must not be injected to another member: %q", other)
	}
	if !strings.Contains(other, "real speech") {
		t.Fatalf("real speech must still be injected: %q", other)
	}

	// A's own warning is filtered by the author rule anyway, but assert the
	// warning text is absent regardless.
	self := buildInjectionText(msgs, 0, "row-a", names, nil, nil, "", "")
	if strings.Contains(self, "create backend") {
		t.Fatalf("warning text must not be injected to the member itself: %q", self)
	}
}

// A message that mixes real content with a warning block must still be injected
// (only PURE-warning messages are excluded) — otherwise a backend that appends
// a warning to a real reply would lose that reply. The warning half must NOT be
// injected (decision #70): asserting only the positive half would let a
// regression that injects the warning pass unnoticed.
func TestBuildMemberInjection_KeepsMixedContent(t *testing.T) {
	mixed := `{"blocks":[{"type":"text","text":"my answer"},{"type":"warning","text":"truncated"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: mixed},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-a": "A"}, nil, nil, "", "")
	if !strings.Contains(got, "my answer") {
		t.Fatalf("a mixed message's real content must be injected: %q", got)
	}
	if strings.Contains(got, "truncated") {
		t.Fatalf("a mixed message's warning half must NOT be injected (decision #70): %q", got)
	}
}

// The host's routing tag is an internal protocol between the backend and the
// host agent. A member must not see it: leaving it in the injected context
// invites the member to imitate `<clawbench-speaker>` in its own output (which
// the frontend would then misrender as a routing card). Only the tag's
// background text belongs in the member's context (decision #67).
func TestBuildMemberInjection_StripsHostRoutingTag(t *testing.T) {
	hostContent := `{"blocks":[{"type":"text","text":"A 的观点不错 <clawbench-speaker>B</clawbench-speaker> 请 B 回应"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "row-host")
	if strings.Contains(got, "<clawbench-speaker>") {
		t.Fatalf("the routing tag must not leak into a member's context: %q", got)
	}
	if !strings.Contains(got, "A 的观点不错") {
		t.Fatalf("the tag's background text must survive: %q", got)
	}
}

// An unparseable host message must be kept verbatim — never strip what we do
// not understand (same contract as askquestion: never lose content).
func TestBuildMemberInjection_KeepsUnparseableHostMessage(t *testing.T) {
	// Malformed payload: empty speaker list.
	hostContent := `{"blocks":[{"type":"text","text":"背景 <clawbench-speaker></clawbench-speaker> 尾巴"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-host": "主持人"}, nil, nil, "", "row-host")
	if !strings.Contains(got, "<clawbench-speaker></clawbench-speaker>") {
		t.Fatalf("a malformed tag must NOT be stripped (never lose content): %q", got)
	}
}

// A non-host member's speech must be untouched even if it happens to contain
// tag-like text (only the host emits routing tags).
func TestBuildMemberInjection_LeavesMemberSpeechAlone(t *testing.T) {
	memberContent := `{"blocks":[{"type":"text","text":"我说 <clawbench-speaker>X</clawbench-speaker> 这些字"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: memberContent},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-a": "A"}, nil, nil, "", "")
	if !strings.Contains(got, "<clawbench-speaker>") {
		t.Fatalf("a member's own text must not be rewritten: %q", got)
	}
}

// A user message with attachments must carry an attachment summary into the
// members' injected context, using the SAME formatter as single chat (decision
// #65) — otherwise members would be asked to discuss a file they cannot see.
// The bubble itself must NOT contain the markers (decision #65: content stays
// clean; only the injection is enriched).
func TestBuildMemberInjection_RendersUserAttachments(t *testing.T) {
	msgs := []model.ChatMessage{
		{
			ID:      2,
			Role:    "user",
			Content: `{"blocks":[{"type":"text","text":"看看这个"}]}`,
			Files: []model.FileEntry{
				{Path: "/tmp/report.pdf"},
			},
		},
	}
	got := buildInjectionText(msgs, 0, "row-a", nil, nil, nil, "", "")
	if !strings.Contains(got, "User uploaded") {
		t.Fatalf("user attachments must be summarized for members: %q", got)
	}
	if !strings.Contains(got, "report.pdf") {
		t.Fatalf("the attachment label must be present: %q", got)
	}
	if !strings.Contains(got, "看看这个") {
		t.Fatalf("the user's own words must survive: %q", got)
	}
}

// A user message WITHOUT attachments must render exactly as before — no empty
// "[User uploaded 0 file(s)]" noise.
func TestBuildMemberInjection_NoAttachmentNoPrefix(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "user", Content: `{"blocks":[{"type":"text","text":"普通消息"}]}`},
	}
	got := buildInjectionText(msgs, 0, "row-a", nil, nil, nil, "", "")
	if strings.Contains(got, "User uploaded") {
		t.Fatalf("a message without attachments must not gain an attachment header: %q", got)
	}
}

// A role='system' timeline row (a membership change, decision #40/#43) must be
// rendered as a neutral event line, NOT as user speech. Its agent_id is "" by
// design (decision #43), so the old "Role==user || AgentID==""" branch captured
// it and rendered "用户: Claude 加入了讨论" — telling the host a member-change was
// something the USER said, which is exactly the input it routes from.
func TestBuildMemberInjection_SystemEventIsNotUserSpeech(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "system", AgentID: "", Content: `{"blocks":[{"type":"text","text":"Claude 加入了讨论"}]}`},
	}
	got := buildInjectionText(msgs, 0, "row-a", nil, nil, nil, "", "")
	if strings.Contains(got, "用户:") {
		t.Fatalf("a system event must not be rendered as user speech: %q", got)
	}
	if !strings.Contains(got, "Claude 加入了讨论") {
		t.Fatalf("the system event text must survive: %q", got)
	}
	if !strings.Contains(got, "系统") {
		t.Fatalf("a system event must be labeled as such: %q", got)
	}
}

// A real user message must still render as user speech (the system fix must not
// break the normal path).
func TestBuildMemberInjection_UserMessageStillUser(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "user", AgentID: "", Content: `{"blocks":[{"type":"text","text":"请讨论"}]}`},
	}
	got := buildInjectionText(msgs, 0, "row-a", nil, nil, nil, "", "")
	if !strings.Contains(got, "用户: 请讨论") {
		t.Fatalf("a user message must render as 用户: ...: %q", got)
	}
}

// The END tag (<clawbench-group-end/>) is internal protocol and must NOT leak
// into a member's injected context, even when the message carries NO speaker
// tag. hostSpeechForMembers only strips when the SPEAKER tag parses (Found), so
// a message that is just the end signal (e.g. "讨论充分。<clawbench-group-end/>
// 结论：…") previously passed through verbatim — inviting members to imitate
// the tag (decision #67's exact rationale).
func TestBuildMemberInjection_StripsEndTagWithoutSpeakerTag(t *testing.T) {
	hostContent := `{"blocks":[{"type":"text","text":"讨论充分。<clawbench-group-end/> 结论：可以发布"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "row-host")

	if strings.Contains(got, "<clawbench-group-end") {
		t.Fatalf("the end tag must not leak into a member's context: %q", got)
	}
	if !strings.Contains(got, "结论：可以发布") {
		t.Fatalf("the end tag's surrounding text must survive: %q", got)
	}
}

// A host message with BOTH tags must strip both (the speaker tag path already
// handles the end tag via Result.Instruction; this pins the combined case).
func TestBuildMemberInjection_StripsBothTags(t *testing.T) {
	hostContent := `{"blocks":[{"type":"text","text":"<clawbench-speaker>A</clawbench-speaker> 请 A 表态 <clawbench-group-end/>"}]}`
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "row-host")

	if strings.Contains(got, "<clawbench-speaker") || strings.Contains(got, "<clawbench-group-end") {
		t.Fatalf("neither tag may leak into a member's context: %q", got)
	}
}
