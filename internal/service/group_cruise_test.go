package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/grouprouting"
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
	if err := SetSessionCruise(groupID, true); err != nil {
		t.Fatalf("SetSessionCruise(true): %v", err)
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

// A host that considers the task done simply writes a closing paragraph with NO
// routing tag — the most common "hand the floor back" shape in practice. In
// cruise mode this MUST trigger a confirmation round, exactly like an explicit
// <clawbench-group-end/>. Before the fix it fell into the consecutive-parse-
// failure abort (summarize + stop), so cruise never confirmed — the reported
// "cruise is on but no confirmation round ever happens".
//
// Two untagged host turns are needed to reach the escalation: the FIRST is
// tolerated with a round-robin fallback (that tolerance is pinned separately by
// TestGroupOrchestrator_AbortsAfterTwoParseFailures for the non-cruise case).
func TestCruiseHost_UntaggedClosingProseTriggersConfirmation(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-untagged"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: normal routing so the discussion starts.
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: untagged prose -> parse failure #1 (tolerated, fallback).
			`都差不多了，先汇报一下进展。`,
			// Round 3: untagged prose again -> escalation. In cruise this is a
			// confirmation trigger, NOT an abort.
			`这是最终结论，大家都辛苦了。`,
			// Round 4 (confirmation round): confirm completion.
			`确认完成。<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, *order)
	}
	// The confirmation round must have run (the whole point of the fix).
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (order=%v)", n, *order)
	}
	// And it must have been confirmed (the host answered goal-satisfied), which
	// is what writes the cruise-complete line.
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 1 {
		t.Fatalf("expected a confirmed completion, count=%d order=%v", n, *order)
	}
	// Cruise never announces the user's turn.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn (count=%d)", n)
	}
}

// The same untagged-prose escalation must NOT change non-cruise behavior: a
// non-cruise host with two consecutive unparseable turns still aborts (decision
// #56). This is the other half of hostRouteFailure's branch — without it the
// cruise fix could silently make non-cruise hosts spin.
func TestCruiseOff_UntaggedProseStillAborts(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-off-untagged"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	// cruise NOT enabled.

	hostTurns := 0
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			hostTurns++
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"没有标签的收尾散文。"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":"成员发言"}]}`, nil, false, "", turn.MemberRowID)
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始", nil)
	_ = byName

	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 0 {
		t.Fatalf("non-cruise must not confirm (count=%d)", n)
	}
	// 2 failures, then abort. Small margin for the final summary turn.
	if hostTurns > 4 {
		t.Fatalf("non-cruise must still abort after two parse failures; host ran %d turns", hostTurns)
	}
}

// A host that routes to a member AND closes with a plain-language question to
// the human ("要我继续推进吗？") has WORK TO HAND OUT, so the route wins: the
// named member speaks and the question is display-only.
//
// This is the reported bug in its prose form. Before the fix the question won,
// the turn was diverted into a confirmation round, and resolveSpeakerGroups was
// never reached — the named member silently never spoke while the host asked the
// user a question that cruise is supposed to make unnecessary.
func TestCruiseHost_ProseQuestionAfterRouteStillDispatches(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-proseq"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: route to A, then ask the user a question in prose.
			"先派活。\n\n<clawbench-mention targets=\"A\">先做</clawbench-mention>\n\n**要我继续推进吗？**",
			// Round 2: declare done -> confirmation round.
			`好了。<clawbench-goal-satisfied/>`,
			// Round 3 (confirmation): confirm.
			`确认完成。<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, *order)
	}
	// The route must have been dispatched: A speaks BEFORE any confirmation.
	aSpoke := false
	for _, id := range *order {
		if id == byName["A"] {
			aSpoke = true
			break
		}
	}
	if !aSpoke {
		t.Fatalf("A must speak — a route outranks the co-emitted question (order=%v)", *order)
	}
	// Exactly ONE confirmation: round 2's goal-satisfied. Round 1's question
	// must NOT have produced one (that would mean the route was discarded).
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (route must not confirm; order=%v)", n, *order)
	}
	// Cruise never announces the user's turn.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn (count=%d)", n)
	}
}

// A prose question with NOTHING to dispatch is still a hand-back: the host has
// no work to hand out, so the question must trigger a confirmation round
// instead of leaving the turn to spin. This is the other half of the precedence
// rule — dropping the question trigger entirely would hang the turn here.
func TestCruiseHost_ProseQuestionWithoutRouteTriggersConfirmation(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-proseq-noroute"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: route to A so the discussion starts.
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: no route, just a question to the human -> confirmation.
			`我做完了。\n\n要我继续推进吗？`,
			// Round 3 (confirmation): confirm.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("a question with no route must trigger a confirmation round; count=%d (order=%v)", n, *order)
	}
}

// THE reported bug: the host routes to two members AND emits an AskUserQuestion
// card in the same turn. The card must not swallow the route — both members
// speak, and the question is merely displayed.
//
// This is the exact timeline that was reported: "@ 了两拨人，一个都没动，反过来问我
// 问题". The card is persisted as the converted tool_use block, so memberAskedQuestion
// detects it — the route must still win.
func TestCruiseHost_AskQuestionCardAfterRouteStillDispatches(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-askcard"
	groupID, hostID, byName := createHostGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	// Round 1: route to A and B in parallel AND ask the user a question.
	round1 := "先派活。\n\n<clawbench-mention targets=\"A,B\" mode=\"parallel\">各自出方案</clawbench-mention>\n\n另外问一下用户。" +
		"\n<clawbench-ask-question>\n**首发集**\n先做哪一集？\n- EP01\n- EP02\n</clawbench-ask-question>"
	texts := map[string][]string{
		hostID:      {round1, `好了。<clawbench-goal-satisfied/>`, `<clawbench-goal-satisfied/>`},
		byName["A"]: {"A 出了方案"},
		byName["B"]: {"B 出了方案"},
	}
	counts := map[string]int{}
	var order []string
	// A parallel mention runs the members concurrently (`runParallelGroup` fans
	// them out through an errgroup), so this scripted runner is called from
	// several goroutines at once. `counts` (a map) and `order` (a slice) are NOT
	// safe for that: Go 1.25 turns a concurrent map write into an unrecoverable
	// `internal/runtime/maps.fatal`, which crashes the whole test binary with no
	// `--- FAIL` line (observed on the ubuntu CI runner). The sibling test at
	// freeScriptedRunner below already locks for the same reason.
	var mu sync.Mutex
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		order = append(order, turn.MemberRowID)
		seq := texts[turn.MemberRowID]
		i := counts[turn.MemberRowID]
		counts[turn.MemberRowID]++
		mu.Unlock()
		text := "(no script)"
		if i < len(seq) {
			text = seq[i]
		}
		content := `{"blocks":[{"type":"text","text":` + jsonQuote(text) + `}]}`
		if strings.Contains(text, "<clawbench-ask-question>") {
			// Persist the CONVERTED form, which is what memberAskedQuestion reads.
			content = askQuestionBlocks(text)
		}
		if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			content, nil, false, "", turn.MemberRowID); err != nil {
			return groupMemberResult{Err: err.Error()}
		}
		return groupMemberResult{}
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, order)
	}
	// BOTH named members must have spoken. This is the bug: they never did.
	spoke := map[string]bool{}
	for _, id := range order {
		spoke[id] = true
	}
	if !spoke[byName["A"]] || !spoke[byName["B"]] {
		t.Fatalf("both routed members must speak despite the question card; spoke=%v (order=%v)", spoke, order)
	}
	// Exactly one confirmation (the later goal-satisfied), not one from round 1.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (the card must not divert round 1; order=%v)", n, order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn (count=%d)", n)
	}
}

// routeHasDispatchableMembers is the precedence test's whole contract in one
// table: it must agree with resolveCruiseSpeakerGroups EXACTLY. A member it calls
// dispatchable that the resolver then drops would skip the confirmation round
// and produce no speakers — the spin the confirmation exists to prevent.
func TestRouteHasDispatchableMembers(t *testing.T) {
	members := []GroupMember{
		{ID: "m1", Name: "A"},
		{ID: "m2", Name: "B", Disabled: true},
		{ID: groupUserTargetID, Name: "User"},
	}
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"no route", "没有标签", false},
		{"live member", `<clawbench-mention targets="A">做</clawbench-mention>`, true},
		{"disabled member only", `<clawbench-mention targets="B">做</clawbench-mention>`, false},
		{"user only", `<clawbench-mention targets="User">你来定</clawbench-mention>`, false},
		{"unknown name only", `<clawbench-mention targets="Nope">做</clawbench-mention>`, false},
		{"host only", `<clawbench-mention targets="Host">我自己来</clawbench-mention>`, false},
		{"mixed user and member", `<clawbench-mention targets="User,A">都来</clawbench-mention>`, true},
		{"private note only", `<clawbench-mention targets="A" private>悄悄说</clawbench-mention>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route := grouprouting.Parse(tc.out)
			got := routeHasDispatchableMembers(route, members, "host-id")
			if got != tc.want {
				t.Fatalf("routeHasDispatchableMembers(%q)=%v want %v", tc.out, got, tc.want)
			}
			// Agreement with the resolver: a "true" must yield at least one turn.
			if got {
				groups := resolveCruiseSpeakerGroups(route, members, "host-id", func(m GroupMember, instruction string) speakerTurn {
					return speakerTurn{member: m}
				})
				if len(groups) == 0 {
					t.Fatalf("disagreement: dispatchable=true but resolver yielded no groups (%q)", tc.out)
				}
			}
		})
	}
}

// The prose-question detector must NOT fire on a question addressed to a routed
// MEMBER (inside a mention span). This is the isolated integration case: the
// LAST line of the host turn is a question, but it sits inside the mention span,
// so it is addressed to A (who will answer it) — not a hand-back. Without the
// mention-strip step the turn would divert into a confirmation round.
func TestCruiseHost_ProseQuestionIgnoresMemberQuestions(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-proseq-neg"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	enableCruise(t, groupID)

	script := map[string][]string{
		hostID: {
			// Round 1: the ONLY question is inside the mention span, and it is
			// the last thing in the message.
			"派活。\n\n<clawbench-mention targets=\"A\">这个方案行不行？</clawbench-mention>",
			// Round 2: real end tag -> confirmation.
			`差不多了。<clawbench-group-end/>`,
			// Round 3 (confirmation): confirm.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, *order)
	}
	// A must have spoken: the round-1 route was honored (NOT diverted into a
	// confirmation round by the in-span question).
	aSpoke := false
	for _, id := range *order {
		if id == byName["A"] {
			aSpoke = true
		}
	}
	if !aSpoke {
		t.Fatalf("A must speak — the in-span question is not a hand-back (order=%v)", *order)
	}
	// Exactly ONE confirmation (the explicit end tag in round 2), not two.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("confirmation line count=%d want 1 (only the end tag; order=%v)", n, *order)
	}
}

// hostAsksUserInProse is the whole detection contract in one table. Each case
// isolates ONE rule, so removing a guard turns a specific case red.
func TestHostAsksUserInProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain prose question", "都做完了。\n\n要我继续推进吗？", true},
		{"halfwidth question", "Done with the draft.\n\nShall I continue?", true},
		{"bolded question", "先这样。\n\n**要我继续推进吗？**", true},
		// The last-line rule is what makes this precise: a mid-explanation
		// rhetorical question must NOT count, or every routing turn would divert.
		{"question not on the last line", "为什么没挡住？\n\n我已经修好了，派 A 继续。", false},
		// A mention span ends with `>`, so it can never be the last-line question.
		{"question inside a mention span", `<clawbench-mention targets="A">这个方案行不行？</clawbench-mention>`, false},
		{"question inside a mention then routed", "要我继续吗？\n\n<clawbench-mention targets=\"A\">继续做</clawbench-mention>", false},
		// A code span ends with a backtick, so the suffix test fails as desired.
		{"question in a code fence", "看这段：\n\n```\nls -la?\n```", false},
		{"question in inline code", "路径是 `ls -la?` 那个。", false},
		{"statement only", "都做完了，已派 A 收尾。", false},
		{"empty", "", false},
		{"whitespace only", "   \n\n  ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostAsksUserInProse(tc.in); got != tc.want {
				t.Fatalf("hostAsksUserInProse(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Non-cruise is UNCHANGED: a host question is how the host addresses the human,
// so it hands the floor back — it must NOT be diverted by the cruise precedence
// rule (that rule exists only inside the cruiseOn() branch).
//
// This pins the boundary the cruise fix must not cross: outside cruise, asking
// the user is the host's normal way of talking to them, and the named member
// speaking first does not make the question go away — the turn still ends with
// the floor back at the user.
func TestCruiseOff_HostQuestionHandsFloorBack(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-off-question"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	// cruise NOT enabled.

	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			`我做完了一版。\n\n要我继续吗？`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "做个任务", nil)

	if n := countUserTurnLines(t, groupID); n != 1 {
		t.Fatalf("non-cruise host question must hand the floor back exactly once, count=%d (order=%v)", n, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 0 {
		t.Fatalf("non-cruise must never write a cruise confirmation line, count=%d", n)
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

// Toggling cruise ON while a turn is already running must take effect on the
// NEXT hand-back point, without waiting for a new user message. This pins the
// live switch read (cruiseOn): with a start-of-turn snapshot, the end tag in
// round 2 would end the turn directly and NO confirmation round would run.
func TestCruiseHost_SwitchToggledMidTurnTakesEffect(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-host-live-toggle"
	groupID, hostID, byName := createHostGroup(t, project, "A")
	// cruise starts OFF.

	script := map[string][]string{
		hostID: {
			// Round 1: route to A. The runner flips cruise ON while this turn
			// runs (see the wrapper below).
			`<clawbench-mention targets="A">先做</clawbench-mention>`,
			// Round 2: end tag. Cruise is now ON, so this must trigger a
			// confirmation round rather than ending the turn.
			`差不多了。<clawbench-group-end/>`,
			// Round 3 (confirmation): confirm.
			`<clawbench-goal-satisfied/>`,
		},
		byName["A"]: {"A 做了"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	// Flip cruise on after the host's FIRST turn (once the turn is underway).
	hostTurns := 0
	wrapped := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		res := runner(ctx, gid, turn)
		if turn.MemberRowID == hostID {
			hostTurns++
			if hostTurns == 1 {
				if err := SetSessionCruise(groupID, true); err != nil {
					t.Fatalf("SetSessionCruise: %v", err)
				}
			}
		}
		return res
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = wrapped
	res := runGroupTurnForTest(t, o, project, "做个任务", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean confirmed end, got %+v (order=%v)", res, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupCruiseConfirming")); n != 1 {
		t.Fatalf("mid-turn cruise toggle must take effect; confirmation count=%d want 1 (order=%v)", n, *order)
	}
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 0 {
		t.Fatalf("cruise must not announce the user's turn (count=%d)", n)
	}
}

// Turning cruise OFF mid-turn must end a free-mode relay at the NEXT decision
// point, not at the member-speech cap.
//
// cruiseFreeRound is the only exit for a cruising free group, and it used to
// check nothing but the cap: the switch was read once before entering, so a
// user who switched cruise off while the group was talking watched it keep
// going (default cap: 100 member speeches). Host mode already re-read the
// switch at every hand-back point — this pins the free-mode equivalent.
func TestCruiseFree_SwitchToggledOffMidTurnStops(t *testing.T) {
	setupGroupDB(t)
	// High cap: without the fix the loop runs until this is exhausted, which is
	// exactly the symptom. The test would also time out, so keep it bounded.
	setGroupMaxSpeechesForTest(t, 100)
	project := "/tmp/cruise-free-live-off"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	enableCruise(t, groupID)

	// Nobody ever votes, so the cruise would loop forever if the switch were
	// ignored.
	script := map[string][]string{
		byName["A"]: {"A 一直说"},
		byName["B"]: {"B 一直说"},
	}
	base, order := freeScriptedRunner(t, project, groupID, script)

	// Flip cruise OFF once the discussion round that follows the first
	// confirmation round is under way (member turn #5). Counting turns rather
	// than hooking a specific member keeps the assertion independent of the
	// confirmation round's internal ordering.
	wrapped := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		res := base(ctx, gid, turn)
		if len(*order) == 5 {
			if err := SetSessionCruise(groupID, false); err != nil {
				t.Fatalf("SetSessionCruise(false): %v", err)
			}
		}
		return res
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = wrapped
	res := runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("expected a clean stop, got %+v (order=%v)", res, *order)
	}
	// The switch must be honored at the top of the next iteration: seed(2) +
	// confirmation(2) + discussion(2) = 6 turns, then stop. Anything close to
	// the cap means the check never ran.
	if len(*order) > 10 {
		t.Fatalf("cruise ignored the mid-turn switch-off: %d member turns (order=%v)", len(*order), *order)
	}
	// A cap stop writes its own line; this was a voluntary switch-off, so the
	// cap line must be absent.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupDiscussionLimitReached")); n != 0 {
		t.Fatalf("switch-off must not be reported as hitting the speech cap (count=%d)", n)
	}
	// runLoop's non-cruise branch announces the floor returning to the human.
	if n := countSystemLine(t, groupID, i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")); n != 1 {
		t.Fatalf("switch-off must hand the floor back to the user (count=%d want 1, order=%v)", n, *order)
	}
	// And it is NOT a confirmed completion.
	if n := countSystemLine(t, groupID, "自动巡航：任务已确认完成"); n != 0 {
		t.Fatalf("switch-off must not be reported as a confirmed goal (count=%d)", n)
	}
}
