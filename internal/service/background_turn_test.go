package service

import (
	"strings"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/store"
)

// background_turn_test.go covers the service half of issue #644: a turn the
// AGENT started on its own (CodeBuddy background_drain) must land in the
// timeline as an ordinary assistant reply.
//
// The AI layer's half (detecting the announcement and routing the frames) is
// covered in internal/ai/acp_background_turn_test.go.

// insertBackgroundTurnSession creates an ordinary chat session row for the
// project so the adopted turn has a timeline to write to.
func insertBackgroundTurnSession(t *testing.T, sessionID string) {
	t.Helper()
	project := t.TempDir()
	projectID, err := store.ProjectIDForPath(project)
	if err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	if _, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES (?, ?, 'codebuddy', 'bg', '')`,
		sessionID, projectID,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func TestRunBackgroundTurn_PersistsAgentInitiatedTurn(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	const sid = "bg-turn-session"
	insertBackgroundTurnSession(t, sid)

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "content", Content: "后台任务完成了"}
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	ended := 0
	RunBackgroundTurn(sid, events, func() { ended++ })

	if ended != 1 {
		t.Fatalf("route release called %d times, want exactly once", ended)
	}
	if IsSessionRunning(sid) {
		t.Fatal("session left running after the adopted turn finished")
	}

	var content string
	var streaming int
	if err := store.ReadDB().QueryRow(
		`SELECT content, streaming FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`,
		sid,
	).Scan(&content, &streaming); err != nil {
		t.Fatalf("assistant row not written: %v", err)
	}
	if streaming != 0 {
		t.Fatal("assistant row left streaming after the turn finished")
	}
	if !strings.Contains(content, "后台任务完成了") {
		t.Fatalf("assistant row does not carry the turn's content: %s", content)
	}
}

// TestRunBackgroundTurn_SkipsWhenSessionBusy: the agent's premise is that the
// session is idle. If a user turn claimed it first, the adopted turn must not
// open a second placeholder on the same timeline.
func TestRunBackgroundTurn_SkipsWhenSessionBusy(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	const sid = "bg-turn-busy"
	insertBackgroundTurnSession(t, sid)

	if !TrySetSessionRunning(sid) {
		t.Fatal("could not claim the session for the test")
	}
	t.Cleanup(func() { SetSessionRunning(sid, false, true) })

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "content", Content: "should be dropped"}
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	RunBackgroundTurn(sid, events, nil)

	var count int
	if err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant'`, sid,
	).Scan(&count); err != nil {
		t.Fatalf("count assistant rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("wrote %d assistant rows for a session owned by a live turn", count)
	}
}

// TestRunBackgroundTurn_GroupMemberWritesToGroupTimeline: a member row is a
// hidden connection binding, so its background turn must land on the GROUP
// timeline and carry the speaker, or the user would never see it.
func TestRunBackgroundTurn_GroupMemberWritesToGroupTimeline(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "content", Content: "成员的产出"}
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	RunBackgroundTurn(memberID, events, nil)

	var content, agentID string
	if err := store.ReadDB().QueryRow(
		`SELECT content, agent_id FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`,
		groupID,
	).Scan(&content, &agentID); err != nil {
		t.Fatalf("assistant row not written to the group timeline: %v", err)
	}
	if !strings.Contains(content, "成员的产出") {
		t.Fatalf("group timeline row does not carry the turn's content: %s", content)
	}
	if agentID != memberID {
		t.Fatalf("speaker attribution = %q, want the member row %q", agentID, memberID)
	}
}

// TestRunBackgroundTurn_SkipsWhileGroupDiscussionRuns: a group run registers its
// runner under the GROUP id, so the member-row claim would succeed and inject
// this turn into the middle of a discussion.
func TestRunBackgroundTurn_SkipsWhileGroupDiscussionRuns(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, memberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if !TrySetSessionRunning(groupID) {
		t.Fatal("could not claim the group for the test")
	}
	t.Cleanup(func() { SetSessionRunning(groupID, false, true) })

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "content", Content: "should be dropped"}
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	RunBackgroundTurn(memberID, events, nil)

	var count int
	if err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant'`, groupID,
	).Scan(&count); err != nil {
		t.Fatalf("count assistant rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("wrote %d rows into a group whose discussion is running", count)
	}
}

// TestRunBackgroundTurn_CancelledTurnClosesBubble: the user can stop a turn the
// agent started. The turn must end (not wait for its backstop timeout) and the
// partial content must be finalized rather than left streaming.
func TestRunBackgroundTurn_CancelledTurnClosesBubble(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	const sid = "bg-turn-cancel"
	insertBackgroundTurnSession(t, sid)

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "content", Content: "partial answer"}
	// No terminal event: the turn is cancelled instead.

	finished := make(chan struct{})
	go func() {
		RunBackgroundTurn(sid, events, nil)
		close(finished)
	}()

	// Wait until the streaming placeholder exists, then cancel as the user would.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if err := store.ReadDB().QueryRow(
			`SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 1`, sid,
		).Scan(&n); err == nil && n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("streaming placeholder never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !CancelSession(sid) {
		t.Fatal("CancelSession reported no runner")
	}

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled background turn did not end")
	}

	var streaming int
	if err := store.ReadDB().QueryRow(
		`SELECT streaming FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1`,
		sid,
	).Scan(&streaming); err != nil {
		t.Fatalf("assistant row missing: %v", err)
	}
	if streaming != 0 {
		t.Fatal("cancelled turn left its row streaming")
	}

	// The cancel path must NOT report a completion: the user just stopped it,
	// and a "completed" notification would contradict what they saw.
	var completed int
	if err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM notifications WHERE session_id = ? AND event_type = 'completed'`, sid,
	).Scan(&completed); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if completed != 0 {
		t.Fatalf("cancelled turn logged %d completion notification(s)", completed)
	}
}

// TestRunBackgroundTurn_EmptyTurnLeavesNoBubble: an announcement that closes
// without producing anything must not leave a blank assistant bubble behind.
func TestRunBackgroundTurn_EmptyTurnLeavesNoBubble(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	const sid = "bg-turn-empty"
	insertBackgroundTurnSession(t, sid)

	events := make(chan ai.StreamEvent, 2)
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	ended := 0
	RunBackgroundTurn(sid, events, func() { ended++ })
	if ended != 1 {
		t.Fatalf("route release called %d times, want exactly once", ended)
	}

	var count int
	if err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant'`, sid,
	).Scan(&count); err != nil {
		t.Fatalf("count assistant rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty turn created %d assistant row(s)", count)
	}
	if IsSessionRunning(sid) {
		t.Fatal("session left running after an empty turn")
	}
}

// TestRunBackgroundTurn_ReleasesRouteWhenSessionGone: an archived/deleted
// session must not leak the stream route the AI layer opened.
func TestRunBackgroundTurn_ReleasesRouteWhenSessionGone(t *testing.T) {
	setupGroupDB(t)
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	events := make(chan ai.StreamEvent, 8)
	events <- ai.StreamEvent{Type: "done"}
	close(events)

	ended := 0
	RunBackgroundTurn("no-such-session", events, func() { ended++ })
	if ended != 1 {
		t.Fatalf("route release called %d times, want exactly once", ended)
	}
}
