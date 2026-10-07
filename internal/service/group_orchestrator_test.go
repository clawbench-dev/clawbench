package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"clawbench/internal/model"
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

// runGroupTurnForTest drives ONE group turn through the production entry point
// (RunGroupTurnDrain) and then emits the terminal event the drain loop would
// send, so tests exercise the same code path production does. The standalone
// RunGroupTurn entry was deleted (review C1): it had no production callers and
// its terminal responsibilities silently stopped running.
//
// It materializes the user message first, exactly as the drain loop does before
// calling RunGroupTurnDrain, then returns the DrainResult.
func runGroupTurnForTest(t *testing.T, o *GroupOrchestrator, project, userMessage string, files []model.FileEntry) DrainResult {
	t.Helper()
	groupID := o.groupID
	msgID, err := AddChatMessageWithAgent(project, groupBackend(groupID), groupID, "user", userMessage, files, false, "", "")
	if err != nil {
		t.Fatalf("seed group user message: %v", err)
	}
	return RunGroupTurnDrain(context.Background(), groupID, msgID, QueuedRow{Content: userMessage, Files: files}, o.runTurn)
}

func TestGroupOrchestrator_SequentialRouting(t *testing.T) {
	setupGroupDB(t)
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
	// NOTE: RunGroupTurnDrain never emits a terminal event by design — the drain
	// loop owns it (and sends it exactly once). The terminal contract is pinned
	// by the drain-loop tests, not here.
	_ = runGroupTurnForTest(t, o, project, "大家讨论一下", nil)

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

	// B must have seen A's same-round speech: the sequential-round invariant.
	// Assert it directly by capturing each member's injected prompt (the old
	// comment claimed this but only checked timeline ordering).
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

// A member's injected context must contain the PREVIOUS speakers' same-round
// speech (design §5.1: "后者可见前者本轮发言"), which is what makes "B responds to
// A" possible without the host re-ordering across rounds.
func TestGroupOrchestrator_MemberSeesPriorSameRoundSpeech(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-sameround"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	script := map[string][]string{
		hostID: {`<clawbench-speaker>A,B</clawbench-speaker> 请分别表态`, `<clawbench-group-end/> 结束`},
		mA:     {"A_UNIQUE_SPEECH"},
		mB:     {"B 的观点"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	var bPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mB {
			bPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if !strings.Contains(bPrompt, "A_UNIQUE_SPEECH") {
		t.Fatalf("B must see A's same-round speech in its injected context; got %q", bPrompt)
	}
}

// Decision #9: members exchange SPEECH TEXT only — tool calls and thinking are
// private. A member's injected context must never contain another member's
// tool_use / thinking content.
func TestGroupOrchestrator_ToolAndThinkingNotInjected(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-speechonly"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	// A's timeline row carries a text block, a tool_use block, and a thinking
	// block. Only the text may reach B.
	aContent := `{"blocks":[{"type":"text","text":"A 的发言文本"},{"type":"tool_use","name":"Bash","input":{"command":"SECRET_TOOL_CALL"}},{"type":"thinking","text":"SECRET_THINKING"}]}`
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant", aContent, nil, false, "", mA); err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// The host routes to B then ends (a scripted host avoids looping rounds,
	// which would advance B's cursor past A's row).
	script := map[string][]string{
		hostID: {`<clawbench-speaker>B</clawbench-speaker> 请 B 表态`, `<clawbench-group-end/> 结束`},
		mB:     {"B 的观点"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	var bPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mB {
			bPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if !strings.Contains(bPrompt, "A 的发言文本") {
		t.Fatalf("B must see A's speech text; got %q", bPrompt)
	}
	if strings.Contains(bPrompt, "SECRET_TOOL_CALL") {
		t.Fatalf("a member's tool call must NOT be injected to another member (decision #9): %q", bPrompt)
	}
	if strings.Contains(bPrompt, "SECRET_THINKING") {
		t.Fatalf("a member's thinking must NOT be injected to another member (decision #9): %q", bPrompt)
	}
}

// TestGroupOrchestrator_CancelReachesMemberTurns pins that a cancel on the
// context passed to RunGroupTurnDrain stops the loop mid-turn — that context is
// the handler's claim context, which is what CancelSession cancels. Without the
// propagation a "stop" on the group session would not reach the member turns.
//
// (The complementary invariant — RunGroupTurnDrain must not itself touch the
// running flag — is pinned by TestRunGroupTurnDrain_DoesNotTouchRunningFlag.)
func TestGroupOrchestrator_CancelReachesMemberTurns(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch3"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	// The caller owns the running state (as the handler's claim does).
	runCtx, claimed := TryClaimSessionRun(groupID)
	if !claimed {
		t.Fatal("precondition: the group must start idle")
	}
	defer FinishSessionRun(groupID)

	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"<clawbench-speaker>A</clawbench-speaker> 请发言"}]}`, nil, false, "", turn.MemberRowID)
			return groupMemberResult{}
		}
		// Simulate a long member turn that observes the cancel.
		CancelSession(groupID)
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"A 发言"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msgID, err := AddChatMessageWithAgent(project, groupBackend(groupID), groupID, "user", "开始", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	res := RunGroupTurnDrain(runCtx, groupID, msgID, QueuedRow{Content: "开始"}, o.runTurn)

	if res.CancelReason != cancelReasonUser {
		t.Fatalf("a cancel on the claim context must reach the member turn; got %+v", res)
	}
	_ = mA
}

func TestGroupOrchestrator_MaxRoundsSummary(t *testing.T) {
	setupGroupDB(t)
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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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

// A group turn must finalize any streaming row left behind by a failed
// Finalize, or the frontend reloads into a phantom streaming bubble that never
// ends (decision #71). This runs on the LIVE path (RunGroupTurnDrain → runLoop),
// not on the deleted standalone terminal emitter (review C1).
//
// The orphan is written MID-TURN (by a member runner, simulating a Finalize
// that failed), not before the turn. runRounds already finalizes orphans at the
// START of a turn, so a pre-seeded row would be closed by that first call and
// the test would pass even with the TERMINAL cleanup deleted (review B-2). Only
// the terminal call can close a row that appears after the turn began.
func TestGroupTurn_FinalizesOrphanStreamingRow(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-orphan"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	var orphanID int64
	// A trivial turn that ends immediately, but leaves a streaming row behind
	// (as a member whose Finalize failed would).
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			id, e := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant", "half a thought", nil, true, "", hostID)
			if e != nil {
				return groupMemberResult{Err: e.Error()}
			}
			orphanID = id
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if orphanID == 0 {
		t.Fatal("precondition: the turn must have left a streaming row behind")
	}
	if isStreaming(t, orphanID) {
		t.Fatal("a group turn must finalize orphaned streaming rows (decision #71) at its END, not only at its start")
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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if got := GetMemberCursor(mA); got != before {
		t.Fatalf("a failed member turn must not advance the cursor: before=%d after=%d", before, got)
	}
}

// A failed HOST turn must not advance the host's cursor either — the host routes
// from what it has seen, so skipping content would corrupt its next decision.
func TestGroupOrchestrator_FailedHostDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if got := GetMemberCursor(hostID); got != 0 {
		t.Fatalf("a failed host turn must not advance the cursor, got %d", got)
	}
}

// A failed SUMMARY turn must not advance the host's cursor either (decision
// #69). The summary turn is the round-cap/abort wrap-up; the host routes from
// what it has seen, so advancing past context it never processed makes that
// stretch permanently unreachable and corrupts the next routing decision.
//
// This pins the fourth cursor site — the other three (host, routed member,
// round-robin fallback) were covered, but summarize() was missed.
func TestGroupOrchestrator_SummaryFailureDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-summary-cursor"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// Cap at 1 round so the host never ends on its own → the round cap triggers
	// the summary turn.
	if err := SetGroupMaxRounds(groupID, 1); err != nil {
		t.Fatalf("SetGroupMaxRounds: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	SetMemberCursor(hostID, 0)

	// Seed a timeline message so a high-water mark exists to (wrongly) advance to.
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "seed question", nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// EVERY host turn fails — the routing turn and the summary turn alike.
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			return groupMemberResult{Err: "host backend down"}
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if got := GetMemberCursor(hostID); got != 0 {
		t.Fatalf("a failed summary turn must not advance the host cursor, got %d", got)
	}
}

// A SUCCESSFUL member turn must still advance the cursor (the fix must not
// break the normal path).
func TestGroupOrchestrator_SuccessfulMemberAdvancesCursor(t *testing.T) {
	setupGroupDB(t)
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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

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
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	// 2 failures, then abort. Allow a small margin for the final summary turn.
	if hostTurns > 4 {
		t.Fatalf("must abort after two consecutive parse failures, but the host ran %d turns", hostTurns)
	}
}

// A group turn must summarize the discussion — member turns deliberately skip
// it (decision #55), so this is the only place it happens. It must run on the
// LIVE path (RunGroupTurnDrain → runLoop); the standalone entry that used to
// carry it was dead code (review C1), which is exactly the regression this pins.
func TestGroupTurn_SummarizesOnce(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-summarize"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// A turn that leaves a finalized assistant row on the timeline (the row is
	// what gets summarized).
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"结论：可以发布。<clawbench-group-end/>"}]}`, nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	// A summary must now exist for the host's reply (triggerChatSummarization ran).
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
		t.Fatal("a group turn must summarize the discussion (decision #55) on the live path")
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

// A CANCELLED member turn must NOT advance the cursor (decision #69). The
// production runTurn reports a user cancel in CancelReason with Err EMPTY, so
// the old "Err == """ test treated a cancelled turn as processed and pushed the
// cursor past context the member never consumed — the member then silently
// answers off-topic with nothing on the timeline to show why.
//
// The runner here mirrors production: it sets CancelReason (not Err), which is
// exactly why the previous Err-only test could not catch this.
func TestGroupOrchestrator_CancelledMemberDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cancel-cursor"
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

	// A timeline message the member must still see next time.
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "seed question", nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	SetMemberCursor(mA, 0)

	// Host routes to A; A's turn is CANCELLED (CancelReason set, Err empty).
	script := map[string][]string{
		hostID: {`<clawbench-speaker>A</clawbench-speaker> 请表态`},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			return groupMemberResult{CancelReason: cancelReasonUser}
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if got := GetMemberCursor(mA); got != 0 {
		t.Fatalf("a cancelled member turn must not advance the cursor, got %d", got)
	}
}

// The cursor must be set to the PRE-speech high-water (design §5.1), not the
// post-speech one. A synchronous HTTP write can land a row WHILE the member is
// speaking (adding/removing a member writes a role='system' event without going
// through the queue); that row was never injected to the member, so advancing
// past it would swallow it forever.
//
// This simulates that race: the runner writes a NEW system row (higher id) in
// the middle of its turn, AFTER the pre-speech high-water was captured. The
// cursor must stay at the pre-speech mark so the system row is still injected
// next round.
func TestGroupOrchestrator_CursorUsesPreSpeechHighWater(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-preh"
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
	// Seed so the pre-speech high-water is a known, non-zero id.
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "seed", nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	SetMemberCursor(mA, 0)

	// The id of the row written DURING A's turn (a concurrent membership
	// event). The cursor must stay strictly below it so it is still injected
	// next round.
	var concurrentRowID int64

	script := map[string][]string{
		hostID: {`<clawbench-speaker>A</clawbench-speaker> 请表态`, `<clawbench-group-end/> 结束`},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			// A concurrent HTTP write lands a system event DURING A's turn,
			// AFTER the orchestrator captured the pre-speech high-water.
			id, err := AddSystemMessage(project, groupID, "B 加入了讨论")
			if err == nil {
				concurrentRowID = id
			}
			return base(ctx, gid, turn)
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if concurrentRowID == 0 {
		t.Fatal("precondition: the concurrent system row must have been written")
	}
	// Post-speech high-water would be A's own reply id (after the system row) —
	// swallowing it. Pre-speech high-water stays below the system row.
	if got := GetMemberCursor(mA); got >= concurrentRowID {
		t.Fatalf("cursor %d must stay below the concurrent row %d (post-speech high-water swallowed it)", got, concurrentRowID)
	}
}

// The round-robin FALLBACK path must also honour the cancel rule (decision
// #69): unlike the host/routed member loops, speakNextMember has no early
// return, so its cursor rule lives entirely in advanceCursorOnSuccess. A
// cancelled fallback turn must not advance the cursor either.
func TestGroupOrchestrator_CancelledFallbackDoesNotAdvanceCursor(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cancel-fallback"
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
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "seed", nil, false, "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	SetMemberCursor(mA, 0)

	// The host never emits a parseable route, so every round falls back to
	// round-robin; the fallback member's turn is CANCELLED.
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"(no tag)"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		return groupMemberResult{CancelReason: cancelReasonUser}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if got := GetMemberCursor(mA); got != 0 {
		t.Fatalf("a cancelled fallback turn must not advance the cursor, got %d", got)
	}
}

// A host that keeps failing must terminate the turn, not burn the whole round
// budget. Design §6: "主持人跑挂 → 可见错误 + 回退轮转继续；**再失败终止群回合**".
// Without a host-failure counter the loop runs every round (a broken host
// backend means maxRounds host turns + 10 members monologuing).
func TestGroupOrchestrator_AbortsAfterRepeatedHostFailure(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-hostfail"
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
	// A large cap: without early termination the host would run ~this many times.
	if err := SetGroupMaxRounds(groupID, 20); err != nil {
		t.Fatalf("SetGroupMaxRounds: %v", err)
	}

	hostTurns := 0
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			hostTurns++
			return groupMemberResult{Err: "host backend down"}
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	// Design §6: fall back once, then terminate on the next failure. Allow a
	// small margin (the fallback + one retry) but nowhere near maxRounds.
	if hostTurns > 4 {
		t.Fatalf("repeated host failure must terminate the turn, but the host ran %d turns", hostTurns)
	}
	_ = hostID
}

// A successful host turn must RESET the host-failure streak, so an occasional
// transient failure does not accumulate across a long discussion and abort it.
func TestGroupOrchestrator_SuccessfulHostResetsFailureStreak(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-hostreset"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	// Alternating: host fails once (falls back), then succeeds and routes to A,
	// then fails once again, then succeeds and ends. If the streak did NOT
	// reset, the second failure would abort before the end signal.
	hostTurns := 0
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			hostTurns++
			switch hostTurns {
			case 1:
				return groupMemberResult{Err: "transient 1"}
			case 3:
				return groupMemberResult{Err: "transient 2"}
			case 2:
				_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
					`{"blocks":[{"type":"text","text":"<clawbench-speaker>A</clawbench-speaker> 请 A 表态"}]}`, nil, false, "", hostID)
				return groupMemberResult{}
			default:
				_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
					`{"blocks":[{"type":"text","text":"<clawbench-group-end/> 结束"}]}`, nil, false, "", hostID)
				return groupMemberResult{}
			}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"A 的观点"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "开始", nil)

	if res.Err != "" {
		t.Fatalf("the turn must reach the end signal (streak reset on success), got Err=%q", res.Err)
	}
	_ = mA
}

// ── BCC (密送) ────────────────────────────────────────────────────────────
//
// The host may attach a private note to a named speaker. End to end: only that
// speaker's prompt carries it; every other member's prompt is free of it, and a
// note addressed to someone NOT named this round reaches nobody.

func TestGroupOrchestrator_BccOnlyToTarget(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-bcc"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	script := map[string][]string{
		hostID: {`<clawbench-speaker>A,B</clawbench-speaker> 请分别表态<clawbench-bcc targets="A">SECRET_FOR_A</clawbench-bcc>`, `<clawbench-group-end/> 结束`},
		mA:     {"A 发言"},
		mB:     {"B 发言"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	var aPrompt, bPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		switch turn.MemberRowID {
		case mA:
			aPrompt = turn.Prompt
		case mB:
			bPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if !strings.Contains(aPrompt, "SECRET_FOR_A") {
		t.Fatalf("A (the target) must receive the note; got %q", aPrompt)
	}
	if !strings.Contains(aPrompt, "主持人密送给你") {
		t.Fatalf("A's note must be labeled as a private note; got %q", aPrompt)
	}
	if strings.Contains(bPrompt, "SECRET_FOR_A") {
		t.Fatalf("B must NOT receive A's note; got %q", bPrompt)
	}
}

// A note addressed to a member the host did not name this round reaches nobody.
func TestGroupOrchestrator_BccToUnnamedReachesNobody(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-bcc-unnamed"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	// Only A is named, but the note is addressed to B (and C, who is not a member).
	script := map[string][]string{
		hostID: {`<clawbench-speaker>A</clawbench-speaker> 表态<clawbench-bcc targets="B,C">不该有人收到</clawbench-bcc>`, `<clawbench-group-end/> 结束`},
		mA:     {"A 发言"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	var aPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			aPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)
	_ = mB

	if strings.Contains(aPrompt, "不该有人收到") {
		t.Fatalf("a note to an unnamed member must reach nobody; got %q", aPrompt)
	}
}

// A member's turn prompt must carry the group-role instruction: it is a
// participant, NOT the chair, and must not emit the host's protocol tags.
// Without it members imitated the host's routing tag and fought over the
// microphone (real incident: three members opened with "🎙️ 主持人").
func TestGroupOrchestrator_MemberPromptCarriesGroupRole(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-memberrole"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	script := map[string][]string{
		hostID: {`<clawbench-speaker>A</clawbench-speaker> 请表态`, `<clawbench-group-end/> 结束`},
		mA:     {"A 发言"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	var aPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == mA {
			aPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)

	if !strings.Contains(aPrompt, "群聊成员") || !strings.Contains(aPrompt, "不是主持人") {
		t.Fatalf("member prompt must state the participant role; got %q", aPrompt)
	}
	if !strings.Contains(aPrompt, "不要") || !strings.Contains(aPrompt, "clawbench-speaker") {
		t.Fatalf("member prompt must forbid emitting routing tags; got %q", aPrompt)
	}
	// The host's routing list must NOT be handed to a member.
	if strings.Contains(aPrompt, "可选的成员名：") {
		t.Fatalf("member prompt must not carry the host's routing list; got %q", aPrompt)
	}
}
