package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// --- per-member turn registry ------------------------------------------------

func TestGroupMemberTurn_InterruptCancelsAndClears(t *testing.T) {
	cancelled := 0
	memberID := "member-stop-1"
	groupID := "group-stop-1"
	t.Cleanup(func() { UnregisterGroupMemberTurnCancel(memberID, 0) })

	id := RegisterGroupMemberTurnCancel(groupID, memberID, func() { cancelled++ })
	if id == 0 {
		t.Fatal("register must return a non-zero turn id")
	}
	if !InterruptGroupMemberTurn(groupID, memberID) {
		t.Fatal("interrupt must succeed while the member is speaking")
	}
	if cancelled != 1 {
		t.Fatalf("interrupt must cancel exactly once, got %d", cancelled)
	}
	if InterruptGroupMemberTurn(groupID, memberID) {
		t.Fatal("a second interrupt must report nothing stopped (no handle left)")
	}
	if reason := GetAndClearCancelReason(memberID); reason != cancelReasonMemberStop {
		t.Fatalf("cancel reason = %q, want %q", reason, cancelReasonMemberStop)
	}
}

// A member id from ANOTHER group must not be stoppable through this group.
func TestGroupMemberTurn_InterruptRejectsOtherGroup(t *testing.T) {
	memberID := "member-stop-2"
	t.Cleanup(func() { UnregisterGroupMemberTurnCancel(memberID, 0) })

	RegisterGroupMemberTurnCancel("group-a", memberID, func() {})
	if InterruptGroupMemberTurn("group-b", memberID) {
		t.Fatal("interrupt must not act on a member registered under a different group")
	}
	// The handle must survive the refused call: the real group can still stop it.
	if !InterruptGroupMemberTurn("group-a", memberID) {
		t.Fatal("the refused interrupt must leave the handle in place")
	}
}

// The teardown of an OLD turn must not clear the handle of a NEWER speech of the
// same member (the id check is what makes that safe).
func TestGroupMemberTurn_StaleUnregisterKeepsNewerTurn(t *testing.T) {
	memberID := "member-stop-3"
	t.Cleanup(func() { UnregisterGroupMemberTurnCancel(memberID, 0) })

	oldID := RegisterGroupMemberTurnCancel("g", memberID, func() {})
	newID := RegisterGroupMemberTurnCancel("g", memberID, func() {})
	if oldID == newID {
		t.Fatal("each registration must get its own id")
	}
	UnregisterGroupMemberTurnCancel(memberID, oldID)
	if !InterruptGroupMemberTurn("g", memberID) {
		t.Fatal("a stale unregister must not clear the newer turn's handle")
	}
	// The interrupt consumed the handle; a fresh one must be clearable by id.
	UnregisterGroupMemberTurnCancel(memberID, newID)
	if InterruptGroupMemberTurn("g", memberID) {
		t.Fatal("unregister with the current id must clear the handle")
	}
}

// --- orchestrator: stopping one member keeps the round alive -----------------

// stopMemberRunner returns a scripted runner that, for the FIRST member it is
// asked to run, simulates the user's "stop this member" click from inside the
// turn (exactly what the HTTP endpoint does while the speech is in flight) and
// reports the member-level cancel. Every other member answers normally.
func stopMemberRunner(t *testing.T, project, groupID, stoppedMemberID string) (groupTurnRunner, *[]string) {
	t.Helper()
	order := &[]string{}
	var mu sync.Mutex
	fired := false
	return func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		*order = append(*order, turn.MemberRowID)
		shouldStop := turn.MemberRowID == stoppedMemberID && !fired
		fired = fired || shouldStop
		mu.Unlock()

		if shouldStop {
			if !InterruptGroupMemberTurn(gid, turn.MemberRowID) {
				t.Errorf("interrupt must find the live handle for member %s", turn.MemberRowID)
			}
			// What runTurn reports after its context was cancelled with the
			// member-stop reason (see run_turn.go's cancel-reason switch).
			return groupMemberResult{CancelReason: cancelReasonMemberStop}
		}
		if _, err := AddChatMessageWithAgent(project, "claude", groupID, "assistant",
			assistantText("正常发言"), nil, false, "", turn.MemberRowID); err != nil {
			return groupMemberResult{Err: err.Error()}
		}
		return groupMemberResult{}
	}, order
}

func assertMemberSpoke(t *testing.T, groupID, memberID string) {
	t.Helper()
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		t.Fatalf("read group messages: %v", err)
	}
	for _, m := range msgs {
		if m.Role == "assistant" && m.AgentID == memberID {
			return
		}
	}
	t.Fatalf("member %s must have spoken", memberID)
}

// A sequential group: stopping A must not stop B, and the turn must still count
// as a clean discussion.
func TestGroupStop_SequentialMemberStopKeepsRound(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-stop-seq"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	runner, order := stopMemberRunner(t, project, groupID, byName["A"])
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `">逐个发言</clawbench-mention> 开始`
	res := runGroupTurnForTest(t, o, project, msg, nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("stopping one member must not end the turn: cancel=%q err=%q", res.CancelReason, res.Err)
	}
	assertMemberSpoke(t, groupID, byName["B"])
	if len(*order) < 2 {
		t.Fatalf("both members must have been asked to speak, got %v", *order)
	}
}

// A parallel group: stopping one member must neither abort its sibling nor stop
// the remaining batches.
func TestGroupStop_ParallelMemberStopKeepsSiblings(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-stop-par"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	runner, order := stopMemberRunner(t, project, groupID, byName["A"])
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `" mode="parallel">同时发言</clawbench-mention> 开始`
	res := runGroupTurnForTest(t, o, project, msg, nil)

	if res.CancelReason != "" || res.Err != "" {
		t.Fatalf("stopping one member must not end the turn: cancel=%q err=%q", res.CancelReason, res.Err)
	}
	assertMemberSpoke(t, groupID, byName["B"])
	if len(*order) != 2 {
		t.Fatalf("both parallel members must have run, got %v", *order)
	}
}

// The stopped member's cursor must NOT advance: it consumed nothing, so the
// context it missed is re-injected the next time it speaks (decision #69).
func TestGroupStop_StoppedMemberCursorNotAdvanced(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-stop-cursor"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	before := GetMemberCursor(byName["A"])
	runner, _ := stopMemberRunner(t, project, groupID, byName["A"])
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `">逐个</clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if after := GetMemberCursor(byName["A"]); after != before {
		t.Fatalf("a stopped member must not advance its cursor: before=%d after=%d", before, after)
	}
}

// Stopping the HOST is not "one member goes quiet": the host routes the round,
// so there is nothing left to continue. It must degrade to a whole-turn stop
// instead of handing the drain loop an unknown cancel reason.
func TestGroupStop_StoppingTheHostEndsTheTurn(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-stop-host"
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

	var memberSpoke bool
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			if !InterruptGroupMemberTurn(gid, turn.MemberRowID) {
				t.Errorf("the host's live handle must be found")
			}
			return groupMemberResult{CancelReason: cancelReasonMemberStop}
		}
		memberSpoke = true
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner

	res := runGroupTurnForTest(t, o, project, "开始讨论", nil)
	// Interrupt (not "user"): the host's speech was cut short, but the messages
	// the user already queued are still wanted — the drain loop keeps them.
	if res.CancelReason != cancelReasonInterrupt {
		t.Fatalf("stopping the host must end the turn as an interrupt, got %q", res.CancelReason)
	}
	if memberSpoke {
		t.Fatal("no member turn may run after the host was stopped")
	}
}

// --- persistence: `stopped`, never `cancelled` -------------------------------

func TestBuildContentJSON_MemberStopStampsStopped(t *testing.T) {
	e := &SessionExecutor{}
	content, _ := e.buildContentJSON(
		[]model.ContentBlock{{Type: "text", Text: "说了一半"}},
		RunResult{CancelReason: cancelReasonMemberStop},
		&ai.Metadata{},
	)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("content must be valid JSON: %v", err)
	}
	if parsed["stopped"] != true {
		t.Fatalf("a stopped member speech must be stamped stopped: %s", content)
	}
	if _, ok := parsed["cancelled"]; ok {
		t.Fatalf("a stopped member speech must NOT be stamped cancelled: %s", content)
	}
	if !strings.Contains(content, "说了一半") {
		t.Fatalf("the words already spoken must survive: %s", content)
	}
}

// The cancel-reason pass-through set: a deliberate stop must survive
// classification, while a bare context cancel (and a shutdown "restart") still
// degrades to the generic "cancel".
func TestIsExplicitCancelReason(t *testing.T) {
	for _, reason := range []string{cancelReasonUser, cancelReasonInterrupt, cancelReasonMemberStop} {
		if !isExplicitCancelReason(reason) {
			t.Fatalf("%q must pass through verbatim", reason)
		}
	}
	for _, reason := range []string{"", "cancel", cancelReasonRestart, "timeout"} {
		if isExplicitCancelReason(reason) {
			t.Fatalf("%q must NOT be treated as an explicit cancel reason", reason)
		}
	}
}

// The member's executor is what carries "stop this member" from the endpoint
// into the turn result: it reads the reason from the MEMBER row's slot (not the
// group's). Without this the interrupt would be indistinguishable from a bare
// context cancel and the orchestrator would stop the whole round.
func TestSessionExecutor_BuildResult_ReadsMemberStopReason(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	SetCancelReason(sid, cancelReasonMemberStop)

	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	result := NewSessionExecutor(context.Background(), cfg).RunWithChannel(ch)
	if result.CancelReason != cancelReasonMemberStop {
		t.Fatalf("executor must read the member-stop reason, got %q", result.CancelReason)
	}
	if result.Empty {
		t.Fatal("a stopped turn is not an empty turn")
	}
}
