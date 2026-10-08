package service

import (
	"encoding/json"
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

// assistantText builds a chat_history content JSON for an assistant message
// whose single text block is `text`, so tests can embed quotes/tags safely.
func assistantText(text string) string {
	b, _ := json.Marshal(map[string]any{"blocks": []any{map[string]any{"type": "text", "text": text}}})
	return string(b)
}

// An agent's mention tag is an internal protocol between the backend and the
// speaker. A member must not see the RAW tag (leaving it in invites imitation,
// which the frontend would then misrender as a routing card) — but a PUBLIC
// mention is rendered as readable "@name body" prose, because in free mode a
// member's @ is what drives the relay chain (decision #67 / free-mode §13.3).
func TestBuildMemberInjection_RendersMentionReadable(t *testing.T) {
	hostContent := assistantText(`A 的观点不错 <clawbench-mention targets="B">请 B 回应</clawbench-mention>`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "")
	if strings.Contains(got, "<clawbench-mention") {
		t.Fatalf("the raw mention tag must not leak into a member's context: %q", got)
	}
	if !strings.Contains(got, "A 的观点不错") {
		t.Fatalf("the surrounding text must survive: %q", got)
	}
	if !strings.Contains(got, "@B 请 B 回应") {
		t.Fatalf("a public mention must render as readable @name prose: %q", got)
	}
}

// A malformed mention must still have its tag stripped (a member must never see
// the raw protocol, malformed or not), while the surrounding text survives.
func TestBuildMemberInjection_StripsMalformedMentionTag(t *testing.T) {
	hostContent := assistantText(`背景 <clawbench-mention targets=""></clawbench-mention> 尾巴`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-host": "主持人"}, nil, nil, "", "")
	if strings.Contains(got, "<clawbench-mention") {
		t.Fatalf("a mention tag must NOT reach a member (they imitate it): %q", got)
	}
	if !strings.Contains(got, "背景") || !strings.Contains(got, "尾巴") {
		t.Fatalf("the surrounding text must survive: %q", got)
	}
}

// EVERY agent's speech is rendered readable, not just the host's: in free mode
// a member's @ must be visible to the relay chain, so a member's mention is
// rendered as @name prose (the tag itself still never survives).
func TestBuildMemberInjection_RendersMemberMentionToo(t *testing.T) {
	memberContent := assistantText(`我说 <clawbench-mention targets="X">你来补充</clawbench-mention> 这些字`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: memberContent},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-a": "A"}, nil, nil, "", "")
	if strings.Contains(got, "<clawbench-mention") {
		t.Fatalf("a member's raw mention tag must not survive either: %q", got)
	}
	if !strings.Contains(got, "@X 你来补充") {
		t.Fatalf("a member's mention must render readable: %q", got)
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
// it and rendered "User: Claude 加入了讨论" — telling the host a member-change was
// something the USER said, which is exactly the input it routes from.
func TestBuildMemberInjection_SystemEventIsNotUserSpeech(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "system", AgentID: "", Content: `{"blocks":[{"type":"text","text":"Claude 加入了讨论"}]}`},
	}
	got := buildInjectionText(msgs, 0, "row-a", nil, nil, nil, "", "")
	if strings.Contains(got, "User:") {
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
	// The prefix is the user's reserved participant name (English), the SAME
	// name the host addresses in a routing tag.
	if !strings.Contains(got, "User: 请讨论") {
		t.Fatalf("a user message must render as User: ...: %q", got)
	}
}

// A user's @-mention carries the member ROW id (the frontend writes ids). It
// must be rendered readable AND resolved to the display name: a member must see
// "User: @Alice 请你说说", not the raw protocol tag with a UUID — the raw tag
// would both leak the protocol (members imitate it) and be unreadable.
func TestBuildMemberInjection_ResolvesUserMentionIDToName(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 2, Role: "user", AgentID: "", Content: assistantText(`<clawbench-mention targets="row-a"></clawbench-mention> 请你说说`)},
	}
	names := map[string]string{"row-a": "Alice", "row-b": "Bob"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "")
	if strings.Contains(got, "<clawbench-mention") {
		t.Fatalf("the raw mention tag must not leak into a member's context: %q", got)
	}
	if strings.Contains(got, "row-a") {
		t.Fatalf("the member ROW id must be resolved to a name: %q", got)
	}
	if !strings.Contains(got, "User: @Alice 请你说说") {
		t.Fatalf("the user's mention must render as @<name>: %q", got)
	}
}

// The END tag (<clawbench-group-end/>) is internal protocol and must NOT leak
// into a member's injected context, even when the message carries NO mention
// tag — a message that is just the end signal (e.g. "讨论充分。<clawbench-group-end/>
// 结论：…") must not pass through verbatim (decision #67's rationale).
func TestBuildMemberInjection_StripsEndTagWithoutMentionTag(t *testing.T) {
	hostContent := assistantText(`讨论充分。<clawbench-group-end/> 结论：可以发布`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "")

	if strings.Contains(got, "<clawbench-group-end") {
		t.Fatalf("the end tag must not leak into a member's context: %q", got)
	}
	if !strings.Contains(got, "结论：可以发布") {
		t.Fatalf("the end tag's surrounding text must survive: %q", got)
	}
}

// A host message with BOTH a mention and the end tag must strip both.
func TestBuildMemberInjection_StripsBothTags(t *testing.T) {
	hostContent := assistantText(`<clawbench-mention targets="A">请 A 表态</clawbench-mention> <clawbench-group-end/>`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	names := map[string]string{"row-host": "主持人"}
	got := buildInjectionText(msgs, 0, "row-b", names, nil, nil, "", "")

	if strings.Contains(got, "<clawbench-mention") || strings.Contains(got, "<clawbench-group-end") {
		t.Fatalf("neither tag may leak into a member's context: %q", got)
	}
}

// ── BCC (密送) ────────────────────────────────────────────────────────────
//
// A private note reaches ONLY its target member. It is rendered as a closing
// paragraph after the public instruction; every other member's context must be
// free of it — and of the raw tag.

func TestBuildMemberInjection_PrivateNoteOnlyToTarget(t *testing.T) {
	got := buildInjectionText(nil, 0, "row-a", map[string]string{}, nil, nil, "公共指令", "只给A的话")
	if !strings.Contains(got, "主持人密送给你") {
		t.Fatalf("the target must receive its private note: %q", got)
	}
	if !strings.Contains(got, "只给A的话") {
		t.Fatalf("the note's content must be present: %q", got)
	}
	// Public directive comes first, private note after (叠加 contract).
	if strings.Index(got, "公共指令") > strings.Index(got, "只给A的话") {
		t.Fatalf("public instruction must precede the private note: %q", got)
	}

	// A different member (no bcc passed) must not carry the note.
	other := buildInjectionText(nil, 0, "row-b", map[string]string{}, nil, nil, "公共指令", "")
	if strings.Contains(other, "只给A的话") || strings.Contains(other, "主持人密送") {
		t.Fatalf("a non-target member must not receive the note: %q", other)
	}
}

// A host message whose private note is well-formed but whose public mention is
// missing must STILL have the note removed from the shared body — that fallback
// path is the one place a note could leak to everyone.
func TestBuildMemberInjection_StripsNoteOnFallbackPath(t *testing.T) {
	hostContent := assistantText(`背景在此 <clawbench-mention targets="A" private>只给A</clawbench-mention>`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-host": "主持人"}, nil, nil, "", "")
	if strings.Contains(got, "只给A") {
		t.Fatalf("a note must not leak to a non-target member via the fallback path: %q", got)
	}
	if !strings.Contains(got, "背景在此") {
		t.Fatalf("the background must survive: %q", got)
	}
}

// A private note alongside a public mention must not leak into the shared body.
func TestBuildMemberInjection_NoteNotInInstruction(t *testing.T) {
	hostContent := assistantText(`<clawbench-mention targets="A,B">请表态</clawbench-mention><clawbench-mention targets="A" private>私密</clawbench-mention>`)
	msgs := []model.ChatMessage{
		{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
	}
	// Member B: no private note, and the shared body must not carry A's note.
	got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-host": "主持人"}, nil, nil, "", "")
	if strings.Contains(got, "私密") {
		t.Fatalf("note leaked into the shared body: %q", got)
	}
	if !strings.Contains(got, "请表态") {
		t.Fatalf("the public body must survive: %q", got)
	}
}

// joinPendingBcc renders the notes accumulated for one target, joined. The
// per-target filtering now happens in the store (pendingBccForTarget), so this
// only formats what it is given.
func TestJoinPendingBcc(t *testing.T) {
	if got := joinPendingBcc(nil); got != "" {
		t.Fatalf("no notes must render empty: %q", got)
	}
	pending := []pendingBcc{{ID: 1, Content: "给A一"}, {ID: 2, Content: "给A二"}}
	if got := joinPendingBcc(pending); got != "给A一\n\n给A二" {
		t.Fatalf("joined: %q", got)
	}
}

// A malformed private note must not reach a non-target member through the
// fallback path either — the injection boundary is fail-closed.
func TestBuildMemberInjection_StripsMalformedNoteOnFallbackPath(t *testing.T) {
	cases := []struct{ name, hostText string }{
		{"single_quotes", `背景 <clawbench-mention targets='A' private>秘密</clawbench-mention>`},
		{"unclosed", `背景 <clawbench-mention targets="A" private>秘密`},
		{"no_attr", `背景 <clawbench-mention private>秘密</clawbench-mention>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hostContent := assistantText(tc.hostText)
			msgs := []model.ChatMessage{
				{ID: 2, Role: "assistant", AgentID: "row-host", Content: hostContent},
			}
			got := buildInjectionText(msgs, 0, "row-b", map[string]string{"row-host": "主持人"}, nil, nil, "", "")
			if strings.Contains(got, "秘密") {
				t.Fatalf("a malformed note must not leak to a non-target member: %q", got)
			}
			if !strings.Contains(got, "背景") {
				t.Fatalf("background must survive: %q", got)
			}
		})
	}
}

// A member must SEE the host's rules/announcement. Those live AFTER the routing
// tag (Instruction), which hostSpeechForMembers used to discard — so members
// never saw the game rules and behaved as if unaddressed. The protocol tags
// inside that text must still be stripped, or a member imitates them and
// declares itself the chair (the real "everyone is the host" incident).
func TestRenderMentionsReadable_KeepsRulesDropsTagsAndPrivate(t *testing.T) {
	hostMsg := "<clawbench-mention targets=\"A,B\">本轮规则：每人一句话描述。</clawbench-mention>\n" +
		"<clawbench-mention targets=\"A\">请 A 先描述</clawbench-mention><clawbench-mention targets=\"A\" private>你的词是西瓜</clawbench-mention>"
	got := renderMentionsReadable(hostMsg, nil)
	if !strings.Contains(got, "本轮规则") {
		t.Fatalf("member must see the host's rules; got %q", got)
	}
	if !strings.Contains(got, "请 A 先描述") {
		t.Fatalf("member must see the host's public directive; got %q", got)
	}
	if strings.Contains(got, "clawbench-mention") {
		t.Fatalf("mention tags must be stripped so members do not imitate them; got %q", got)
	}
	if !strings.Contains(got, "@A @B") {
		t.Fatalf("public mention targets must render as @name; got %q", got)
	}
	if strings.Contains(got, "西瓜") {
		t.Fatalf("private note must never reach the shared body; got %q", got)
	}
}

// A message with no mention tag keeps its full text (minus the end signal).
func TestRenderMentionsReadable_NoTagKeepsText(t *testing.T) {
	got := renderMentionsReadable("大家注意，本局是友谊赛。", nil)
	if got != "大家注意，本局是友谊赛。" {
		t.Fatalf("plain speech must pass through; got %q", got)
	}
}
