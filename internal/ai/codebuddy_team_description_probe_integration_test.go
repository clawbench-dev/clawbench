//go:build integration

package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCodebuddyACP_TeamMemberDescriptionProbe answers ONE question with real
// wire data: does the roster's `description` equal the spawn frame's `prompt`?
//
// The team member detail sheet currently fetches the member's full prompt from
// `chat_tool_calls` and renders it as Markdown. The user's claim is that this is
// redundant: the roster's `description` already carries the same text, and —
// more importantly — `description` SURVIVES an interruption/reconnect while the
// prompt does not (the slim block loses its input, and after a respawn the
// prompt may no longer be recoverable).
//
// If `description == prompt` on the wire, the sheet can drop the fetch entirely
// and render `description` as Markdown. If they differ (description is a short
// summary, prompt is the long instruction), the claim is wrong for this agent
// version and the fetch has to stay.
//
// Run:
//
//	go test -v -run 'TestCodebuddyACP_TeamMemberDescriptionProbe' -tags integration \
//	    -timeout 600s ./internal/ai/
func TestCodebuddyACP_TeamMemberDescriptionProbe(t *testing.T) {
	ctx, cancel := contextWithTimeout(t, 540*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	// A distinctive prompt so a match cannot be accidental.
	const memberPrompt = "你是 probe-alpha，团队成员。任务：用 Bash 运行 `echo TEAMPROBE-ALPHA`，" +
		"然后用一句话报告输出。完成后用 SendMessage 把这一句结果发给 team-lead，然后结束。"

	spawnPrompt := `当前团队是 "clawbench-probe"。请执行：
1. 用 Agent 工具生成一个团队成员，name 为 "probe-alpha"，description 为 "回声测试员"，
   prompt 为下面这段（原样传，不要改写）：
` + memberPrompt + `
2. 等它返回后用一句中文汇总。`

	if _, err := setup.prompt(ctx, 180*time.Second, teamProbeCreatePrompt); err != nil {
		t.Logf("NOTE: create prompt error: %v", err)
	}
	time.Sleep(2 * time.Second)

	prevNotifs := len(setup.client.notifications())
	if _, err := setup.prompt(ctx, 300*time.Second, spawnPrompt); err != nil {
		t.Logf("NOTE: spawn prompt error: %v", err)
	}
	time.Sleep(3 * time.Second)

	// ALL events, not a slice after the create prompt: the first roster
	// snapshots (which carry the member) arrive during the create phase.
	notifs := setup.client.notifications()[prevNotifs:]

	// ---- 1. roster: what does teamUpdate report as `description`? ----
	//
	// Read the RAW notifications, not mapped StreamEvents: this probe wires a
	// bare ACP connection (no stream mapper), so `team_update` — a synthesized
	// event — never exists here. The roster lives in
	// session_info_update._meta["codebuddy.ai/teamUpdate"].
	type rosterMember struct {
		name        string
		agentType   string
		description string
	}
	var roster []rosterMember
	var teamUpdates int
	for _, n := range setup.client.notifications() {
		info := n.Update.SessionInfoUpdate
		if info == nil {
			continue
		}
		raw, ok := info.Meta[metaKeyCodeBuddyTeamUpdate].(map[string]any)
		if !ok {
			continue
		}
		state := parseTeamState(raw)
		if state == nil {
			continue
		}
		teamUpdates++
		t.Logf("  team_update type=%q teamName=%q members=%d", state.Type, state.TeamName, len(state.Members))
		for _, m := range state.Members {
			roster = append(roster, rosterMember{m.Name, m.AgentType, m.Description})
		}
	}
	t.Logf("=== teamUpdate snapshots: %d ===", teamUpdates)
	t.Logf("=== roster descriptions (%d observations) ===", len(roster))
	for _, m := range roster {
		t.Logf("  name=%q type=%q description=%q (len=%d)", m.name, m.agentType, m.description, len(m.description))
	}

	// ---- 2. spawn frame: what did the prompt actually contain? ----
	type spawnInfo struct {
		memberName   string
		description  string
		prompt       string
		promptLength int
	}
	var spawns []spawnInfo
	for _, n := range notifs {
		if n.Update.ToolCall == nil {
			continue
		}
		tc := n.Update.ToolCall
		if metaString(tc.Meta[metaKeyCodeBuddySubagentType]) == "" {
			continue
		}
		raw, _ := json.Marshal(tc.RawInput)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		spawns = append(spawns, spawnInfo{
			memberName:   metaString(tc.Meta[metaKeyCodeBuddyMemberName]),
			description:  metaString(parsed["description"]),
			prompt:       metaString(parsed["prompt"]),
			promptLength: len(metaString(parsed["prompt"])),
		})
	}
	t.Logf("=== spawn frames (%d) ===", len(spawns))
	for _, s := range spawns {
		t.Logf("  member=%q description=%q promptLen=%d", s.memberName, s.description, s.promptLength)
		t.Logf("      prompt head: %q", headOf(s.prompt, 160))
	}

	// ---- 3. the decisive comparison ----
	t.Logf("=== comparison ===")
	for _, s := range spawns {
		var matching []rosterMember
		for _, m := range roster {
			if m.name == s.memberName {
				matching = append(matching, m)
			}
		}
		if len(matching) == 0 {
			t.Logf("  member %q: no roster entry observed", s.memberName)
			continue
		}
		// Use the longest description seen — later snapshots are fuller.
		best := matching[0]
		for _, m := range matching {
			if len(m.description) > len(best.description) {
				best = m
			}
		}
		t.Logf("  member %q:", s.memberName)
		t.Logf("    roster description (len=%d): %q", len(best.description), headOf(best.description, 160))
		t.Logf("    spawn prompt      (len=%d)", s.promptLength)
		t.Logf("    description == prompt ? %v", best.description == s.prompt)
		t.Logf("    prompt contains description ? %v", s.prompt != "" && best.description != "" && strings.Contains(s.prompt, best.description))
	}

	require.NotEmpty(t, roster, "the spawn phase must produce roster snapshots")
}

// headOf truncates s to at most n bytes for logging (rune-safe enough for a
// diagnostic: the probes only ever log ASCII or Chinese, never a cut rune in a
// position that matters).
func headOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
