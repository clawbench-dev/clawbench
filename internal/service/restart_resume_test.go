package service

import (
	"context"
	"path/filepath"
	"strings"
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

// stubRestartLaunch replaces the single-chat launch seam and records the configs.
func stubRestartLaunch(t *testing.T) *[]LaunchConfig {
	t.Helper()
	var calls []LaunchConfig
	orig := launchConsumerExecution
	launchConsumerExecution = func(cfg LaunchConfig) { calls = append(calls, cfg) }
	t.Cleanup(func() { launchConsumerExecution = orig })
	return &calls
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
	if len(*launched) != 1 {
		t.Fatalf("expected exactly one launched turn, got %d: %+v", len(*launched), *launched)
	}
	if (*launched)[0].SessionID != sid {
		t.Fatalf("launched for the wrong session: got %q want %q", (*launched)[0].SessionID, sid)
	}
	if (*launched)[0].Message != RestartResumePrompt() {
		t.Fatalf("launched with the wrong prompt: got %q want %q", (*launched)[0].Message, RestartResumePrompt())
	}
	if (*launched)[0].RunCtx == nil {
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
	if len(*launched) != 0 {
		t.Fatalf("a running session must not be resumed again, got %d launches", len(*launched))
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
	if len(*launched) != 0 {
		t.Fatalf("a session with queued work must be left to the reaper, got %d launches", len(*launched))
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
	if len(*launched) != 0 {
		t.Fatalf("nothing may be launched for a missing session, got %d", len(*launched))
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

	if len(*launched) != 0 {
		t.Fatalf("a group must not run as a single-agent turn, got %d launches", len(*launched))
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

// The worker is gated on the auto-continue switch: with it off, nothing is
// resumed and the recorded set is dropped (so enabling later does not resurrect
// an old restart).
func TestStartRestartResume_GatedOnAutoContinue(t *testing.T) {
	setupRestartResumeDB(t)
	launched := stubRestartLaunch(t)

	sid, err := CreateSession("/proj/resume", "claude", "gated", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	prevEnabled := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = false
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prevEnabled })

	// Drop the delay so the (gated) worker body would run immediately: without
	// this the goroutine would still be sleeping when the test ends, and the
	// "nothing launched" assertion would pass even if the gate were deleted.
	prevDelay := restartResumeDelay
	restartResumeDelay = 0
	t.Cleanup(func() { restartResumeDelay = prevDelay })

	recordInterruptedSessions([]string{sid})
	StartRestartResume()

	// The set is drained regardless of the gate.
	if got := takeInterruptedSessions(); len(got) != 0 {
		t.Fatalf("the recorded set must be drained on every start, still holds %+v", got)
	}
	// Give a wrongly-started worker a chance to be observed. With the gate
	// removed it launches synchronously within this window.
	time.Sleep(100 * time.Millisecond)
	if len(*launched) != 0 {
		t.Fatalf("nothing may be resumed while auto-continue is off, got %d launches", len(*launched))
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
