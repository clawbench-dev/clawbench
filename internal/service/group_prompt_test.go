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
