package service

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// This file implements the capabilities the IM push backends need for the
// /show, /stop and /new commands. They live in service (not in internal/push)
// because the push packages must not import service — the dependency direction
// is push → service, bridged in cmd/server/main.go.

// i18nT localizes a key for the push path, which runs without an HTTP request.
// Matches the pattern already used by group_store.go / group_orchestrator.go:
// the language comes from model.Language rather than a request context.
func i18nT(key string, data ...map[string]any) string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), key, data...)
}

// GetLastAssistantReply returns the session's most recent FINALIZED assistant
// reply as plain text. ok is false when the session has no finished reply yet;
// a read failure is returned as an error so the bot can say "unavailable"
// instead of falsely claiming the session has no reply.
//
// Reuses GetAssistantRawContents + extractPreviewFromBlocksRaw so the text is
// identical to what the completion push showed (group-chat protocol tags are
// stripped there too). It deliberately does NOT read the streaming row: a
// half-written answer is worse than "no reply yet".
//
// Rows are walked newest-first and the first one yielding text wins. A finalized
// turn that produced no text at all (tool-only, or a summarized row stripped to
// empty blocks) is skipped rather than treated as "no reply", because the
// previous turn's real answer is more useful than nothing.
func GetLastAssistantReply(sessionID string) (string, bool, error) {
	contents, err := GetAssistantRawContents(sessionID)
	if err != nil {
		slog.Warn("push: read assistant replies failed",
			"session_id", sessionID, "error", err)
		return "", false, fmt.Errorf("%s", i18nT("PushSessionUnavailable"))
	}
	for _, raw := range contents {
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			continue
		}
		if text := extractPreviewFromBlocksRaw(sessionID, content.Blocks); text != "" {
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
		return wasRunning, 0, fmt.Errorf("%s", i18nT("PushStopFailed"))
	}

	CancelSession(sessionID)

	if err := ClearQueuedMessages(sessionID); err != nil {
		slog.Warn("push: clear queued messages failed", "session_id", sessionID, "error", err)
		return wasRunning, 0, fmt.Errorf("%s", i18nT("PushStopFailed"))
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
	Index        int
	Path         string
	Exists       bool
	SessionCount int
}

// ListProjectsForPush returns every registered project in the order /new
// renders them, with 1-based indices the user types back.
func ListProjectsForPush() ([]ProjectRef, error) {
	items, err := ListAllProjects()
	if err != nil {
		return nil, err
	}
	refs := make([]ProjectRef, 0, len(items))
	for i, item := range items {
		refs = append(refs, ProjectRef{
			Index:        i + 1,
			Path:         item.Path,
			Exists:       item.Exists,
			SessionCount: item.SessionCount,
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
	projectID, ok, err := store.ProjectIDByPath(projectPath)
	if err != nil {
		slog.Warn("push: resolve project failed", "path", projectPath, "error", err)
		return "", "", fmt.Errorf("%s", i18nT("PushNewProjectNotFound"))
	}
	if !ok {
		return "", "", fmt.Errorf("%s", i18nT("PushNewProjectNotFound"))
	}

	// The per-project cap is enforced on every other create entry point
	// (chat_session.go, group.go, scheduler.go, session_resume.go). Without it
	// here, a repeated "/new 1" from IM is the only unbounded create path.
	if model.SessionMaxCount > 0 {
		if n, cerr := GetSessionCount(projectPath); cerr == nil && n >= model.SessionMaxCount {
			return "", "", fmt.Errorf("%s", i18nT("SessionLimitReached",
				map[string]any{"MaxCount": model.SessionMaxCount}))
		}
	}

	agentID := defaultAgentForProject(projectID)
	if agentID == "" {
		return "", "", fmt.Errorf("%s", i18nT("PushNewNoAgent"))
	}
	agent := model.GetAgent(agentID)
	if agent == nil {
		return "", "", fmt.Errorf("%s", i18nT("PushNewNoAgent"))
	}

	baseTitle := i18nT("NewSession")
	if n, numErr := NextSessionNumber(projectPath, baseTitle); numErr == nil {
		title = i18nT("NewSessionN", map[string]any{"N": n})
	} else {
		title = baseTitle
	}

	sessionID, err = CreateSession(projectPath, agent.Backend, title, agentID, "", "default", "chat")
	if err != nil {
		slog.Warn("push: create session failed", "path", projectPath, "error", err)
		return "", "", fmt.Errorf("%s", i18nT("PushNewCreateFailed"))
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
