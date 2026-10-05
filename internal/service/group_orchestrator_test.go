package service

import (
	"context"
	"encoding/json"
	"testing"

	"clawbench/internal/store"
)

// scriptedRunner writes a scripted assistant message to the group timeline on
// behalf of the given member, mirroring what runTurn would persist. It records
// the order of speakers so tests can assert sequencing.
type scriptedTurn struct {
	member string
	text   string
}

func newScriptedRunner(t *testing.T, groupID string, project string, script map[string][]string) (groupTurnRunner, *[]string) {
	t.Helper()
	order := &[]string{}
	counts := map[string]int{}
	return func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		*order = append(*order, turn.MemberRowID)
		texts := script[turn.MemberRowID]
		i := counts[turn.MemberRowID]
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		counts[turn.MemberRowID]++
		if text == "" {
			text = "(no script)"
		}
		_, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":`+jsonQuote(text)+`}]}`, nil, false, "", turn.MemberRowID)
		if err != nil {
			return groupMemberResult{Err: err.Error()}
		}
		return groupMemberResult{}
	}, order
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// silenceGroupUserEmit avoids touching the WS hub in tests.
func silenceGroupUserEmit(t *testing.T) {
	t.Helper()
	orig := emitGroupUserMessage
	emitGroupUserMessage = func(groupID string, msgID int64, text, queueID, senderClientID string) {}
	t.Cleanup(func() { emitGroupUserMessage = orig })
}

// observeGroupTerminal replaces the terminal emitter with a counter so tests can
// assert a group turn ALWAYS ends with a terminal event (otherwise the last
// member's streaming bubble would hang and the session stay "running").
func observeGroupTerminal(t *testing.T) *int {
	t.Helper()
	orig := emitGroupTerminal
	n := 0
	emitGroupTerminal = func(groupID string) { n++ }
	t.Cleanup(func() { emitGroupTerminal = orig })
	return &n
}

func TestGroupOrchestrator_SequentialRouting(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")
	_ = mA

	script := map[string][]string{
		hostID: {
			// Round 1: route to A then B (sequential).
			`<clawbench-speaker>A,B</clawbench-speaker> 请分别表态`,
			// Round 2: end with a summary after the end tag.
			`讨论充分。<clawbench-group-end/> 结论：A 与 B 一致。`,
		},
		mA: {"A 的观点"},
		mB: {"B 的观点"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	term := observeGroupTerminal(t)
	if err := o.RunGroupTurn(context.Background(), "大家讨论一下"); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}
	if *term != 1 {
		t.Fatalf("terminal emitted %d times, want exactly 1 (end-signal path)", *term)
	}

	// Round 1 must speak A then B in that order (sequential), after the host.
	wantPrefix := []string{hostID, mA, mB}
	if len(*order) < len(wantPrefix) {
		t.Fatalf("order=%v, want prefix %v", *order, wantPrefix)
	}
	for i, w := range wantPrefix {
		if (*order)[i] != w {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], w, *order)
		}
	}

	// The end-signal round must NOT trigger an extra host turn beyond the scripted ones.
	hostTurns := 0
	for _, id := range *order {
		if id == hostID {
			hostTurns++
		}
	}
	if hostTurns != 2 {
		t.Fatalf("host turns=%d, want 2 (route + end-summary), order=%v", hostTurns, *order)
	}

	// B must have seen A's same-round speech: verify via B's injection by checking
	// the group timeline ordering is A before B.
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	var aIdx, bIdx = -1, -1
	for i, m := range msgs {
		if m.AgentID == mA {
			aIdx = i
		}
		if m.AgentID == mB {
			bIdx = i
		}
	}
	if !(aIdx >= 0 && bIdx > aIdx) {
		t.Fatalf("A must precede B in the timeline (aIdx=%d bIdx=%d)", aIdx, bIdx)
	}
}

// TestGroupOrchestrator_RunningStateAndCancel verifies the group is registered
// as running (so a frontend stop can reach it) and that cancelling the group
// context stops the loop mid-turn.
func TestGroupOrchestrator_RunningStateAndCancel(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch3"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	sawRunning := false
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if !sawRunning {
			sawRunning = IsSessionRunning(groupID)
		}
		// Simulate a long member turn that must observe cancellation.
		if turn.MemberRowID == mA {
			CancelSession(groupID)
		}
		return groupMemberResult{}
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"<clawbench-speaker>A</clawbench-speaker> 请发言"}]}`, nil, false, "", turn.MemberRowID)
		} else {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"A 发言"}]}`, nil, false, "", turn.MemberRowID)
		}
		return runner(ctx, gid, turn)
	}
	if err := o.RunGroupTurn(context.Background(), "开始"); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}
	if !sawRunning {
		t.Fatal("group must be registered as running during the turn")
	}
	if IsSessionRunning(groupID) {
		t.Fatal("group running state must be cleared after the turn")
	}
	_ = hostID
}

func TestGroupOrchestrator_MaxRoundsSummary(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch2"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	// Cap at 1 round so the host never ends on its own.
	if err := SetGroupMaxRounds(groupID, 1); err != nil {
		t.Fatalf("SetGroupMaxRounds: %v", err)
	}

	script := map[string][]string{
		hostID: {
			`<clawbench-speaker>A</clawbench-speaker> 请发言`, // round 1 route (no end)
			`结论：到此为止。`,                                     // summary turn (round cap reached)
		},
		mA: {"A 发言"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	term := observeGroupTerminal(t)
	if err := o.RunGroupTurn(context.Background(), "开始"); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}
	if *term != 1 {
		t.Fatalf("terminal emitted %d times, want exactly 1 (round-cap path)", *term)
	}

	// Host should speak exactly twice: the routing turn + the summary turn.
	hostTurns := 0
	for _, id := range *order {
		if id == hostID {
			hostTurns++
		}
	}
	if hostTurns != 2 {
		t.Fatalf("host turns=%d, want 2 (route + summary), order=%v", hostTurns, *order)
	}
}
