package service

import "testing"

// TestBuildChatRequest_KeepsClawbenchSessionIDOnResumedTurn pins the id that
// mid-turn insertion is addressed by.
//
// A resumed turn of a self-hosted backend has no external session id, so
// resolveResumeSessionID blanks SessionID — the CLI must never be told to
// --resume an id it has never seen. Anything keyed by the conversation the USER
// sees has to survive that blanking: the built-in backend registers its
// injection mailbox under ClawbenchSessionID, and reading SessionID there left
// the registry empty from the second turn on. Every insertion then declined as
// "idle" while the reply was still streaming (the message stayed queued).
func TestBuildChatRequest_KeepsClawbenchSessionIDOnResumedTurn(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "resumed-sess", [][2]string{
		{"user", "first question"},
		{"assistant", "first answer"},
	})

	req := BuildChatRequest("next question", "resumed-sess", "/tmp/p", builtinBackendID,
		builtinBackendID, "", "", "", "/tmp/p", false)

	if req.SessionID != "" {
		t.Fatalf("a resumed self-hosted turn must not hand the CLI a resume id, got %q", req.SessionID)
	}
	if req.ClawbenchSessionID != "resumed-sess" {
		t.Fatalf("the conversation's own id must survive the blanking, got %q", req.ClawbenchSessionID)
	}
}
