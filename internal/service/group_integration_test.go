package service

import (
	"context"
	"testing"

	"clawbench/internal/store"
)

// group_integration_test.go holds end-to-end-ish orchestrator tests that run the
// REAL loop (no per-turn scripting of the decision) and the REAL defaultRunner
// wiring. The scripted-runner tests in group_orchestrator_test.go exercise the
// routing state machine; these pin the properties that only show up across a
// full multi-round conversation.

// TestGroupOrchestrator_MultiRoundClosedLoop drives several rounds and asserts
// the full conversational shape: the host routes to a real member each round,
// that member speaks, the NEXT round routes to a different member, and the host
// only speaks once per round. This is the shape the "only the host ever speaks"
// bug violated — the host routed to itself and no member ever ran.
func TestGroupOrchestrator_MultiRoundClosedLoop(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-loop"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	// Host name == backend name, the shape that triggered the production bug.
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Codebuddy")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	script := map[string][]string{
		hostID: {
			// R1: name SELF first (must be dropped) then route to A.
			`<clawbench-speaker>Codebuddy,A</clawbench-speaker> A 你先说`,
			// R2: route to B.
			`<clawbench-speaker>B</clawbench-speaker> B 补充`,
			// R3: end.
			`充分了。<clawbench-group-end/> 结论：A、B 各抒己见。`,
		},
		mA: {"A 的观点"},
		mB: {"B 的观点"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	term := observeGroupTerminal(t)
	if err := o.RunGroupTurn(context.Background(), "开始讨论"); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}
	if *term != 1 {
		t.Fatalf("terminal emitted %d times, want 1", *term)
	}

	// Exactly one host turn per round (3 rounds), and the host must never be
	// counted as a "member" turn. Members A and B must each run once.
	counts := map[string]int{}
	for _, id := range *order {
		counts[id]++
	}
	if counts[hostID] != 3 {
		t.Fatalf("host turns=%d, want 3 (one per round); order=%v", counts[hostID], *order)
	}
	if counts[mA] != 1 || counts[mB] != 1 {
		t.Fatalf("members must each speak once (A=%d B=%d); order=%v", counts[mA], counts[mB], *order)
	}

	// Ordering: host → A → host → B → host(end).
	want := []string{hostID, mA, hostID, mB, hostID}
	if len(*order) != len(want) {
		t.Fatalf("order=%v, want %v", *order, want)
	}
	for i := range want {
		if (*order)[i] != want[i] {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], want[i], *order)
		}
	}
}

// TestGroupOrchestrator_HostOnlyGroupDoesNotLoop covers a group with no other
// members: routing must not spin (the host cannot name anyone, including
// itself), and the turn must terminate cleanly with exactly one terminal event.
func TestGroupOrchestrator_HostOnlyGroupDoesNotLoop(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-solo"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "Solo", "codebuddy", "agent-host", "Codebuddy")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	script := map[string][]string{
		// Host names itself — must be dropped, leaving no target → round-robin
		// finds no non-host member → loop ends.
		hostID: {`<clawbench-speaker>Codebuddy</clawbench-speaker> 我先说`, `结束。<clawbench-group-end/> 结论：无人可点。`},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	term := observeGroupTerminal(t)
	if err := o.RunGroupTurn(context.Background(), "开始"); err != nil {
		t.Fatalf("RunGroupTurn: %v", err)
	}
	if *term != 1 {
		t.Fatalf("terminal emitted %d times, want 1", *term)
	}
	// Every turn must be the host — the host can never become a "member" target.
	for _, id := range *order {
		if id != hostID {
			t.Fatalf("non-host turn %q in a host-only group; order=%v", id, *order)
		}
	}
}

// TestGroupOrchestrator_DefaultRunnerWiring pins the load-bearing fields the
// production runner sets: the CONNECTION session is the member row while the
// TIMELINE is the group, and SpeakerID attributes the output to the member.
// Without these the member's output would land on (or be attributed to) the
// wrong session. No backend runs here — only the spec is assembled.
func TestGroupOrchestrator_DefaultRunnerWiring(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-wire"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")

	o := NewGroupOrchestrator(groupID)
	spec := o.buildMemberTurnSpec(context.Background(), groupID, groupMemberTurn{
		MemberRowID: mA,
		Prompt:      "请发言",
	})

	if spec.SessionID != mA {
		t.Errorf("SessionID=%q, want member row %q (connection must be the member)", spec.SessionID, mA)
	}
	if spec.TimelineSessionID != groupID {
		t.Errorf("TimelineSessionID=%q, want group %q (output must land on the group timeline)", spec.TimelineSessionID, groupID)
	}
	if spec.SpeakerID != mA {
		t.Errorf("SpeakerID=%q, want member row %q (attribution)", spec.SpeakerID, mA)
	}
	if !spec.DrainOnFinalize {
		t.Error("DrainOnFinalize must be true for member turns")
	}
}
