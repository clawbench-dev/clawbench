package service

import (
	"strings"
	"testing"
)

func TestBuildHostSystemPrompt(t *testing.T) {
	members := []HostMemberInfo{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	p := BuildHostSystemPrompt(members)
	if !strings.Contains(p, "<clawbench-mention targets=") {
		t.Fatal("must document the mention tag")
	}
	if !strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("must document end signal")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("must ask for summary after end tag")
	}
	for _, m := range members {
		if !strings.Contains(p, m.Name) {
			t.Fatalf("member %q missing from prompt", m.Name)
		}
	}
	// The host must be told explicitly not to name itself (the self-route loop
	// defect: the host named itself and every "member turn" was a host turn).
	if !strings.Contains(p, "不要点名你自己") {
		t.Fatal("prompt must forbid the host from naming itself")
	}
}

// TestBuildHostSystemPrompt_Specialty: the host routes better when it knows what
// each member is good at (decision #39). A member with a specialty renders as
// "Name（specialty）"; one without renders as a bare name — no empty "（）".
func TestBuildHostSystemPrompt_Specialty(t *testing.T) {
	p := BuildHostSystemPrompt([]HostMemberInfo{
		{Name: "Claude", Specialty: "代码编写与推理"},
		{Name: "Plain"},
	})
	if !strings.Contains(p, "Claude（代码编写与推理）") {
		t.Fatalf("specialty must render in parentheses after the name: %q", p)
	}
	if !strings.Contains(p, "Plain") {
		t.Fatalf("member without specialty must still be listed: %q", p)
	}
	if strings.Contains(p, "Plain（") {
		t.Fatalf("member without specialty must NOT render empty parentheses: %q", p)
	}
}

// TestBuildHostSystemPrompt_LeftMarked: a member that left keeps its slot in the
// prompt list (its past speech is still on the timeline) but must be visibly
// marked so the host does not route to it (decision #39).
func TestBuildHostSystemPrompt_LeftMarked(t *testing.T) {
	p := BuildHostSystemPrompt([]HostMemberInfo{
		{Name: "Active", Specialty: "X"},
		{Name: "Gone", Left: true},
	})
	if !strings.Contains(p, "Gone（已离场）") {
		t.Fatalf("left member must be marked 已离场: %q", p)
	}
}

// TestBuildHostSystemPrompt_NoOtherMembers covers the "host-only group" shape:
// there is nobody to route to, so the prompt must tell the host to answer
// directly instead of naming a non-existent member.
func TestBuildHostSystemPrompt_NoOtherMembers(t *testing.T) {
	p := BuildHostSystemPrompt(nil)
	if !strings.Contains(p, "没有其他成员") {
		t.Fatal("host-only prompt must state there are no other members")
	}
	if strings.Contains(p, "可选的成员名：（暂无其他成员）") == false {
		t.Fatal("host-only prompt must not present an empty member list as addressable")
	}
}

func TestBuildHostSummaryPromptHasNoRoutingTag(t *testing.T) {
	p := BuildHostSummaryPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if strings.Contains(p, "clawbench-mention") || strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("summary prompt must not ask for routing/end tags (would re-enter loop)")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("summary prompt must ask for a conclusion")
	}
}

// TestBuildHostSystemPrompt_DocumentsBcc: the host must be told how to send a
// private note, that targets are comma-separated and must be named this round,
// and that other members cannot see it.
func TestBuildHostSystemPrompt_DocumentsBcc(t *testing.T) {
	p := BuildHostSystemPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if !strings.Contains(p, "<clawbench-mention targets=") {
		t.Fatalf("prompt must document the bcc tag syntax: %q", p)
	}
	if !strings.Contains(p, "密送") {
		t.Fatalf("prompt must name the feature (密送): %q", p)
	}
	if !strings.Contains(p, "逗号") {
		t.Fatalf("prompt must explain comma-separated targets: %q", p)
	}
	if !strings.Contains(p, "看不到") {
		t.Fatalf("prompt must state other members cannot see the note: %q", p)
	}
}

// A host-only group has nobody to send a private note to, so the prompt must
// not document the private (密送) form (it would invite a note that goes
// nowhere). The plain mention form is still documented — it is how the host
// answers — so the check is for the `private` attribute, not the tag name.
func TestBuildHostSystemPrompt_NoMembersOmitsBcc(t *testing.T) {
	p := BuildHostSystemPrompt(nil)
	if strings.Contains(p, "private") {
		t.Fatalf("host-only prompt must not document the private form: %q", p)
	}
}

// The summary prompt is a pure wrap-up: it must not invite any tag, bcc
// included (the orchestrator ignores tags in that turn).
func TestBuildHostSummaryPromptOmitsBcc(t *testing.T) {
	p := BuildHostSummaryPrompt([]HostMemberInfo{{Name: "A"}})
	if strings.Contains(p, "clawbench-mention") {
		t.Fatalf("summary prompt must not document bcc: %q", p)
	}
}

// BuildMemberSystemPrompt: a group MEMBER must be told it is a participant, not
// the chair — without this, members imitated the host's routing tag and fought
// over the microphone (real incident: 3 members all opened with "🎙️ 主持人").
func TestBuildMemberSystemPrompt(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "Host"}, {Name: "B"}, {Name: "C"}}, "B")
	if !strings.Contains(p, "参与者") {
		t.Fatalf("member prompt must state the participant role: %q", p)
	}
	if !strings.Contains(p, "不是主持人") {
		t.Fatalf("member prompt must say it is NOT the chair: %q", p)
	}
	// It must forbid emitting the host's protocol tags.
	if !strings.Contains(p, "clawbench-mention") || !strings.Contains(p, "不要") {
		t.Fatalf("member prompt must forbid emitting routing tags: %q", p)
	}
	// It must list who else is in the group (excluding self).
	if !strings.Contains(p, "Host") || !strings.Contains(p, "C") {
		t.Fatalf("member prompt must list other participants: %q", p)
	}
	if strings.Contains(p, "可选的成员名：") {
		t.Fatalf("member prompt must NOT carry the host's routing list: %q", p)
	}
}

// The host must see the user as a routable participant, and know that naming
// the user ends the round.
func TestBuildHostSystemPrompt_IncludesUser(t *testing.T) {
	p := BuildHostSystemPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}, {Name: groupUserTarget()}})
	if !strings.Contains(p, "可选的成员名：") || !strings.Contains(p, groupUserTarget()) {
		t.Fatalf("the user must be in the routable list: %q", p)
	}
	if !strings.Contains(p, "点到 User 后本轮结束") {
		t.Fatalf("the host must be told naming the user ends the round: %q", p)
	}
	// A note may target someone not named this round (deferred delivery).
	if strings.Contains(p, "必须是本轮") {
		t.Fatalf("the old 'target must be named this round' rule must be gone: %q", p)
	}
	if !strings.Contains(p, "下次被点名时送达") {
		t.Fatalf("the deferred-delivery rule must be documented: %q", p)
	}
}

// A member should know a human is in the room.
func TestBuildMemberSystemPrompt_ListsUser(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "Host"}, {Name: "B"}}, "B")
	if !strings.Contains(p, groupUserTarget()) {
		t.Fatalf("a member must be told the user is present: %q", p)
	}
}
