package handler

import (
	"context"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionSettingsRoute mounts the shared /api/ai/session/settings path.
//
// GET and PATCH are two handlers on one path, so the route needs a dispatcher.
// Registering only the GET handler is a real failure mode, not a hypothetical
// one: it shipped that way, and every PATCH from the settings drawer (a group
// member's model / effort / mode / transport) answered 405 "MethodNotAllowed"
// because the mux never reached ServeSessionSettingsUpdate. The handler tests
// all call the functions directly, so none of them could see it —
// TestSessionSettingsRoute_DispatchesPatch is the route-level guard.
func ServeSessionSettingsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		ServeSessionSettingsUpdate(w, r)
		return
	}
	ServeSessionSettings(w, r)
}

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
		"configOptions":       st.ConfigOptions,
		"modelListState":      st.ModelList,
		"usageState":          st.Usage,
		jsonCommands:          st.Commands,
		"isGroupMember":       service.IsGroupMemberSession(sessionID),
	})
}

// memberConfigWritable reports whether a config write may proceed, and whether
// it must be DEFERRED — stored in the DB but NOT pushed to the member's agent.
//
// A group member's connection is busy during a group turn, and compaction drives
// it directly. Pushing config at it mid-turn would interleave with the running
// prompt (the agent rejects it, or worse, applies it to the turn in flight).
//
// A MODEL switch, however, does not need that push: the next member turn reads
// the persisted model back (service.applySessionOverrides) and applies it before
// its prompt, which is exactly the "remember and defer" path a busy single-chat
// agent already takes (issue #671). So a model-only write is accepted with
// deferred=true; everything else still needs the connection and is refused with
// 409, as before. Compaction always refuses — it is the same connection being
// driven right now, not a state we can just store.
//
// The caller writes the 409 itself (needs the request fields to decide), so this
// helper only answers the question.
func memberConfigWritable(sessionID string) (deferOnly, ok bool) {
	if !service.IsGroupMemberSession(sessionID) {
		return false, true
	}
	if service.IsGroupMemberCompacting(sessionID) {
		return false, false
	}
	if parent := service.ResolveGroupTimelineID(sessionID); parent != sessionID && service.IsSessionRunning(parent) {
		return true, true
	}
	return false, true
}

// sessionSettingsPatch is the PATCH /api/ai/session/settings body.
type sessionSettingsPatch struct {
	SessionID      string `json:"sessionId"`
	ModelID        string `json:"modelId"`
	ThinkingEffort string `json:"thinkingEffort"`
	ModeID         string `json:"modeId"`
	Transport      string `json:"transport"`
	AutoApprove    *bool  `json:"autoApprove"`
	Cruise         *bool  `json:"cruise"`
	// ConfigOptions sets agent-specific select options by the agent's own
	// config-option id (e.g. {"fast-mode": "on"}). Same semantics as
	// PATCH /api/ai/session — see applyExtraConfigOptions.
	ConfigOptions map[string]string `json:"configOptions"`
}

// isModelOnly reports whether the request touches nothing but the model. The
// other fields all need the member's live connection (mode / effort / config
// options are pushed at it, transport decides the next spawn), so they cannot be
// "stored and deferred" while a group turn owns that connection.
func (req sessionSettingsPatch) isModelOnly() bool {
	return req.ModelID != "" &&
		req.ThinkingEffort == "" &&
		req.ModeID == "" &&
		req.Transport == "" &&
		req.AutoApprove == nil &&
		req.Cruise == nil &&
		len(req.ConfigOptions) == 0
}

// checkMemberConfigWrite applies the group-member concurrency guard, writing the
// 409 itself. It reports whether the write may proceed, and whether it must be
// DEFERRED (stored in the DB but not pushed to the member's agent).
func checkMemberConfigWrite(w http.ResponseWriter, r *http.Request, req sessionSettingsPatch) (deferOnly, ok bool) {
	deferOnly, writable := memberConfigWritable(req.SessionID)
	if !writable {
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupMemberCompacting")
		return false, false
	}
	// A group turn owns the member's connection. Only a model switch can be
	// stored without pushing at it; anything else is still refused so the user
	// retries when the group is idle.
	if deferOnly && !req.isModelOnly() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupRunning")
		return false, false
	}
	return deferOnly, true
}

// applySessionModelForSettings switches the model, or — when deferOnly — only
// stores it. deferOnly is the "the member's connection is mid-turn" case: a push
// would either be rejected or rewrite the model of the turn in flight, while the
// next member turn re-reads the column and applies it before its prompt.
func applySessionModelForSettings(ctx context.Context, sessionID, modelID string, deferOnly bool) (deferred bool, err error) {
	if !deferOnly {
		return applyAndPersistSessionModel(ctx, sessionID, modelID)
	}
	//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
	service.UpdateSessionModel(sessionID, modelID)
	return true, nil
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
	var req sessionSettingsPatch
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

	deferOnly, writable := checkMemberConfigWrite(w, r, req)
	if !writable {
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
		ConfigOptions:  req.ConfigOptions,
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
		deferred, err = applySessionModelForSettings(r.Context(), req.SessionID, req.ModelID, deferOnly)
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
	applyExtraConfigOptions(req.SessionID, req.ConfigOptions)
	resp := map[string]any{
		"ok":                  true,
		"modelSwitchDeferred": deferred,
		"effortAccepted":      effortAccepted,
	}
	attachConfigOptionsAfterModelSwitch(resp, req.SessionID, req.ModelID)
	if effectiveEffort != "" {
		resp["thinkingEffort"] = effectiveEffort
	}
	if !effortRemembered {
		resp["thinkingEffortNotRemembered"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}
