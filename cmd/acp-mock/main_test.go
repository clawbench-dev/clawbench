package main

import (
	"strings"
	"testing"
)

// TestGroupRoutingReply covers the mock's group-host behaviour: when the prompt
// carries ClawBench's host instruction it must emit a routing tag naming a real
// member, so the group E2E can exercise the multi-agent loop.
func TestGroupRoutingReply(t *testing.T) {
	t.Run("non_host_prompt_returns_empty", func(t *testing.T) {
		if got := groupRoutingReply("just a normal user message"); got != "" {
			t.Fatalf("non-host prompt must not route, got %q", got)
		}
	})

	t.Run("host_prompt_names_first_member", func(t *testing.T) {
		prompt := "some context\n[群聊主持人] ...\n可选的成员名：Alice、Bob。\n"
		got := groupRoutingReply(prompt)
		if !strings.Contains(got, "<clawbench-speaker>Alice</clawbench-speaker>") {
			t.Fatalf("expected routing tag for Alice, got %q", got)
		}
	})

	t.Run("comma_separated_list", func(t *testing.T) {
		got := groupRoutingReply("可选的成员名：A,B,C。\n")
		if !strings.Contains(got, "<clawbench-speaker>A</clawbench-speaker>") {
			t.Fatalf("expected first comma-separated name, got %q", got)
		}
	})

	t.Run("empty_list_ends_discussion", func(t *testing.T) {
		got := groupRoutingReply("可选的成员名：（暂无其他成员）\n")
		if !strings.Contains(got, "<clawbench-group-end/>") {
			t.Fatalf("empty member list must end the discussion, got %q", got)
		}
		if strings.Contains(got, "<clawbench-speaker>") {
			t.Fatalf("empty member list must not emit a speaker tag, got %q", got)
		}
	})
}
