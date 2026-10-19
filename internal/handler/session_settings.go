package handler

import (
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionSettings returns one session's settings state in the exact shape
// the settings drawer (SessionDrawer) needs: model, thinking effort, mode,
// transport, auto-approve, auto-cruise, plus the context-usage snapshot.
//
//	GET /api/ai/session/settings?session_id=<id>
//
// Why a dedicated endpoint instead of reusing GET /api/ai/chat: that endpoint
// returns message history (and pages it), which the drawer does not need. More
// importantly, a GROUP MEMBER row has no chat_history of its own — all member
// speech lands on the group timeline — so the history endpoint cannot describe
// it. The drawer must be able to open on a member row (clicking a member in the
// group roster), and a member row IS a session, so this endpoint addresses
// sessions uniformly: chat, group, or group_member.
//
// The state assembly is sessionStateSnapshot, the SAME function the history
// endpoint uses, so a member row resolves mode/effort/usage identically to a
// single chat (two copies would drift).
func ServeSessionSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	if !requireSessionOwnership(w, r, sessionID, projectPath) {
		return
	}

	// GetSessionProjectPathAny (not GetSessionFullInfo) so an archived session
	// still resolves: the drawer is read-only, and refusing to describe an
	// archived session would blank every chip the moment it is archived.
	info := service.GetSessionFullInfo(sessionID)
	if info == nil {
		// Archived: fall back to the any-path lookup for existence, then answer
		// with what the DB still holds. The drawer shows the values; switching
		// is rejected by the write endpoints on their own.
		if _, found := service.GetSessionProjectPathAny(sessionID); !found {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
			return
		}
	}

	sessionType := service.GetSessionType(sessionID)
	agentID := ""
	backendName := ""
	modelID := ""
	transport := ""
	autoApprove := false
	if info != nil {
		agentID = info.AgentID
		backendName = info.Backend
		modelID = info.Model
		transport = info.Transport
		autoApprove = info.AutoApprove
	}
	if agentID == "" {
		agentID = service.GetSessionAgentID(sessionID)
	}
	// Resolve the EFFECTIVE transport rather than echoing the raw column.
	//
	// A group member row has transport='' by design (it inherits whatever its
	// agent uses, exactly as a turn does through resolveIsACP). Echoing the empty
	// column made the drawer's protocol tab render no selection at all.
	//
	// An explicit per-session override wins; otherwise take the agent's own
	// transport, then "has an ACP command" (the same fallback resolveIsACP uses).
	// If neither is known the value stays EMPTY — deliberately NOT "cli": a row
	// with no transport is not a CLI row, and claiming so would hide the ACP-only
	// tabs. The client already treats empty as "ask the agent config".
	if transport == "" {
		if agent := model.GetAgent(agentID); agent != nil {
			transport = agent.Transport
			if transport == "" && agent.AcpCommand != "" {
				transport = transportACP
			}
		}
	}

	st := sessionStateSnapshot(sessionID, agentID, transport)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		strSessionID:          sessionID,
		"sessionType":         sessionType,
		"agentId":             agentID,
		"backend":             backendName,
		"modelId":             modelID,
		selfTestKeyTransport:  transport,
		jsonAutoApprove:       autoApprove,
		jsonCruise:            service.GetSessionCruise(sessionID),
		"modeState":           st.Mode,
		"thinkingEffortState": st.Effort,
		"modelListState":      st.ModelList,
		"usageState":          st.Usage,
		"commands":            st.Commands,
		"isGroupMember":       service.IsGroupMemberSession(sessionID),
	})
}

// requireMemberConfigWritable refuses a config write while the member's
// connection is busy, writing the response itself.
//
// A group member's connection is busy during a group turn, and compaction drives
// it directly. Writing config mid-turn would interleave with the running prompt
// (the agent rejects it, or worse, applies it to the turn in flight). Refuse
// instead of silently deferring: the drawer is interactive, so the user can
// retry when the group is idle. Non-member sessions are always writable here.
func requireMemberConfigWritable(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	if !service.IsGroupMemberSession(sessionID) {
		return true
	}
	if parent := service.ResolveGroupTimelineID(sessionID); parent != sessionID && service.IsSessionRunning(parent) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupRunning")
		return false
	}
	if service.IsGroupMemberCompacting(sessionID) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberCompacting")
		return false
	}
	return true
}

// ServeSessionSettingsUpdate applies one setting to an arbitrary session,
// including a group MEMBER row.
//
//	PATCH /api/ai/session/settings {sessionId, modelId?, thinkingEffort?, modeId?,
//	                                transport?, autoApprove?, cruise?}
//
// This is deliberately NOT /api/ai/session/update: that endpoint resolves its
// target through the session cookie/query, and its write path assumes a
// user-visible session. A member row is hidden from the session list but is
// still a real session with its own agent binding — exactly what the drawer
// edits when the user clicks a member. Reusing the update endpoint would have
// worked only by accident (it does accept a body sessionId); keeping a separate
// route makes the "member rows are editable targets" contract explicit and
// gives this path its own guards.
func ServeSessionSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req struct {
		SessionID      string `json:"sessionId"`
		ModelID        string `json:"modelId"`
		ThinkingEffort string `json:"thinkingEffort"`
		ModeID         string `json:"modeId"`
		Transport      string `json:"transport"`
		AutoApprove    *bool  `json:"autoApprove"`
		Cruise         *bool  `json:"cruise"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	if !requireSessionOwnership(w, r, req.SessionID, projectPath) {
		return
	}

	if !requireMemberConfigWritable(w, r, req.SessionID) {
		return
	}

	up := sessionUpdateRequest{
		SessionID:      req.SessionID,
		ModeID:         req.ModeID,
		ThinkingEffort: req.ThinkingEffort,
		ModelID:        req.ModelID,
		Transport:      req.Transport,
		AutoApprove:    req.AutoApprove,
		Cruise:         req.Cruise,
	}
	// Reuse the canonical write path: it already handles the model-last
	// ordering, per-model effort memory, the cruise→auto-approve hard default,
	// and the "deferred vs rejected" distinction. Duplicating it here is how the
	// two paths would drift.
	if err := up.applyNonModelFields(req.SessionID); err != nil {
		writeLocalizedError(w, r, model.Internal(err))
		return
	}
	// Model first, then the effort pick that belongs to it: the registry's level
	// list is per model, so validating a pick before the switch compares it
	// against the previous model's list (see ServeAISessionUpdate).
	deferred := false
	effectiveEffort := ""
	if req.ModelID != "" {
		var err error
		deferred, err = applyAndPersistSessionModel(r.Context(), req.SessionID, req.ModelID)
		if err != nil {
			writeModelSwitchError(w, r, err)
			return
		}
	}
	effortAccepted, effortRemembered := applySessionConfigSelections(
		req.SessionID, req.ModeID, req.ThinkingEffort, req.ModelID, !deferred,
	)
	if req.ModelID != "" && req.ThinkingEffort == "" {
		effectiveEffort = switchSessionEffortToModel(req.SessionID, req.ModelID, deferred)
	}
	resp := map[string]any{
		"ok":                  true,
		"modelSwitchDeferred": deferred,
		"effortAccepted":      effortAccepted,
	}
	if effectiveEffort != "" {
		resp["thinkingEffort"] = effectiveEffort
	}
	if !effortRemembered {
		resp["thinkingEffortNotRemembered"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}
