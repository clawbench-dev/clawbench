package service

import (
	"context"
	"testing"
)

// Group unread is one item per TURN, not one per member speech.
//
// A group turn writes one assistant row per speaker, so the generic
// per-message unread count made the badge climb with every member — during a
// discussion the user is watching, the session flipped unread between speakers
// and the count kept rising. What the user owes a group is a single thing: a
// turn. These tests pin the replacement rule (user_turn_at vs last_read_at).

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
