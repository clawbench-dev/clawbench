package handler

import (
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// group.go holds the AI group-chat HTTP endpoints (design §8). All routes are
// project-scoped and authenticated via middleware.Auth, like the rest of /api/.

// ServeGroupCreate creates a group plus its host member.
//
//	POST /api/group/create  {title, hostAgentId} -> {ok, groupId, hostMemberId}
func ServeGroupCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	// Groups are user-visible sessions: they count toward the session limit.
	if model.SessionMaxCount > 0 {
		if count, cerr := service.GetSessionCount(projectPath); cerr == nil && count >= model.SessionMaxCount {
			writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
			return
		}
	}

	var req struct {
		Title       string `json:"title"`
		HostAgentID string `json:"hostAgentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	agentID := req.HostAgentID
	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}
	backend, _, _, _, ok := resolveAgentConfig(agentID)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
		return
	}
	hostName := service.GetAgentDisplayName(agentID)
	title := req.Title
	if title == "" {
		title = hostName + " 的群聊"
	}
	groupID, hostMemberID, err := service.CreateGroup(projectPath, title, backend, agentID, hostName)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "CreateSessionFailed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"groupId":      groupID,
		"hostMemberId": hostMemberID,
	})
}

// ServeGroupMembers lists, adds, or removes group members.
//
//	GET    /api/group/members?groupId=            -> {ok, members:[...]}
//	POST   /api/group/members  {groupId, agentIds:[]} -> {ok, memberIds:[]}
//	DELETE /api/group/members  {groupId, memberId}     -> {ok}
func ServeGroupMembers(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		groupID := r.URL.Query().Get("groupId")
		if groupID == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		if !requireSessionOwnership(w, r, groupID, projectPath) {
			return
		}
		members, err := service.ListGroupMembers(groupID)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		hostMemberID := service.GetGroupHostMember(groupID)
		out := make([]map[string]any, 0, len(members))
		for _, m := range members {
			out = append(out, map[string]any{
				"id":      m.ID,
				"agentId": m.AgentID,
				"name":    m.Name,
				"backend": m.Backend,
				"left":    m.Left,
				"isHost":  m.ID == hostMemberID,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "members": out})

	case http.MethodPost:
		var req struct {
			GroupID  string   `json:"groupId"`
			AgentIDs []string `json:"agentIds"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.GroupID == "" || len(req.AgentIDs) == 0 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
			return
		}
		memberIDs := make([]string, 0, len(req.AgentIDs))
		for _, agentID := range req.AgentIDs {
			backend, _, _, _, ok := resolveAgentConfig(agentID)
			if !ok {
				continue
			}
			id, err := service.AddGroupMember(projectPath, req.GroupID, backend, agentID, service.GetAgentDisplayName(agentID))
			if err != nil {
				writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
				return
			}
			memberIDs = append(memberIDs, id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "memberIds": memberIDs})

	case http.MethodDelete:
		var req struct {
			GroupID  string `json:"groupId"`
			MemberID string `json:"memberId"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.GroupID == "" || req.MemberID == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
			return
		}
		if err := service.RemoveGroupMember(req.GroupID, req.MemberID); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// ServeGroupSettings reads/updates group settings (currently maxRounds).
//
//	PATCH /api/group/settings {groupId, maxRounds} -> {ok}
func ServeGroupSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req struct {
		GroupID   string `json:"groupId"`
		MaxRounds int    `json:"maxRounds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.MaxRounds <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
		return
	}
	if err := service.SetGroupMaxRounds(req.GroupID, req.MaxRounds); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
