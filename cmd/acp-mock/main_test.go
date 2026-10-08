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
		if !strings.Contains(got, "<clawbench-mention targets=\"Alice\">") {
			t.Fatalf("expected routing tag for Alice, got %q", got)
		}
	})

	t.Run("comma_separated_list", func(t *testing.T) {
		got := groupRoutingReply("可选的成员名：A,B,C。\n")
		if !strings.Contains(got, "<clawbench-mention targets=\"A\">") {
			t.Fatalf("expected first comma-separated name, got %q", got)
		}
	})

	t.Run("empty_list_ends_discussion", func(t *testing.T) {
		got := groupRoutingReply("可选的成员名：（暂无其他成员）\n")
		if !strings.Contains(got, "<clawbench-group-end/>") {
			t.Fatalf("empty member list must end the discussion, got %q", got)
		}
		if strings.Contains(got, "<clawbench-mention") {
			t.Fatalf("empty member list must not emit a speaker tag, got %q", got)
		}
	})
}

// TestFreeMemberReply covers the mock's free-chat behavior: a member prompt
// must relay the floor by @-mentioning another member, and the relay must
// terminate (stop mentioning) after the configured number of turns.
func TestFreeMemberReply(t *testing.T) {
	relayTurnsMu.Lock()
	relayTurns = map[string]int{}
	relayTurnsMu.Unlock()

	t.Run("non_free_prompt_returns_empty", func(t *testing.T) {
		if got := freeMemberReply("s", "just a normal message"); got != "" {
			t.Fatalf("a non-free prompt must not relay, got %q", got)
		}
	})

	t.Run("free_prompt_mentions_another_member", func(t *testing.T) {
		prompt := "上下文\n[自由群聊] ...\n本群其他成员：Alice（代码）、Bob、User。\n"
		got := freeMemberReply("s", prompt)
		if !strings.Contains(got, "<clawbench-mention targets=\"Alice\">") {
			t.Fatalf("expected a mention of Alice, got %q", got)
		}
	})

	t.Run("relay_terminates_after_budget", func(t *testing.T) {
		relayTurnsMu.Lock()
		relayTurns = map[string]int{}
		relayTurnsMu.Unlock()
		prompt := "[自由群聊] 本群其他成员：Alice。"
		// Default budget is 2 mentions; the third turn must stop.
		_ = freeMemberReply("s", prompt)
		_ = freeMemberReply("s", prompt)
		got := freeMemberReply("s", prompt)
		if strings.Contains(got, "<clawbench-mention") {
			t.Fatalf("relay must terminate after the budget, got %q", got)
		}
	})

	t.Run("skips_the_reserved_user_name", func(t *testing.T) {
		relayTurnsMu.Lock()
		relayTurns = map[string]int{}
		relayTurnsMu.Unlock()
		got := freeMemberReply("s", "[自由群聊] 本群其他成员：User。")
		if strings.Contains(got, "targets=\"User\"") {
			t.Fatalf("the mock must not mention the user, got %q", got)
		}
	})
}
