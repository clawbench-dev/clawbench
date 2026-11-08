package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeProjectSessions_listsEverySessionOfTheProject verifies the
// read-only cross-project list: unlike /api/ai/sessions/overview (active rows
// only), it must return the project's whole list, because the user asked to
// browse it.
func TestServeProjectSessions_listsEverySessionOfTheProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	runningID, err := service.CreateSession(env.ProjectDir, "claude", "Running one", "claude", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(runningID, true)
	t.Cleanup(func() { service.SetSessionRunning(runningID, false) })

	quietID, err := service.CreateSession(env.ProjectDir, "claude", "Quiet one", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/by-project?path="+url.QueryEscape(env.ProjectDir), nil)
	w := callHandler(ServeProjectSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Running bool   `json:"running"`
		} `json:"sessions"`
		TotalCount int `json:"totalCount"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Sessions, 2)
	assert.Equal(t, 2, result.TotalCount)

	byID := map[string]bool{}
	for _, s := range result.Sessions {
		byID[s.ID] = s.Running
	}
	assert.True(t, byID[runningID], "the running session keeps its live flag")
	// A session with no activity is still listed — that is the whole point of
	// browsing a project rather than watching it.
	assert.False(t, byID[quietID])
}

// TestServeProjectSessions_rejectsUnknownProject keeps the endpoint from being
// usable as a probe: only registered projects answer.
func TestServeProjectSessions_rejectsUnknownProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	unknown := filepath.Join(env.WatchDir, "never-opened")
	req := newRequest(t, http.MethodGet, "/api/ai/sessions/by-project?path="+url.QueryEscape(unknown), nil)
	w := callHandler(ServeProjectSessions, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeProjectSessions_requiresPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/by-project", nil)
	w := callHandler(ServeProjectSessions, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeProjectSessions_methodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions/by-project?path=/tmp/x", nil)
	w := callHandler(ServeProjectSessions, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
