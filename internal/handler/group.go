package handler

import (
	"errors"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// group.go holds the AI group-chat HTTP endpoints (design §8). All routes are
// project-scoped and authenticated via middleware.Auth, like the rest of /api/.

// respKeyMode is the JSON response field carrying the group's mode
// ("host" | "free") on both the create and members endpoints.
const respKeyMode = "mode"

// ServeGroupCreate creates a group plus ALL its members in one call (design
// §7.1, decision #25). The frontend picks members and (optionally) a host
// together, so they arrive in a single request and are created atomically.
//
// The host is OPTIONAL (design §13.1): when hostAgentId is omitted/empty the
// group is created in FREE mode (no host, participants relay via @-mentions).
// When present, the group is HOST mode and the host must be one of the members.
//
//	POST /api/group/create  {hostAgentId?, memberAgentIds:[]} -> {ok, groupId, hostMemberId, mode}
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
	if !groupSessionLimitOK(w, r, projectPath) {
		return
	}

	var req struct {
		HostAgentID    string   `json:"hostAgentId"`
		MemberAgentIDs []string `json:"memberAgentIds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	hostAgentID := req.HostAgentID
	agentIDs := req.MemberAgentIDs
	if hostAgentID != "" {
		// Host mode: the host must be a member. If the caller omitted the
		// member list (older client), fall back to a host-only group; otherwise
		// require the host to be present in the list — otherwise no member row
		// would match it.
		if len(agentIDs) == 0 {
			agentIDs = []string{hostAgentID}
		} else if !containsString(agentIDs, hostAgentID) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
	}
	if len(agentIDs) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	// Resolve every agent BEFORE creating anything: an unknown agent id must
	// fail the whole request (the service also rolls back, but resolving first
	// avoids opening a transaction for a request that cannot succeed).
	specs, ok := groupMemberSpecs(agentIDs)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	groupID, hostMemberID, err := service.CreateGroupWithMembers(projectPath, groupPlaceholderTitle(hostAgentID), hostAgentID, specs)
	if err != nil {
		writeGroupCreateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"groupId":      groupID,
		"hostMemberId": hostMemberID,
		respKeyMode:    service.GetGroupMode(groupID),
	})
}

// groupSessionLimitOK reports whether the project may open another group (groups
// count toward the session limit). It writes the 409 and returns false when the
// cap is reached, so the caller can return immediately.
func groupSessionLimitOK(w http.ResponseWriter, r *http.Request, projectPath string) bool {
	if model.SessionMaxCount <= 0 {
		return true
	}
	count, err := service.GetSessionCount(projectPath)
	if err == nil && count >= model.SessionMaxCount {
		writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{jsonMaxCount: model.SessionMaxCount})
		return false
	}
	return true
}

// groupMemberSpecs resolves each agent id to a GroupMemberSpec. ok is false when
// any id is unknown, in which case the whole request must fail (nothing created).
func groupMemberSpecs(agentIDs []string) ([]service.GroupMemberSpec, bool) {
	specs := make([]service.GroupMemberSpec, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		backend, _, _, _, ok := resolveAgentConfig(agentID)
		if !ok {
			return nil, false
		}
		specs = append(specs, service.GroupMemberSpec{
			AgentID:     agentID,
			Backend:     backend,
			DisplayName: service.GetAgentDisplayName(agentID),
		})
	}
	return specs, true
}

// groupPlaceholderTitle is the group's initial title: host mode uses the host's
// name; free mode has none, so a generic placeholder (auto-title replaces it
// after the first message).
func groupPlaceholderTitle(hostAgentID string) string {
	if hostAgentID != "" {
		return service.GetAgentDisplayName(hostAgentID) + " 的群聊"
	}
	return "群聊"
}

// writeGroupCreateError maps a CreateGroupWithMembers error to the right status.
func writeGroupCreateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrGroupMemberLimit):
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberLimitReached", map[string]any{jsonMaxCount: service.MaxGroupMembers})
	case errors.Is(err, service.ErrFreeGroupNeedsTwoMembers):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
	default:
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "CreateSessionFailed")
	}
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
		serveGroupMembersList(w, r, projectPath)
	case http.MethodPost:
		serveGroupMembersAdd(w, r, projectPath)
	case http.MethodDelete:
		serveGroupMembersRemove(w, r, projectPath)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// serveGroupMembersList handles GET /api/group/members?groupId=.
func serveGroupMembersList(w http.ResponseWriter, r *http.Request, projectPath string) {
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
			"id":        m.ID,
			jsonAgentID: m.AgentID,
			jsonName:    m.Name,
			jsonBackend: m.Backend,
			"left":      m.Left,
			"isHost":    m.ID == hostMemberID,
		})
	}
	// mode tells the frontend whether a host exists (host mode) or the group is
	// free (design §13). parallelDefault is the free-mode action-bar switch's
	// server value. The member-speech cap is a GLOBAL setting
	// (chat.group_max_speeches), not a per-group one, so it is not returned
	// here — the group settings sheet no longer shows it.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"members":         out,
		respKeyMode:       service.GetGroupMode(groupID),
		"parallelDefault": service.GetGroupParallelDefault(groupID),
	})
}

// serveGroupMembersAdd handles POST /api/group/members {groupId, agentIds}.
func serveGroupMembersAdd(w http.ResponseWriter, r *http.Request, projectPath string) {
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
			writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberLimitReached", map[string]any{jsonMaxCount: service.MaxGroupMembers})
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "memberIds": memberIDs})
}

// serveGroupMembersRemove handles DELETE /api/group/members {groupId, memberId}.
func serveGroupMembersRemove(w http.ResponseWriter, r *http.Request, projectPath string) {
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
}

// ServeGroupSettings reads/updates group settings (parallelDefault).
//
//	PATCH /api/group/settings {groupId, parallelDefault} -> {ok}
//
// parallelDefault is the only remaining per-group setting. The member-speech
// cap moved to the global config (chat.group_max_speeches), so it is no longer
// accepted here.
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
		GroupID         string `json:"groupId"`
		ParallelDefault *bool  `json:"parallelDefault"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || req.ParallelDefault == nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
		return
	}
	if err := service.SetGroupParallelDefault(req.GroupID, *req.ParallelDefault); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
