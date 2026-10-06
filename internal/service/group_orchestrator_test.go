package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	if err := o.RunGroupTurn(context.Background(), "大家讨论一下", nil); err != nil {
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
	if err := o.RunGroupTurn(context.Background(), "开始", nil); err != nil {
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
	if err := o.RunGroupTurn(context.Background(), "开始", nil); err != nil {
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

// TestGroupOrchestrator_HostNeverRoutesToItself is a regression for the reported
// "only the host ever speaks" defect: the host's selectable list included the
// host itself, so the model named itself, resolveTargets mapped that back to
// the host row, and every "member turn" was another host turn — the real
// members were never called.
//
// It asserts two invariants:
//  1. the host instruction never lists the host's own display name;
//  2. even if the host DOES name itself, that target is dropped (so the turn
//     falls through to real members instead of looping on the host).
func TestGroupOrchestrator_HostNeverRoutesToItself(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-self"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	// Host display name equals its backend name ("Codebuddy") — the exact shape
	// that triggered the production bug.
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Codebuddy")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	// Capture the host prompt to assert the host is not offered as a target.
	var hostPrompts []string
	script := map[string][]string{
		hostID: {
			// The host names ITSELF first — must be dropped, then the round
			// continues to a real member in the next host turn.
			`<clawbench-speaker>Codebuddy</clawbench-speaker> 我先补充一句`,
			`<clawbench-speaker>A</clawbench-speaker> 请你表态`,
			`讨论充分。<clawbench-group-end/> 结论：到此为止。`,
		},
		mA: {"A 的观点"},
	}
	order := &[]string{}
	counts := map[string]int{}
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		*order = append(*order, turn.MemberRowID)
		if turn.IsHost {
			hostPrompts = append(hostPrompts, turn.Prompt)
		}
		texts := script[turn.MemberRowID]
		i := counts[turn.MemberRowID]
		text := "(no script)"
		if i < len(texts) {
			text = texts[i]
		}
		counts[turn.MemberRowID]++
		_, e := AddChatMessageWithAgent(project, "codebuddy", gid, "assistant",
			`{"blocks":[{"type":"text","text":`+jsonQuote(text)+`}]}`, nil, false, "", turn.MemberRowID)
		if e != nil {
			return groupMemberResult{Err: e.Error()}
		}
		return groupMemberResult{}
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	observeGroupTerminal(t)
	if err := o.RunGroupTurn(context.Background(), "开始", nil); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}

	// (1) The host must never be told it may address itself.
	for _, p := range hostPrompts {
		if strings.Contains(p, "可选的成员名：Codebuddy") || strings.Contains(p, "、Codebuddy") || strings.HasSuffix(p, "Codebuddy。\n") {
			t.Fatalf("host prompt offers the host as a target:\n%s", p)
		}
	}

	// (2) A is a real member and MUST have been called. Before the fix the host
	// self-route swallowed every turn and A never ran.
	aCalled := false
	for _, id := range *order {
		if id == mA {
			aCalled = true
		}
	}
	if !aCalled {
		t.Fatalf("member A was never called; order=%v (host self-route loop)", *order)
	}
}

// The terminal emitter must finalize any streaming row left behind by a failed
// Finalize, or the frontend reloads into a phantom streaming bubble that never
// ends (decision #71). This is a safety net, not a race guard: every caller of
// emitGroupTerminal runs after the turn's runner returned.
func TestEmitGroupTerminal_FinalizesOrphanStreamingRow(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-orphan"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// A streaming assistant row left behind (as if Finalize failed).
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant", "half a thought", nil, true, "", "row-x")
	if err != nil {
		t.Fatalf("AddChatMessageWithAgent: %v", err)
	}
	if !isStreaming(t, msgID) {
		t.Fatal("precondition: the row must start out streaming")
	}

	// Call the REAL terminal emitter (the seam is not replaced here).
	emitGroupTerminal(groupID)

	if isStreaming(t, msgID) {
		t.Fatal("emitGroupTerminal must finalize orphaned streaming rows")
	}
}

// A clean turn (no orphan) must still emit the terminal contract exactly once.
func TestEmitGroupTerminal_NoOrphanStillTerminal(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-clean"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	SetSessionRunning(groupID, true, true)

	emitGroupTerminal(groupID)

	if IsSessionRunning(groupID) {
		t.Fatal("emitGroupTerminal must clear the running flag")
	}
}

// isStreaming reports whether a chat_history row is still marked streaming.
func isStreaming(t *testing.T, msgID int64) bool {
	t.Helper()
	var s int
	if err := store.ReadDB().QueryRow("SELECT streaming FROM chat_history WHERE id = ?", msgID).Scan(&s); err != nil {
		t.Fatalf("read streaming: %v", err)
	}
	return s == 1
}

// A failed member turn must NOT advance that member's cursor: it produced no
// output, so nothing was actually processed. Advancing it would make the
// messages injected into that turn permanently unreachable — the member would
// silently lose that stretch of the discussion and answer off-topic, with
// nothing on the timeline to show why (decision #69).
func TestGroupOrchestrator_FailedMemberDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-cursor-fail"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Seed a timeline message the member must still see next time.
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "seed question", nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	SetMemberCursor(mA, 0)
	before := GetMemberCursor(mA)

	// Host routes to A; A's turn FAILS (no message written, Err set).
	script := map[string][]string{
		hostID: {`<clawbench-speaker>A</clawbench-speaker> 请表态`},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	baseRunner := runner
	runner = func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			return groupMemberResult{Err: "backend exploded"}
		}
		return baseRunner(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	if got := GetMemberCursor(mA); got != before {
		t.Fatalf("a failed member turn must not advance the cursor: before=%d after=%d", before, got)
	}
}

// A failed HOST turn must not advance the host's cursor either — the host routes
// from what it has seen, so skipping content would corrupt its next decision.
func TestGroupOrchestrator_FailedHostDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-host-cursor"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	SetMemberCursor(hostID, 0)

	// The host turn always fails.
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			return groupMemberResult{Err: "host backend down"}
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	if got := GetMemberCursor(hostID); got != 0 {
		t.Fatalf("a failed host turn must not advance the cursor, got %d", got)
	}
}

// A SUCCESSFUL member turn must still advance the cursor (the fix must not
// break the normal path).
func TestGroupOrchestrator_SuccessfulMemberAdvancesCursor(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-cursor-ok"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	SetMemberCursor(mA, 0)

	script := map[string][]string{
		hostID: {
			`<clawbench-speaker>A</clawbench-speaker> 请表态`,
			`<clawbench-group-end/> 讨论结束`,
		},
		mA: {"我的观点"},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	if got := GetMemberCursor(mA); got <= 0 {
		t.Fatalf("a successful member turn must advance the cursor, got %d", got)
	}
}

// A group turn touches every member, but each member's ACP connection is keyed
// by the MEMBER row id — which is never registered as "running". The ACP idle
// sweep therefore sees an idle member connection mid-discussion and kills it,
// forcing a respawn (seconds, per member) during a live debate (decision #73).
//
// IsSessionRunningForSweep exists for exactly this: it reports member rows as
// busy while the group turn runs, WITHOUT polluting GetRunningSessionIDs (which
// feeds the session list and the "project has a running session" delete guard).
func TestGroupTurn_MarksMembersRunningForSweep(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-sweep"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Observe the sweep-visible state DURING a member turn.
	var seenRunning bool
	var seenInRunningIDs bool
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			seenRunning = IsSessionRunningForSweep(mA)
			for _, id := range GetRunningSessionIDs() {
				if id == mA {
					seenInRunningIDs = true
				}
			}
		}
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-speaker>A</clawbench-speaker> 请表态"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"我的观点"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	if !seenRunning {
		t.Fatal("a member row must read as running for the ACP sweep during its turn")
	}
	if seenInRunningIDs {
		t.Fatal("member rows must NOT appear in GetRunningSessionIDs (session list / project-delete guard)")
	}
}

// After the turn ends, no member row may linger as "running for sweep".
func TestGroupTurn_ClearsMemberSweepStateAfterTurn(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-sweep-clear"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-speaker>A</clawbench-speaker> 请表态"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"我的观点"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	if IsSessionRunningForSweep(mA) {
		t.Fatal("member sweep state must be cleared once the group turn ends")
	}
	if IsSessionRunningForSweep(hostID) {
		t.Fatal("host sweep state must be cleared once the group turn ends")
	}
}

// When the host's routing is unparseable the fallback must ROTATE through the
// members. Always picking the first non-host member means only one member ever
// speaks during a parse-failure streak, and the discussion collapses to a
// monologue (decision #56).
func TestGroupOrchestrator_FallbackRotates(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-rotate"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")
	mC, _ := AddGroupMember(project, groupID, "claude", "agent-c", "C")

	// Alternate a parse failure with a usable route: the failure streak resets
	// each time (so the abort does not kick in), and the fallback is exercised
	// repeatedly — which is where rotation must show.
	runner, order := newScriptedRunner(t, groupID, project, map[string][]string{
		hostID: {
			"(no routing tag at all)",
			"<clawbench-speaker>A</clawbench-speaker> 请 A 表态",
			"(no routing tag at all)",
			"<clawbench-speaker>A</clawbench-speaker> 请 A 表态",
			"(no routing tag at all)",
			"<clawbench-speaker>A</clawbench-speaker> 请 A 表态",
			"(no routing tag at all)",
			"<clawbench-group-end/> 讨论结束",
		},
		mA: {"A 的观点", "A 的观点", "A 的观点"},
	})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	// Fallback picks are those that are NOT the member the host explicitly
	// routed to (A) and not the host itself. Those must rotate across B and C.
	picks := []string{}
	for _, id := range *order {
		if id != hostID && id != mA {
			picks = append(picks, id)
		}
	}
	if len(picks) < 2 {
		t.Fatalf("expected at least two fallback picks, got %v (full order=%v)", picks, *order)
	}
	if picks[0] == picks[1] {
		t.Fatalf("fallback must rotate, but picked %q twice: %v", picks[0], picks)
	}
	// Both non-routed members must be reached by rotation.
	seen := map[string]bool{}
	for _, p := range picks {
		seen[p] = true
	}
	if !seen[mB] || !seen[mC] {
		t.Fatalf("rotation must reach B and C: picks=%v (B=%s C=%s)", picks, mB, mC)
	}
}

// Two consecutive parse failures mean the host is not producing usable routing:
// continuing burns the whole round budget for nothing. Abort and finalize
// (decision #56).
func TestGroupOrchestrator_AbortsAfterTwoParseFailures(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-abort"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	// A large cap so that, without the abort, the loop would run many rounds.
	if err := SetGroupMaxRounds(groupID, 20); err != nil {
		t.Fatalf("SetGroupMaxRounds: %v", err)
	}

	hostTurns := 0
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			hostTurns++
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"no tag here"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"member speech"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}
	term := observeGroupTerminal(t)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = o.RunGroupTurn(context.Background(), "开始", nil)

	// 2 failures, then abort. Allow a small margin for the final summary turn.
	if hostTurns > 4 {
		t.Fatalf("must abort after two consecutive parse failures, but the host ran %d turns", hostTurns)
	}
	if *term != 1 {
		t.Fatalf("aborting must still emit exactly one terminal event, got %d", *term)
	}
}

// The group turn's terminal path must summarize the discussion once — member
// turns deliberately skip it (decision #55), so this is the only place it can
// happen.
func TestEmitGroupTerminal_SummarizesOnce(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-summarize"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// A finalized assistant row so there is something to summarize.
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
		`{"blocks":[{"type":"text","text":"结论：可以发布"}]}`, nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	emitGroupTerminal(groupID)

	// A summary must now exist for that message (triggerChatSummarization ran).
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	found := false
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		if s, ok := GetSummary("chat_message", m.ID); ok && s != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the group terminal path must summarize the discussion once")
	}
}

// A group member turn that fails early must persist its warning block
// attributed to THAT member (decision #51), exactly like single chat. The
// member path builds its TurnSpec via buildMemberTurnSpec, so this pins that
// SpeakerID carries the member row id — without it the timeline would show an
// unattributed error and the user could not tell which member failed.
//
// NOTE: this already holds by construction — both failure sources write a
// warning (failTurn for early failures, the executor's content assembly for a
// timeout). This test PINS the attribution so a future change to the spec
// wiring cannot silently drop it.
func TestGroupMemberTurn_FailureWarningIsAttributed(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-failturn"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	o := NewGroupOrchestrator(groupID)
	spec := o.buildMemberTurnSpec(context.Background(), groupID, groupMemberTurn{MemberRowID: mA, Prompt: "hi"})

	// failTurn is the early-failure path the member turn uses.
	res := spec.failTurn(errors.New("backend exploded"), reasonBackendCreateFailed)
	if res.Err == "" {
		t.Fatal("failTurn must report the error")
	}

	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	found := false
	for _, m := range msgs {
		if m.Role != "assistant" || m.AgentID != mA {
			continue
		}
		if strings.Contains(m.Content, "warning") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a failed member turn must leave a warning block attributed to the member; msgs=%v", msgs)
	}
}
