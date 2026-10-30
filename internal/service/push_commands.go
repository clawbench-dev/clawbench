package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// This file implements the capabilities the IM push backends need for the
// /show, /stop and /new commands. They live in service (not in internal/push)
// because the push packages must not import service — the dependency direction
// is push → service, bridged in cmd/server/main.go.
//
// Localization goes through the package-wide background-path helper
// (backgroundText) rather than a second copy of the same three-line wrapper.

// GetLastAssistantReply returns the session's most recent FINALIZED assistant
// reply as plain text. ok is false when the session has no finished reply yet;
// a read failure is returned as an error so the bot can say "unavailable"
// instead of falsely claiming the session has no reply.
//
// The text is the WHOLE final answer (every text block after the last tool_use),
// with the group-chat protocol policy applied — /show exists because the push
// preview is truncated, so returning only the first of several answer blocks
// would defeat its purpose.
//
// It deliberately does NOT read the streaming row: a half-written answer is worse
// than "no reply yet". Callers should tell the user when a turn is in flight, so
// a reply that names the previous turn cannot be mistaken for the current one.
//
// Rows are walked newest-first and the first one yielding text wins. A finalized
// turn that produced no text at all (tool-only, or a summarized row stripped to
// empty blocks) is skipped rather than treated as "no reply", because the
// previous turn's real answer is more useful than nothing.
func GetLastAssistantReply(sessionID string) (string, bool, error) {
	if !store.ReadDBReady() {
		// A nil read handle would panic inside the query; report it as
		// "unavailable" instead. Mirrors FindSessionsByPrefix's guard.
		return "", false, fmt.Errorf("%s", backgroundText("PushSessionUnavailable"))
	}
	contents, err := GetAssistantRawContents(sessionID)
	if err != nil {
		slog.Warn("push: read assistant replies failed",
			"session_id", sessionID, "error", err)
		return "", false, fmt.Errorf("%s", backgroundText("PushSessionUnavailable"))
	}
	for _, raw := range contents {
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			continue
		}
		if text := AssistantFullAnswer(sessionID, content.Blocks); text != "" {
			return text, true, nil
		}
	}
	return "", false, nil
}

// StopAndClearSession stops the session's in-flight turn and drops its queue.
//
// Both facts reported to the user are sampled BEFORE the cancel, because
// CancelSession's drain-loop branch clears the queue on its own goroutine as
// soon as the context is cancelled. Reading afterwards races it and reports
// "cleared 0" for exactly the case /stop exists for (a running session with a
// backlog), which would look like the command did nothing.
//
// Emitting queue_cancel twice (once here, once from the drain branch) is
// harmless: the frontend's removeQueuedMany early-returns on an empty list, and
// emitting ids the drain already removed is a no-op. Doing it here is what makes
// the "nothing was running at all" case deterministic.
func StopAndClearSession(sessionID string) (wasRunning bool, cleared int, err error) {
	wasRunning = IsSessionRunning(sessionID)

	// Snapshot before the cancel — see the doc comment above.
	queueIDs, listErr := GetQueuedQueueIDs(sessionID)
	if listErr != nil {
		slog.Warn("push: list queued messages failed", "session_id", sessionID, "error", listErr)
		return wasRunning, 0, fmt.Errorf("%s", backgroundText("PushStopFailed"))
	}

	CancelSession(sessionID)

	if err := ClearQueuedMessages(sessionID); err != nil {
		slog.Warn("push: clear queued messages failed", "session_id", sessionID, "error", err)
		return wasRunning, 0, fmt.Errorf("%s", backgroundText("PushStopFailed"))
	}
	cleared = len(queueIDs)

	// Emit even when queueIDs is empty: a cross-device _remote bubble must not
	// linger (the frontend treats an empty array as a no-op).
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: eventTypeQueueCancel,
		QueueEvent: &ai.QueueEventData{
			SessionID: sessionID,
			QueueIDs:  queueIDs,
		},
	})
	return wasRunning, cleared, nil
}

// ProjectRef mirrors common.ProjectRef without importing the push package.
type ProjectRef struct {
	Index  int
	Path   string
	Exists bool
}

// ListProjectsForPush returns every registered project in the order /new
// renders them, with 1-based indices the user types back.
//
// The order MUST be stable across a create: ListAllProjects sorts by
// "most recently active", and creating a session in a project updates exactly
// that. A user who reads "2. /path/b" and types "/new 2" would therefore find
// the same index pointing at a different project on their next command — the
// list reorders itself as a side effect of being used. Sorting by path here
// makes the index a stable address, which is what a selector needs.
//
// SessionCount is deliberately not carried: the list never renders it.
func ListProjectsForPush() ([]ProjectRef, error) {
	if !store.ReadDBReady() {
		return nil, fmt.Errorf("database not initialized")
	}
	items, err := ListAllProjects()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	refs := make([]ProjectRef, 0, len(items))
	for i, item := range items {
		refs = append(refs, ProjectRef{
			Index:  i + 1,
			Path:   item.Path,
			Exists: item.Exists,
		})
	}
	return refs, nil
}

// CreatePushSession creates an empty chat session in projectPath using the
// default agent visible in that project.
//
// Mirrors the handler's create path (scope-aware default agent, per-project
// session cap, numbered placeholder title, auto-approve snapshot) so a session
// created from IM is indistinguishable from one created in the UI.
//
// Errors returned here are already localized: the IM backends reply with
// err.Error() verbatim, so a raw Go error ("failed to generate unique session
// ID after 10 attempts", a SQLite message) must never escape.
func CreatePushSession(projectPath string) (sessionID, title string, err error) {
	// An empty path resolves to the GLOBAL scope sentinel in
	// store.ProjectIDByPath, which would create a project_id=0 session instead
	// of refusing. The contract is "the path must already be a registered
	// project", so reject it before the lookup can answer "yes".
	if projectPath == "" {
		return "", "", fmt.Errorf("%s", backgroundText("PushNewProjectNotFound"))
	}
	projectID, ok, err := store.ProjectIDByPath(projectPath)
	if err != nil {
		slog.Warn("push: resolve project failed", "path", projectPath, "error", err)
		return "", "", fmt.Errorf("%s", backgroundText("PushNewProjectNotFound"))
	}
	if !ok {
		return "", "", fmt.Errorf("%s", backgroundText("PushNewProjectNotFound"))
	}

	// The per-project cap is enforced on every other create entry point
	// (chat_session.go, group.go, scheduler.go, session_resume.go). Without it
	// here, a repeated "/new 1" from IM is the only unbounded create path.
	if model.SessionMaxCount > 0 {
		if n, cerr := GetSessionCount(projectPath); cerr == nil && n >= model.SessionMaxCount {
			return "", "", fmt.Errorf("%s", backgroundText("SessionLimitReached",
				map[string]any{"MaxCount": model.SessionMaxCount}))
		}
	}

	agentID := defaultAgentForProject(projectID)
	if agentID == "" {
		return "", "", fmt.Errorf("%s", backgroundText("PushNewNoAgent"))
	}
	agent := model.GetAgent(agentID)
	if agent == nil {
		return "", "", fmt.Errorf("%s", backgroundText("PushNewNoAgent"))
	}

	baseTitle := backgroundText("NewSession")
	if n, numErr := NextSessionNumber(projectPath, baseTitle); numErr == nil {
		title = backgroundText("NewSessionN", map[string]any{"N": n})
	} else {
		title = baseTitle
	}

	sessionID, err = CreateSession(projectPath, agent.Backend, title, agentID, "", "default", "chat")
	if err != nil {
		slog.Warn("push: create session failed", "path", projectPath, "error", err)
		return "", "", fmt.Errorf("%s", backgroundText("PushNewCreateFailed"))
	}
	return sessionID, title, nil
}

// defaultAgentForProject resolves the default agent scoped to projectID.
//
// A project-scoped agent is only usable in its own project; a global one is
// usable everywhere. This mirrors handler.resolveDefaultAgentForProject, which
// cannot be reused here (it lives in the handler package, above service).
func defaultAgentForProject(projectID int64) string {
	visible := func(a *model.Agent) bool {
		if a == nil {
			return false
		}
		if a.ProjectID == nil {
			return true
		}
		return *a.ProjectID == projectID
	}
	if def := model.GetDefaultAgentID(); visible(model.GetAgent(def)) {
		return def
	}
	for _, a := range model.GetAgentList() {
		if visible(a) {
			return a.ID
		}
	}
	return ""
}
