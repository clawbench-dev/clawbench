//go:build integration

package ai

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCodebuddyACP_TeamSpawnMetaProbe answers ONE question with real wire data:
// what does the Agent SPAWN frame carry, and does the resulting tool_use event
// reach the frontend with the member's name?
//
// Background: before an interruption the team's Agent cards all render as
// "general-purpose" (the subagent TYPE), while after an interruption they show
// the member name (because the fallback groups by member_name). The user wants
// the pre-interruption cards to show the member name too.
//
// Two candidate causes, and this probe distinguishes them:
//   A. the spawn frame carries `_meta.memberName` but the mapper drops it, so
//      the block never learns the name; or
//   B. the spawn frame carries no `memberName` at all, and only `subagentType`
//      ("general-purpose") — in which case nothing on the wire can name it and
//      the name has to come from the team roster join instead.
//
// Run:
//
//	go test -v -run 'TestCodebuddyACP_TeamSpawnMetaProbe' -tags integration \
//	    -timeout 600s ./internal/ai/
func TestCodebuddyACP_TeamSpawnMetaProbe(t *testing.T) {
	ctx, cancel := contextWithTimeout(t, 540*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	// An explicit team survives the turn, so the spawn frames and the roster
	// snapshot are both observable in one probe.
	if _, err := setup.prompt(ctx, 180*time.Second, teamProbeCreatePrompt); err != nil {
		t.Logf("NOTE: create prompt error: %v", err)
	}
	time.Sleep(2 * time.Second)

	prevEvents := len(setup.streamEvents())
	prevNotifs := len(setup.client.notifications())
	if _, err := setup.prompt(ctx, 300*time.Second, teamProbeSpawnPrompt); err != nil {
		t.Logf("NOTE: spawn prompt error: %v", err)
	}
	time.Sleep(3 * time.Second)

	events := setup.streamEvents()[prevEvents:]
	notifs := setup.client.notifications()[prevNotifs:]

	// ---- 1. raw wire: what does a spawn tool_call carry? ----
	type spawnMeta struct {
		toolCallID   string
		title        string
		memberName   string
		subagentType string
		keys         []string
	}
	var spawns []spawnMeta
	for _, n := range notifs {
		if n.Update.ToolCall == nil {
			continue
		}
		tc := n.Update.ToolCall
		meta := tc.Meta
		memberName := metaString(meta[metaKeyCodeBuddyMemberName])
		subagentType := metaString(meta[metaKeyCodeBuddySubagentType])
		// A spawn frame is the one that carries subagentType.
		if subagentType == "" {
			continue
		}
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		title := tc.Title
		spawns = append(spawns, spawnMeta{
			toolCallID:   string(tc.ToolCallId),
			title:        title,
			memberName:   memberName,
			subagentType: subagentType,
			keys:         keys,
		})
	}

	t.Logf("=== spawn frames on the wire: %d ===", len(spawns))
	for _, s := range spawns {
		t.Logf("  toolCallId=%s title=%q memberName=%q subagentType=%q",
			s.toolCallID, s.title, s.memberName, s.subagentType)
		raw, _ := json.Marshal(s.keys)
		t.Logf("      _meta keys: %s", string(raw))
	}

	// ---- 2. mapped events: what reaches the frontend? ----
	t.Logf("=== ALL mapped tool_use events ===")
	var agentCards int
	var agentCardsWithMember int
	for _, ev := range events {
		if ev.Type != "tool_use" || ev.Tool == nil {
			continue
		}
		input, _ := json.Marshal(ev.Tool.Input)
		t.Logf("  name=%q id=%s MemberName=%q done=%v input=%s",
			ev.Tool.Name, ev.Tool.ID, ev.MemberName, ev.Tool.Done, string(input))
		if ev.Tool.Name == "Agent" {
			agentCards++
			if ev.MemberName != "" {
				agentCardsWithMember++
			}
		}
	}
	t.Logf("Agent cards: %d | with MemberName: %d", agentCards, agentCardsWithMember)

	// ---- 3. roster: does teamUpdate carry the authoritative names? ----
	t.Logf("=== team_update snapshots ===")
	for _, ev := range events {
		if ev.Type != "team_update" || ev.Team == nil {
			continue
		}
		for _, m := range ev.Team.Members {
			t.Logf("  member name=%q agentType=%q status=%q", m.Name, m.AgentType, m.Status)
		}
	}

	require.NotEmpty(t, spawns, "the spawn phase must produce at least one Agent spawn frame")
	// The decisive assertion: the wire DOES carry the member name on spawn
	// frames. If this ever fails, the name has to come from the roster instead.
	named := 0
	for _, s := range spawns {
		if s.memberName != "" {
			named++
		}
	}
	require.Equal(t, len(spawns), named,
		"every spawn frame must carry _meta.memberName (that is the only place the name exists)")
}
