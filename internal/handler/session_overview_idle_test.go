package handler

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overviewProject is one entry of the overview response, decoded loosely: the
// idle-card fields are what these tests assert on.
type overviewProject struct {
	Name         string            `json:"name"`
	Sessions     []json.RawMessage `json:"sessions"`
	LastActiveAt string            `json:"lastActiveAt"`
}

func getOverview(t *testing.T, currentProject string) []overviewProject {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, currentProject)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)
	var result struct {
		Projects []overviewProject `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result.Projects
}

// setCardCap pins chat.recent_project_cards_max_count for the duration of a test.
func setCardCap(t *testing.T, n int) {
	t.Helper()
	orig := model.ChatRecentProjectCardsMaxCount
	model.ChatRecentProjectCardsMaxCount = n
	t.Cleanup(func() { model.ChatRecentProjectCardsMaxCount = orig })
}

// TestServeSessionsOverview_includesRecentlyActiveProjects covers the collapsed
// card: a project the user worked in this morning still gets an entry after
// every reply has been read, carrying the time instead of sessions.
func TestServeSessionsOverview_includesRecentlyActiveProjects(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setCardCap(t, 10)

	quiet := canonPath(filepath.Join(env.WatchDir, "project-quiet"))
	_, err := service.CreateSession(quiet, "codebuddy", "read already", "", "", "default", "chat")
	require.NoError(t, err)

	projects := getOverview(t, env.ProjectDir)
	require.Len(t, projects, 1)
	assert.Equal(t, quiet, projects[0].Name)
	assert.Empty(t, projects[0].Sessions, "an idle card carries no rows")
	assert.NotEmpty(t, projects[0].LastActiveAt, "it carries when the project was last touched instead")
}

// TestServeSessionsOverview_doesNotDuplicateActiveProjects pins the "one card
// per project" rule: a project that already has active sessions must not also
// come back as an idle entry (it would render twice, once with rows and once
// without).
func TestServeSessionsOverview_doesNotDuplicateActiveProjects(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setCardCap(t, 10)

	busy := canonPath(filepath.Join(env.WatchDir, "project-busy"))
	sessionID, err := service.CreateSession(busy, "codebuddy", "running now", "", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	projects := getOverview(t, env.ProjectDir)
	require.Len(t, projects, 1)
	assert.Equal(t, busy, projects[0].Name)
	assert.Len(t, projects[0].Sessions, 1)
	assert.Empty(t, projects[0].LastActiveAt, "active cards are not idle entries")
}

// TestServeSessionsOverview_excludesCurrentProjectFromIdleCards keeps the
// caller's own project out of the card list: its sessions are the main list
// right above, so a card for it would duplicate them.
func TestServeSessionsOverview_excludesCurrentProjectFromIdleCards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setCardCap(t, 10)

	// A session in the CURRENT project that is neither running nor unread.
	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "mine", "", "", "default", "chat")
	require.NoError(t, err)

	assert.Empty(t, getOverview(t, env.ProjectDir), "the current project never becomes a card")
}

// TestServeSessionsOverview_capsCards covers chat.recent_project_cards_max_count:
// the cap bounds what is displayed, and active projects win the slots.
func TestServeSessionsOverview_capsCards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setCardCap(t, 2)

	// One active project plus three idle ones. With a cap of 2 the response must
	// hold exactly two entries, and the active one must be among them.
	busy := canonPath(filepath.Join(env.WatchDir, "project-busy"))
	sessionID, err := service.CreateSession(busy, "codebuddy", "running", "", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	for _, name := range []string{"idle-1", "idle-2", "idle-3"} {
		p := canonPath(filepath.Join(env.WatchDir, name))
		_, cerr := service.CreateSession(p, "codebuddy", name, "", "", "default", "chat")
		require.NoError(t, cerr)
	}

	projects := getOverview(t, env.ProjectDir)
	require.Len(t, projects, 2, "the cap bounds the number of cards")
	assert.Equal(t, busy, projects[0].Name, "an active project takes the first slot")
}

// TestServeSessionsOverview_ignoresSessionsOutsideTheWindow pins the recency
// window: an old session with nothing active must not produce a card at all.
func TestServeSessionsOverview_ignoresSessionsOutsideTheWindow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setCardCap(t, 10)

	old := canonPath(filepath.Join(env.WatchDir, "project-old"))
	_, err := service.CreateSession(old, "codebuddy", "long ago", "", "", "default", "chat")
	require.NoError(t, err)
	// Push the session's activity past the window. CreateSession stamps
	// CURRENT_TIMESTAMP, so the row has to be aged explicitly.
	_, err = store.WriteExec(
		"UPDATE chat_sessions SET updated_at = ? WHERE title = ?",
		time.Now().UTC().Add(-service.RecentProjectActivityWindow-time.Hour).Format("2006-01-02 15:04:05"),
		"long ago",
	)
	require.NoError(t, err)

	assert.Empty(t, getOverview(t, env.ProjectDir))
}
