package service

import (
	"strings"
	"testing"
)

func TestBuildHostSystemPrompt(t *testing.T) {
	members := []HostMemberInfo{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	p := BuildHostSystemPrompt(members)
	if !strings.Contains(p, "<clawbench-speaker>") {
		t.Fatal("must document speaker tag")
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
	if strings.Contains(p, "<clawbench-speaker>") || strings.Contains(p, "<clawbench-group-end/>") {
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
	if !strings.Contains(p, "<clawbench-bcc targets=") {
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
// not mention the feature (it would invite a tag that goes nowhere).
func TestBuildHostSystemPrompt_NoMembersOmitsBcc(t *testing.T) {
	p := BuildHostSystemPrompt(nil)
	if strings.Contains(p, "clawbench-bcc") {
		t.Fatalf("host-only prompt must not document bcc: %q", p)
	}
}

// The summary prompt is a pure wrap-up: it must not invite any tag, bcc
// included (the orchestrator ignores tags in that turn).
func TestBuildHostSummaryPromptOmitsBcc(t *testing.T) {
	p := BuildHostSummaryPrompt([]HostMemberInfo{{Name: "A"}})
	if strings.Contains(p, "clawbench-bcc") {
		t.Fatalf("summary prompt must not document bcc: %q", p)
	}
}
