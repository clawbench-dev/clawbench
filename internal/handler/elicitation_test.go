package handler

import (
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
)

func TestServeElicitationRespond_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/elicitation/respond", nil)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeElicitationRespond_MissingProjectCookie(t *testing.T) {
	body := map[string]any{"sessionId": "s1", "elicitationId": "elic_1"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeElicitationRespond_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"elicitationId": "elic_1"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeElicitationRespond_MissingElicitationID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"sessionId": "s1"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeElicitationRespond_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession("/other-project", "claude", "Other Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessionID, "elicitationId": "elic_1"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeElicitationRespond_SessionExistsNoACPClient(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]any{"sessionId": sessionID, "elicitationId": "elic_1"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeElicitationRespond_NoPendingElicitation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	mgr.SetConnForTest(sessionID, conn)
	defer mgr.CloseConn(sessionID)

	body := map[string]any{"sessionId": sessionID, "elicitationId": "elic_missing"}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeElicitationRespond_DeliversAnswer(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	mgr.SetConnForTest(sessionID, conn)
	defer mgr.CloseConn(sessionID)

	answered := client.RegisterPendingElicitationForTest("elic_1", "acp-session-1")

	body := map[string]any{
		"sessionId":     sessionID,
		"elicitationId": "elic_1",
		"content":       map[string]any{"q0": "docker"},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/elicitation/respond", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeElicitationRespond, req)
	assertStatus(t, w, http.StatusOK)

	select {
	case ans := <-answered:
		if ans.Cancelled || ans.Content["q0"] != "docker" {
			t.Fatalf("answer = %+v, want q0=docker", ans)
		}
	default:
		t.Fatal("the pending elicitation received nothing")
	}
}
