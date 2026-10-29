package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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
// reply as plain text. ok is false when there is no finished reply yet.
//
// Reuses GetAssistantRawContents + extractPreviewFromBlocksRaw so the text is
// identical to what the completion push showed (group-chat protocol tags are
// stripped there too). It deliberately does NOT read the streaming row: a
// half-written answer is worse than "no reply yet".
func GetLastAssistantReply(sessionID string) (string, bool) {
	contents, err := GetAssistantRawContents(sessionID)
	if err != nil {
		slog.Warn("push: read assistant replies failed",
			"session_id", sessionID, "error", err)
		return "", false
	}
	for _, raw := range contents {
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			continue
		}
		if text := extractPreviewFromBlocksRaw(sessionID, content.Blocks); text != "" {
			return text, true
		}
	}
	return "", false
}

// StopAndClearSession stops the session's in-flight turn and drops its queue.
//
// CancelSession is idempotent (it returns true for an already-idle session), so
// wasRunning is sampled BEFORE the cancel. The queue is cleared afterwards:
// CancelSession's drain-loop branch also collects queue IDs and emits
// queue_cancel, but that runs on the agent goroutine and can lose the race with
// this call. Clearing here and emitting again is harmless (the frontend treats
// an empty queueIds array as a no-op) and is what makes the "not running at all"
// case deterministic.
func StopAndClearSession(sessionID string) (wasRunning bool, cleared int, err error) {
	wasRunning = IsSessionRunningForSweep(sessionID)

	CancelSession(sessionID)

	queueIDs, listErr := GetQueuedQueueIDs(sessionID)
	if listErr != nil {
		return wasRunning, 0, fmt.Errorf("list queued messages: %w", listErr)
	}
	if err := ClearQueuedMessages(sessionID); err != nil {
		return wasRunning, 0, fmt.Errorf("clear queued messages: %w", err)
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

// ResolveProjectSelector resolves a /new argument against the project list.
//
// Only two forms are accepted: a 1-based index from that same list, or a path
// that exactly matches a registered project. Basename matching is deliberately
// absent — two projects named "web" would be indistinguishable, and silently
// picking one would create the session in the wrong repo.
//
// A path that is not registered is refused rather than registered on the fly:
// store.ProjectIDForPath would happily insert a row for any string, which would
// let a typo permanently pollute the project list.
func ResolveProjectSelector(arg string, refs []ProjectRef) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("empty project selector")
	}

	if n, err := strconv.Atoi(arg); err == nil {
		for _, r := range refs {
			if r.Index == n {
				return r.Path, nil
			}
		}
		return "", fmt.Errorf("project index %d out of range (1-%d)", n, len(refs))
	}

	canon := store.NormalizeProjectPath(arg)
	for _, r := range refs {
		if store.NormalizeProjectPath(r.Path) == canon {
			return r.Path, nil
		}
	}
	return "", fmt.Errorf("project not registered: %s", arg)
}

// CreatePushSession creates an empty chat session in projectPath using the
// default agent visible in that project.
//
// Mirrors the handler's create path (scope-aware default agent, numbered
// placeholder title, auto-approve snapshot) so a session created from IM is
// indistinguishable from one created in the UI.
func CreatePushSession(projectPath string) (sessionID, title string, err error) {
	projectID, ok, err := store.ProjectIDByPath(projectPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve project: %w", err)
	}
	if !ok {
		return "", "", fmt.Errorf("project not registered: %s", projectPath)
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
		return "", "", err
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
