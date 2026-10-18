package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"clawbench/internal/ai"
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
	specs, ok := groupMemberSpecs(agentIDs, projectPath)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	groupID, hostMemberID, err := service.CreateGroupWithMembers(projectPath, groupPlaceholderTitle(r, hostAgentID), hostAgentID, specs)
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
// any id is unknown, disabled, or scoped to a different project, in which case
// the whole request must fail (nothing created). A disabled or out-of-project
// agent must not be seeded into a NEW group, just as it is hidden from every
// other new-entry picker.
//
// The display name is NOT set here: the service assigns a unique name per
// instance (the same agent may appear multiple times), deriving the base from
// the agent's name. AgentIDs may contain duplicates.
func groupMemberSpecs(agentIDs []string, projectPath string) ([]service.GroupMemberSpec, bool) {
	specs := make([]service.GroupMemberSpec, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		backend, _, _, _, ok := resolveAgentConfig(agentID)
		if !ok {
			return nil, false
		}
		a := model.GetAgent(agentID)
		if a != nil && a.Disabled {
			return nil, false
		}
		if !agentVisibleInProject(a, projectPath) {
			return nil, false
		}
		specs = append(specs, service.GroupMemberSpec{
			AgentID: agentID,
			Backend: backend,
		})
	}
	return specs, true
}

// groupPlaceholderTitle is the group's initial title: host mode uses the host's
// name; free mode has none, so a generic placeholder (auto-title replaces it
// after the first message).
//
// Localized from the request's locale: the title is stored on the session row
// and shown in the session list, so an English user must not get a Chinese
// placeholder.
func groupPlaceholderTitle(r *http.Request, hostAgentID string) string {
	if hostAgentID != "" {
		return T(r, "GroupHostTitle", map[string]any{"Name": service.GetAgentDisplayName(hostAgentID)})
	}
	return T(r, "GroupFreeTitle")
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
	// Context state (how full each member's OWN window is + its model) is a
	// second, single query. A failure here degrades to "no usage" instead of
	// failing the roster: identity is what the sheet needs to be usable.
	ctxs, ctxErr := service.GroupMemberContexts(groupID)
	if ctxErr != nil {
		slog.Warn("group members: load member contexts failed", "group", groupID, "err", ctxErr)
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		agentName := ""
		if a := model.GetAgent(m.AgentID); a != nil {
			agentName = a.Name
		}
		c := ctxs[m.ID]
		// A live ACP connection's cached usage is fresher than the persisted
		// copy (the DB is written once per 500ms flush window). The connection
		// is keyed by the member row id, because a member turn's RunConfig
		// SessionID IS that row. After the idle sweep reclaims it the cache is
		// gone and the DB value (still authoritative) is used.
		if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(m.ID); s.Usage != nil {
			c.Usage = s.Usage
		}
		out = append(out, map[string]any{
			"id":        m.ID,
			jsonAgentID: m.AgentID,
			jsonName:    m.Name,
			jsonBackend: m.Backend,
			"avatar":    m.Avatar,
			"agentName": agentName,
			"left":      m.Left,
			"isHost":    m.ID == hostMemberID,
			jsonModel:   c.Model,
			// usage is null (not {}) when the member never reported any: the
			// client distinguishes "no data" from "0% used" on exactly that.
			jsonUsage: c.Usage,
		})
	}
	// mode tells the frontend whether a host exists (host mode) or the group is
	// free (design §13). parallelDefault is the free-mode action-bar switch's
	// server value; cruise is the auto-cruise switch (design §15). The
	// member-speech cap is a GLOBAL setting (chat.group_max_speeches), not a
	// per-group one, so it is not returned here — the group settings sheet no
	// longer shows it.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"members":         out,
		respKeyMode:       service.GetGroupMode(groupID),
		"parallelDefault": service.GetGroupParallelDefault(groupID),
		"cruise":          service.GetSessionCruise(groupID),
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
		// Server-side fallback for the disabled-agent picker filter: a disabled
		// agent must not be added as a NEW group member. Skipped (like unknown
		// ids) rather than failing the whole batch.
		a := model.GetAgent(agentID)
		if a != nil && a.Disabled {
			continue
		}
		// Same fallback for project scope: an agent scoped to another project is
		// not visible here and must not be added.
		if !agentVisibleInProject(a, projectPath) {
			continue
		}
		specs = append(specs, service.GroupMemberSpec{
			AgentID: agentID,
			Backend: backend,
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

// ServeGroupMemberCompact asks ONE group member's agent to compact its own
// context.
//
//	POST /api/group/member-compact {groupId, memberId} -> {ok, usage?}
//
// Deliberately separate from /api/ai/chat: a group turn writes to the GROUP
// timeline, so routing "/compact" through it would leave the command text and
// an assistant reply in the discussion every member then reads. This endpoint
// drives the member's agent directly and produces no timeline output at all.
//
// Errors: 409 GroupRunning / MemberCompacting, 404 MemberNotFound, 400 for a
// member that does not use ACP (no slash-command channel), 500 otherwise.
func ServeGroupMemberCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
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
	// Detach from the request context's cancellation but keep its values: a
	// client that navigates away mid-compaction must not abort the agent's
	// compaction half-way (the agent would keep the rewritten context and we
	// would never persist the resulting usage).
	usage, err := service.CompactGroupMember(context.WithoutCancel(r.Context()), req.GroupID, req.MemberID)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrGroupRunning):
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupRunning")
		return
	case errors.Is(err, service.ErrMemberCompacting):
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberCompacting")
		return
	case errors.Is(err, service.ErrGroupMemberNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "GroupMemberNotFound")
		return
	case errors.Is(err, service.ErrCompactNeedsACP):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "CompactNeedsACP")
		return
	default:
		slog.Warn("group member compact failed", "group", req.GroupID, "member", req.MemberID, "err", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "usage": usage})
}

// ServeGroupSettings reads/updates group settings (parallelDefault, cruise).
//
//	PATCH /api/group/settings {groupId, parallelDefault?, cruise?} -> {ok}
//
// Both fields are optional pointers: at least one must be present, and only the
// ones provided are written, so a caller toggling one switch does not clobber
// the other. parallelDefault is the free-mode user-seed concurrency switch;
// cruise is the auto-cruise switch (design §15). The member-speech cap moved to
// the global config (chat.group_max_speeches), so it is no longer accepted here.
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
		Cruise          *bool  `json:"cruise"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || (req.ParallelDefault == nil && req.Cruise == nil) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
		return
	}
	if req.ParallelDefault != nil {
		if err := service.SetGroupParallelDefault(req.GroupID, *req.ParallelDefault); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
	}
	if req.Cruise != nil {
		if err := service.SetSessionCruise(req.GroupID, *req.Cruise); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		// Hard default (one-directional): enabling auto-cruise FORCES auto-approve
		// on for the whole group — cruise runs unattended, so a pending permission
		// prompt would stall the loop with no human watching. applyAutoApprove
		// fans the flag out to every member row AND the live ACP connections.
		// Disabling cruise does NOT touch auto-approve.
		if *req.Cruise {
			applyAutoApprove(req.GroupID, true)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// maxGroupMemberAvatarsBody caps the raw request body of
// ServeGroupMemberAvatars: every member slot at the maximum avatar size, plus
// 1 MiB of slack for JSON syntax and the memberId/groupId keys.
const maxGroupMemberAvatarsBody = service.MaxGroupMembers*maxAgentAvatarBytes + 1<<20

// ServeGroupMemberAvatars persists per-member avatar SVGs generated client-side
// (DiceBear). Batch so a freshly created/expanded group writes all its new
// members in ONE round trip.
//
//	POST /api/group/member-avatars {groupId, avatars:[{memberId, avatar}]} -> {ok, saved:int}
//
// Each avatar must pass the same SVG safety gate (and size cap) as agent
// avatars: this is a stored-XSS sink reachable by any authenticated caller, and
// the stored string is later replayed into the public share page. The batch is
// validated in FULL before any write, so an invalid item never leaves the group
// half-updated.
//
// The batch is BOUNDED on both axes. The entry count is capped at the group
// member cap (a group can never have more members, so a larger batch is
// meaningless), and the raw body is capped at the same per-avatar size times
// that count (plus slack), so an unauthenticated-sized allocation from a valid
// caller is impossible.
func ServeGroupMemberAvatars(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	// Bound the body BEFORE decodeJSON reads it: decoding builds the whole
	// []avatar first, so without this a single request could make the server
	// allocate hundreds of MiB. The 1 MiB slack absorbs JSON punctuation and key
	// names, which are not counted by the avatar cap.
	r.Body = http.MaxBytesReader(w, r.Body, maxGroupMemberAvatarsBody)
	var req struct {
		GroupID string `json:"groupId"`
		Avatars []struct {
			MemberID string `json:"memberId"`
			Avatar   string `json:"avatar"`
		} `json:"avatars"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GroupID == "" || len(req.Avatars) == 0 || len(req.Avatars) > service.MaxGroupMembers {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	if !requireSessionOwnership(w, r, req.GroupID, projectPath) {
		return
	}
	// Pass 1: validate EVERY item before writing anything. A per-item
	// validate-and-write loop would persist the valid items before returning
	// 400 on a later invalid one, leaving the batch half-applied.
	for _, a := range req.Avatars {
		if a.MemberID == "" || len(a.Avatar) > maxAgentAvatarBytes || !avatarSVGLooksSafe([]byte(a.Avatar)) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
	}
	// Pass 2: write. saved counts rows actually affected — a member id that
	// belongs to another group matches no row and contributes 0 rather than
	// inflating the total.
	saved := int64(0)
	for _, a := range req.Avatars {
		n, err := service.SetMemberAvatar(req.GroupID, a.MemberID, a.Avatar)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		saved += n
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}
