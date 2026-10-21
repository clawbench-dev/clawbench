package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"clawbench/internal/store"
)

// Group unread is one item per TURN, not one per member speech.
//
// A group turn writes one assistant row per speaker, so the generic
// per-message unread count made the badge climb with every member — during a
// discussion the user is watching, the session flipped unread between speakers
// and the count kept rising. What the user owes a group is a single thing: a
// turn. These tests pin the replacement rule (awaiting_user_at, a flag cleared
// by UpdateLastRead).

// unreadOf returns the badge count GetSessions reports for one session.
func unreadOf(t *testing.T, project, sessionID string) int {
	t.Helper()
	sessions, err := GetSessions(project, "")
	if err != nil {
		t.Fatalf("GetSessions: %v", err)
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return s.UnreadCount
		}
	}
	t.Fatalf("session %q not in GetSessions", sessionID)
	return 0
}

// awaitingOf reports whether the group's "waiting on you" flag is set.
func awaitingOf(t *testing.T, groupID string) bool {
	t.Helper()
	var isNull int
	if err := store.UnsafeDBForTest().QueryRow(
		"SELECT awaiting_user_at IS NULL FROM chat_sessions WHERE id = ?", groupID,
	).Scan(&isNull); err != nil {
		t.Fatalf("read awaiting_user_at: %v", err)
	}
	return isNull == 0
}

// Three members speaking during one turn must produce ONE unread item, not
// three — and none at all until the turn hands the floor back.
func TestGroupUnread_MemberSpeechesDoNotCountIndividually(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-members"
	groupID, hostID, byName := createHostGroup(t, project, "A", "B")

	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A,B">请分别表态</clawbench-mention>`,
			`讨论充分。<clawbench-group-end/> 结论：一致。`,
		},
		byName["A"]: {"A 的观点"},
		byName["B"]: {"B 的观点"},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	// Mark the group read up front so the assertion below cannot be satisfied by
	// "never read at all".
	UpdateLastRead(groupID)

	_ = runGroupTurnForTest(t, o, project, "大家讨论一下", nil)

	// Four assistant rows landed (host ×2, A, B) but the user owes ONE turn.
	if n := unreadOf(t, project, groupID); n != 1 {
		t.Fatalf("unread=%d want 1 (one finished turn = one item, regardless of how many members spoke)", n)
	}

	// Reading it clears the badge.
	UpdateLastRead(groupID)
	if n := unreadOf(t, project, groupID); n != 0 {
		t.Fatalf("unread=%d want 0 after reading", n)
	}
}

// A turn that ends in a cancel or an error does NOT hand the floor back: the
// user must read the failure first, so no unread item is raised for it.
func TestGroupUnread_CancelledTurnDoesNotRaiseUnread(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-cancel"
	groupID, hostID, _ := createHostGroup(t, project, "A")

	runner := func(_ context.Context, _ string, _ groupMemberTurn) groupMemberResult {
		return groupMemberResult{CancelReason: "user"}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = hostID

	UpdateLastRead(groupID)
	_ = runGroupTurnForTest(t, o, project, "做点事", nil)

	if n := unreadOf(t, project, groupID); n != 0 {
		t.Fatalf("unread=%d want 0 (a cancelled turn is not the user's turn)", n)
	}
}

// A satisfied cruise also ends with the floor back in the user's hands, so it
// must raise the unread item — the only difference from a normal turn is which
// system line announces it.
func TestGroupUnread_CruiseSatisfiedRaisesUnread(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-cruise"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			`差不多了。<clawbench-group-end/>`,
			`确认完成。<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做完了"},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	UpdateLastRead(groupID)
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	if n := unreadOf(t, project, groupID); n != 1 {
		t.Fatalf("unread=%d want 1 (a confirmed cruise also hands the floor back)", n)
	}
}

// The overview path shares the subquery and must agree with the list, including
// the group branch.
func TestGroupUnread_OverviewAgreesWithList(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-overview"
	groupID, hostID, _ := createHostGroup(t, project, "A")

	script := map[string][]string{
		hostID: {`<clawbench-mention targets="A">说说</clawbench-mention>`, `<clawbench-group-end/>`},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	UpdateLastRead(groupID)
	_ = runGroupTurnForTest(t, o, project, "聊聊", nil)

	fromList := unreadOf(t, project, groupID)
	overview, err := GetOverviewSessions()
	if err != nil {
		t.Fatalf("GetOverviewSessions: %v", err)
	}
	fromOverview := -1
	for _, s := range overview {
		if s.ID == groupID {
			fromOverview = s.UnreadCount
		}
	}
	if fromOverview != fromList {
		t.Fatalf("overview unread=%d, list unread=%d — the two must agree", fromOverview, fromList)
	}
	if fromList != 1 {
		t.Fatalf("unread=%d want 1", fromList)
	}
}

// The pending-events read gate must use the same group rule: once the finished
// turn has been read, its queued push is suppressed. If the gate still counted
// member messages, the group would keep notifying for a turn the user already
// read.
func TestGroupUnread_PendingEventSuppressedAfterRead(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-pending"
	groupID, hostID, _ := createHostGroup(t, project, "A")

	script := map[string][]string{
		hostID: {`<clawbench-mention targets="A">说说</clawbench-mention>`, `<clawbench-group-end/>`},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	UpdateLastRead(groupID)
	_ = runGroupTurnForTest(t, o, project, "聊聊", nil)

	// The finished turn re-armed the flag; reading it is what clears it.
	UpdateLastRead(groupID)

	if read := lookupReadSessions([]string{groupID}); !read[groupID] {
		t.Fatal("a group whose finished turn has been read must be reported as read")
	}

	// A NEW finished turn re-arms it: the user owes another turn.
	markGroupUserTurn(groupID)
	if read := lookupReadSessions([]string{groupID}); read[groupID] {
		t.Fatal("a group with a NEW finished turn must not be reported as read")
	}
}

// A multi-message drain must not raise the badge until the LAST message has
// run: mid-drain the floor is still with the AI, and lighting "your turn"
// while two of the user's own messages are still queued is the exact confusion
// this whole change exists to remove.
//
// The sample point is the turn-runner seam, i.e. IMMEDIATELY after a whole
// group turn (runLoop included) has returned and before the drain loop dequeues
// the next message — the only moment at which the decision under test is
// observable.
//
// This is the only test that exercises the `GetQueuedCount(groupID) == 0`
// guard. Verified by mutation: deleting that clause leaves every other test
// green, so without this one the guard could be dropped silently.
func TestGroupUnread_MidDrainDoesNotRaiseBadge(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-drain"
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

	// Two queued messages behind the first: three turns in one drain.
	for _, q := range []string{"q1", "q2"} {
		if _, err := AddQueuedMessage(project, "codebuddy", groupID, "msg "+q, nil, q, ""); err != nil {
			t.Fatalf("AddQueuedMessage(%s): %v", q, err)
		}
	}

	// The member runner only produces speech; the host ends each turn so the
	// drain advances.
	memberRunner := func(_ context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID != hostID {
			return groupMemberResult{}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", gid, "assistant",
			assistantText(`<clawbench-mention targets="A">做</clawbench-mention>`), nil, false, "", hostID)
		return groupMemberResult{}
	}
	restoreRunner := SetGroupDrainRunnerForTest(memberRunner)
	defer SetGroupDrainRunnerForTest(restoreRunner)

	// Observe the flag right AFTER each whole group turn returns. GetQueuedCount
	// still reflects the messages the drain has not yet claimed, which is exactly
	// what the guard under test reads.
	var flagAfterTurn []bool
	orig := SetRunGroupTurnDrainForTest(func(ctx context.Context, gid string, msgID int64, row QueuedRow, runner groupTurnRunner) DrainResult {
		res := RunGroupTurnDrain(ctx, gid, msgID, row, runner)
		flagAfterTurn = append(flagAfterTurn, awaitingOf(t, gid))
		return res
	})
	defer SetRunGroupTurnDrainForTest(orig)

	UpdateLastRead(groupID)
	firstID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	RunGroupDrainLoop(context.Background(), groupID, project, firstID, "开始", nil)

	// Three turns ran; the first two ended with queued messages still waiting.
	if len(flagAfterTurn) != 3 {
		t.Fatalf("expected 3 turns, got %d (%v)", len(flagAfterTurn), flagAfterTurn)
	}
	if flagAfterTurn[0] || flagAfterTurn[1] {
		t.Fatalf("the badge was raised while queued messages were still waiting (%v) — "+
			"the floor is not back until the last one runs", flagAfterTurn)
	}
	// After the whole drain the queue is empty, so the badge IS raised.
	if !flagAfterTurn[2] {
		t.Fatalf("after the drain finished the group must be waiting on the user (%v)", flagAfterTurn)
	}
}

// A clean turn in which NOBODY spoke is not a turn the user owes anything on.
// The roster can be non-empty while every member has LEFT: the relay then has
// nobody to dispatch to, drains without a single speech, and returns an empty
// result. Without the speech-count guard the badge would tell the user to speak
// in a group that cannot answer.
//
// Verified by mutation: dropping `o.memberSpeeches > 0` makes this fail.
func TestGroupUnread_SilentTurnDoesNotRaiseBadge(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gunread-silent"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	// Both members leave: the roster still lists them (as Left) but the relay
	// has no active speaker, so the turn produces no speech at all.
	for _, name := range []string{"A", "B"} {
		if err := RemoveGroupMember(groupID, byName[name]); err != nil {
			t.Fatalf("RemoveGroupMember(%s): %v", name, err)
		}
	}

	runner, _ := freeScriptedRunner(t, project, groupID, map[string][]string{})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	UpdateLastRead(groupID)
	_ = runGroupTurnForTest(t, o, project, "随便说说", nil)

	if awaitingOf(t, groupID) {
		t.Fatal("a turn where nobody could speak must not tell the user it is their turn")
	}
}

// Every test schema that declares chat_sessions AND exercises a query reading
// awaiting_user_at must declare the column, or the test fails with a bare
// "no such column" that looks like a product bug.
//
// The column is only read by unreadCountSubquery (GetSessions /
// GetSessionsPaged / GetOverviewSessions) and lookupReadSessions, so schemas
// that never touch those are left alone on purpose — adding a column to an
// unrelated minimal fixture would imply it belongs to that subsystem.
//
// This is a source guard, not a runtime one: the failure it prevents only
// appears when someone LATER adds a query call to one of those files, and a
// runtime test cannot anticipate that.
func TestGroupUnread_TestSchemasDeclareAwaitingUserAt(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	const marker = "CREATE TABLE IF NOT EXISTS chat_sessions"
	queries := []string{"GetSessions(", "GetOverviewSessions(", "UpdateLastRead(", "lookupReadSessions("}

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(src)
		if !strings.Contains(text, marker) {
			continue
		}
		usesUnreadQuery := false
		for _, q := range queries {
			if strings.Contains(text, q) {
				usesUnreadQuery = true
				break
			}
		}
		if !usesUnreadQuery {
			continue
		}
		checked++
		if !strings.Contains(text, "awaiting_user_at") {
			t.Errorf("%s defines a chat_sessions schema and calls an unread query, "+
				"but its schema has no awaiting_user_at column — those queries select it, "+
				"so the test will fail with 'no such column'", name)
		}
	}
	if checked == 0 {
		t.Fatal("no test schema matched — the guard's detection is broken, not the schemas")
	}
}
