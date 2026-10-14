package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// ── Auto-cruise (design §15) ─────────────────────────────────────────────
//
// Cruise intercepts every "hand the floor back to the human" point and runs a
// confirmation round instead, ending only when the goal is declared satisfied
// (host mode: the host; free mode: ≥ half the active members).

// createHostGroup builds a host-mode group and returns the group id, the host
// row id, and a row id per member display name. The host is always named "Host".
func createHostGroup(t *testing.T, project string, memberNames ...string) (string, string, map[string]string) {
	t.Helper()
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	const hostName = "Host"
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", hostName)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	byName := map[string]string{hostName: hostID}
	for _, name := range memberNames {
		id, err := AddGroupMember(project, groupID, "claude", "agent-"+strings.ToLower(name), name)
		if err != nil {
			t.Fatalf("AddGroupMember(%s): %v", name, err)
		}
		byName[name] = id
	}
	return groupID, hostID, byName
}

// enableCruise turns the group's cruise switch on.
func enableCruise(t *testing.T, groupID string) {
	t.Helper()
	if err := SetGroupCruise(groupID, true); err != nil {
		t.Fatalf("SetGroupCruise(true): %v", err)
	}
}

// countSystemLine returns how many timeline rows carry the given system text.
func countSystemLine(t *testing.T, groupID, text string) int {
	t.Helper()
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		t.Fatalf("GetMessagesBySessionIDRaw: %v", err)
	}
	n := 0
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), text) {
			n++
		}
	}
	return n
}

// ── host mode ────────────────────────────────────────────────────────────

// The host's end tag no longer ends a cruising turn directly: it triggers a
// confirmation round. When the host confirms, the turn ends with the cruise
// summary line (not "your turn").
func TestCruiseHost_EndTagTriggersConfirmationAndHostConfirms(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-confirm"
	groupID, hostID, byName := createHostGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: route to A.
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: host thinks it is done -> end tag (triggers confirmation).
			`差不多了。<clawbench-group-end/>`,
			// Round 3: confirmation round -> host confirms.
			`确认完成。<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 完成了一部分"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	// The confirmation system line must be written once.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (order=%v)", n, *order)
	}
	// The cruise-complete line must be written (completion confirmed).
	if n := countSystemLine(t, groupID, "自动巡航"); n < 1 {
		t.Fatalf("expected a cruise system line, order=%v", *order)
	}
	// "Your turn" must NOT be written in cruise mode.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn (count=%d)", n)
	}
	// The host must have spoken 3 times: route, end-tag, confirmation. There is
	// NO separate summary turn — the confirmation round's reply already carries
	// the final conclusion (design §15.7, merged wrap-up).
	hostTurns := 0
	for _, id := range *order {
		if id == hostID {
			hostTurns++
		}
	}
	if hostTurns != 3 {
		t.Fatalf("host turns=%d want 3 (route + end + confirm; no separate summary), order=%v", hostTurns, *order)
	}
}

// A host that @-names the USER while cruising must NOT spin forever: cruise
// never hands the floor back, so an un-intercepted @User route yields a dropped
// turn, the loop re-runs the host, and (host turns not counting toward the cap)
// it would never stop. The @User must be treated as a confirmation trigger.
func TestCruiseHost_UserMentionDoesNotSpin(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-usermen"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: route to A so the discussion starts.
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: name the USER -> must trigger confirmation, not loop.
			`你来定。<clawbench-mention targets="User">该你决定了</clawbench-mention>`,
			// Round 3 (confirmation): confirm, ending the turn.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v", res)
	}
	// The turn must have reached confirmation and confirmed (not spun).
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (order=%v)", n, *order)
	}
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 1 {
		t.Fatalf("expected a confirmed completion, count=%d order=%v", n, *order)
	}
	// The turn must be BOUNDED (no spin). Expected host turns: route, the @User
	// turn, the confirmation round, the summary = 4. A spin would blow past this
	// (the buggy path re-runs the host until the parse-failure abort).
	hostTurns := 0
	for _, id := range *order {
		if id == hostID {
			hostTurns++
		}
	}
	if hostTurns > 5 {
		t.Fatalf("host spoke %d times — @User must not spin the loop (order=%v)", hostTurns, *order)
	}
}

// A host that emits goal-satisfied UNSOLICITED (in a normal routing turn, before
// any confirmation was asked) must NOT end the turn directly: an unsolicited
// declaration is exactly the premature wrap-up cruise exists to prevent, so it
// triggers a confirmation round.
//
// The declaration is emitted ALONE — no co-emitted end tag or mention — so this
// isolates the rule: if goal-satisfied stopped triggering confirmation, the turn
// would fall through to routing (round-robin to A) and NO confirmation line
// would be written. (An end tag in the same reply would mask the rule, since it
// triggers confirmation on its own.)
func TestCruiseHost_UnsolicitedGoalSatisfiedTriggersConfirmation(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-goalwins"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: UNSOLICITED declaration, ALONE. It must NOT end directly —
			// it enters a confirmation round.
			`都好了。<clawbench-goal-satisfied/>`,
			// Round 3 (confirmation): confirm.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	// The unsolicited declaration did NOT skip the confirmation round.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("an unsolicited goal-satisfied must still confirm, count=%d order=%v", n, *order)
	}
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 1 {
		t.Fatalf("expected a confirmed completion, count=%d order=%v", n, *order)
	}
	// A spoke exactly once (round 1's route). The unsolicited declaration must NOT
	// have been treated as "route on" (which would round-robin A a second time).
	aTurns := 0
	for _, id := range *order {
		if id == byName["A"] {
			aTurns++
		}
	}
	if aTurns != 1 {
		t.Fatalf("A must speak exactly once (no round-robin fallback), aTurns=%d order=%v", aTurns, *order)
	}
}

// When the host does NOT confirm and instead routes on, the discussion
// continues with the routed members (the "double intent" path).
func TestCruiseHost_NotConfirmedKeepsRouting(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-continue"
	groupID, hostID, byName := createHostGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: route to A.
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: end tag -> triggers confirmation.
			`差不多了。<clawbench-group-end/>`,
			// Round 3 (confirmation): not done -> route to B instead.
			`还没完。<clawbench-mention targets="B">你接手</clawbench-mention>`,
			// Round 4: end tag again -> confirmation.
			`好了。<clawbench-group-end/>`,
			// Round 5 (confirmation): confirm.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
		byName["B"]: {"B 接手做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	// B must have spoken (the not-confirmed route was honored).
	bSpoke := false
	for _, id := range *order {
		if id == byName["B"] {
			bSpoke = true
		}
	}
	if !bSpoke {
		t.Fatalf("B must speak after the host re-routed, order=%v", *order)
	}
	// Two confirmation rounds happened (round 2 and round 4).
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 2 {
		t.Fatalf("confirmation line count=%d want 2 (order=%v)", n, *order)
	}
}

// A host that keeps emitting group-end without ever confirming must not spin:
// each confirmation consumes budget and the cap eventually stops the turn.
func TestCruiseHost_SilentHostBoundedByCap(t *testing.T) {
	setupGroupDB(t)
	setGroupMaxSpeechesForTest(t, 6)
	project := "/tmp/cruise-host-cap"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	// The host ALWAYS emits the end tag and never confirms; the confirmation
	// round therefore always falls back. The cap must stop it.
	script := map[string][]string{}
	for range 40 {
		script[hostID] = append(script[hostID], `结束。<clawbench-group-end/>`)
	}
	script[byName["A"]] = []string{"A 说"}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean cap stop, got %+v", res)
	}
	// The cap must have been hit and the limit line written.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupDiscussionLimitReached")); n != 1 {
		t.Fatalf("limit line count=%d want 1 (order len=%d)", n, len(*order))
	}
	// It must terminate well before the 40 scripted host turns.
	if len(*order) > 20 {
		t.Fatalf("turn did not terminate promptly: %d turns", len(*order))
	}
}

// A non-cruise host group must behave exactly as before (regression guard).
func TestCruiseHost_OffEndsDirectly(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-off"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	// cruise NOT enabled.

	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			`结束。<clawbench-group-end/> 结论。`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 0 {
		t.Fatalf("non-cruise must not confirm (count=%d)", n)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 1 {
		t.Fatalf("non-cruise must announce the user's turn once, count=%d", n)
	}
	hostTurns := 0
	for _, id := range *order {
		if id == hostID {
			hostTurns++
		}
	}
	if hostTurns != 2 {
		t.Fatalf("host turns=%d want 2, order=%v", hostTurns, *order)
	}
}

// ── free mode ────────────────────────────────────────────────────────────

// Free-mode cruise: the relay draining triggers a broadcast confirmation round;
// when ≥ half declare satisfied, the turn ends with the cruise summary.
func TestCruiseFree_ConfirmationReachesHalf(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-free-half"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	script := map[string][]string{
		// Discussion seed (user mentions nobody -> both speak).
		byName["A"]: {"A 说", `<clawbench-goal-satisfied/>`},
		byName["B"]: {"B 说", "B 觉得还不行"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	// n=2 -> ceil(2/2)=1 vote suffices; A voted in the confirmation round.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (order=%v)", n, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn, count=%d", n)
	}
	if n := countSystemLine(t, groupID, "自动巡航"); n < 1 {
		t.Fatalf("expected cruise complete line, order=%v", *order)
	}
}

// Free-mode cruise with no votes keeps alternating discussion and confirmation
// rounds until the cap stops it.
func TestCruiseFree_NoVotesBoundedByCap(t *testing.T) {
	setupGroupDB(t)
	setGroupMaxSpeechesForTest(t, 8)
	project := "/tmp/cruise-free-cap"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	// Nobody ever votes.
	script := map[string][]string{
		byName["A"]: {"A 一直说"},
		byName["B"]: {"B 一直说"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean cap stop, got %+v", res)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupDiscussionLimitReached")); n != 1 {
		t.Fatalf("limit line count=%d want 1 (order=%v)", n, *order)
	}
	// The cruise-complete line must NOT appear (never confirmed).
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 0 {
		t.Fatalf("no completion line expected, count=%d", n)
	}
	if len(*order) > 30 {
		t.Fatalf("turn did not terminate promptly: %d turns", len(*order))
	}
}

// A member's goal-satisfied from an EARLIER round must not count for a later
// confirmation round (per-round reset). Every active member speaks once in the
// confirmation round, so the reset only matters when a member's confirmation
// turn produces NO output (an error): its earlier vote must not be inherited.
// Here A votes in the seed discussion, then A's confirmation turn FAILS and B
// does not vote — with n=2 that is 0 valid votes, so the turn must not confirm.
func TestCruiseFree_EarlierVoteDoesNotCount(t *testing.T) {
	setupGroupDB(t)
	setGroupMaxSpeechesForTest(t, 4)
	project := "/tmp/cruise-free-reset"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	// A: seed vote, then its confirmation turn ERRORS (no output written).
	// B: never votes.
	script := map[string][]string{
		byName["A"]: {`完成 <clawbench-goal-satisfied/>`},
		byName["B"]: {"B 说", "B 说"},
	}
	base, order := freeScriptedRunner(t, project, groupID, script)
	counts := map[string]int{}
	var mu sync.Mutex
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		counts[turn.MemberRowID]++
		n := counts[turn.MemberRowID]
		mu.Unlock()
		// A's SECOND turn (the confirmation round) fails: no output persisted.
		if turn.MemberRowID == byName["A"] && n == 2 {
			return groupMemberResult{Err: "boom"}
		}
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	// A's stale seed vote must NOT confirm the turn (its confirmation turn
	// failed, so it cast no vote this round).
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 0 {
		t.Fatalf("an earlier-round vote must not confirm, count=%d order=%v", n, *order)
	}
	// The turn must still stop (the cap bounds it).
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupDiscussionLimitReached")); n != 1 {
		t.Fatalf("expected the cap to stop the turn, count=%d order=%v", n, *order)
	}
}

// n=3 with 2 votes reaches ceil(3/2)=2 -> confirmed.
func TestCruiseFree_ThreeMembersTwoVotesConfirms(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-free-three"
	groupID, byName := createFreeGroup(t, project, "A", "B", "C")
	enableCruise(t, groupID)

	script := map[string][]string{
		byName["A"]: {"A 说", `<clawbench-goal-satisfied/>`},
		byName["B"]: {"B 说", `<clawbench-goal-satisfied/>`},
		byName["C"]: {"C 说", "C 保留意见"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 1 {
		t.Fatalf("2/3 votes must confirm, count=%d order=%v", n, *order)
	}
}

// The confirmation round discards @-mentions: a member that @s a peer during
// confirmation must not spawn an extra speaker from that mention.
//
// The assertion is the EXACT turn sequence, not "the tail is a permutation":
// with the mention wrongly honored, B's confirmation @A appends an extra A turn
// (order becomes [A,B,A,B,A]); with it discarded the turn ends right after the
// confirmation round ([A,B,A,B]). A permutation-of-tail assertion passes in
// BOTH cases, so it cannot fail for the defect it names.
func TestCruiseFree_ConfirmRoundDiscardsMentions(t *testing.T) {
	setupGroupDB(t)
	setGroupMaxSpeechesForTest(t, 10)
	project := "/tmp/cruise-free-discard"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	script := map[string][]string{
		// Seed: A @s B so B speaks; B says plain.
		byName["A"]: {`<clawbench-mention targets="B">B 补充</clawbench-mention>`, `<clawbench-goal-satisfied/>`},
		// B, during confirmation, @s A. That must be discarded (no extra A turn);
		// A's confirmation vote alone reaches the 1/2 threshold and ends the turn.
		byName["B"]: {"B 补充", `<clawbench-mention targets="A">A 你再看</clawbench-mention>`},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	// Exactly: seed A, seed B, confirmation A, confirmation B. No fifth turn.
	want := []string{byName["A"], byName["B"], byName["A"], byName["B"]}
	if len(*order) != len(want) {
		t.Fatalf("order=%v want exactly %v (an honored confirmation @A would add a 5th turn)", *order, want)
	}
	for i := range want {
		if (*order)[i] != want[i] {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], want[i], *order)
		}
	}
}

// A non-cruise free group must behave exactly as before (regression guard).
func TestCruiseFree_OffNaturalExhaustionAnnouncesUserTurn(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-free-off"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	// cruise NOT enabled.

	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 0 {
		t.Fatalf("non-cruise must not confirm, count=%d order=%v", n, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 1 {
		t.Fatalf("non-cruise must announce the user's turn once, count=%d", n)
	}
}

// A member @-mentioning the user during a cruising discussion must NOT hand the
// floor back (cruise never returns to the human): the turn continues to the
// confirmation round and ends confirmed.
func TestCruiseFree_UserMentionDoesNotHandBack(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-free-usermen"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	script := map[string][]string{
		// A hands the floor to the user; in cruise this must be ignored.
		byName["A"]: {`<clawbench-mention targets="User">你来定</clawbench-mention>`, `<clawbench-goal-satisfied/>`},
		byName["B"]: {"B 说", `<clawbench-goal-satisfied/>`},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	// The turn must reach the confirmation round and be confirmed, not stop at
	// the @User.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (order=%v)", n, *order)
	}
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 1 {
		t.Fatalf("expected a confirmed completion, count=%d order=%v", n, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn, count=%d", n)
	}
}

// cancelCruiseContext is a small guard that the free cruise loop honors
// cancellation rather than looping forever.
func TestCruiseFree_CancelStops(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-free-cancel"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	ctx, cancel := context.WithCancel(context.Background())
	// The runner cancels the context on the first confirmation-round turn so the
	// loop must observe it and stop.
	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说"},
	}
	runner, _ := freeScriptedRunner(t, project, groupID, script)
	wrapped := func(c context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		cancel()
		return runner(c, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = wrapped
	msgID, err := AddChatMessageWithAgent(project, groupBackend(groupID), groupID, "user", "大家聊聊", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	res := RunGroupTurnDrain(ctx, groupID, msgID, QueuedRow{Content: "大家聊聊"}, o.runTurn)
	if res.CancelReason == "" && res.Err == "" {
		t.Fatalf("expected a cancel/error, got %+v", res)
	}
}
