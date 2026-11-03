package main

import (
	"strings"
	"testing"
)

// TestGroupRoutingReply covers the mock's group-host behavior: when the prompt
// carries ClawBench's host instruction it must emit a routing tag naming a real
// member, so the group E2E can exercise the multi-agent loop.
func TestGroupRoutingReply(t *testing.T) {
	t.Run("non_host_prompt_returns_empty", func(t *testing.T) {
		if got := groupRoutingReply("s", "just a normal user message"); got != "" {
			t.Fatalf("non-host prompt must not route, got %q", got)
		}
	})

	t.Run("host_prompt_names_first_member", func(t *testing.T) {
		prompt := "some context\n[群聊主持人] ...\n可选的成员名：Alice、Bob。\n"
		got := groupRoutingReply("s", prompt)
		if !strings.Contains(got, "<clawbench-mention targets=\"Alice\">") {
			t.Fatalf("expected routing tag for Alice, got %q", got)
		}
	})

	t.Run("comma_separated_list", func(t *testing.T) {
		got := groupRoutingReply("s", "可选的成员名：A,B,C。\n")
		if !strings.Contains(got, "<clawbench-mention targets=\"A\">") {
			t.Fatalf("expected first comma-separated name, got %q", got)
		}
	})

	// The roster renders "Name（specialty）". A specialty may contain a comma,
	// and the parenthetical is not part of the name — taking the text up to the
	// first comma yielded "ACP Mock Agent B（E2E Testing (ACP", a target no
	// member answers to. The orchestrator then aborted the group turn after two
	// consecutive unroutable host turns (see group-speaker-switch).
	t.Run("specialty_with_comma_does_not_truncate_the_name", func(t *testing.T) {
		prompt := "可选的成员名：ACP Mock Agent B（E2E Testing (ACP, member)）、User。\n"
		got := groupRoutingReply("s", prompt)
		if !strings.Contains(got, "<clawbench-mention targets=\"ACP Mock Agent B\">") {
			t.Fatalf("expected the bare member name, got %q", got)
		}
	})

	t.Run("specialty_without_comma_is_stripped_too", func(t *testing.T) {
		prompt := "可选的成员名：Alice（代码）、Bob。\n"
		got := groupRoutingReply("s", prompt)
		if !strings.Contains(got, "<clawbench-mention targets=\"Alice\">") {
			t.Fatalf("expected the bare member name, got %q", got)
		}
	})

	t.Run("empty_list_ends_discussion", func(t *testing.T) {
		got := groupRoutingReply("s", "可选的成员名：（暂无其他成员）\n")
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

// TestCruiseConfirmReply covers the auto-cruise confirmation reply: a
// confirmation prompt makes the mock declare the goal satisfied (so a cruising
// turn terminates), and ACP_MOCK_NO_CRUISE_CONFIRM suppresses that.
func TestCruiseConfirmReply(t *testing.T) {
	t.Run("non_confirm_prompt_returns_empty", func(t *testing.T) {
		if got := cruiseConfirmReply("just a normal message"); got != "" {
			t.Fatalf("a non-confirm prompt must not reply, got %q", got)
		}
	})

	t.Run("confirm_prompt_declares_satisfied", func(t *testing.T) {
		got := cruiseConfirmReply("上下文\n[自动巡航·完成确认] 请评估……\n")
		if !strings.Contains(got, "<clawbench-goal-satisfied/>") {
			t.Fatalf("confirmation must declare satisfied, got %q", got)
		}
	})

	t.Run("suppressed_when_env_set", func(t *testing.T) {
		t.Setenv("ACP_MOCK_NO_CRUISE_CONFIRM", "1")
		got := cruiseConfirmReply("[自动巡航·完成确认] 请评估……\n")
		if strings.Contains(got, "<clawbench-goal-satisfied/>") {
			t.Fatalf("suppressed confirmation must not declare satisfied, got %q", got)
		}
	})
}

// TestGroupRoutingReply_Cruise: in cruise mode the host routes once then emits
// the end tag (which triggers the confirmation round) instead of routing
// forever until the speech cap.
func TestGroupRoutingReply_Cruise(t *testing.T) {
	relayTurnsMu.Lock()
	relayTurns = map[string]int{}
	relayTurnsMu.Unlock()

	prompt := "[自动巡航模式] ...\n可选的成员名：Alice、Bob。\n"
	first := groupRoutingReply("cs", prompt)
	if !strings.Contains(first, "<clawbench-mention targets=\"Alice\">") {
		t.Fatalf("first cruise host turn must route, got %q", first)
	}
	second := groupRoutingReply("cs", prompt)
	if !strings.Contains(second, "<clawbench-group-end/>") {
		t.Fatalf("second cruise host turn must emit the end tag, got %q", second)
	}
}

// TestSlowReplyDelayMs: the per-turn reply-delay marker is opt-in and must
// ignore malformed forms rather than silently producing a 0ms delay.
func TestSlowReplyDelayMs(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		wantMs int
		wantOK bool
	}{
		{"absent", "just a normal user message", 0, false},
		{"valid", "run [mock:slow-reply:12000] please", 12000, true},
		{"not_an_int", "[mock:slow-reply:abc]", 0, false},
		{"unterminated", "[mock:slow-reply:12000", 0, false},
		{"zero", "[mock:slow-reply:0]", 0, false},
		{"negative", "[mock:slow-reply:-5]", 0, false},
		{"other_marker_only", "[mock:omit-perm-rawinput]", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, ok := slowReplyDelayMs(tc.prompt)
			if ms != tc.wantMs || ok != tc.wantOK {
				t.Fatalf("slowReplyDelayMs(%q) = (%d, %v), want (%d, %v)", tc.prompt, ms, ok, tc.wantMs, tc.wantOK)
			}
		})
	}
}

// TestEscapeMarkdownImageEmbeds: the echoed prompt must not create image
// requests in the UI (the injected system prompt carries literal `![...](...)`
// examples). Escaping only the image marker leaves ordinary links intact.
func TestEscapeMarkdownImageEmbeds(t *testing.T) {
	in := "image: ![desc](.clawbench/generated/img_chart.png) and [label](a.mp3)"
	got := escapeMarkdownImageEmbeds(in)
	if strings.Contains(got, "![") {
		t.Fatalf("image embed must be escaped, got %q", got)
	}
	if !strings.Contains(got, `!\[desc](.clawbench/generated/img_chart.png)`) {
		t.Fatalf("escaped form must keep the text readable, got %q", got)
	}
	if !strings.Contains(got, "[label](a.mp3)") {
		t.Fatalf("plain links must be left alone, got %q", got)
	}
}
