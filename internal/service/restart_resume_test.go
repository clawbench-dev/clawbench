package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// restart_resume_test.go covers the "a restart interrupted this turn, resume it"
// path end to end at the service layer.
//
// The seams are the same ones the queue reaper's tests use (launchConsumerExecution
// for single chat, runGroupDrainLoopFn for groups), so no AI backend runs.

// setupRestartResumeDB builds the real schema in a temp data dir. The resume path
// touches chat_sessions, chat_history, queued_messages and projects, so a
// hand-rolled fixture would drift; InitDB is the production DDL.
func setupRestartResumeDB(t *testing.T) {
	t.Helper()
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)
	// The resume window is a config-driven global that main.go assigns at
	// startup. Tests that exercise the age bound must set it explicitly, or the
	// zero value (0 = no age bound) would silently disable the check — which is
	// exactly how the first version of the "stale warning" assertion went green
	// with the bound missing.
	prevWindow := model.ChatRestartResumeWindowMinutes
	model.ChatRestartResumeWindowMinutes = model.DefaultRestartResumeWindowMinutes
	t.Cleanup(func() { model.ChatRestartResumeWindowMinutes = prevWindow })
}

// launchRecorder records the configs handed to the single-chat launch seam.
//
// Mutex-guarded because the resume worker calls the seam from its OWN goroutine
// while the test reads it: an unguarded slice here is a real data race (caught
// by -race), not a theoretical one.
type launchRecorder struct {
	mu    sync.Mutex
	calls []LaunchConfig
}

func (r *launchRecorder) record(cfg LaunchConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, cfg)
}

// count returns how many launches were recorded so far.
func (r *launchRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// first returns the earliest recorded launch. Every current test resumes exactly
// one session, so a general index would be an unused knob.
func (r *launchRecorder) first() LaunchConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[0]
}

// stubRestartLaunch replaces the single-chat launch seam and records the configs.
func stubRestartLaunch(t *testing.T) *launchRecorder {
	t.Helper()
	rec := &launchRecorder{}
	orig := launchConsumerExecution
	launchConsumerExecution = rec.record
	t.Cleanup(func() { launchConsumerExecution = orig })
	return rec
}

// stubRestartGroupDrainHeld replaces the group drain seam and keeps the drain
// goroutine ALIVE until the test releases it.
//
// A stub that returns immediately lets the goroutine continue into
// RunGroupDrainLoop's terminal path (push notification → store read) after the
// test has already torn the DB down — a real data race that -race reports
// intermittently, depending on whether the teardown wins. Holding the seam open
// makes the goroutine's lifetime explicit: the test inspects state while it is
// blocked, then releases it and waits for the claim to be gone.
func stubRestartGroupDrainHeld(t *testing.T) (prompts *[]string, reached <-chan struct{}) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	hit := make(chan struct{}, 1)

	orig := SetRunGroupDrainLoopForTest(func(_ context.Context, _ string, _ string, _ int64, text string, _ []model.FileEntry) {
		mu.Lock()
		got = append(got, text)
		mu.Unlock()
		select {
		case hit <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() { SetRunGroupDrainLoopForTest(orig) })

	return &got, hit
}

// systemRowTexts returns the role='system' rows of a session, oldest first.
func systemRowTexts(t *testing.T, sessionID string) []string {
	t.Helper()
	rows, err := store.ReadDB().Query(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'system' ORDER BY id ASC", sessionID,
	)
	if err != nil {
		t.Fatalf("query system rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan system row: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// countUserRows reports how many role='user' rows a session has. The resume turn
// must add ZERO of them: the instruction reaches the AI as a prompt, never as a
// persisted user message.
func countUserRows(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'user'", sessionID,
	).Scan(&n); err != nil {
		t.Fatalf("count user rows: %v", err)
	}
	return n
}

// The single-chat path: one system row, one launched turn, no user row.
func TestResumeInterruptedSession_SingleChat(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "long task", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}

	// The turn was launched with the resume prompt, on the session's own agent.
	if launched.count() != 1 {
		t.Fatalf("expected exactly one launched turn, got %d", launched.count())
	}
	if launched.first().SessionID != sid {
		t.Fatalf("launched for the wrong session: got %q want %q", launched.first().SessionID, sid)
	}
	if launched.first().Message != RestartResumePrompt() {
		t.Fatalf("launched with the wrong prompt: got %q want %q", launched.first().Message, RestartResumePrompt())
	}
	if launched.first().RunCtx == nil {
		t.Fatal("the launched turn must carry the claim's context, or a cancel cannot reach it")
	}

	// Exactly one persisted row, and it is the system notice — not a user bubble.
	if n := countUserRows(t, sid); n != 0 {
		t.Fatalf("the resume instruction must NOT be persisted as a user message, found %d user rows", n)
	}
	rows := systemRowTexts(t, sid)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one system row, got %d: %+v", len(rows), rows)
	}
	if rows[0] != RestartResumeNotice() {
		t.Fatalf("system row must carry the USER-facing recovery notice: got %q", rows[0])
	}
	if rows[0] == RestartResumePrompt() {
		t.Fatal("the system row must NOT carry the agent instruction — that wording is for the model only")
	}

	// The claim must be held for the running turn (the launcher is stubbed, so
	// nothing releases it here).
	if !IsSessionRunning(sid) {
		t.Fatal("the session must be claimed as running for the resumed turn")
	}
}

// A session that is already running must not be resumed twice.
func TestResumeInterruptedSession_SkipsRunningSession(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "busy", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Simulate the user having pressed "continue" first.
	if _, created := TryClaimSessionRun(sid); !created {
		t.Fatal("setup: expected to win the claim")
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}
	if launched.count() != 0 {
		t.Fatalf("a running session must not be resumed again, got %d launches", launched.count())
	}
	// The claim is taken before the notice is written, so a skipped resume leaves
	// no stray system row behind.
	if rows := systemRowTexts(t, sid); len(rows) != 0 {
		t.Fatalf("a skipped resume must not write a system row, got %+v", rows)
	}
}

// A session with queued work belongs to the queue reaper; resuming here would
// start a second consumer.
func TestResumeInterruptedSession_DefersToQueueReaper(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "queued", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	projectID, err := store.ProjectIDForPath("/proj/resume")
	if err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	if _, err := store.WriteExec(
		`INSERT INTO queued_messages (project_id, session_id, backend, queue_id, content) VALUES (?, ?, 'claude', 'q-1', 'hi')`,
		projectID, sid,
	); err != nil {
		t.Fatalf("insert queued row: %v", err)
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}
	if launched.count() != 0 {
		t.Fatalf("a session with queued work must be left to the reaper, got %d launches", launched.count())
	}
	if rows := systemRowTexts(t, sid); len(rows) != 0 {
		t.Fatalf("a deferred resume must not write a system row, got %+v", rows)
	}
}

// A deleted/archived session is not an error — there is simply nothing to resume.
func TestResumeInterruptedSession_MissingSessionIsNotAnError(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	if err := ResumeInterruptedSession("no-such-session"); err != nil {
		t.Fatalf("a missing session must not error: %v", err)
	}
	if launched.count() != 0 {
		t.Fatalf("nothing may be launched for a missing session, got %d", launched.count())
	}
}

// The group path must go through the orchestrator (host routes, members speak),
// never a host-only single-agent turn.
func TestResumeInterruptedSession_GroupUsesOrchestrator(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)
	prompts, groupDone := stubRestartGroupDrainHeld(t)

	drainDone := make(chan struct{})
	prevDone := SetGroupResumeDoneForTest(func() { close(drainDone) })
	t.Cleanup(func() { SetGroupResumeDoneForTest(prevDone) })

	project := "/proj/resume-group"
	groupID, _, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	if err := ResumeInterruptedSession(groupID); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}

	if launched.count() != 0 {
		t.Fatalf("a group must not run as a single-agent turn, got %d launches", launched.count())
	}
	// Wait for the drain goroutine to actually invoke the seam before asserting.
	select {
	case <-groupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the group drain was never driven (goroutine did not reach the seam)")
	}
	if len(*prompts) != 1 {
		t.Fatalf("expected the group drain to be driven once, got %d", len(*prompts))
	}
	if (*prompts)[0] != RestartResumePrompt() {
		t.Fatalf("group drain must receive the AGENT instruction: got %q", (*prompts)[0])
	}
	if (*prompts)[0] == RestartResumeNotice() {
		t.Fatal("the group drain must not receive the user-facing notice as its prompt")
	}

	// AddGroupMember already wrote a "joined the discussion" row, so the resume
	// notice is the LAST system row rather than the only one.
	rows := systemRowTexts(t, groupID)
	if len(rows) == 0 {
		t.Fatal("the recovery notice was not persisted to the group timeline")
	}
	if last := rows[len(rows)-1]; last != RestartResumeNotice() {
		t.Fatalf("the newest system row must be the recovery notice: got %q (all: %+v)", last, rows)
	}

	// Wait for the goroutine's whole lifetime, not just the claim: the terminal
	// push runs after FinishSessionRun and would otherwise race the DB teardown.
	select {
	case <-drainDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the group drain goroutine did not finish within the timeout")
	}
}

// setAutoContinue flips the global switch for one test.
func setAutoContinue(t *testing.T, enabled bool) {
	t.Helper()
	prev := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = enabled
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prev })
}

// waitForCount blocks until cond is true, failing the test after a generous
// timeout. Used instead of a fixed sleep: the worker runs in a goroutine, so
// "it has not been called yet" and "it will never be called" are only
// distinguishable by waiting for a POSITIVE signal.
func waitForCount(t *testing.T, want int, get func() int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if get() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d calls, got %d", want, get())
}

// The worker's POSITIVE path: switch on, delay elapsed, the session is actually
// resumed. Without this the whole goroutine body could be deleted and the gate
// test below would still pass.
func TestStartRestartResume_ResumesWhenEnabled(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)
	setAutoContinue(t, true)

	sid, err := CreateSession("/proj/resume", "claude", "resumed", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	recordInterruptedSessions([]string{sid})
	scheduled := startRestartResume(0, ResumeInterruptedSession)
	if len(scheduled) != 1 || scheduled[0] != sid {
		t.Fatalf("expected the session to be scheduled, got %+v", scheduled)
	}

	waitForCount(t, 1, func() int { return launched.count() })
	if launched.first().SessionID != sid {
		t.Fatalf("resumed the wrong session: got %q want %q", launched.first().SessionID, sid)
	}
}

// The delay is the only thing that keeps a resumed turn from streaming before
// clients have reconnected and re-subscribed — its whole reason for existing.
// Pinned here because every other test passes delay=0, so deleting the sleep (or
// zeroing the default) would otherwise go unnoticed.
func TestStartRestartResume_HonoursTheDelay(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)
	setAutoContinue(t, true)

	sid, err := CreateSession("/proj/resume", "claude", "delayed", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	recordInterruptedSessions([]string{sid})
	// The delay must be several times the observation window: the worker is a
	// goroutine, so "assert nothing happened" needs an actual window in which it
	// COULD have happened. Checking immediately after the call would pass even
	// with the sleep deleted (the goroutine may not be scheduled yet), which is
	// exactly how this test failed its first mutation.
	const (
		delay      = 400 * time.Millisecond
		observeFor = 150 * time.Millisecond
	)
	startRestartResume(delay, ResumeInterruptedSession)

	time.Sleep(observeFor)
	if n := launched.count(); n != 0 {
		t.Fatalf("nothing may be resumed during the delay, got %d launches after %v (delay %v)",
			n, observeFor, delay)
	}
	waitForCount(t, 1, launched.count)
}

// A shutdown during the delay must abort the resume: CancelAllSessions only
// sweeps the runners that exist at that instant, so a turn claimed afterwards
// would run while the DB is closing — leaving a fresh streaming=1 row that the
// NEXT startup resumes again (the user sees the notice twice).
func TestStartRestartResume_AbortsOnShutdown(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)
	setAutoContinue(t, true)

	prevShutdown := ResetShuttingDownForTest()
	if prevShutdown {
		t.Fatal("setup: the package must not already be marked as shutting down")
	}
	t.Cleanup(func() { ResetShuttingDownForTest() })

	sid, err := CreateSession("/proj/resume", "claude", "shutdown", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	recordInterruptedSessions([]string{sid})
	// Delay long enough to mark the shutdown before the worker wakes.
	startRestartResume(120*time.Millisecond, ResumeInterruptedSession)
	MarkShuttingDown()

	// Wait past the delay, then confirm nothing was started.
	time.Sleep(300 * time.Millisecond)
	if n := launched.count(); n != 0 {
		t.Fatalf("a shutdown must abort the resume, got %d launches", n)
	}
}

// The gate: with the switch off nothing is resumed AND the recorded set is still
// drained (so turning the switch on later cannot resurrect an old restart).
//
// The assertion is on the return value plus a POSITIVE-absence check via a
// recorder seam — no fixed sleep, so it cannot go green on a slow machine by
// finishing before a wrongly-started worker runs.
func TestStartRestartResume_GatedOnAutoContinue(t *testing.T) {
	setupRestartResumeDB(t)
	setAutoContinue(t, false)

	sid, err := CreateSession("/proj/resume", "claude", "gated", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var mu sync.Mutex
	var resumed []string
	record := func(s string) error {
		mu.Lock()
		resumed = append(resumed, s)
		mu.Unlock()
		return nil
	}

	recordInterruptedSessions([]string{sid})
	scheduled := startRestartResume(0, record)

	// Gated: nothing scheduled, and the set is drained regardless.
	if len(scheduled) != 0 {
		t.Fatalf("nothing may be scheduled while auto-continue is off, got %+v", scheduled)
	}
	if got := takeInterruptedSessions(); len(got) != 0 {
		t.Fatalf("the recorded set must be drained on every start, still holds %+v", got)
	}
	// No goroutine was started at all, so the recorder can never fire. Reading it
	// under the mutex is enough — there is nothing to wait for.
	mu.Lock()
	defer mu.Unlock()
	if len(resumed) != 0 {
		t.Fatalf("nothing may be resumed while auto-continue is off, got %+v", resumed)
	}
}

// A panic while resuming ONE session must not kill the process (an uncaught
// panic in a goroutine terminates everything) and must not stop the remaining
// sessions from being resumed.
//
// The injected function CLAIMS first and then panics. That is what makes the
// "claim was released" assertion real: without the claim, IsSessionRunning is
// false no matter what the recover block does, so deleting the FinishSessionRun
// call would go unnoticed.
func TestResumeOneSafely_PanicDoesNotStopTheRest(t *testing.T) {
	setupRestartResumeDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	// Capture the terminal broadcast: a panic after the claim must clear the
	// client's spinner, exactly like the notice-write failure branch does.
	var mu sync.Mutex
	var statuses []string
	origEmitWS := emitTerminalWSOnly
	emitTerminalWSOnly = func(_ string, status string, _ bool) {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
	}
	t.Cleanup(func() { emitTerminalWSOnly = origEmitWS })

	var seen []string
	panicking := func(s string) error {
		seen = append(seen, s)
		if _, created := TryClaimSessionRun(s); !created {
			t.Fatalf("setup: expected to win the claim for %q", s)
		}
		if s == "boom" {
			panic("simulated failure")
		}
		// The healthy one owns its claim the way a real resume would; release it
		// so the test leaves no runner behind.
		FinishSessionRun(s)
		return nil
	}

	resumeOneSafely("boom", panicking)
	resumeOneSafely("ok", panicking)

	if len(seen) != 2 {
		t.Fatalf("the second session must still be resumed after the panic, saw %+v", seen)
	}
	// The panic path must not leave a claim behind (this is what the
	// FinishSessionRun inside the recover block is for).
	if IsSessionRunning("boom") {
		t.Fatal("a panicking resume must release the session's claim")
	}
	if IsSessionRunning("ok") {
		t.Fatal("the healthy session must have released its claim too")
	}
	// And the panicking session must have been announced terminal, otherwise the
	// client keeps its spinner up forever.
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 1 || statuses[0] != statusCancelled {
		t.Fatalf("a panicking resume must announce exactly one cancelled event, got %+v", statuses)
	}
}

// The whole point of a group resume is that the host AND the members actually
// receive the recovery notice: it rides in as a role='system' row, which the
// injection renders as "[系统] <text>". This drives the REAL orchestration (only
// the per-member backend is stubbed) and asserts the prompt handed to the first
// speaker carries it — otherwise the "resume" would start a group turn that has
// no idea what to continue.
func TestResumeInterruptedSession_GroupPromptReachesSpeaker(t *testing.T) {
	setupRestartResumeDB(t)

	project := "/proj/resume-group-inject"
	groupID, hostID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Capture every prompt the orchestrator builds. The host speaks twice — first
	// to route to A, then to close the discussion — because a single reply
	// carrying both a mention and the end tag ends the turn without A running.
	var mu sync.Mutex
	prompts := map[string][]string{}
	hostTurns := 0
	runner := func(_ context.Context, _ string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		prompts[turn.MemberRowID] = append(prompts[turn.MemberRowID], turn.Prompt)
		mu.Unlock()
		text := "继续完成"
		if turn.MemberRowID == hostID {
			mu.Lock()
			hostTurns++
			first := hostTurns == 1
			mu.Unlock()
			if first {
				text = `<clawbench-mention targets="A">请 A 继续</clawbench-mention>`
			} else {
				text = `完成。<clawbench-group-end/>`
			}
		}
		if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
			`{"blocks":[{"type":"text","text":`+jsonQuote(text)+`}]}`, nil, false, "", turn.MemberRowID); err != nil {
			return groupMemberResult{Err: err.Error()}
		}
		return groupMemberResult{}
	}
	restore := SetGroupDrainRunnerForTest(runner)
	t.Cleanup(func() { SetGroupDrainRunnerForTest(restore) })

	drainDone := make(chan struct{})
	prevDone := SetGroupResumeDoneForTest(func() { close(drainDone) })
	t.Cleanup(func() { SetGroupResumeDoneForTest(prevDone) })

	if err := ResumeInterruptedSession(groupID); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}

	// The group drain runs in a goroutine; wait for BOTH the host and the routed
	// member to have spoken before asserting.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		hostDone, memberDone := len(prompts[hostID]) > 0, len(prompts[memberID]) > 0
		mu.Unlock()
		if hostDone && memberDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the drain goroutine to FINISH — its whole lifetime, not just the
	// claim. Releasing the claim happens in a defer that runs BEFORE the terminal
	// push, so polling IsSessionRunning left a window where the test returned and
	// the goroutine still read the store.
	select {
	case <-drainDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the group drain goroutine did not finish within the timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(prompts[hostID]) == 0 {
		t.Fatal("the host never ran — the group resume did not start a turn")
	}
	// The recovery notice must arrive as an INJECTED SYSTEM LINE, not merely as
	// some text in the prompt: the group already has a "[系统] A加入了讨论" row from
	// AddGroupMember, so asserting on "[系统]" alone (or on the notice text alone)
	// passes even when the notice is written as a fake user/assistant bubble. The
	// two must appear TOGETHER.
	want := "[系统] " + RestartResumeNotice()
	for _, tc := range []struct {
		who    string
		prompt string
	}{{"host", prompts[hostID][0]}, {"member", prompts[memberID][0]}} {
		if !strings.Contains(tc.prompt, want) {
			t.Fatalf("the %s prompt must carry the recovery notice as an injected system line %q, got:\n%s",
				tc.who, want, tc.prompt)
		}
	}
}

// A task execution (session_type='scheduled') must NOT be resumed: the scheduler
// already marked its execution 'failed', the session is not user-visible, and
// cron will run the task again — resuming it here would duplicate the work under
// the interactive mode.
func TestResumeInterruptedSession_SkipsScheduledSession(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "task run", "", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("a scheduled session must be skipped without error: %v", err)
	}
	if launched.count() != 0 {
		t.Fatalf("a scheduled session must not be resumed, got %d launches", launched.count())
	}
	if rows := systemRowTexts(t, sid); len(rows) != 0 {
		t.Fatalf("a skipped session must not get a system row, got %+v", rows)
	}
	if IsSessionRunning(sid) {
		t.Fatal("a skipped session must not be claimed")
	}
}

// InitDB must record exactly the sessions that had a streaming=1 row, deduped,
// and nothing else. This is the "which sessions did the restart interrupt"
// signal the whole feature hangs on.
func TestInitDB_RecordsInterruptedSessions(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	// Plant rows the way a hard kill leaves them: two streaming rows on one
	// session (dedup), one on another, plus a finalized row that must be ignored.
	insert := func(sessionID string, streaming int) {
		if _, err := store.WriteExec(
			`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
			 VALUES (1, 'assistant', '{"blocks":[]}', ?, 'claude', ?)`,
			sessionID, streaming,
		); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	insert("sess-a", 1)
	insert("sess-a", 1)
	insert("sess-b", 1)
	insert("sess-done", 0)

	// A second startup runs the cleanup + recording again.
	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB (second): %v", err)
	}

	got := takeInterruptedSessions()
	if len(got) != 2 {
		t.Fatalf("expected 2 interrupted sessions, got %d: %+v", len(got), got)
	}
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	if seen["sess-a"] != 1 {
		t.Fatalf("sess-a must appear exactly once (deduped), got %d", seen["sess-a"])
	}
	if seen["sess-b"] != 1 {
		t.Fatalf("sess-b must be recorded, got %d", seen["sess-b"])
	}
	if _, ok := seen["sess-done"]; ok {
		t.Fatal("a finalized row must not be treated as interrupted")
	}

	// Drained: a second call must find nothing (at most once per start).
	if again := takeInterruptedSessions(); len(again) != 0 {
		t.Fatalf("take must drain, still holds %+v", again)
	}
}

// A GRACEFUL restart must also be detected.
//
// A clean shutdown finalizes every active row before exiting, so streaming is
// already 0 when the next process starts and the orphan scan alone sees nothing
// — that was the bug that made "restart the service" never resume anything. The
// executor still appends a `reason=restart` warning block, and that is the
// second signal.
func TestInitDB_RecordsSessionsEndingOnRestartWarning(t *testing.T) {
	prevWindow := model.ChatRestartResumeWindowMinutes
	model.ChatRestartResumeWindowMinutes = model.DefaultRestartResumeWindowMinutes
	t.Cleanup(func() { model.ChatRestartResumeWindowMinutes = prevWindow })

	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	// Exactly what a graceful restart leaves: streaming=0, warning block last.
	warned := `{"blocks":[{"type":"text","text":"partial"},{"type":"warning","reason":"restart","text":"Server restarted, AI response interrupted"}],"cancelled":true}`
	// A real chat_sessions row is required: the scan JOINs it to exclude archived
	// sessions, so a bare session_id would never match.
	mkSession := func(sessionID string, archived int) {
		if _, err := store.WriteExec(
			`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived)
			 VALUES (?, 1, 'claude', 't', 'chat', ?)`,
			sessionID, archived,
		); err != nil {
			t.Fatalf("insert session %s: %v", sessionID, err)
		}
	}
	insertRow := func(sessionID, role, content, ageModifier string) {
		if _, err := store.WriteExec(
			`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming, created_at)
			 VALUES (1, ?, ?, ?, 'claude', 0, datetime('now', ?))`,
			role, content, sessionID, ageModifier,
		); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	mkSession("sess-graceful", 0)
	mkSession("sess-continued", 0)
	mkSession("sess-stale", 0)

	// Detected: the last assistant row is a restart warning.
	insertRow("sess-graceful", "user", `{"blocks":[]}`, "-5 minutes")
	insertRow("sess-graceful", "assistant", warned, "-5 minutes")

	// NOT detected: the user already continued after the warning — continuing IS
	// resuming, and another turn on top would redo the work.
	insertRow("sess-continued", "assistant", warned, "-10 minutes")
	insertRow("sess-continued", "user", `{"blocks":[]}`, "-9 minutes")
	insertRow("sess-continued", "assistant", `{"blocks":[]}`, "-9 minutes")

	// NOT detected: the warning is stale (older than the window). Without this
	// bound the first boot after the feature ships would resume every abandoned
	// session in the whole history.
	insertRow("sess-stale", "assistant", warned, "-2 days")

	// NOT detected: the session is archived. It is inside the window and its last
	// row IS a restart warning, so only the archived filter can exclude it — the
	// user put the session away on purpose, so it must not spend tokens.
	mkSession("sess-archived", 1)
	insertRow("sess-archived", "assistant", warned, "-3 minutes")

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB (second): %v", err)
	}

	got := takeInterruptedSessions()
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if !seen["sess-graceful"] {
		t.Fatalf("a session whose last turn ended on a restart warning must be recorded, got %+v", got)
	}
	if seen["sess-continued"] {
		t.Fatalf("a session the user already continued must NOT be resumed, got %+v", got)
	}
	if seen["sess-stale"] {
		t.Fatalf("a restart warning outside the window must NOT be resumed, got %+v", got)
	}
	if seen["sess-archived"] {
		t.Fatalf("an archived session must NOT be resumed, got %+v", got)
	}
}

// The window is a user setting, not a constant: widening it must actually widen
// what gets resumed, and 0 must mean "no age bound". Pinned because the value is
// read from a package global that only main.go assigns — a missing wiring shows
// up as a silently disabled bound (0), not as a compile error.
func TestSessionsEndingOnRestartWarning_HonoursConfiguredWindow(t *testing.T) {
	prevWindow := model.ChatRestartResumeWindowMinutes
	t.Cleanup(func() { model.ChatRestartResumeWindowMinutes = prevWindow })

	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	model.ChatRestartResumeWindowMinutes = model.DefaultRestartResumeWindowMinutes
	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	warned := `{"blocks":[{"type":"warning","reason":"restart","text":"x"}]}`
	if _, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived)
		 VALUES ('sess-3h', 1, 'claude', 't', 'chat', 0)`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming, created_at)
		 VALUES (1, 'assistant', ?, 'sess-3h', 'claude', 0, datetime('now','-3 hours'))`, warned,
	); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	// Default 60-minute window: a 3-hour-old interruption is out of range.
	if got := sessionsEndingOnRestartWarning(); len(got) != 0 {
		t.Fatalf("a 3h-old warning must be outside the default 60m window, got %+v", got)
	}

	// Widen to 4 hours: the same row is now in range.
	model.ChatRestartResumeWindowMinutes = 240
	if got := sessionsEndingOnRestartWarning(); len(got) != 1 || got[0] != "sess-3h" {
		t.Fatalf("widening the window must include the row, got %+v", got)
	}

	// 0 means "no age bound": even a very old row qualifies.
	model.ChatRestartResumeWindowMinutes = 0
	if _, err := store.WriteExec(
		`UPDATE chat_history SET created_at = datetime('now','-30 days') WHERE session_id = 'sess-3h'`,
	); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	if got := sessionsEndingOnRestartWarning(); len(got) != 1 {
		t.Fatalf("0 must disable the age bound, got %+v", got)
	}
}

// An archived session's orphan row must still be FINALIZED (streaming → 0) even
// though the session itself is not resumed.
//
// The two decisions are separate: leaving the row at streaming=1 would render a
// permanent "working" bubble for a session the user put away, while resuming it
// would spend tokens on it.
func TestInitDB_FinalizesArchivedOrphanWithoutResumingIt(t *testing.T) {
	prevWindow := model.ChatRestartResumeWindowMinutes
	model.ChatRestartResumeWindowMinutes = model.DefaultRestartResumeWindowMinutes
	t.Cleanup(func() { model.ChatRestartResumeWindowMinutes = prevWindow })

	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	if _, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived)
		 VALUES ('sess-arch-orphan', 1, 'claude', 't', 'chat', 1)`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (1, 'assistant', '{"blocks":[]}', 'sess-arch-orphan', 'claude', 1)`,
	); err != nil {
		t.Fatalf("insert orphan: %v", err)
	}

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB (second): %v", err)
	}

	// Finalized…
	var streaming int
	if err := store.ReadDB().QueryRow(
		`SELECT streaming FROM chat_history WHERE session_id = 'sess-arch-orphan'`,
	).Scan(&streaming); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if streaming != 0 {
		t.Fatalf("an archived orphan must still be finalized, streaming=%d", streaming)
	}
	// …but NOT resumed.
	for _, id := range takeInterruptedSessions() {
		if id == "sess-arch-orphan" {
			t.Fatal("an archived session must not be in the resume set")
		}
	}
}

// Both signals are unioned: a crash (streaming=1) and a graceful restart
// (warning block) can happen to different sessions in the same database, and
// each must be recorded exactly once.
func TestInitDB_RecordsBothInterruptSignals(t *testing.T) {
	prevWindow := model.ChatRestartResumeWindowMinutes
	model.ChatRestartResumeWindowMinutes = model.DefaultRestartResumeWindowMinutes
	t.Cleanup(func() { model.ChatRestartResumeWindowMinutes = prevWindow })

	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	warned := `{"blocks":[{"type":"warning","reason":"restart","text":"x"}]}`
	// The warning scan JOINs chat_sessions (to exclude archived sessions), so the
	// graceful session needs a real row; the crash path does not.
	if _, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived)
		 VALUES ('sess-graceful', 1, 'claude', 't', 'chat', 0)`,
	); err != nil {
		t.Fatalf("insert graceful session: %v", err)
	}
	// Crash path.
	if _, err := store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (1, 'assistant', '{"blocks":[]}', 'sess-crash', 'claude', 1)`,
	); err != nil {
		t.Fatalf("insert crash row: %v", err)
	}
	// Graceful path.
	if _, err := store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (1, 'assistant', ?, 'sess-graceful', 'claude', 0)`, warned,
	); err != nil {
		t.Fatalf("insert graceful row: %v", err)
	}

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB (second): %v", err)
	}

	got := takeInterruptedSessions()
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	if seen["sess-crash"] != 1 {
		t.Fatalf("the crash signal must be recorded once, got %+v", got)
	}
	if seen["sess-graceful"] != 1 {
		t.Fatalf("the graceful signal must be recorded once, got %+v", got)
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly the two interrupted sessions, got %+v", got)
	}
}

// A session row with an EMPTY session_type must be resumed, not skipped.
//
// An empty value means 'chat' in this schema (ADD COLUMN ... DEFAULT 'chat') and that is the
// convention the rest of the code follows (store.SessionTypeMatchesFilter
// normalizes ” to 'chat'). Treating ” as "not interactive" would silently stop
// resuming every legacy/blank row.
func TestResumeInterruptedSession_EmptySessionTypeIsChat(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "blank-type", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Blank the column the way a legacy row would be.
	if _, err := store.WriteExec("UPDATE chat_sessions SET session_type = '' WHERE id = ?", sid); err != nil {
		t.Fatalf("blank session_type: %v", err)
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("ResumeInterruptedSession: %v", err)
	}
	if launched.count() != 1 {
		t.Fatalf("an empty session_type means 'chat' and must be resumed, got %d launches", launched.count())
	}
}

// An archived session must not be resumed: it is not in the session list and the
// user put it away on purpose. Pinned because GetSessionFullInfoAny sits right
// next to GetSessionFullInfo and would happily return the row.
func TestResumeInterruptedSession_SkipsArchivedSession(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "archived", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := ArchiveSession("/proj/resume", "claude", sid); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	if err := ResumeInterruptedSession(sid); err != nil {
		t.Fatalf("an archived session must be skipped without error: %v", err)
	}
	if launched.count() != 0 {
		t.Fatalf("an archived session must not be resumed, got %d launches", launched.count())
	}
	if rows := systemRowTexts(t, sid); len(rows) != 0 {
		t.Fatalf("a skipped session must not get a system row, got %+v", rows)
	}
	if IsSessionRunning(sid) {
		t.Fatal("a skipped session must not be claimed")
	}
}

// The AddSystemMessage failure branch must release the claim AND announce a
// terminal state — tryClaimSessionRun already broadcast "running", so dropping
// the runner silently leaves the client's spinner up forever.
func TestResumeInterruptedSession_SystemWriteFailureReleasesClaim(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "write-fail", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Force the notice write to fail.
	origAdd := addResumeNotice
	addResumeNotice = func(_, _, _ string) (int64, error) {
		return 0, errors.New("simulated write failure")
	}
	t.Cleanup(func() { addResumeNotice = origAdd })

	// Capture the terminal broadcast so the "spinner cleared" half is asserted.
	var mu sync.Mutex
	var statuses []string
	origEmitWS := emitTerminalWSOnly
	emitTerminalWSOnly = func(_ string, status string, _ bool) {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
	}
	t.Cleanup(func() { emitTerminalWSOnly = origEmitWS })

	if err := ResumeInterruptedSession(sid); err == nil {
		t.Fatal("a failed notice write must surface an error to the caller's log")
	}

	// The claim must be released, or the session stays "running" forever.
	if IsSessionRunning(sid) {
		t.Fatal("a failed notice write must release the session's claim")
	}
	// And a terminal state must be announced, or the client keeps its spinner.
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 1 || statuses[0] != statusCompleted {
		t.Fatalf("exactly one terminal event must be announced, got %+v", statuses)
	}
	if launched.count() != 0 {
		t.Fatalf("nothing may be launched after a failed write, got %d", launched.count())
	}
	if rows := systemRowTexts(t, sid); len(rows) != 0 {
		t.Fatalf("the failed write must not persist a row, got %+v", rows)
	}
}

// A row with a NULL session_id must not take the whole server down.
//
// chat_history.session_id is nullable, and scanning NULL into a plain string
// fails in modernc sqlite — an InitDB error is os.Exit(1) in main. One legacy
// row would then turn "a restart" into "the server never starts again". The row
// must still be finalized; it just has no session to resume.
func TestInitDB_ToleratesNullSessionIDOnStreamingRow(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	// Close the temp database BEFORE the snapshot restore puts the previous
	// handles back. Without this the SQLite file stays open and t.TempDir's
	// RemoveAll fails on Windows ("being used by another process"), which is a
	// cleanup failure attributed to every test in this file.
	defer store.Close()
	cleanupAllSessionState()
	defer cleanupAllSessionState()

	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	// session_id left NULL on purpose.
	if _, err := store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (1, 'assistant', '{"blocks":[]}', NULL, 'claude', 1)`,
	); err != nil {
		t.Fatalf("insert NULL-session row: %v", err)
	}

	// Must not fail — and must still finalize the row.
	if err := InitDB(true); err != nil {
		t.Fatalf("InitDB must tolerate a NULL session_id on a streaming row: %v", err)
	}
	var streaming int
	if err := store.ReadDB().QueryRow(
		`SELECT streaming FROM chat_history WHERE session_id IS NULL`,
	).Scan(&streaming); err != nil {
		t.Fatalf("the NULL-session row disappeared: %v", err)
	}
	if streaming != 0 {
		t.Fatalf("the orphan row must still be finalized, streaming=%d", streaming)
	}
	// And it must not produce a bogus "" session to resume.
	for _, id := range takeInterruptedSessions() {
		if id == "" {
			t.Fatal("a NULL session_id must not become an empty session id to resume")
		}
	}
}

// The prompt must be a real sentence in both locales — it is BOTH the user-visible
// system row and the instruction the AI follows.
func TestRestartResumePrompt_IsLocalized(t *testing.T) {
	prev := model.Language
	t.Cleanup(func() { model.Language = prev })

	model.Language = "zh"
	zhNotice, zhPrompt := RestartResumeNotice(), RestartResumePrompt()
	if zhNotice == "RestartResume" {
		t.Fatal("zh notice missing (raw message id returned)")
	}
	if zhPrompt == "RestartResumeInstruction" {
		t.Fatal("zh instruction missing (raw message id returned)")
	}
	if !strings.Contains(zhNotice, "重启") || !strings.Contains(zhPrompt, "重启") {
		t.Fatalf("both zh strings should mention the restart: notice=%q prompt=%q", zhNotice, zhPrompt)
	}

	model.Language = "en"
	enNotice, enPrompt := RestartResumeNotice(), RestartResumePrompt()
	if enNotice == "RestartResume" {
		t.Fatal("en notice missing (raw message id returned)")
	}
	if enPrompt == "RestartResumeInstruction" {
		t.Fatal("en instruction missing (raw message id returned)")
	}
	if !strings.Contains(strings.ToLower(enNotice), "restart") ||
		!strings.Contains(strings.ToLower(enPrompt), "restart") {
		t.Fatalf("both en strings should mention the restart: notice=%q prompt=%q", enNotice, enPrompt)
	}
	if enNotice == zhNotice || enPrompt == zhPrompt {
		t.Fatal("each locale pair must differ")
	}

	// The two ROLES must be different text in the same language: a user-facing
	// notice ("the agent has been asked to continue") is not an instruction
	// ("re-evaluate the current state and continue"). Sharing one string is
	// exactly the bug this split fixes.
	if enNotice == enPrompt {
		t.Fatalf("notice and instruction must differ, both %q", enNotice)
	}
	if zhNotice == zhPrompt {
		t.Fatalf("notice and instruction must differ, both %q", zhNotice)
	}
	// The instruction is imperative; the notice is descriptive about the agent.
	if !strings.Contains(enPrompt, "re-evaluate") {
		t.Fatalf("the instruction should tell the model to re-evaluate: %q", enPrompt)
	}
	if !strings.Contains(enNotice, "agent") {
		t.Fatalf("the notice should tell the user the agent was notified: %q", enNotice)
	}
}
