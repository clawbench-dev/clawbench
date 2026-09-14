//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"context"
	"encoding/json"
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
		ID              string    `json:"id"`
		Title           string    `json:"title"`
		Backend         string    `json:"backend"`
		AgentID         string    `json:"agentId"`
		Model           string    `json:"model"`
		Running         bool      `json:"running"`
		PendingApproval bool      `json:"pendingApproval"`
		UnreadCount     int       `json:"unreadCount"`
		UpdatedAt       time.Time `json:"updatedAt"`
	}
	type projectGroup struct {
		Name     string            `json:"name"`
		Sessions []overviewSession `json:"sessions"`
	}
	groups := []*projectGroup{}
	groupByName := map[string]*projectGroup{}
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
		g.Sessions = append(g.Sessions, overviewSession{
			ID:              s.ID,
			Title:           s.Title,
			Backend:         s.Backend,
			AgentID:         s.AgentID,
			Model:           s.Model,
			Running:         running,
			PendingApproval: pending,
			UnreadCount:     s.UnreadCount,
			UpdatedAt:       s.UpdatedAt,
		})
		total++
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
		// cursor_pinned completes the keyset: the list is ordered by
		// (pinned DESC, created_at DESC, id DESC), so paging on created_at alone
		// re-returns pinned rows on every page. Absent/empty keeps the legacy
		// created_at-only predicate for older clients.
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
			sessions, hasMore, err = service.GetSessionsPaged(projectPath, "", limit, cursor, cursorID, cursorPinned)
		} else {
			sessions, err = service.GetSessions(projectPath, "")
			hasMore = false
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
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
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
			resolvedAgentID = model.GetDefaultAgentID()
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
			// Numbering is per project: the new unnamed session takes
			// max(existing numbered unnamed sessions) + 1, so unnamed sessions
			// are numbered 1, 2, 3, ... regardless of agent. Explicitly-named
			// sessions don't affect it.
			n, err := service.NextSessionNumber(projectPath, T(r, "NewSession"))
			if err == nil {
				title = T(r, "NewSessionN", map[string]any{"N": n})
			} else {
				title = T(r, "NewSession")
			}
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
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "sessionId": sessionID, "backend": backend, "agentId": resolvedAgentID, "sessionCount": sessionCount, "title": title, "autoApprove": service.GetSessionAutoApprove(sessionID)})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
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
	if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
		slog.Info("acp: closing connection for archived session", "session_id", sessionID, "agent_id", agentID)
		go ai.GetACPConnManager().CloseConn(sessionID)
	}

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
	if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
		slog.Info("acp: closing connection for empty archived session", "session_id", sessionID, "agent_id", agentID)
		go ai.GetACPConnManager().CloseConn(sessionID)
	}

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
		if agent, ok := model.Agents[agentID]; ok && agent.SupportsACP() {
			slog.Info("acp: closing connection for removed session", "session_id", sessionID, "agent_id", agentID)
			go ai.GetACPConnManager().CloseConn(sessionID)
		}
	}

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
	if req.ModeID != "" {
		// Persist mode change to DB context_state so it survives restarts
		persistContextStateModeChange(sessionID, req.ModeID)
		// Forward mode change to ACP agent so it updates its runtime state.
		// The internal category key is passed ("mode"); SetSessionConfigOption
		// resolves it to the config-option id the agent advertised (issue #429).
		forwardSessionConfigOption(sessionID, "mode", req.ModeID)
	}
	if req.ThinkingEffort != "" {
		// Persist thinking effort change to DB context_state so it survives restarts
		persistContextStateThinkingEffortChange(sessionID, req.ThinkingEffort)
		// "thought_level" is the internal category key; SetSessionConfigOption
		// resolves it to the agent-advertised id (claude-agent-acp: "effort"),
		// falling back to the historical "thinkingEffort" when unadvertised —
		// otherwise the effort selector silently fails for agents whose id
		// naming differs from ours (issue #429).
		forwardSessionConfigOption(sessionID, "thought_level", req.ThinkingEffort)
	}
	if req.ModelID != "" {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionModel(sessionID, req.ModelID)
	}
	if req.Transport != "" {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionTransport(sessionID, req.Transport)
		if req.Transport == "cli" {
			ai.GetACPConnManager().CloseConn(sessionID)
		}
	}
	if req.AutoApprove != nil {
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.UpdateSessionAutoApprove(sessionID, *req.AutoApprove)
		// Sync to ACPConn runtime state
		if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
			conn.SetAutoApprove(*req.AutoApprove)
		}
	}
	if title := strings.TrimSpace(req.Title); title != "" {
		// Manual rename: lock the title so the first-message auto-title does
		// not overwrite the user's explicit choice. Trimmed so a whitespace-only
		// value cannot latch a blank title.
		//nolint:errcheck,gosec // best-effort persistence; failure is non-fatal for an idempotent update
		service.SetSessionTitleLocked(sessionID, title)
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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

// ServeSessionTags handles GET/DELETE /api/ai/session/tags.
//
//	GET    → the tag candidates selectable in the current project
//	         (global tags + this project's own tags)
//	DELETE → delete a tag definition everywhere
//	         (name + optional scope in the query string)
func ServeSessionTags(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		tags, err := service.ListSessionTags(projectPath)
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

// persistContextStateThinkingEffortChange updates the thinkingEffort currentId in DB context_state
// using atomic json_set() so it doesn't erase previously saved mode/usage data.
func persistContextStateThinkingEffortChange(sessionID, effortID string) {
	patch := &service.ThinkingEffortPersist{CurrentID: effortID}
	patchJSON, _ := json.Marshal(patch)
	service.PatchContextStateMerge(sessionID, map[string]string{"thinkingEffort": string(patchJSON)})
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
func ServeForkSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	sessionID, ok := requireSessionID(w, r)
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
	// Use body sessionId if provided, otherwise fall back to query/cookie
	sourceID := req.SessionID
	if sourceID == "" {
		sourceID = sessionID
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
	if strings.Contains(errMsg, "session limit") {
		writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{"MaxCount": model.SessionMaxCount})
	} else if strings.Contains(errMsg, "not found in session") || strings.Contains(errMsg, "must be a user or assistant message") || strings.Contains(errMsg, "streaming message") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidForkPoint")
	} else if strings.Contains(errMsg, "not found") {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
	} else {
		model.WriteError(w, model.Internal(err))
	}
}
