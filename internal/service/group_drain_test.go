package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// RunGroupTurnDrain must not touch the session's running flag: the caller (the
// HTTP handler's claim, or the drain loop) owns it for the whole queued run.
// Clearing it here would end a run after the first of several queued messages.
// It must also NOT emit the terminal event (the drain loop does, exactly once).
//
// The flag is claimed up front, exactly as the handler does, and must still be
// set when the drain turn returns.
func TestRunGroupTurnDrain_DoesNotTouchRunningFlag(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-drain"
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
	// The drain loop already materialized the user message; simulate that.
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始讨论", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed user msg: %v", err)
	}

	// The caller claims the run (as the handler does) and owns the flag.
	runCtx, claimed := TryClaimSessionRun(groupID)
	if !claimed {
		t.Fatal("precondition: the group must start idle")
	}
	defer FinishSessionRun(groupID)
	if !IsSessionRunning(groupID) {
		t.Fatal("precondition: the claim must register the group as running")
	}

	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		return groupMemberResult{}
	}
	res := RunGroupTurnDrain(runCtx, groupID, msgID, QueuedRow{Content: "开始讨论"}, runner)
	if res.Err != "" {
		t.Fatalf("drain turn must not error: %q", res.Err)
	}
	if !IsSessionRunning(groupID) {
		t.Fatal("RunGroupTurnDrain must not clear the running flag (the caller owns it)")
	}
}

// The drain path must not persist or re-announce the user message — the drain
// loop already did both when it materialized the row.
func TestRunGroupTurnDrain_DoesNotDuplicateUserMessage(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-drain-nodup"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "只此一条", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		return groupMemberResult{}
	}
	_ = RunGroupTurnDrain(context.Background(), groupID, msgID, QueuedRow{Content: "只此一条"}, runner)

	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	userCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("the drain path must not add a second user message: got %d", userCount)
	}
	_ = model.FileEntry{}
}

// A panic anywhere in a group turn must not unwind the caller's goroutine: an
// uncaught panic in a goroutine terminates the whole process, dropping every
// session, task and WS connection. Single chat has this barrier (handler/chat.go
// and handleSessionPanic); the group drain path did not (review C2).
//
// This drives the barrier directly: a panic inside the deferred call must be
// recovered and leave a terminal state (running cleared, streaming row closed),
// instead of propagating.
func TestRecoverGroupDrainPanic_TerminalCleanup(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-panic"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	SetSessionRunning(groupID, true, true)

	// A streaming assistant row left open by a MEMBER turn. Its backend is the
	// MEMBER's (claude), NOT the group row's (codebuddy, the host's) — this is
	// the heterogeneous-group shape, and it is what a backend-scoped finalize
	// silently fails to close.
	orphanID, err := AddChatMessageWithAgent(project, "claude", groupID, "assistant", "half", nil, true, "", "row-x")
	if err != nil {
		t.Fatalf("seed streaming row: %v", err)
	}

	// The barrier must swallow the panic (the test itself does not crash).
	func() {
		defer recoverGroupDrainPanic(groupID)
		panic("boom in a member turn")
	}()

	if IsSessionRunning(groupID) {
		t.Fatal("a panic must clear the session's running state")
	}
	if isStreaming(t, orphanID) {
		t.Fatal("a panic must close the member's streaming row even when its backend differs from the group's (no backend scope)")
	}
}

// RunGroupDrainLoop must actually install the panic barrier (the helper being
// correct is not enough — a source guard catches the wiring being dropped).
func TestRunGroupDrainLoop_InstallsPanicBarrier(t *testing.T) {
	src, err := os.ReadFile("group_orchestrator.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if !strings.Contains(string(src), "defer recoverGroupDrainPanic(groupID)") {
		t.Fatal("RunGroupDrainLoop must defer recoverGroupDrainPanic; without it a member-turn panic kills the process")
	}
}

// Decision #72: a drained group run notifies once per answered turn — N
// messages produce exactly N completion pushes (N-1 intermediate via the
// unguarded helper, plus the terminal one via EmitSessionPushNotification's
// once-per-run guard). This had implementation but no verification (review W6):
// every existing group test replaced RunGroupDrainLoop wholesale, so its
// markDoneAndSendFinal never actually ran.
//
// pushSessionTerminal writes one pending_event per push (for offline replay), so
// counting pending_events counts the pushes.
func TestRunGroupDrainLoop_NotifiesOncePerTurn(t *testing.T) {
	cases := []struct {
		name   string
		queued []string
		wantN  int
	}{
		{"single message (first only) pushes once", nil, 1},
		{"first + 2 queued pushes 3 times", []string{"q1", "q2"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupGroupDB(t)
			project := "/tmp/gorch-push"
			if _, err := store.ProjectIDForPath(project); err != nil {
				t.Fatalf("ProjectIDForPath: %v", err)
			}
			groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
			if err != nil {
				t.Fatalf("CreateGroup: %v", err)
			}

			// A disconnected client + a started DingTalk manager so
			// pushSessionTerminal writes the pending event (offline-replay branch).
			mgr := ws.NewManagerForTest()
			ws.SetManagerForTest(mgr)
			t.Cleanup(func() { ws.SetManagerForTest(nil) })
			var writeMu sync.Mutex
			_ = mgr.Subscribe(nil, &writeMu, "test-client-group-push", "")
			mgr.DisconnectClient("test-client-group-push")

			origMgr := dingtalk.GetManager()
			dtMgr := dingtalk.NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1})
			dtMgr.SetStartedForTest(true)
			dingtalk.SetManager(dtMgr)
			t.Cleanup(func() {
				dtMgr.SetStartedForTest(false)
				dingtalk.SetManager(origMgr)
			})

			for _, q := range tc.queued {
				if _, err := AddQueuedMessage(project, "codebuddy", groupID, "msg "+q, nil, q, ""); err != nil {
					t.Fatalf("AddQueuedMessage: %v", err)
				}
			}

			// A scripted turn runner that never touches a backend.
			restore := SetRunGroupTurnDrainForTest(func(_ context.Context, _ string, _ int64, _ QueuedRow, _ groupTurnRunner) DrainResult {
				return DrainResult{}
			})
			defer SetRunGroupTurnDrainForTest(restore)

			terminalPushDone.Delete(groupID)
			t.Cleanup(func() { terminalPushDone.Delete(groupID) })

			RunGroupDrainLoop(context.Background(), groupID, project, 1, "first", nil)

			n := pendingEventCount(t, store.UnsafeDBForTest())
			if n != tc.wantN {
				t.Fatalf("want %d completion pushes, got %d", tc.wantN, n)
			}
		})
	}
}

// "Interrupt and send" in a group must stop the turn in flight and KEEP the
// queue, so the drain loop runs the next message. That only works if the group
// turn is registered under the GROUP id — the session the queue belongs to.
// Member rows have no runner, so runTurn's per-turn registration (keyed by the
// member row) is a silent no-op, and the interrupt endpoint's
// CurrentTurnID(groupID) always reported "idle".
//
// This drives the real RunGroupTurnDrain and simulates the interrupt endpoint
// acting while the host speaks, exactly as a click on the queued bubble does.
func TestRunGroupTurnDrain_InterruptKeepsQueue(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-interrupt"
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

	// The caller owns the running state (as the handler's claim does); the turn
	// registration requires a live runner.
	runCtx, claimed := TryClaimSessionRun(groupID)
	if !claimed {
		t.Fatal("precondition: the group must start idle")
	}
	defer FinishSessionRun(groupID)

	foundTurn := false
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID != hostID {
			return groupMemberResult{}
		}
		// The interrupt endpoint's lookup must find the turn under the GROUP id.
		turnID, ok := CurrentTurnID(gid)
		foundTurn = ok
		if ok {
			InterruptSessionTurnIfCurrent(gid, turnID)
		}
		// A cancelled member turn reports the generic "cancel": the member
		// executor reads the MEMBER row's reason, which is empty, and falls back
		// to ctx cancellation.
		return groupMemberResult{CancelReason: "cancel"}
	}

	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	res := RunGroupTurnDrain(runCtx, groupID, msgID, QueuedRow{Content: "开始"}, runner)

	if !foundTurn {
		t.Fatal("the group turn must be registered under the GROUP id so the interrupt can find it")
	}
	if res.CancelReason != cancelReasonInterrupt {
		t.Fatalf("an interrupt must surface as %q so the drain loop keeps the queue; got %q",
			cancelReasonInterrupt, res.CancelReason)
	}
}

// The drain loop must actually DELIVER the queued message after an interrupt:
// an interrupt keeps the queue (unlike a user cancel, which drops it), so the
// next message runs as its own turn. Without the group-level interrupt
// translation the loop reports the member's generic "cancel" and the terminal
// branch drops the queue — the exact opposite of "interrupt and send".
func TestRunGroupDrainLoop_InterruptRunsQueuedMessageNext(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-interrupt-drain"
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

	if _, err := AddQueuedMessage(project, "codebuddy", groupID, "queued next", nil, "q-int", ""); err != nil {
		t.Fatalf("AddQueuedMessage: %v", err)
	}

	runCtx, claimed := TryClaimSessionRun(groupID)
	if !claimed {
		t.Fatal("precondition: the group must start idle")
	}
	defer FinishSessionRun(groupID)

	turnNo := 0
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		turnNo++
		if turnNo == 1 {
			if turnID, ok := CurrentTurnID(gid); ok {
				InterruptSessionTurnIfCurrent(gid, turnID)
			}
			return groupMemberResult{CancelReason: "cancel"}
		}
		// The queued message's own turn: end cleanly.
		if turn.MemberRowID == hostID {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				assistantText("<clawbench-group-end/> 结束"), nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	restore := SetGroupDrainRunnerForTest(runner)
	defer SetGroupDrainRunnerForTest(restore)

	firstID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	RunGroupDrainLoop(runCtx, groupID, project, firstID, "开始", nil)

	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	delivered := false
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "queued next" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("an interrupt must keep the queue and run the next message, not drop it")
	}
}

// A turn that FAILED must not be relabelled as a clean interrupt. The group
// reason is written by the interrupt path, but drainHandleTerminal checks the
// interrupt reason BEFORE Err — so overwriting CancelReason on a failed turn
// silently swallows the terminal error event (the user gets no failure signal
// and the queue is kept as if nothing went wrong).
//
// This is a pure-function table test rather than an integration drive: reaching
// the {Err != "", groupReason == "interrupt"} state through RunGroupTurnDrain
// would require an interrupt that does NOT cancel the context (an interrupt
// always cancels it, so the kernel's ctx check fires first). The state is
// nevertheless reachable in production — an interrupt can land and then the
// member turn fail for an unrelated reason (backend crash, timeout) — so the
// translation must be guarded against it.
func TestTranslateGroupCancelReason(t *testing.T) {
	cases := []struct {
		name        string
		res         DrainResult
		groupReason string
		wantCancel  string
		wantErrKept bool
	}{
		{"clean turn + interrupt → interrupt (keeps the queue)", DrainResult{}, cancelReasonInterrupt, cancelReasonInterrupt, true},
		{"clean turn + user → untouched (drain loop drops the queue)", DrainResult{}, cancelReasonUser, "", true},
		{"clean turn + empty → untouched", DrainResult{}, "", "", true},
		{"failed turn + interrupt → Err survives, NOT relabelled", DrainResult{Err: "backend died"}, cancelReasonInterrupt, "", false},
		{"failed turn + user → untouched", DrainResult{Err: "backend died"}, cancelReasonUser, "", false},
		{"already-cancelled clean turn + interrupt → interrupt", DrainResult{CancelReason: "cancel"}, cancelReasonInterrupt, cancelReasonInterrupt, true},
		// A member executor reports the generic "cancel" (it reads the MEMBER
		// row's slot, which is empty). When the GROUP slot says "user", that is a
		// genuine user cancel and must be restored, so the drain loop drops the
		// queue — otherwise the queue survives a cancel and the reaper revives a
		// turn the user explicitly stopped.
		{"member's generic cancel + group user → restored to user", DrainResult{CancelReason: "cancel"}, cancelReasonUser, cancelReasonUser, true},
		// A clean turn must NOT be turned into a cancel just because the group
		// slot holds a stale "user": the turn finished normally.
		{"clean turn + group user → untouched (not a cancel)", DrainResult{}, cancelReasonUser, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := translateGroupCancelReason(tc.res, tc.groupReason)
			if got.CancelReason != tc.wantCancel {
				t.Fatalf("CancelReason = %q, want %q", got.CancelReason, tc.wantCancel)
			}
			if tc.wantErrKept && got.Err != tc.res.Err {
				t.Fatalf("Err = %q, want %q", got.Err, tc.res.Err)
			}
			if !tc.wantErrKept && got.Err == "" {
				t.Fatal("a failed turn must keep its error — the interrupt must not swallow it")
			}
		})
	}
}

// The REAL production chain (RunGroupDrainLoop → RunGroupTurnDrain → runLoop →
// member turns) must actually run. Every other group test either replaces the
// whole drain loop (SetRunGroupDrainLoopForTest) or the whole turn runner
// (SetRunGroupTurnDrainForTest) — so replacing runGroupTurnDrainFn's production
// default with an empty function left the entire suite green (review B-3), the
// same "tests pass while the feature is gone" failure mode as the original C1.
//
// This test overrides ONLY the per-member runner (the backend boundary) and
// asserts that real orchestration happened: the scripted members' speech lands
// on the group timeline and a summary exists.
func TestRunGroupDrainLoop_RunsRealOrchestration(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-real-orch"
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

	// Stub ONLY the backend: a scripted member runner writes the speech the
	// orchestrator's real routing loop decided on.
	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A"> 请 A 表态</clawbench-mention>`,
			`充分了。<clawbench-group-end/> 结论：同意。`,
		},
		mA: {"A 的观点"},
	}
	runner, _ := newScriptedRunner(t, groupID, project, script)
	restore := SetGroupDrainRunnerForTest(runner)
	defer SetGroupDrainRunnerForTest(restore)

	// First message already materialized (as the drain loop does).
	firstID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	RunGroupDrainLoop(context.Background(), groupID, project, firstID, "开始", nil)

	// The REAL orchestrator must have routed to A and persisted A's speech.
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	aSpoke := false
	for _, m := range msgs {
		if m.AgentID == mA && strings.Contains(m.Content, "A 的观点") {
			aSpoke = true
		}
	}
	if !aSpoke {
		t.Fatal("the real orchestrator must run through RunGroupDrainLoop (A's routed speech is missing)")
	}
}

// A /cb-* built-in command in a group message must reach the HOST's prompt (so
// the host runs it and reports the result as its speech), and ONLY the first
// host turn — injecting every round would re-run the command each round. It
// must never reach a member's prompt (the API contract is noise to them) and
// never enter the timeline content (decision #79 revision / review B6).
func TestGroupTurn_ClawbenchCommandInjectsFirstHostPromptOnly(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cbcmd"
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

	// Stub the renderer: any message becomes a recognizable template.
	const tmpl = "INJECTED_CB_TEMPLATE"
	restore := SetRenderGroupCommandFn(func(rawMsg, _, _ string) (string, error) {
		if strings.HasPrefix(rawMsg, "/cb-") {
			return tmpl + "\n\n" + rawMsg, nil
		}
		return rawMsg, nil
	})
	defer SetRenderGroupCommandFn(restore)

	// Capture every turn's prompt.
	type seen struct {
		id     string
		prompt string
		host   bool
	}
	var prompts []seen
	script := map[string][]string{
		hostID: {
			`<clawbench-mention targets="A"> 请表态</clawbench-mention>`,
			`<clawbench-group-end/> 结束`,
		},
		mA: {"A 的观点"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		prompts = append(prompts, seen{turn.MemberRowID, turn.Prompt, turn.MemberRowID == hostID})
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// The drained message IS the command.
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "/cb-task 每天下午6点", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	RunGroupTurnDrain(context.Background(), groupID, msgID, QueuedRow{Content: "/cb-task 每天下午6点"}, o.runTurn)

	hostSeen := 0
	for _, p := range prompts {
		if p.host {
			hostSeen++
			if hostSeen == 1 {
				if !strings.Contains(p.prompt, tmpl) {
					t.Fatalf("the FIRST host prompt must carry the rendered command; got %q", p.prompt)
				}
			} else if strings.Contains(p.prompt, tmpl) {
				t.Fatalf("only the first host prompt may carry the command; host turn #%d has it", hostSeen)
			}
			continue
		}
		if strings.Contains(p.prompt, tmpl) {
			t.Fatalf("a member prompt must never carry the command template: %q", p.prompt)
		}
	}
	if hostSeen == 0 {
		t.Fatal("precondition: the host must have taken at least one turn")
	}

	// The timeline content stays clean (the user's literal text, no template).
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	for _, m := range msgs {
		if strings.Contains(m.Content, tmpl) {
			t.Fatalf("the command template must not enter the timeline: %q", m.Content)
		}
	}
}

// A non-command message must not inject anything (the renderer returns it
// unchanged).
func TestGroupTurn_NonCommandDoesNotInject(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cbnoop"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	const tmpl = "INJECTED_CB_TEMPLATE"
	restore := SetRenderGroupCommandFn(func(rawMsg, _, _ string) (string, error) {
		if strings.HasPrefix(rawMsg, "/cb-") {
			return tmpl + "\n\n" + rawMsg, nil
		}
		return rawMsg, nil
	})
	defer SetRenderGroupCommandFn(restore)

	var hostPrompts []string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			hostPrompts = append(hostPrompts, turn.Prompt)
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "普通消息", nil)

	for _, p := range hostPrompts {
		if strings.Contains(p, tmpl) {
			t.Fatalf("a non-command message must not inject anything: %q", p)
		}
	}
}

// A nil renderer (not wired, e.g. in a test binary) must not panic and must not
// inject.
func TestGroupTurn_NilRendererDoesNotPanic(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cbnil"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	restore := SetRenderGroupCommandFn(nil)
	defer SetRenderGroupCommandFn(restore)

	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// Must not panic.
	_ = runGroupTurnForTest(t, o, project, "/cb-task x", nil)
}

// A renderer error must not abort the turn: the command is skipped and the turn
// runs normally (the user's literal text is still in the injected context).
func TestGroupTurn_CommandRenderErrorSkipsInjection(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-cberr"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	restore := SetRenderGroupCommandFn(func(rawMsg, _, _ string) (string, error) {
		return "", errors.New("render exploded")
	})
	defer SetRenderGroupCommandFn(restore)

	hostRan := false
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == hostID {
			hostRan = true
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
		}
		return groupMemberResult{}
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "/cb-task x", nil)

	if !hostRan {
		t.Fatal("a render error must not abort the turn")
	}
}
