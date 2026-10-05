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
	got := buildInjectionText(msgs, 1, "row-b" /*self*/, names, "请回应 A")
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

// Same agent added twice must still let each member see the other: identity is
// the member ROW id, not the agent id.
func TestBuildMemberInjection_SameAgentTwoMembers(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 10, Role: "assistant", AgentID: "row-1", Content: `{"blocks":[{"type":"text","text":"from one"}]}`},
		{ID: 11, Role: "assistant", AgentID: "row-2", Content: `{"blocks":[{"type":"text","text":"from two"}]}`},
	}
	names := map[string]string{"row-1": "One", "row-2": "Two"}
	got := buildInjectionText(msgs, 0, "row-1", names, "")
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
	got := buildInjectionText(msgs, 5, "self", nil, "")
	if strings.Contains(got, "old") {
		t.Fatalf("cursor must exclude id<=cursor: %q", got)
	}
	if !strings.Contains(got, "new") {
		t.Fatalf("cursor must include id>cursor: %q", got)
	}
}
