package service

import (
	"strings"
	"testing"
)

func TestBuildHostSystemPrompt(t *testing.T) {
	members := []string{"A", "B", "C"}
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
		if !strings.Contains(p, m) {
			t.Fatalf("member %q missing from prompt", m)
		}
	}
	// The host must be told explicitly not to name itself (the self-route loop
	// defect: the host named itself and every "member turn" was a host turn).
	if !strings.Contains(p, "不要点名你自己") {
		t.Fatal("prompt must forbid the host from naming itself")
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
	p := BuildHostSummaryPrompt([]string{"A", "B"})
	if strings.Contains(p, "<clawbench-speaker>") || strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("summary prompt must not ask for routing/end tags (would re-enter loop)")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("summary prompt must ask for a conclusion")
	}
}
