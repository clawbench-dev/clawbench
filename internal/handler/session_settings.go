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

	// A group member's connection is busy during a group turn, and compaction
	// drives it directly. Writing config mid-turn would interleave with the
	// running prompt (the agent rejects it, or worse, applies it to the turn in
	// flight). Refuse instead of silently deferring: the drawer is interactive,
	// so the user can retry when the group is idle.
	if service.IsGroupMemberSession(req.SessionID) {
		if parent := service.ResolveGroupTimelineID(req.SessionID); parent != req.SessionID && service.IsSessionRunning(parent) {
			writeLocalizedErrorf(w, r, http.StatusConflict, "GroupRunning")
			return
		}
		if service.IsGroupMemberCompacting(req.SessionID) {
			writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberCompacting")
			return
		}
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
	effortModelKey := ""
	if req.ModelID != "" {
		effortModelKey = req.ModelID
	}
	effortAccepted := applySessionConfigSelections(req.SessionID, req.ModeID, req.ThinkingEffort, effortModelKey)
	if err := up.applyNonModelFields(req.SessionID); err != nil {
		writeLocalizedError(w, r, model.Internal(err))
		return
	}
	deferred := false
	effectiveEffort := ""
	if req.ModelID != "" {
		var err error
		deferred, effectiveEffort, err = applySessionModelAndEffort(r.Context(), req.SessionID, req.ModelID, req.ThinkingEffort)
		if err != nil {
			writeModelSwitchError(w, r, err)
			return
		}
	}
	resp := map[string]any{
		"ok":                  true,
		"modelSwitchDeferred": deferred,
		"effortAccepted":      effortAccepted,
	}
	if effectiveEffort != "" {
		resp["thinkingEffort"] = effectiveEffort
	}
	writeJSON(w, http.StatusOK, resp)
}
