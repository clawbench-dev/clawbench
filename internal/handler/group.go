package handler

import (
	"errors"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// group.go holds the AI group-chat HTTP endpoints (design §8). All routes are
// project-scoped and authenticated via middleware.Auth, like the rest of /api/.

// ServeGroupCreate creates a group plus ALL its members in one call (design
// §7.1, decision #25). The frontend picks members and host together, so the
// host and members arrive in a single request and are created atomically.
//
//	POST /api/group/create  {hostAgentId, memberAgentIds:[]} -> {ok, groupId, hostMemberId}
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
		HostAgentID    string   `json:"hostAgentId"`
		MemberAgentIDs []string `json:"memberAgentIds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	hostAgentID := req.HostAgentID
	if hostAgentID == "" {
		hostAgentID = model.GetDefaultAgentID()
	}
	// The host must be a member: if the caller omitted the member list (older
	// client), fall back to a host-only group. Otherwise require the host to be
	// present in the list — otherwise no member row would match it.
	agentIDs := req.MemberAgentIDs
	if len(agentIDs) == 0 {
		agentIDs = []string{hostAgentID}
	} else if !containsString(agentIDs, hostAgentID) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	// Resolve every agent BEFORE creating anything: an unknown agent id must
	// fail the whole request (the service also rolls back, but resolving first
	// avoids opening a transaction for a request that cannot succeed).
	specs := make([]service.GroupMemberSpec, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		backend, _, _, _, ok := resolveAgentConfig(agentID)
		if !ok {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		specs = append(specs, service.GroupMemberSpec{
			AgentID:     agentID,
			Backend:     backend,
			DisplayName: service.GetAgentDisplayName(agentID),
		})
	}

	hostName := service.GetAgentDisplayName(hostAgentID)
	title := hostName + " 的群聊"
	groupID, hostMemberID, err := service.CreateGroupWithMembers(projectPath, title, hostAgentID, specs)
	if err != nil {
		if errors.Is(err, service.ErrGroupMemberLimit) {
			writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberLimitReached", map[string]any{"MaxCount": service.MaxGroupMembers})
			return
		}
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
		// maxRounds is included so the member sheet can show the SERVER's
		// current value instead of a hardcoded default (the PATCH endpoint had
		// no read-back, so the UI lied after a change).
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "members": out, "maxRounds": service.GetGroupMaxRounds(groupID)})

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
		// Resolve every agent first (skip unknown ids), then add them in ONE
		// service call so the member cap is enforced on the WHOLE batch
		// (decision #78). Looping AddGroupMember here would commit the earlier
		// additions and then fail on a later one — the request errors but the
		// roster silently changed.
		specs := make([]service.GroupMemberSpec, 0, len(req.AgentIDs))
		for _, agentID := range req.AgentIDs {
			backend, _, _, _, ok := resolveAgentConfig(agentID)
			if !ok {
				continue
			}
			specs = append(specs, service.GroupMemberSpec{
				AgentID:     agentID,
				Backend:     backend,
				DisplayName: service.GetAgentDisplayName(agentID),
			})
		}
		memberIDs, err := service.AddGroupMembers(projectPath, req.GroupID, specs)
		if err != nil {
			if errors.Is(err, service.ErrGroupMemberLimit) {
				writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberLimitReached", map[string]any{"MaxCount": service.MaxGroupMembers})
				return
			}
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
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
			if errors.Is(err, service.ErrCannotRemoveHost) {
				writeLocalizedErrorf(w, r, http.StatusConflict, "CannotRemoveHost")
				return
			}
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
