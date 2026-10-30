package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeGroupCreateAndMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a group with an explicit host agent.
	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"title": "讨论组", "hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		OK           bool   `json:"ok"`
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.OK)
	require.NotEmpty(t, created.GroupID)
	require.NotEmpty(t, created.HostMemberID)

	// The group session has session_type='group'.
	require.Equal(t, "group", service.GetSessionType(created.GroupID))

	// Add a member (batch).
	req = newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": created.GroupID, "agentIds": []string{"claude"}})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// List members: host + added, with isHost on the host.
	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+created.GroupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed struct {
		Members []struct {
			ID       string `json:"id"`
			IsHost   bool   `json:"isHost"`
			Disabled bool   `json:"disabled"`
		} `json:"members"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Members, 2)
	hostSeen := false
	for _, m := range listed.Members {
		if m.ID == created.HostMemberID {
			hostSeen = true
			assert.True(t, m.IsHost, "host member must carry isHost=true")
		}
	}
	assert.True(t, hostSeen, "host member must be listed")
}

// TestServeGroupCreateWithMembers pins the one-step creation endpoint (design
// §7.1, decision #25): a single POST with hostAgentId + memberAgentIds creates
// the group and ALL members at once, so the frontend no longer follows up with
// a separate add-members call.
func TestServeGroupCreateWithMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		OK           bool   `json:"ok"`
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.OK)
	require.NotEmpty(t, created.GroupID)

	// Both members exist after the single call — no follow-up needed.
	members, err := service.ListGroupMembers(created.GroupID)
	require.NoError(t, err)
	require.Len(t, members, 2, "host + 1 member must be created in one call")
	require.Equal(t, created.HostMemberID, service.GetGroupHostMember(created.GroupID))

	hostSeen := false
	for _, m := range members {
		if m.ID == created.HostMemberID {
			hostSeen = true
			require.Equal(t, "codebuddy", m.AgentID)
		}
	}
	require.True(t, hostSeen, "host row must be among the created members")
}

// TestServeGroupCreateInvalidMemberAborts pins atomicity at the HTTP boundary:
// an unknown agent id must fail the request and leave no group behind.
func TestServeGroupCreateInvalidMemberAborts(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "no-such-agent-xyz"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE session_type = 'group'",
	).Scan(&count))
	require.Zero(t, count, "failed create must not leave a group row")
}

// POST /api/group/members must SKIP a disabled agent (like an unknown id)
// rather than failing the whole batch, so a stale picker payload cannot add a
// disabled agent to a live group.
func TestServeGroupMembersAdd_SkipsDisabledAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	// "claude" is disabled; "codebuddy" is a valid addable agent.
	require.NotNil(t, model.Agents["claude"])
	model.Agents["claude"].Disabled = true
	defer func() { model.Agents["claude"].Disabled = false }()

	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{
		"groupId":  groupID,
		"agentIds": []string{"claude", "codebuddy"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		MemberIDs []string `json:"memberIds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.MemberIDs, 1, "only the enabled agent may be added")

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	added := 0
	for _, m := range members {
		if m.AgentID == "claude" && !m.Disabled {
			added++
		}
	}
	require.Zero(t, added, "the disabled agent must not become an active member")
}

// A disabled agent must not be seeded into a NEW group (server-side fallback
// for the picker filter), and — like an unknown id — must abort the whole
// request rather than silently dropping the member.
func TestServeGroupCreateDisabledMemberAborts(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Mark one of the default test agents disabled.
	require.NotNil(t, model.Agents["claude"])
	model.Agents["claude"].Disabled = true
	defer func() { model.Agents["claude"].Disabled = false }()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE session_type = 'group'",
	).Scan(&count))
	require.Zero(t, count, "failed create must not leave a group row")
}

// parallelDefault is the group's only remaining per-group setting (the
// member-speech cap moved to the global config), so a PATCH carrying it must
// succeed and round-trip.
func TestServeGroupSettings_ParallelDefaultRoundTrip(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID, "parallelDefault": true})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.True(t, service.GetGroupParallelDefault(groupID), "parallelDefault must round-trip true")

	// Turning it back off must also persist.
	req = newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID, "parallelDefault": false})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, service.GetGroupParallelDefault(groupID), "parallelDefault must round-trip false")
}

// Enabling group auto-cruise must FORCE auto-approve on (cruise runs
// unattended), and disabling cruise must leave auto-approve alone.
func TestServeGroupSettings_CruiseForcesAutoApprove(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	// Start from auto-approve OFF.
	service.SetGroupAutoApprove(groupID, false)
	require.False(t, service.GetSessionAutoApprove(groupID))

	// Enable cruise -> auto-approve must flip on.
	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID, "cruise": true})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, service.GetSessionAutoApprove(groupID),
		"enabling cruise must force the group's auto-approve on")

	// Disable cruise -> auto-approve must stay on (one-directional).
	req = newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID, "cruise": false})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, service.GetSessionAutoApprove(groupID),
		"disabling cruise must NOT turn auto-approve off")
}

// A PATCH with NO setting is a no-op request and must be rejected: it would
// otherwise report success while changing nothing.
func TestServeGroupSettings_NoSettingsIsBadRequest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/group/settings", map[string]any{"groupId": groupID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupSettings, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// TestAIChatDelegatesGroupSend verifies a POST to /api/ai/chat for a group
// session returns the group-delegation response (the orchestrator runs in the
// background; the handler itself must not run a single-agent turn).
func TestAIChatDelegatesGroupSend(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"message": "hi"})
	_ = body
	req := newRequest(t, http.MethodPost, "/api/ai/chat", map[string]any{"message": "hi"})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: groupID})
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["group"], "group send must be delegated to the orchestrator")

	// The idle group send MUST return msgId, exactly like the single-agent path.
	// Without it the frontend cannot adopt the DB id for its optimistic bubble,
	// so the bubble stays in the transient sort domain (after every DB-backed
	// message) while the self-echo renders the real row in place — the reported
	// "my new message appears twice, one stuck at the bottom" defect. Relying on
	// the self-echo alone is a single point of failure: it only adopts when the
	// echo's senderClientId matches, and a group drain-materialized echo carries
	// none.
	msgID, ok := resp["msgId"].(float64)
	require.True(t, ok, "idle group send must return msgId so the optimistic bubble can adopt its DB id")
	assert.Greater(t, int64(msgID), int64(0))
}

// TestServeSessions_GroupRowCarriesMemberPreview pins the session-list contract
// for group rows: a group session carries a compact `groupMembers` preview
// (active members only), while a plain chat session omits the key entirely.
func TestServeSessions_GroupRowCarriesMemberPreview(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, hostMemberID, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// A plain chat session in the same project must NOT grow the field.
	chatID, err := service.CreateSession(env.ProjectDir, "codebuddy", "plain", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []model.ChatSession `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	byID := map[string]model.ChatSession{}
	for _, s := range result.Sessions {
		byID[s.ID] = s
	}

	group, ok := byID[groupID]
	require.True(t, ok, "group session must appear in the list")
	require.Len(t, group.GroupMembers, 2, "host + added member")
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range group.GroupMembers {
		names[m.Name] = true
		ids[m.ID] = true
	}
	assert.True(t, names["Host"] && names["Claude"], "preview names=%v", names)
	assert.True(t, ids[hostMemberID] && ids[memberID], "preview must carry member row ids")

	// The plain chat row omits the field entirely (omitempty + nil slice → the
	// key is absent, so a non-group payload is byte-for-byte unchanged). Assert
	// on the raw JSON, not the decoded slice, which cannot tell absent from [].
	// The `byID` guard is load-bearing: without it a payload that dropped the
	// plain row entirely would make the loop below pass vacuously.
	require.Contains(t, byID, chatID, "the plain chat row must be in the payload")
	require.Contains(t, w.Body.String(), `"groupMembers"`, "the group row must carry the preview key")
	var raw struct {
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	sawChat := false
	for _, s := range raw.Sessions {
		if s["id"] == chatID {
			sawChat = true
			_, present := s["groupMembers"]
			assert.False(t, present, "non-group session must not carry the groupMembers key")
		}
	}
	assert.True(t, sawChat, "plain chat row must appear in the raw payload")

	// Members themselves are hidden from the list.
	assert.NotContains(t, byID, hostMemberID)
	assert.NotContains(t, byID, memberID)
}

// TestServeSessionsOverview_GroupRowCarriesMemberPreview pins the same contract
// on the cross-project overview endpoint. A group only appears there when it is
// running/pending/unread, so the fixture marks it unread.
func TestServeSessionsOverview_GroupRowCarriesMemberPreview(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	_, err = service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Make the group unread so the overview (running/pending/unread only)
	// includes it. A group's unread is the single "waiting on you" flag — NOT a
	// count of member messages — so the fixture sets that flag. Inserting an
	// assistant row would no longer make a group unread (see
	// service.unreadCountSubquery).
	_, err = store.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET awaiting_user_at = CURRENT_TIMESTAMP WHERE id = ?", groupID,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Sessions []struct {
				ID           string                     `json:"id"`
				SessionType  string                     `json:"sessionType"`
				GroupMembers []model.GroupMemberPreview `json:"groupMembers"`
			} `json:"sessions"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	found := false
	for _, p := range result.Projects {
		for _, s := range p.Sessions {
			if s.ID != groupID {
				continue
			}
			found = true
			assert.Equal(t, "group", s.SessionType)
			require.Len(t, s.GroupMembers, 2)
		}
	}
	assert.True(t, found, "group session must appear in the overview")
}

// Muting the host must be refused with 409 so the frontend can explain why,
// rather than the generic 500 the unguarded path produced.
func TestServeGroupMembers_RefuseHostDisable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"title": "讨论组", "hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	req = newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": created.GroupID, "memberId": created.HostMemberID, "disabled": true})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "CannotDisableHost")
}

// TestAIChatGet_ReturnsSessionType pins decision #62: GET /api/ai/chat must
// return the stored session_type so the frontend can decide "group vs single"
// from the TYPE. The member roster is not authoritative — useGroupMembers clears
// it on any fetch failure, and deriving group-ness from "roster is non-empty"
// would make a group render as a single chat after one network blip.
func TestAIChatGet_ReturnsSessionType(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(AIChat, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "group", result["sessionType"])

	// A plain chat session reports its own type, not "group".
	chatID, err := service.CreateSession(env.ProjectDir, "codebuddy", "single", "", "", "default", "chat")
	require.NoError(t, err)

	req = newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+chatID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(AIChat, req)
	assertOK(t, w)

	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "chat", result["sessionType"])
}

// A group send WITH attachments must no longer be rejected (decision #65): the
// files are carried to the orchestrator, which persists them on the user row
// for the injection layer. Before this, the group branch returned 400.
func TestAIChatDelegatesGroupSendWithAttachments(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	// A REAL file inside the project: the handler validates attachment paths
	// (os.Stat + containment), so a fake path is correctly rejected. Use a
	// project-RELATIVE path — that is the form the frontend sends, and it is
	// resolved against the project cookie.
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "report.pdf"), []byte("x"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/ai/chat", map[string]any{
		"message": "看看这个",
		"files":   []map[string]any{{"path": "report.pdf"}},
	})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: groupID})
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["group"], "group send with attachments must be delegated, not rejected")
}

// A second message sent while a group turn is running must be QUEUED, exactly
// like single chat — never run as a second concurrent orchestrator (decision
// #45). Two group turns on one timeline would interleave member turns.
func TestAIChat_GroupBusyEnqueues(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	// Simulate a live group run: claim the session so the handler sees "busy".
	runCtx, claimed := service.TryClaimSessionRun(groupID)
	require.True(t, claimed, "precondition: the group must start idle")
	t.Cleanup(func() {
		_ = runCtx
		service.FinishSessionRun(groupID)
	})

	req := newRequest(t, http.MethodPost, "/api/ai/chat", map[string]any{"message": "第二条"})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: groupID})
	w := callHandlerWithAuth(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["queued"], "a message to a busy group must be queued")
	assert.Equal(t, true, resp["running"])

	// It landed in the queue, not the timeline.
	queued, err := service.GetQueuedMessages(groupID)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, "第二条", queued[0].Text)
}

// POST /api/group/members must enforce the member cap on the WHOLE batch: an
// over-limit batch is rejected without adding ANY member (decision #78 / O19).
// The handler must delegate to the service batch entry, not loop per member.
func TestServeGroupMembers_BatchOverLimitIsAtomic(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Register enough distinct agents for the handler's resolveAgentConfig to
	// accept a full-size roster.
	origAgents := model.Agents
	agents := map[string]*model.Agent{}
	for i := range 20 {
		id := "agent-" + string(rune('a'+i))
		agents[id] = &model.Agent{ID: id, Name: id, Backend: "claude"}
	}
	agents["codebuddy"] = &model.Agent{ID: "codebuddy", Name: "Test", Backend: "codebuddy"}
	model.Agents = agents
	t.Cleanup(func() { model.Agents = origAgents })

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	// Fill to (cap - 1) active members (host + cap-2) directly at the service
	// layer. The cap counts INSTANCES.
	memberCap := service.MaxGroupMembers
	for i := range memberCap - 2 {
		agentID := "agent-" + string(rune('a'+i))
		_, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", agentID, agentID)
		require.NoError(t, err)
	}

	// Two more instances would exceed the cap → 409 and NO change. "agent-k"/"agent-l"
	// are registered but not yet members (members are agent-a .. agent-j).
	body := map[string]any{"groupId": groupID, "agentIds": []string{"agent-k", "agent-l"}}
	req := newRequest(t, http.MethodPost, "/api/group/members", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	active := 0
	for _, m := range members {
		if !m.Disabled {
			active++
		}
	}
	require.Equal(t, memberCap-1, active, "a rejected batch must not add any member")
}

// GET /api/group/members must include the free-mode parallelDefault (the
// action-bar switch reads its value back from here) and must NOT include the
// member-speech cap, which is now a global setting rather than a per-group one.
func TestServeGroupMembers_ReturnsParallelDefault(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "g", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/group/members?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		MaxRounds       *int `json:"maxRounds"`
		ParallelDefault bool `json:"parallelDefault"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.MaxRounds, "maxRounds must no longer be returned (it is a global setting)")
	assert.False(t, resp.ParallelDefault, "parallelDefault defaults to false (sequential)")

	// The free-mode action-bar switch reads its value back from this endpoint.
	require.NoError(t, service.SetGroupParallelDefault(groupID, true))
	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.ParallelDefault, "parallelDefault must reflect the stored value")
}

// TestServeGroupMembersCarriesContext pins the member-list additions the group
// roster UI reads: every member carries `model` and `usage`, and a member that
// never reported usage gets an explicit `null` (the client distinguishes that
// from "0% used").
func TestServeGroupMembersCarriesContext(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Give the host a reported context window and a model override.
	service.PatchContextStateMerge(created.HostMemberID, map[string]string{
		"usage": `{"used":50000,"size":200000}`,
	})
	require.NoError(t, service.UpdateSessionModel(created.HostMemberID, "gpt-4o"))

	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+created.GroupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed struct {
		Members []struct {
			ID    string         `json:"id"`
			Model string         `json:"model"`
			Usage map[string]any `json:"usage"`
		} `json:"members"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Members, 2)

	byID := map[string]struct {
		Model string
		Usage map[string]any
	}{}
	for _, m := range listed.Members {
		byID[m.ID] = struct {
			Model string
			Usage map[string]any
		}{m.Model, m.Usage}
	}
	host := byID[created.HostMemberID]
	assert.Equal(t, "gpt-4o", host.Model)
	require.NotNil(t, host.Usage)
	assert.EqualValues(t, 50000, host.Usage["used"])
	assert.EqualValues(t, 200000, host.Usage["size"])

	// The other member reported nothing → usage must be null, not {}.
	for id, m := range byID {
		if id == created.HostMemberID {
			continue
		}
		assert.Nil(t, m.Usage, "a member with no reported usage must serialize as null")
	}
}

// TestServeGroupMemberCompactGuards pins the endpoint's pre-flight guards. The
// happy path needs a live ACP agent, so only the rejections are asserted here —
// they are what a user actually hits (group busy, wrong member, CLI transport).
func TestServeGroupMemberCompactGuards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Method guard.
	req = newRequest(t, http.MethodGet, "/api/group/member-compact", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMemberCompact, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// Missing fields.
	req = newRequest(t, http.MethodPost, "/api/group/member-compact", map[string]any{"groupId": created.GroupID})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMemberCompact, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A member id that is not part of this group.
	req = newRequest(t, http.MethodPost, "/api/group/member-compact", map[string]any{
		"groupId": created.GroupID, "memberId": "not-a-member",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMemberCompact, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestServeGroupMemberCompactStatusMapping pins the remaining status codes.
// The service returns typed errors; the handler must map each to its own HTTP
// status — collapsing them (e.g. reporting every failure as 500) would hide a
// "the group is busy, try again" from the user and from the frontend's retry.
func TestServeGroupMemberCompactStatusMapping(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// A missing project cookie must be rejected before the service is reached.
	req = newRequest(t, http.MethodPost, "/api/group/member-compact", map[string]any{
		"groupId": created.GroupID, "memberId": created.HostMemberID,
	})
	w = callHandlerWithAuth(ServeGroupMemberCompact, req)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// A group turn in flight -> 409, so the client knows to queue and retry.
	service.SetSessionRunning(created.GroupID, true, true)
	t.Cleanup(func() { service.SetSessionRunning(created.GroupID, false, true) })
	req = newRequest(t, http.MethodPost, "/api/group/member-compact", map[string]any{
		"groupId": created.GroupID, "memberId": created.HostMemberID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMemberCompact, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

// TestServeGroupMembersList_MissingGroupID pins the query-parameter guard.
func TestServeGroupMembersList_MissingGroupID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/group/members", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// TestServeGroupMembersList_OtherProjectIsForbidden pins the ownership check:
// a group id from another project must not leak its roster (agent names, model
// ids, context usage) to a project that merely guesses the id.
func TestServeGroupMembersList_OtherProjectIsForbidden(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId": "codebuddy",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID string `json:"groupId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	other := filepath.Join(filepath.Dir(env.ProjectDir), "other-project")
	require.NoError(t, os.MkdirAll(other, 0o755))

	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+created.GroupID, nil)
	req = withProjectCookie(req, other)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	assert.NotEqual(t, http.StatusOK, w.Code, "another project must not read this roster")
	assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, w.Code, w.Body.String())
}

// ─── PATCH /api/group/members: the error branches ───────────────────────────
//
// Each of these maps one service-level guard onto a status the frontend can act
// on. They are pinned at the handler layer because the mapping (409 + a specific
// message key) is what the UI branches on — the service tests already prove the
// guards fire, but nothing there would catch a handler that answered 500.

// Disabling the last member who can speak must be a 409 with its own key, not
// the host key: the two have different remedies (add a member vs. you cannot
// mute the host at all).
func TestServeGroupMembers_RefuseLastSpeakerDisable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A free group so the host guard cannot mask the last-member guard.
	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "自由群", "", []service.GroupMemberSpec{
		{AgentID: "codebuddy", Backend: "codebuddy", DisplayName: "A"},
		{AgentID: "claude", Backend: "claude", DisplayName: "B"},
	})
	require.NoError(t, err)

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	require.Len(t, members, 2)

	// Mute one, then the other: the second must be refused.
	req := newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": members[0].ID, "disabled": true})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": members[1].ID, "disabled": true})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "CannotDisableLastMember")
}

// Re-enabling past the cap is a 409 GroupMemberLimitReached. The cap counts
// speaking members only, so this needs a full roster with one member parked and
// a newcomer in the freed slot.
func TestServeGroupMembers_ReEnableOverCap(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	agents := map[string]*model.Agent{}
	specs := make([]service.GroupMemberSpec, 0, service.MaxGroupMembers)
	for i := range service.MaxGroupMembers {
		id := "agent-" + string(rune('a'+i))
		agents[id] = &model.Agent{ID: id, Name: id, Backend: "claude"}
		specs = append(specs, service.GroupMemberSpec{AgentID: id, Backend: "claude", DisplayName: id})
	}
	agents["newcomer"] = &model.Agent{ID: "newcomer", Name: "newcomer", Backend: "claude"}
	model.Agents = agents
	t.Cleanup(func() { model.Agents = origAgents })

	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "满员群", "", specs)
	require.NoError(t, err)

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	require.Len(t, members, service.MaxGroupMembers)
	parkedID := members[0].ID

	// Park one member, then fill the freed slot through the handler so the cap
	// is genuinely reached again.
	req := newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": parkedID, "disabled": true})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": groupID, "agentIds": []string{"newcomer"}})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Now un-parking would put one member too many on the mic.
	req = newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": parkedID, "disabled": false})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "GroupMemberLimitReached")
}

// A PATCH without `disabled` is malformed, NOT "disabled: false". An older or
// buggy client that forgets the field must not silently put a parked member back
// on the roster.
func TestServeGroupMembers_PatchWithoutDisabledIsRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Park it first, so a mistaken "enable" would be observable.
	require.NoError(t, service.SetGroupMemberDisabled(groupID, memberID, true))

	req := newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": memberID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	for _, m := range members {
		if m.ID == memberID {
			assert.True(t, m.Disabled, "a rejected PATCH must not enable the member")
		}
	}
}

// DELETE was removed when "离场" became a reversible "禁用": the route must now
// answer 405 rather than falling through to a nil handler.
func TestServeGroupMembers_DeleteIsGone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/group/members", map[string]any{"groupId": groupID, "memberId": "whatever"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code, w.Body.String())
}

// A DB failure while listing must surface as 500, not an empty roster: the UI
// reads an empty list as "this group has no members" and would offer to add
// everyone again.
func TestServeGroupMembersList_DBErrorIs500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	defer swapClosedReadDB(t, store.UnsafeDBForTest())()

	req := newRequest(t, http.MethodGet, "/api/group/members?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// A PATCH whose UPDATE fails must be a 500 rather than a silent success — the
// client would otherwise show the member as muted while it keeps speaking.
func TestServeGroupMembers_DisableDBErrorIs500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "Claude")
	require.NoError(t, err)

	// Reads stay healthy (the guards run first), writes fail.
	defer swapClosedWriteDB(t, store.UnsafeDBForTest())()

	req := newRequest(t, http.MethodPatch, "/api/group/members", map[string]any{"groupId": groupID, "memberId": memberID, "disabled": true})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// An add whose INSERT fails must be a 500, not an empty memberIds list.
func TestServeGroupMembers_AddDBErrorIs500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)

	defer swapClosedWriteDB(t, store.UnsafeDBForTest())()

	req := newRequest(t, http.MethodPost, "/api/group/members", map[string]any{"groupId": groupID, "agentIds": []string{"claude"}})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupMembers, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// ── 更换主持人 (issue #608) ──────────────────────────────────────────────

// The endpoint must actually MOVE the host: the roster's isHost flag is what the
// sheet renders (host pill, "cannot disable" guard, "set as host" action), so
// asserting the DB pointer alone would let a handler that returns 200 while
// doing nothing pass.
func TestServeGroupHost_ChangesHost(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "A")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/group/host", map[string]any{"groupId": groupID, "memberId": memberID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupHost, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, memberID, service.GetGroupHostMember(groupID))

	// The roster is the surface the UI reads: exactly one isHost, on the new host.
	req = newRequest(t, http.MethodGet, "/api/group/members?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupMembers, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Members []struct {
			ID     string `json:"id"`
			IsHost bool   `json:"isHost"`
		} `json:"members"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	hosts := 0
	for _, m := range listed.Members {
		if m.IsHost {
			hosts++
			assert.Equal(t, memberID, m.ID)
		}
	}
	assert.Equal(t, 1, hosts, "exactly one member must be the host")
}

// Every refusal must be a distinct, actionable status: 400 for a malformed or
// foreign target, 409 for a state that cannot host.
func TestServeGroupHost_Refusals(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroup(env.ProjectDir, "讨论组", "codebuddy", "codebuddy", "Host")
	require.NoError(t, err)
	memberID, err := service.AddGroupMember(env.ProjectDir, groupID, "claude", "claude", "A")
	require.NoError(t, err)

	// Missing fields.
	req := newRequest(t, http.MethodPatch, "/api/group/host", map[string]any{"groupId": groupID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupHost, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	// Unknown member id.
	req = newRequest(t, http.MethodPatch, "/api/group/host", map[string]any{"groupId": groupID, "memberId": "nope"})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupHost, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "HostNotMember")

	// Disabled member: enable first.
	require.NoError(t, service.SetGroupMemberDisabled(groupID, memberID, true))
	req = newRequest(t, http.MethodPatch, "/api/group/host", map[string]any{"groupId": groupID, "memberId": memberID})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupHost, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "HostDisabled")

	// Wrong method.
	req = newRequest(t, http.MethodGet, "/api/group/host?groupId="+groupID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeGroupHost, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code, w.Body.String())
}

// A free group has no host, so the endpoint must say so instead of converting
// the group's routing model behind the user's back.
func TestServeGroupHost_RefusesFreeGroup(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "自由群", "", []service.GroupMemberSpec{
		{AgentID: "codebuddy", Backend: "codebuddy"},
		{AgentID: "claude", Backend: "claude"},
	})
	require.NoError(t, err)
	members, err := service.ListGroupMembers(groupID)
	require.NoError(t, err)
	require.Len(t, members, 2)

	req := newRequest(t, http.MethodPatch, "/api/group/host", map[string]any{"groupId": groupID, "memberId": members[0].ID})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupHost, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "GroupNotHostMode")
	assert.Empty(t, service.GetGroupHostMember(groupID))
}
