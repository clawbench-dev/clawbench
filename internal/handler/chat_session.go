//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionsOverview handles GET /api/ai/sessions/overview.
// Returns sessions across ALL projects that are running, pending approval, or
// have unread messages, grouped by project name. Requires auth (session cookie);
// no project cookie needed since it spans projects.
func ServeSessionsOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	sessions, err := service.GetOverviewSessions()
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to load overview sessions")))
		return
	}
	runningIDs := service.GetRunningSessionIDs()
	runningSet := make(map[string]bool, len(runningIDs))
	for _, id := range runningIDs {
		runningSet[id] = true
	}
	pendingSet := ai.GetACPConnManager().GetPendingApprovalSessionIDs()

	type overviewSession struct {
		ID              string                     `json:"id"`
		Title           string                     `json:"title"`
		Backend         string                     `json:"backend"`
		AgentID         string                     `json:"agentId"`
		Model           string                     `json:"model"`
		SessionType     string                     `json:"sessionType,omitempty"`
		Running         bool                       `json:"running"`
		PendingApproval bool                       `json:"pendingApproval"`
		UnreadCount     int                        `json:"unreadCount"`
		UpdatedAt       time.Time                  `json:"updatedAt"`
		GroupMembers    []model.GroupMemberPreview `json:"groupMembers,omitempty"`
	}
	type projectGroup struct {
		Name     string            `json:"name"`
		Sessions []overviewSession `json:"sessions"`
	}
	groups := []*projectGroup{}
	groupByName := map[string]*projectGroup{}
	groupIDs := []string{}
	total := 0
	for _, s := range sessions {
		running := runningSet[s.ID]
		pending := pendingSet[s.ID]
		if !running && !pending && s.UnreadCount <= 0 {
			continue
		}
		g, ok := groupByName[s.ProjectPath]
		if !ok {
			g = &projectGroup{Name: s.ProjectPath}
			groupByName[s.ProjectPath] = g
			groups = append(groups, g)
		}
		if service.IsGroupSessionType(s.SessionType) {
			groupIDs = append(groupIDs, s.ID)
		}
		g.Sessions = append(g.Sessions, overviewSession{
			ID:              s.ID,
			Title:           s.Title,
			Backend:         s.Backend,
			AgentID:         s.AgentID,
			Model:           s.Model,
			SessionType:     s.SessionType,
			Running:         running,
			PendingApproval: pending,
			UnreadCount:     s.UnreadCount,
			UpdatedAt:       s.UpdatedAt,
		})
		total++
	}
	// Group rows carry the same active-member preview as the main list. One
	// batch query for all groups on this page (attachGroupMembers works on
	// model.ChatSession, so this path inlines the lookup).
	if membersByGroup, err := service.GroupMembersForGroups(groupIDs); err != nil {
		slog.Warn("failed to load overview group members", "error", err)
	} else {
		for gi := range groups {
			for si := range groups[gi].Sessions {
				if members := membersByGroup[groups[gi].Sessions[si].ID]; len(members) > 0 {
					groups[gi].Sessions[si].GroupMembers = members
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": groups, "total": total})
}

// ServeSessions handles GET (list) and POST (create) for chat sessions.
func ServeSessions(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // multi-method session handler
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Parse optional pagination parameters
		limit := 0
		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				limit = v
			}
		}
		cursor := r.URL.Query().Get("cursor")
		cursorID := r.URL.Query().Get("cursor_id")
		// Optional tag filter: restricts the page to sessions carrying this tag
		// (visible in this project). Applied server-side so pagination and
		// hasMore stay correct — filtering only the loaded page client-side
		// would show "no matches" until the user scrolled far enough.
		tagName := strings.TrimSpace(r.URL.Query().Get("tag"))
		// cursor_sort_order + cursor_pinned complete the keyset: the list is
		// ordered by (pinned DESC, sort_order ASC, created_at DESC, id DESC), so
		// paging on created_at alone re-returns rows sharing the cursor's
		// (pinned, sort_order) on every page. Absent/empty keeps the legacy
		// created_at-only predicate.
		var cursorSortOrder *int
		if p := r.URL.Query().Get("cursor_sort_order"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				cursorSortOrder = &v
			}
		}
		var cursorPinned *bool
		if p := r.URL.Query().Get("cursor_pinned"); p != "" {
			v := p == "1" || strings.EqualFold(p, "true")
			cursorPinned = &v
		}
		// Normalize cursor timestamp: frontend sends ISO 8601 (2026-05-16T15:25:50Z)
		// but SQLite stores as "2026-05-16 15:25:50". Convert T→space and strip Z/+00:00.
		if cursor != "" {
			cursor = strings.ReplaceAll(cursor, "T", " ")
			cursor = strings.TrimSuffix(cursor, "Z")
			cursor = strings.TrimSuffix(cursor, "+00:00")
		}

		var sessions []model.ChatSession
		var hasMore bool
		var err error

		if limit > 0 {
			sessions, hasMore, err = service.GetSessionsPaged(projectPath, "", limit, cursor, cursorID, cursorSortOrder, cursorPinned, tagName)
		} else {
			sessions, err = service.GetSessions(projectPath, "")
			hasMore = false
			if err == nil && tagName != "" {
				sessions, err = service.FilterSessionsByTag(sessions, projectPath, tagName)
			}
		}
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load sessions")))
			return
		}
		// Batch-check running state: single mutex acquisition instead of N
		runningIDs := service.GetRunningSessionIDs()
		runningSet := make(map[string]bool, len(runningIDs))
		for _, id := range runningIDs {
			runningSet[id] = true
		}
		// Batch-check pending approval state from ACP connection pool
		pendingApprovalSet := ai.GetACPConnManager().GetPendingApprovalSessionIDs()
		for i := range sessions {
			sessions[i].Running = runningSet[sessions[i].ID]
			sessions[i].PendingApproval = pendingApprovalSet[sessions[i].ID]
		}
		attachSessionTags(sessions)
		attachGroupMembers(sessions)
		totalCount, _ := service.GetSessionCount(projectPath)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"sessions":   sessions,
			"hasMore":    hasMore,
			"totalCount": totalCount,
		})

	case http.MethodPost:
		// Check session count limit before creating (0 = unlimited)
		if model.SessionMaxCount > 0 {
			if count, cerr := service.GetSessionCount(projectPath); cerr == nil && count >= model.SessionMaxCount {
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{jsonMaxCount: model.SessionMaxCount})
				return
			}
		}

		var req struct {
			Title   string `json:"title"`
			Backend string `json:"backend"`
			AgentID string `json:"agentId"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
		if !decodeJSON(w, r, &req) {
			return
		}
		backend := req.Backend
		agentID := req.AgentID
		resolvedAgentID := agentID
		agentSource := "default"
		// Server-side fallback for the disabled-agent picker filter: an old
		// client (or a race) must not open a NEW session on an agent the user
		// disabled. Only an EXPLICIT agent choice is checked — the default
		// fallback can never be disabled (the PATCH guard refuses it).
		if agentID != "" {
			a := model.GetAgent(agentID)
			if a != nil && a.Disabled {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "AgentDisabled")
				return
			}
			// Server-side fallback for the project-scope picker filter: a
			// project-only agent must not be used to open a NEW session from a
			// different project. Same reasoning as the disabled check above.
			if a != nil && !agentVisibleInProject(a, projectPath) {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "AgentNotInProject")
				return
			}
		}
		backend2, _, _, _, ok := resolveAgentConfig(agentID)
		if !ok {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
			return
		}
		if backend2 != "" {
			backend = backend2
		}
		// Don't pre-fill agent default model into session — leave model empty so
		// the frontend falls back to the global localStorage preference, making the
		// user's model choice persist across projects. The model will be persisted
		// to the session only when the user explicitly sends a message with a modelId.
		agentModel := ""
		if resolvedAgentID == "" {
			// Scope-aware default: model.GetDefaultAgentID() can be an agent
			// scoped to another project (AgentList[0] fallback), so resolve to
			// the first agent visible HERE. See resolveDefaultAgentForProject.
			resolvedAgentID = resolveDefaultAgentForProject(projectPath)
			if resolvedAgentID == "" {
				writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "NoAgentsAvailable")
				return
			}
		}
		// If user explicitly specified an agent, mark source as "user"
		if agentID != "" {
			agentSource = "user"
		}
		if backend == "" {
			backend = "codebuddy"
		}
		title := req.Title
		// A caller-supplied title is deliberately chosen; only the generated
		// "NewSession N" placeholder may be replaced by first-message auto-titling.
		lockTitle := title != ""
		if title == "" {
			title = newUnnamedSessionTitle(r, projectPath)
		}
		createSession := service.CreateSession
		if lockTitle {
			createSession = service.CreateSessionWithLockedTitle
		}
		sessionID, err := createSession(projectPath, backend, title, resolvedAgentID, agentModel, agentSource, "chat")
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to create session")))
			return
		}
		setSessionID(w, r, sessionID)
		// Return session count for UI indicator, plus the persisted
		// auto-approve flag (initialized from the agent's configured default) so
		// the frontend reflects server state instead of re-deriving it.
		sessionCount, _ := service.GetSessionCount(projectPath)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": sessionID, jsonBackend: backend, jsonAgentID: resolvedAgentID, "sessionCount": sessionCount, "title": title, "autoApprove": service.GetSessionAutoApprove(sessionID)})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// newUnnamedSessionTitle builds the localized placeholder title for a session
// the caller did not name, numbering it per project.
//
// Numbering is per project: the new unnamed session takes
// max(existing numbered unnamed sessions) + 1, so unnamed sessions are numbered
// 1, 2, 3, ... regardless of agent. Explicitly-named sessions don't affect it.
//
// Every unnamed-session creation path must go through here — the manual
// "new session" POST and the implicit creation when a project has no sessions
// at all (AIChat GET). Two paths numbering independently would both hand out
// the same number.
func newUnnamedSessionTitle(r *http.Request, projectPath string) string {
	n, err := service.NextSessionNumber(projectPath, T(r, "NewSession"))
	if err != nil {
		return T(r, "NewSession")
	}
	return T(r, "NewSessionN", map[string]any{"N": n})
}

// ServeSessionsReorder handles PUT /api/ai/sessions/reorder — persists a manual
// drag order for the current project's chat sessions (#492).
//
// The body carries the visible order as `{ids: [...]}`; each session's
// sort_order becomes its index. The client can only reorder rows it has loaded,
// so ids is a prefix of the project's sessions — the untouched tail is
// renumbered after the prefix in its previous relative order (see
// service.ReorderSessions).
func ServeSessionsReorder(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := service.ReorderSessions(projectPath, req.IDs); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to reorder sessions")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// ArchiveSession handles DELETE for archiving a single session.
func ArchiveSession(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodDelete) {
		return
	}

	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}

	backend := r.URL.Query().Get("backend")
	if backend == "" {
		backend = "codebuddy"
	}

	// Cancel the running session before archiving to kill the CLI process.
	// This ensures no orphan CLI processes remain after archive.
	if service.IsSessionRunning(sessionID) {
		slog.Info("cancelling running session before archive", "session_id", sessionID)
		service.CancelSession(sessionID)
	}

	// Close the ACP connection for this session before archive
	// (GetSessionAgentID queries WHERE archived=0, so we must read it first)
	// Run in a goroutine because CloseConn calls cmd.Wait() which can
	// block indefinitely if the agent subprocess doesn't exit cleanly,
	// preventing the HTTP response from being sent.
	agentID := service.GetSessionAgentID(sessionID)

	// Empty sessions have nothing worth preserving for RAG — hard-delete instead.
	// Use GetFinalizedMessageCount to exclude streaming placeholder rows,
	// so a session with only a streaming row (e.g. interrupted mid-generation)
	// is still considered empty.
	msgCount, err := service.GetFinalizedMessageCount(sessionID)
	if err != nil {
		// Count failure must NOT fall through to the hard-delete branch — the
		// session may have real content and an error returning 0 must never
		// destroy it. Fall back to a regular archive (soft delete).
		slog.Error("archiving session: GetFinalizedMessageCount failed, falling back to soft archive", "session_id", sessionID, "err", err)
	} else if msgCount == 0 {
		archiveEmptySessionHardDelete(w, projectPath, sessionID, agentID)
		return
	}

	if err := service.ArchiveSession(projectPath, backend, sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to archive session")))
		return
	}

	// Close the ACP connection for a non-empty archived session.
	if agent := model.GetAgent(agentID); agent != nil && agent.SupportsACP() {
		slog.Info("acp: closing connection for archived session", "session_id", sessionID, "agent_id", agentID)
		go ai.GetACPConnManager().CloseConn(sessionID)
	}
	closeGroupMemberConns(sessionID)

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": false, "sessionCount": sessionCount})
}

// archiveEmptySessionHardDelete destroys an archived session that has no
// finalized messages. It closes (but does NOT ACP-delete) the agent connection,
// purges RAG chunks, hard-deletes the DB records, and writes the response. The
// empty-session path always terminates the request, so the caller returns
// immediately after invoking it.
func archiveEmptySessionHardDelete(w http.ResponseWriter, projectPath, sessionID, agentID string) {
	slog.Info("archiving empty session → hard-delete", "session_id", sessionID)

	// Close (but do NOT ACP-delete) the agent connection. The agent-side
	// transcript stays on disk so the session remains recoverable from the
	// external list. Closing alone is safe for empty sessions — their
	// ephemeral mapping is dropped here, and re-discovery happens on the
	// next session/list enumeration.
	// 仅关闭（不 ACP 删除）agent 连接：磁盘上的 agent 会话文件保持原样，
	// 会话仍可从外部列表重新发现。空会话的连接映射在此释放。
	if agent := model.GetAgent(agentID); agent != nil && agent.SupportsACP() {
		slog.Info("acp: closing connection for empty archived session", "session_id", sessionID, "agent_id", agentID)
		go ai.GetACPConnManager().CloseConn(sessionID)
	}
	// A group's members are separate connections keyed by MEMBER row id; close
	// them too so the hard-delete does not leak member agent processes
	// (decision #57). No-op for non-group sessions.
	closeGroupMemberConns(sessionID)

	// Delete RAG chunks (best-effort, no-op if RAG not initialized)
	if chunksDeleted, err := service.PurgeRAGChunksBySessionIDs([]string{sessionID}); err != nil {
		slog.Warn("failed to delete RAG chunks for empty archived session", "session_id", sessionID, "err", err)
	} else if chunksDeleted > 0 {
		slog.Info("deleted RAG chunks for empty archived session", "session_id", sessionID, "chunks", chunksDeleted)
	}

	if err := service.HardDeleteSession(sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to destroy empty session")))
		return
	}

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": true, "sessionCount": sessionCount})
}

// DestroySession handles DELETE for physically removing a session and all its data.
// Unlike ArchiveSession, this irreversibly removes the session
// from the database — chat_history, tool_calls, raw_responses, task_executions, and
// the session record itself.
func DestroySession(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !requireMethod(w, r, http.MethodDelete) {
		return
	}

	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}

	// Cancel the running session before destroying to kill the CLI process.
	if service.IsSessionRunning(sessionID) {
		slog.Info("cancelling running session before destroy", "session_id", sessionID)
		service.CancelSession(sessionID)
	}

	// Close (but do NOT ACP-delete) the agent connection. The agent-side
	// transcript stays on disk so the session remains recoverable from the
	// external list. Destroy now only removes the ClawBench DB records.
	// 仅关闭（不 ACP 删除）agent 连接：磁盘上的 agent 会话文件保持原样，
	// 会话仍可从外部列表重新发现。destroy 现在只删 ClawBench 数据库记录。
	agentID := service.GetSessionAgentID(sessionID)
	if agentID != "" {
		if agent := model.GetAgent(agentID); agent != nil && agent.SupportsACP() {
			slog.Info("acp: closing connection for removed session", "session_id", sessionID, "agent_id", agentID)
			go ai.GetACPConnManager().CloseConn(sessionID)
		}
	}
	closeGroupMemberConns(sessionID)

	// Delete RAG chunks for this session before hard-deleting session data.
	// Best-effort — if RAG is not initialized, this is a no-op.
	if chunksDeleted, err := service.PurgeRAGChunksBySessionIDs([]string{sessionID}); err != nil {
		slog.Warn("failed to delete RAG chunks for destroyed session", "session_id", sessionID, "err", err)
	} else if chunksDeleted > 0 {
		slog.Info("deleted RAG chunks for destroyed session", "session_id", sessionID, "chunks", chunksDeleted)
	}

	if err := service.HardDeleteSession(sessionID); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to destroy session")))
		return
	}

	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "destroyed": true, "sessionCount": sessionCount})
}

// closeGroupMemberConns closes the ACP connection of every member of a group.
//
// Each member owns a separate connection keyed by the MEMBER row id, so closing
// the group row's connection (what the archive/destroy paths already do) leaves
// every member agent process running. A group's members must be closed by their
// own ids (decision #57). No-op for non-group sessions.
//
// Each close runs in its own goroutine, matching the existing convention:
// CloseConn may block on cmd.Wait() if the agent does not exit cleanly, and the
// HTTP response must not wait on it.
func closeGroupMemberConns(sessionID string) {
	if !service.IsGroupSession(sessionID) {
		return
	}
	members, err := service.ListGroupMembers(sessionID)
	if err != nil {
		slog.Warn("acp: listing group members to close connections failed", "session_id", sessionID, "err", err)
		return
	}
	for _, m := range members {
		slog.Info("acp: closing connection for group member", "group_id", sessionID, "member_id", m.ID)
		go ai.GetACPConnManager().CloseConn(m.ID)
	}
}

// getSessionID retrieves session ID from query param or cookie.
func getSessionID(r *http.Request) string {
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		return sessionID
	}
	cookie, err := r.Cookie(model.ScopedCookieName("chat_session_id"))
	if err != nil {
		return ""
	}
	return cookie.Value
}

// resolveUpdateTargetSession resolves the target session for PATCH
// /api/ai/session/update. An explicit body sessionId wins over the query
// param / cookie: the frontend sends {sessionId, title} in the body, and the
// cookie may point at a different session (another project, or a background
// refresh). Falling back to the cookie there would rename the WRONG session,
// leaving the intended one unlocked so its first message overwrites the title
// — the "renamed title gets clobbered" bug.
//
// Returns ("", false) after writing an error response when the target is
// missing or not owned by the project.
//
// Ownership is enforced on BOTH paths. Previously only the body-sessionId path
// was checked, so `?session_id=<id of another project's session>` bypassed the
// guard entirely (requireSessionID only checks presence) and let a caller
// rewrite or clear another project's session settings — most damagingly its
// tags, which are the first project-visible data written through this endpoint.
// The check is skipped when no project cookie is present, preserving the
// historical lenient behavior for tests and pre-project callers.
func resolveUpdateTargetSession(w http.ResponseWriter, r *http.Request, bodySessionID string) (string, bool) {
	sessionID := bodySessionID
	if sessionID == "" {
		var ok bool
		sessionID, ok = requireSessionID(w, r)
		if !ok {
			return "", false
		}
	}

	// GetSessionProjectPathAny (not GetSessionFullInfo) so an archived session
	// still resolves to its owning project instead of an empty path.
	projectPath, found := service.GetSessionProjectPathAny(sessionID)
	if !found {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return "", false
	}
	if cookieProject := middleware.GetProjectFromCookie(r); cookieProject != "" && projectPath != cookieProject {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return "", false
	}
	return sessionID, true
}

// ServeAISessionUpdate handles PATCH /api/ai/session — immediately persists
// session-scoped settings (mode, thinkingEffort, model, transport) so they
// survive page reload even without sending a chat message.
func ServeAISessionUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPatch) {
		return
	}
	var req struct {
		SessionID      string `json:"sessionId"` // explicit target; overrides query/cookie
		ModeID         string `json:"modeId"`
		ThinkingEffort string `json:"thinkingEffort"`
		ModelID        string `json:"modelId"`
		Transport      string `json:"transport"`
		AutoApprove    *bool  `json:"autoApprove"` // pointer: distinguish "not sent" from false
		Cruise         *bool  `json:"cruise"`      // pointer: distinguish "not sent" from false (auto-cruise)
		Title          string `json:"title"`
		Pinned         *bool  `json:"pinned"` // pointer: distinguish "not sent" from false
		// Tags replaces the session's full tag set when non-nil. A pointer is
		// required here: `"tags": []` (clear all) must be distinguishable from
		// the field being absent (leave tags untouched), which a plain slice
		// cannot express.
		Tags *[]service.SessionTagRef `json:"tags"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	sessionID, ok := resolveUpdateTargetSession(w, r, req.SessionID)
	if !ok {
		return
	}
	// Whether the model switch could only be deferred to the next prompt
	// (the agent was busy). Reported in the response body so the frontend can
	// say so instead of claiming the switch already took effect.
	modelSwitchDeferred := false
	// A pick sent together with a model switch belongs to the NEW model, so the
	// per-model memory must be keyed by the requested id — the session row still
	// holds the previous one at this point (the model is applied last).
	effortModelKey := ""
	if req.ModelID != "" {
		effortModelKey = req.ModelID
	}
	effortAccepted := applySessionConfigSelections(sessionID, req.ModeID, req.ThinkingEffort, effortModelKey)
	if req.Transport != "" {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionTransport(sessionID, req.Transport)
		if req.Transport == "cli" {
			ai.GetACPConnManager().CloseConn(sessionID)
		}
	}
	if req.AutoApprove != nil {
		applyAutoApprove(sessionID, *req.AutoApprove)
	}
	if req.Cruise != nil {
		applySessionCruise(sessionID, *req.Cruise)
	}
	if title := strings.TrimSpace(req.Title); title != "" {
		// Manual rename: lock the title so the first-message auto-title does
		// not overwrite the user's explicit choice. Trimmed so a whitespace-only
		// value cannot latch a blank title.
		//
		// A group MEMBER row is a hidden session with no meaningful title (its
		// display name lives in agent_name). Renaming one is a no-op with a
		// confusing side effect (it would set title_source), so ignore it. This
		// is defense in depth: member rows are not reachable from the UI, but a
		// hand-crafted request could still name one.
		if !service.IsGroupMemberSession(sessionID) {
			//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
			service.SetSessionTitleLocked(sessionID, title)
		}
	}
	if req.Pinned != nil {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionPinned(sessionID, *req.Pinned)
	}
	if req.Tags != nil {
		// Tag writes can genuinely fail (e.g. a conflicting definition), and
		// unlike the settings above the frontend has no optimistic copy to fall
		// back on — so surface the error instead of reporting a silent ok.
		if err := applySessionTags(sessionID, *req.Tags); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to save session tags")))
			return
		}
	}
	// Model LAST: it is the only field that can hard-fail the whole request, and
	// doing it here means a rejection leaves every other field above applied
	// (a 422 then means "everything else saved, the model was refused"). Running
	// it earlier would silently drop transport/autoApprove/title/pinned/tags.
	effectiveEffort := ""
	if req.ModelID != "" {
		deferred, err := applyAndPersistSessionModel(r.Context(), sessionID, req.ModelID)
		if err != nil {
			writeModelSwitchError(w, r, err)
			return
		}
		modelSwitchDeferred = deferred
		// A model switch invalidates the session's effort pick: it was made for
		// the previous model and its level may not even exist here (issue #552).
		// Unless the request carried an explicit pick for the new model, resolve
		// what this model will actually run so the client can show it — instead
		// of leaving the old level on the chip while the agent silently uses a
		// different one.
		if req.ThinkingEffort == "" {
			effectiveEffort = switchSessionEffortToModel(sessionID, req.ModelID, modelSwitchDeferred)
		} else if !modelSwitchDeferred {
			// The pick was validated and persisted before the switch, but its RPC
			// was deliberately withheld until the agent is on the new model.
			forwardSessionConfigOption(sessionID, "thought_level", req.ThinkingEffort)
		}
	}
	resp := map[string]interface{}{"ok": true, "modelSwitchDeferred": modelSwitchDeferred}
	if effectiveEffort != "" {
		resp["thinkingEffort"] = effectiveEffort
	}
	if !effortAccepted {
		// The level was rejected by the agent and deliberately NOT persisted.
		// Without this signal the client would keep showing (and re-sending) a
		// pick the server discarded, so the chip would disagree with the agent
		// for the rest of the session.
		resp["thinkingEffortRejected"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// switchSessionEffortToModel re-derives the session's thinking effort after a
// model switch and pushes it to the live agent.
//
// ACP backends advertise levels PER MODEL, so the previous pick must not survive
// a switch: OpenCode rejects a level the new model does not offer ("Invalid
// params: effort not found: low") and the session would keep reporting a level
// the agent never accepted.
//
// CLI backends have STATIC levels (BackendSpec), identical for every model, so a
// switch cannot invalidate the pick — clearing it there would silently discard
// the user's choice and make them re-pick, which is the complaint in issue #552
// wearing a different hat.
//
// The value returned is what the new model will actually run (remembered pick >
// supported global default), or empty for "let the agent keep its own default";
// the caller omits it from the response in that case.
//
// deferred=true means the agent is still running the PREVIOUS model (busy /
// timed out): the DB is updated, but pushing the new level now would change the
// effort of a session that has not switched yet. The next prompt applies both.
func switchSessionEffortToModel(sessionID, modelID string, deferred bool) string {
	agent := model.GetAgent(service.GetSessionAgentID(sessionID))
	if agent == nil {
		return ""
	}
	isACP := sessionUsesACP(sessionID, agent)

	if !isACP {
		// Model-independent levels: keep the session's pick untouched. Report it
		// so the client can keep showing it; nothing to persist or push.
		if effort, _, ok := service.UserThinkingEffortPick(sessionID); ok {
			return effort
		}
		return service.EffectiveThinkingEffortForModel(agent, modelID, false)
	}

	effort := service.EffectiveThinkingEffortForModel(agent, modelID, true)
	if effort == "" {
		// Nothing configured for this model (or the configured default is not
		// offered here): clear the session's stored pick so the next turn
		// derives from the agent's own report instead of replaying the previous
		// model's level.
		persistContextStateThinkingEffortChange(sessionID, "", modelID)
		return ""
	}
	// A remembered pick may not exist on this model (OpenCode's lists differ per
	// model). Applying it would be rejected on every turn, so clear the pick
	// instead of leaving a value that can never take effect.
	//
	// The registry's list is agent-level and, before this fix, was only updated
	// when a model advertised an ID it had not seen — a narrower model's list
	// (deepseek: high/max/default) is a SUBSET of the previous one (glm:
	// low/high/max/default), so it was never refreshed and this check would
	// happily accept a level the current model rejects. The registry now also
	// tracks removals, and an UNKNOWN list (nothing advertised yet) must not
	// discard the user's choice — the agent stays authoritative in that case.
	if levels := ai.GetAgentCapabilityRegistry().GetThinkingEffortState(agent.ID, ""); levels != nil && len(levels.AvailableLevels) > 0 {
		valid := false
		for _, l := range levels.AvailableLevels {
			if l.ID == effort {
				valid = true
				break
			}
		}
		if !valid {
			slog.Info("remembered effort not offered by this model, clearing it",
				"session_id", sessionID, "agent_id", agent.ID, "model", modelID, "level", effort,
				"available", len(levels.AvailableLevels))
			persistContextStateThinkingEffortChange(sessionID, "", modelID)
			return ""
		}
	}
	persistContextStateThinkingEffortChange(sessionID, effort, modelID)
	if !deferred {
		forwardSessionConfigOption(sessionID, "thought_level", effort)
	}
	return effort
}

// sessionUsesACP reports whether this session's turns go over ACP. The session
// row's transport override is authoritative; otherwise the agent's configured
// transport decides.
func sessionUsesACP(sessionID string, agent *model.Agent) bool {
	if t := service.GetSessionTransport(sessionID); t != "" {
		return t == "acp-stdio"
	}
	return agent != nil && agent.Transport == "acp-stdio"
}

// applySessionTags replaces a session's tag set, filing any brand-new labels
// under the session's OWN project.
//
// The session's project — not the request cookie — is authoritative: filing the
// new label under the cookie's project would drop it from the session's own
// project candidate list whenever the cookie points elsewhere (e.g. tagging
// from the floating window or a cross-project view).
//
// Uses GetSessionProjectPathAny rather than GetSessionFullInfo: the latter
// filters archived=0, so a session archived while the dialog was open would
// resolve to "" and its new tags would be filed under project_path=” — a
// definition no project's candidate list matches, making the tag permanently
// invisible and undeletable from the UI.
func applySessionTags(sessionID string, tags []service.SessionTagRef) error {
	projectPath, found := service.GetSessionProjectPathAny(sessionID)
	if !found {
		// The caller already validated the session exists; reaching here means
		// it was deleted between validation and write. Refuse rather than
		// creating link rows for a session that no longer exists.
		return fmt.Errorf("session %s no longer exists", sessionID)
	}
	return service.SetSessionTags(sessionID, projectPath, tags)
}

// applyAutoApprove persists the auto-approve toggle and syncs it to any live
// ACP connection(s).
//
// Group chats need a fan-out: each member owns its own ACP connection keyed by
// the MEMBER row id, and the permission handler reads the member row's flag —
// so writing the group row alone (whose connection is always nil) made the
// switch dead (decision #61). SetGroupAutoApprove writes every member row plus
// the group row; the live member connections are synced here.
func applyAutoApprove(sessionID string, enabled bool) {
	if service.IsGroupSession(sessionID) {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.SetGroupAutoApprove(sessionID, enabled)
		members, err := service.ListGroupMembers(sessionID)
		if err != nil {
			return
		}
		for _, m := range members {
			if conn := ai.GetACPConnManager().GetConn(m.ID); conn != nil {
				conn.SetAutoApprove(enabled)
			}
		}
		return
	}
	//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
	service.UpdateSessionAutoApprove(sessionID, enabled)
	if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
		conn.SetAutoApprove(enabled)
	}
}

// applySessionCruise persists the auto-cruise switch.
//
// Auto-cruise (single chat: keep confirming until the AI declares the goal met;
// group: the four hand-back points become confirmation rounds) is stored in the
// session's context_state and read by BuildChatRequest (single chat) and the
// group orchestrator.
//
// Enabling cruise FORCES auto-approve on: cruise only works unattended, and a
// pending permission prompt would stall the loop with no human watching. The
// link is one-directional — disabling cruise does NOT touch auto-approve, since
// the user may still want unattended runs. The caller invokes this AFTER the
// autoApprove block so a request carrying both {autoApprove:false, cruise:true}
// still ends with auto-approve on.
func applySessionCruise(sessionID string, enabled bool) {
	//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
	service.SetSessionCruise(sessionID, enabled)
	if enabled {
		applyAutoApprove(sessionID, true)
	}
}

// forwardSessionConfigOption pushes a config-option change to the session's ACP
// agent, if one is connected.
//
// Run asynchronously: the RPC can block for up to 30s if the agent is slow
// (e.g., the Claude bridge adapter starting its CLI subprocess). Blocking the
// HTTP handler would tie up a browser HTTP/1.1 connection and prevent other
// requests (like the session list) from being served.
func forwardSessionConfigOption(sessionID, category, value string) {
	conn := ai.GetACPConnManager().GetConn(sessionID)
	if conn == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn.SetSessionConfigOption(ctx, category, value)
	}()
}

// modelSwitchHTTPTimeout bounds the click-time model switch. Kept above the
// agent-side budget (ai.modelSwitchTimeout, 30s) so the client is not the one
// that gives up: a slow-but-successful switch must not be reported as a
// failure. The frontend uses a slightly longer budget still (its own timeout
// must outlast this handler).
//
// DELIBERATELY SYNCHRONOUS, unlike forwardSessionConfigOption above. That
// helper's async comment warns about tying up a browser HTTP/1.1 connection,
// and the trade-off is real here too — but for model it is the point of the
// feature: the user clicked a model and is standing in the picker waiting to
// learn whether it took, so the outcome has to reach THIS response. Reporting
// it over the WebSocket instead would need a new event, a correlation id, and a
// listener for a case that is otherwise a plain request/response.
//
// The cost is bounded in practice: an agent that is BUSY rejects instantly
// (Hermes checks is_running before any I/O), so only a genuine switch pays the
// ~10s, and only when the user just asked for it. Non-ACP sessions (no live
// conn) return immediately.
const modelSwitchHTTPTimeout = 35 * time.Second

// applySessionModel pushes a model selection to the session's live ACP agent.
//
// Returns (deferred=false, nil) when the switch was applied now (or there was
// nothing to push: no live connection — CLI transport, or an ACP session that
// has not started yet).
//
// Returns (deferred=true, nil) when the agent rejected it transiently (busy /
// timed out / connection dropped): the caller still persists the choice, and
// the next prompt re-applies it — the "remember and defer" behavior.
//
// Returns a non-nil error only for a HARD rejection (e.g. the provider is not
// connected), which would fail identically on every retry and must be reported
// to the user immediately instead of being silently deferred.
func applySessionModel(ctx context.Context, sessionID, modelID string) (deferred bool, err error) {
	conn := ai.GetACPConnManager().GetConn(sessionID)
	if conn == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelSwitchHTTPTimeout)
	defer cancel()

	err = conn.ApplyModel(ctx, modelID)
	if err == nil {
		return false, nil
	}
	if ai.IsModelSwitchRetryable(err) {
		// Keep the user's choice (the caller persists it) and re-apply on the
		// next prompt — the same end state as any other agent, whose model has
		// always been applied at prompt time.
		slog.Info("model switch deferred to the next prompt",
			"session_id", sessionID, "model", modelID, "error", err)
		return true, nil
	}
	return false, err
}

// applySessionConfigSelections persists and forwards the mode / thinking-effort
// selections, each only when the request actually carried one.
//
// Config ids are resolved to what the agent advertised (issue #429):
// "mode" is the internal category key; "thought_level" resolves to
// claude-agent-acp's "effort", falling back to the historical "thinkingEffort"
// when unadvertised — otherwise the selector silently fails for agents whose id
// naming differs from ours.
//
// The effort is marked as a USER PICK (issue #552) and remembered per model on
// the agent. Both matter:
//   - without the flag, the next turn cannot tell this choice apart from the
//     agent's own per-model default and would re-derive it from context_state
//     as if the user had chosen whatever the agent last reported;
//   - without the per-model memory, the choice dies with the session, so a new
//     conversation starts again from the agent's "low" default.
//
// modelKey names the model the effort pick belongs to. It is passed explicitly
// when the same request also switches models (the session row still holds the
// previous id at that point); empty means "the session's current model".
//
// Reports whether the effort was accepted. A rejected level is NOT persisted,
// so the client must be told — otherwise it keeps showing (and re-sending) a
// pick the server discarded.
func applySessionConfigSelections(sessionID, modeID, thinkingEffort, modelKey string) (effortAccepted bool) {
	if modeID != "" {
		// Persist to DB context_state so it survives restarts.
		persistContextStateModeChange(sessionID, modeID)
		forwardSessionConfigOption(sessionID, "mode", modeID)
	}
	if thinkingEffort == "" {
		return true
	}
	{
		modelID := modelKey
		if modelID == "" {
			modelID = currentEffortModelKey(sessionID)
		}
		// Validate only when the model is not changing in this same request: the
		// registry still holds the previous model's level list (see
		// rememberEffortForModel).
		//
		// Validate BEFORE persisting: a level the agent does not advertise must
		// not become a stored user pick. The ACP layer no longer records a
		// rejected value (so it stays retryable), which means a persisted
		// invalid pick would be re-sent on EVERY turn and shown by GET as the
		// user's choice, with no way for them to notice.
		if !rememberEffortForModel(sessionID, modelID, thinkingEffort, modelKey == "") {
			slog.Info("thinking effort not accepted for this agent/model, not persisting the pick",
				"session_id", sessionID, "model", modelID, "level", thinkingEffort)
			return false
		}
		persistContextStateThinkingEffortChange(sessionID, thinkingEffort, modelID)
		// The RPC is NOT sent here when a model switch rides the same request:
		// the agent is still on the OLD model until applyAndPersistSessionModel
		// runs (model is applied last), so pushing the new model's level now
		// would target the wrong model — and could be rejected for a level the
		// old model does not offer. The caller forwards it after the switch.
		if modelKey == "" {
			forwardSessionConfigOption(sessionID, "thought_level", thinkingEffort)
		}
	}
	return true
}

// currentEffortModelKey resolves which model a pick made right now belongs to.
//
// The session's persisted model is authoritative, but a session created via
// POST /api/ai/sessions deliberately stores an empty model (the agent default
// is not pre-filled), so the fallback is the agent's own default — the model
// the very first turn will run. An empty key would make the pick
// model-independent, which is precisely the behaviour this feature replaces.
func currentEffortModelKey(sessionID string) string {
	if m := service.GetSessionModel(sessionID); m != "" {
		return m
	}
	agent := model.GetAgent(service.GetSessionAgentID(sessionID))
	if agent == nil {
		return ""
	}
	return agent.DefaultModelID()
}

// rememberEffortForModel stores the pick on the agent, keyed by model, so new
// sessions and future switches back to that model restore it. It reports
// whether the level is acceptable, so the caller can avoid persisting a pick
// the agent would refuse.
//
// Failures are logged, not surfaced: the caller still applies the level to this
// turn, so a storage problem does not block the user's click.
//
// validate is false when the same request also switches models: the ACP
// registry still advertises the PREVIOUS model's levels at that point (the
// model is applied last), and those differ per model — OpenCode offers
// low/high/max for glm-5.3 but only high/max for deepseek-v4-pro. Validating
// there would silently drop a legitimate pick (e.g. "low" while switching to a
// model that supports it). The agent itself is authoritative and rejects a
// level it cannot run, which the ACP layer now reports instead of recording.
func rememberEffortForModel(sessionID, modelID, level string, validate bool) bool {
	if modelID == "" {
		return true
	}
	agentID := service.GetSessionAgentID(sessionID)
	agent := model.GetAgent(agentID)
	if agent == nil {
		return true
	}
	if validate && !isValidThinkingEffort(agent, level, sessionID) {
		slog.Info("thinking effort pick not valid for agent, skipping per-model memory",
			"session_id", sessionID, "agent_id", agentID, "model", modelID, "level", level)
		return false
	}
	// Already remembered: skip the write. This is the picker's click path, so it
	// is not hot, but a no-op save would still take the process-global write
	// mutex and republish the agent pointer for nothing.
	if agent.ThinkingEffortByModel[modelID] == level {
		return true
	}
	if err := service.PatchAgentFields(agentID, service.AgentPatch{
		ThinkingEffortForModel: &service.ThinkingEffortModelPatch{ModelID: modelID, Level: &level},
	}); err != nil {
		slog.Warn("failed to remember thinking effort for model", "session_id", sessionID, "agent_id", agentID, "model", modelID, "error", err)
		return true
	}
	// Publish the change to in-memory readers (copy-on-write, same as the
	// settings PATCH path — readers hold the previous *Agent without a lock).
	model.UpdateAgent(agentID, func(a *model.Agent) {
		a.SetThinkingEffortForModel(modelID, level)
	})
	return true
}

// applyAndPersistSessionModel applies a model selection at CLICK time and
// persists it.
//
// Applied at click time (not deferred to the next message) because the picker
// wants immediate feedback and the user is standing there when they choose.
//
// Persist AFTER the apply, so a hard rejection (e.g. the provider is not
// connected) is not written to the DB — while a retryable one (agent busy) still
// is, which is the "remember and defer" behavior every other agent has always
// had. Returns deferred=true for that case.
func applyAndPersistSessionModel(ctx context.Context, sessionID, modelID string) (deferred bool, err error) {
	deferred, err = applySessionModel(ctx, sessionID, modelID)
	if err != nil {
		return false, err
	}
	//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
	service.UpdateSessionModel(sessionID, modelID)
	return deferred, nil
}

// writeModelSwitchError maps a rejected model switch to an HTTP response the
// frontend can toast. The agent's own explanation (data.details) rides in
// Detail so the toast can name the real cause (e.g. which provider to connect).
func writeModelSwitchError(w http.ResponseWriter, r *http.Request, err error) {
	var mse *ai.ModelSwitchError
	// ApplyModel wraps every failure in a *ModelSwitchError, so this branch is
	// defensive: it keeps a non-nil deref safe if that ever stops being true.
	if !errors.As(err, &mse) {
		slog.Warn("model switch failed", "error", err)
		writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "ModelSwitchFailed")
		return
	}
	slog.Warn("model switch rejected", "code", mse.Code, "detail", mse.Detail)
	if mse.Detail == "" {
		// No agent explanation to add — use the bare key rather than a message
		// that dangles on a colon.
		writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "ModelSwitchFailed")
		return
	}
	// The template key is Detail so the agent's own explanation reaches the
	// toast text (e.g. which provider to connect); the same map also lands in
	// ErrorResponse.Detail for a client that wants it structured.
	writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "ModelSwitchFailedDetail", map[string]any{"Detail": mse.Detail})
}

// attachSessionTags batch-loads tags for a page of sessions and sets them in
// place. Failures are logged and swallowed: tags are decoration, so a tag
// lookup error must not take down the whole session list.
func attachSessionTags(sessions []model.ChatSession) {
	if len(sessions) == 0 {
		return
	}
	ids := make([]string, 0, len(sessions))
	for i := range sessions {
		ids = append(ids, sessions[i].ID)
	}
	tagsBySession, err := service.GetTagsForSessions(ids)
	if err != nil {
		slog.Warn("failed to load session tags", "error", err)
		return
	}
	for i := range sessions {
		for _, t := range tagsBySession[sessions[i].ID] {
			sessions[i].Tags = append(sessions[i].Tags, model.SessionTag{Name: t.Name, Scope: t.Scope})
		}
	}
}

// attachGroupMembers batch-loads the active-member preview for group sessions
// in a page and sets it in place. Only group rows get the field, so a page of
// plain chat sessions pays no extra work. Like attachSessionTags, failures are
// logged and swallowed: the preview is decoration and must not take down the
// list.
func attachGroupMembers(sessions []model.ChatSession) {
	if len(sessions) == 0 {
		return
	}
	groupIDs := make([]string, 0, len(sessions))
	for i := range sessions {
		if service.IsGroupSessionType(sessions[i].SessionType) {
			groupIDs = append(groupIDs, sessions[i].ID)
		}
	}
	if len(groupIDs) == 0 {
		return
	}
	membersByGroup, err := service.GroupMembersForGroups(groupIDs)
	if err != nil {
		slog.Warn("failed to load group members", "error", err)
		return
	}
	for i := range sessions {
		if members := membersByGroup[sessions[i].ID]; len(members) > 0 {
			sessions[i].GroupMembers = members
		}
	}
}

// ServeSessionTags handles GET/DELETE /api/ai/session/tags.
//
//	GET    → tag candidates for the current project
//	         (?inUse=1 narrows to tags actually carried by a session here,
//	          which is what the session-list filter bar shows)
//	DELETE → delete a tag definition everywhere
//	         (name + optional scope in the query string)
func ServeSessionTags(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Two distinct views share this endpoint: the dialog needs every
		// selectable candidate (including unused global tags), while the filter
		// bar must not offer a tag that would always yield an empty list.
		inUse := r.URL.Query().Get("inUse") == "1"
		var tags []service.SessionTag
		var err error
		if inUse {
			tags, err = service.ListProjectTagsInUse(projectPath)
		} else {
			tags, err = service.ListSessionTags(projectPath)
		}
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load session tags")))
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"tags": tags})

	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "TagNameRequired")
			return
		}
		// The scope the client saw identifies WHICH definition to delete when
		// the same name exists both globally and as a project tag. Without it,
		// deleting a project-scoped label could destroy a global label shared
		// by every project. Empty scope keeps the legacy global-first fallback.
		scope := strings.TrimSpace(r.URL.Query().Get("scope"))
		if err := service.DeleteSessionTag(name, projectPath, scope); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "TagDeleteFailed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// persistContextStateModeChange updates the mode currentModeId in DB context_state
// using atomic json_set() so it doesn't erase previously saved thinking/usage data.
func persistContextStateModeChange(sessionID, modeID string) {
	// Only update currentModeId; availableModes list stays as-is from ACP events.
	patch := &service.ModeStatePersist{CurrentModeID: modeID}
	patchJSON, _ := json.Marshal(patch)
	service.PatchContextStateMerge(sessionID, map[string]string{"mode": string(patchJSON)})
}

// persistContextStateThinkingEffortChange records an EXPLICIT user effort pick.
// The UserPicked/ModelID flags are what let a later turn tell this apart from
// the agent's own per-model default (issue #552) and know which model the pick
// belongs to.
func persistContextStateThinkingEffortChange(sessionID, effortID, modelID string) {
	service.PersistThinkingEffortPick(sessionID, effortID, modelID)
}

// setSessionID sets session ID in cookie.
// HttpOnly: true prevents JavaScript access, mitigating XSS-based session hijack (ISS-123).
func setSessionID(w http.ResponseWriter, r *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     model.ScopedCookieName("chat_session_id"),
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// ServeForkSession handles POST /api/ai/session/fork — creates a new chat session
// by copying all messages from the current session (without external_session_id).
//
// The body's sessionId is authoritative; the query param / chat_session_id cookie
// is only a fallback. This matters because the cookie is cleared whenever the
// user switches project (see ServeProject), so a client that switched projects
// and forked immediately would otherwise be rejected with SessionIdRequired even
// though it sent a perfectly good sessionId in the body. Reading the body first
// also mirrors resolveUpdateTargetSession, which made the same choice for the
// same reason: the cookie may point at a different session than the one the user
// acted on.
func ServeForkSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req struct {
		SessionID       string `json:"sessionId"`
		BeforeMessageID int64  `json:"beforeMessageId"`
		AgentID         string `json:"agentId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if !decodeJSON(w, r, &req) {
		return
	}
	// Body sessionId wins; fall back to query/cookie for clients that omit it.
	sourceID := req.SessionID
	if sourceID == "" {
		sourceID = getSessionID(r)
	}
	if sourceID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Resolve agent override: if the user selected a different agent, validate it exists
	overrideAgentID := req.AgentID
	if overrideAgentID != "" {
		if _, _, _, _, ok := resolveAgentConfig(overrideAgentID); !ok {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentID")
			return
		}
		// A fork creates a NEW session, so the disabled-agent fallback applies
		// here too (the picker hides disabled agents, but the server is the
		// authoritative gate).
		if a := model.GetAgent(overrideAgentID); a != nil && a.Disabled {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "AgentDisabled")
			return
		}
		// Same for project scope: the fork is a NEW session in the caller's
		// project, so a project-only agent from another project is refused.
		if a := model.GetAgent(overrideAgentID); a != nil && !agentVisibleInProject(a, projectPath) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "AgentNotInProject")
			return
		}
	}

	title := buildForkTitle(r, sourceID)

	newSessionID, err := service.ForkSession(sourceID, projectPath, title, req.BeforeMessageID, overrideAgentID)
	if err != nil {
		writeForkError(w, r, err)
		return
	}

	setSessionID(w, r, newSessionID)
	sessionCount, _ := service.GetSessionCount(projectPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": newSessionID, "sessionCount": sessionCount})
}

// buildForkTitle builds the title for a forked session.
// It uses the original session title with a fork emoji prefix.
func buildForkTitle(r *http.Request, sourceID string) string {
	sourceTitle, _ := service.GetSessionTitle(sourceID)
	if sourceTitle == "" {
		sourceTitle = T(r, "Session")
	}
	return T(r, "ForkPrefix") + sourceTitle
}

// writeForkError writes the appropriate error response for a ForkSession error.
func writeForkError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("handler: failed to fork session", "error", err)
	errMsg := err.Error()
	if strings.Contains(errMsg, "group sessions are not forkable") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "GroupNotForkable")
	} else if strings.Contains(errMsg, "session limit") {
		writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{jsonMaxCount: model.SessionMaxCount})
	} else if strings.Contains(errMsg, "not found in session") || strings.Contains(errMsg, "must be a user or assistant message") || strings.Contains(errMsg, "streaming message") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidForkPoint")
	} else if strings.Contains(errMsg, "not found") {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
	} else {
		model.WriteError(w, model.Internal(err))
	}
}
