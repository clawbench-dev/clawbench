package handler

import (
	"context"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compactFlagBackend is a stub AI backend that produces one trivial turn.
//
// The test asserts on the DB flag, not on any AI output, so a real agent
// process would only add cost, latency, credentials, and a leaked goroutine
// that outlives the test's DB. Same pattern as promptCapturingBackend in
// chat_test.go.
type compactFlagBackend struct{}

func (m *compactFlagBackend) Name() string { return "mock-compactflag" }

func (m *compactFlagBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "content", Content: "ok"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

// compactFlagBackendID is the stub backend both tests use. RegisterBackend
// panics on a duplicate id, so registration is guarded — the two tests share
// one process and each calls the setup helper.
const compactFlagBackendID = "mock-compactflag"

// registerCompactFlagBackend registers the stub exactly once per process.
func registerCompactFlagBackend() {
	if ai.LookupBackendFactoryForTest(compactFlagBackendID) != nil {
		return
	}
	ai.RegisterBackend(compactFlagBackendID, func() ai.AIBackend { return &compactFlagBackend{} })
}

// setupCompactFlagSession registers the stub backend and creates a session of
// the requested type. sessionType is passed straight through so the group case
// gets a real `session_type='group'` row without going through the group API
// (which would create member rows and a host — irrelevant here).
func setupCompactFlagSession(t *testing.T, projectDir, sessionID, sessionType string) {
	t.Helper()

	registerCompactFlagBackend()
	agentID := "compactflag-agent"
	model.Agents[agentID] = &model.Agent{ID: agentID, Backend: compactFlagBackendID}

	_, err := store.UnsafeDBForTest().Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, ?, ?, ?, 'default', '', ?, 0)`,
		sessionID, store.ProjectIDForTest(t, projectDir), compactFlagBackendID, sessionID, agentID, sessionType,
	)
	require.NoError(t, err)
}

// readCompactedFlag reads chat_sessions.compacted for a session.
func readCompactedFlag(t *testing.T, sessionID string) int {
	t.Helper()
	var flag int
	require.NoError(t, store.ReadDB().
		QueryRow("SELECT compacted FROM chat_sessions WHERE id = ?", sessionID).
		Scan(&flag))
	return flag
}

// TestAIChat_CompactFlag_SingleChat exercises the ordinary path: a user-issued
// /compact flags the session so the NEXT turn re-injects the system prompt.
//
// This is the behavior the group guard must not disturb.
func TestAIChat_CompactFlag_SingleChat(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "compact-flag-single"
	setupCompactFlagSession(t, env.ProjectDir, sessionID, "chat")

	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, map[string]any{
		"message": "/compact",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, 1, readCompactedFlag(t, sessionID),
		"a single-chat /compact must flag the session for one-shot re-injection")
}

// TestAIChat_CompactFlag_GroupSessionNotFlagged is the regression test for the
// dirty flag: a group row never runs a turn, so nothing ever consumes (and
// clears) its `compacted` flag — setting it left the column at 1 forever.
func TestAIChat_CompactFlag_GroupSessionNotFlagged(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID := "compact-flag-group"
	setupCompactFlagSession(t, env.ProjectDir, groupID, "group")
	require.Equal(t, "group", service.GetSessionType(groupID))

	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+groupID, map[string]any{
		"message": "/compact",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, 0, readCompactedFlag(t, groupID),
		"a group's /compact must not set a flag on the group row (nothing would clear it)")
}
