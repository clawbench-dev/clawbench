package service

import (
	"context"
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

// at returns the i-th recorded launch.
func (r *launchRecorder) at(i int) LaunchConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[i]
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

// stubRestartGroupDrain replaces the group drain seam and records the prompts.
//
// The returned channel is closed by the first (and only expected) call: the
// resume path drives the group drain from a goroutine, so without a handshake the
// test could finish, tear the DB down and restore the seam while that goroutine
// is still starting — it would then run the REAL orchestrator against a closed
// database. Awaiting the signal makes the assertion order deterministic instead
// of "usually fast enough".
func stubRestartGroupDrain(t *testing.T) (*[]string, <-chan struct{}) {
	t.Helper()
	var prompts []string
	done := make(chan struct{}, 1)
	orig := SetRunGroupDrainLoopForTest(func(_ context.Context, _ string, _ string, _ int64, text string, _ []model.FileEntry) {
		prompts = append(prompts, text)
		select {
		case done <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() { SetRunGroupDrainLoopForTest(orig) })
	return &prompts, done
}

// systemRowTexts returns the role='system' rows of a session, oldest first.
func systemRowTexts(t *testing.T, sessionID string) []string {
	t.Helper()
	rows, err := store.ReadDB().Query(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'system' ORDER BY id ASC", sessionID)
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
	if launched.at(0).SessionID != sid {
		t.Fatalf("launched for the wrong session: got %q want %q", launched.at(0).SessionID, sid)
	}
	if launched.at(0).Message != RestartResumePrompt() {
		t.Fatalf("launched with the wrong prompt: got %q want %q", launched.at(0).Message, RestartResumePrompt())
	}
	if launched.at(0).RunCtx == nil {
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
	if rows[0] != RestartResumePrompt() {
		t.Fatalf("system row must carry the recovery notice: got %q", rows[0])
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
	prompts, groupDone := stubRestartGroupDrain(t)

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
		t.Fatalf("group drain must receive the resume prompt: got %q", (*prompts)[0])
	}

	// AddGroupMember already wrote a "joined the discussion" row, so the resume
	// notice is the LAST system row rather than the only one.
	rows := systemRowTexts(t, groupID)
	if len(rows) == 0 {
		t.Fatal("the recovery notice was not persisted to the group timeline")
	}
	if last := rows[len(rows)-1]; last != RestartResumePrompt() {
		t.Fatalf("the newest system row must be the recovery notice: got %q (all: %+v)", last, rows)
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
	scheduled := startRestartResume(0, resumeSessionFn)
	if len(scheduled) != 1 || scheduled[0] != sid {
		t.Fatalf("expected the session to be scheduled, got %+v", scheduled)
	}

	waitForCount(t, 1, func() int { return launched.count() })
	if launched.at(0).SessionID != sid {
		t.Fatalf("resumed the wrong session: got %q want %q", launched.at(0).SessionID, sid)
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
func TestResumeOneSafely_PanicDoesNotStopTheRest(t *testing.T) {
	setupRestartResumeDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	calls := stubRestartLaunch(t)

	// The first session panics; the second must still be resumed.
	var seen []string
	panicking := func(s string) error {
		seen = append(seen, s)
		if s == "boom" {
			panic("simulated failure")
		}
		return nil
	}

	resumeOneSafely("boom", panicking)
	resumeOneSafely("ok", panicking)

	if len(seen) != 2 {
		t.Fatalf("the second session must still be resumed after the panic, saw %+v", seen)
	}
	// The panic path must not leave a claim behind.
	if IsSessionRunning("boom") {
		t.Fatal("a panicking resume must release the session's claim")
	}
	_ = calls
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

	// Wait for the drain goroutine to FINISH before touching the assertions: it
	// holds the session claim until it exits, and letting it outlive the test
	// would run the real orchestrator against a torn-down DB.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && IsSessionRunning(groupID) {
		time.Sleep(5 * time.Millisecond)
	}
	if IsSessionRunning(groupID) {
		t.Fatal("the group drain did not finish within the timeout")
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
	want := "[系统] " + RestartResumePrompt()
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
	zh := RestartResumePrompt()
	if zh == "RestartResume" {
		t.Fatal("zh translation missing (raw message id returned)")
	}
	if !strings.Contains(zh, "重启") {
		t.Fatalf("zh prompt should mention the restart: %q", zh)
	}

	model.Language = "en"
	en := RestartResumePrompt()
	if en == "RestartResume" {
		t.Fatal("en translation missing (raw message id returned)")
	}
	if !strings.Contains(strings.ToLower(en), "restart") {
		t.Fatalf("en prompt should mention the restart: %q", en)
	}
	if en == zh {
		t.Fatalf("the two locales must differ: both %q", en)
	}
}
