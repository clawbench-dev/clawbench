package ai

import (
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// CodeBuddy Task* → plan_update bridge
// ---------------------------------------------------------------------------
//
// CodeBuddy does NOT emit ACP session/update.plan notifications (only its
// TodoWrite tool translates to a plan, and TaskCreate/TaskUpdate/TaskList are
// the tools CodeBuddy actually uses to manage its task list). Without this
// bridge, the frontend PlanPanel ("执行计划" checklist) stays empty for
// CodeBuddy sessions even while the agent creates and progresses tasks.
//
// CodeBuddy's Task tools carry the FULL post-operation task snapshot in each
// terminal tool result, inside `_meta["codebuddy.ai/rawResponse"]`
// (verified empirically at the wire level — see
// codebuddy_task_wire_probe_integration_test.go):
//
//	TaskCreate: { content, rawResponse: { task: {...}, todos: [{id, content, status, ...}] } }
//	TaskUpdate: { content, rawResponse: { task: {...status}, todos: [...] } }
//	TaskUpdate delete: { content, rawResponse: { taskId, deleted: true, todos: [...] } }
//	TaskList:   { content, rawResponse: { tasks: [...], todos: [...] } }
//
// todos[i].content is the task title (todos items carry `content`, while the
// `tasks` array carries `subject`). status is one of pending | in_progress |
// completed. The snapshot is always the complete list after the operation.
//
// This bridge maps every terminal TaskCreate/TaskUpdate/TaskList result into a
// `plan_update` StreamEvent (full-snapshot replace semantics — exactly what
// the frontend PlanPanel consumes from real ACP plan notifications), and caches
// the resulting PlanState on the connection so refresh/reconnect/REST session
// load repopulates the panel.

// codebuddyBridgedTaskTools are the CodeBuddy tools whose terminal result is a
// full post-operation task-list snapshot safe to drive plan_update from.
// TaskGet returns a single task (no full snapshot); TaskStop/TaskOutput are
// not task-list mutations — all ignored.
var codebuddyBridgedTaskTools = map[string]bool{
	"TaskCreate": true,
	"TaskUpdate": true,
	"TaskList":   true,
}

// codebuddyTaskStatusToPlan maps a CodeBuddy todo status to the PlanEntry
// status vocabulary the frontend PlanPanel renders.
func codebuddyTaskStatusToPlan(status string) string {
	switch status {
	case "in_progress", "completed":
		return status
	default:
		// pending, unknown, missing → pending (PlanPanel only knows three states)
		return "pending"
	}
}

// parseCodeBuddyTaskMetaTodos extracts PlanEntry items from a CodeBuddy Task*
// terminal tool result `_meta` map. It looks up `_meta["codebuddy.ai/rawResponse"]`
// and reads the full `todos` snapshot inside it (falling back to the `tasks`
// array, whose items carry `subject` instead of `content`).
//
// Returns (nil, false) when no task snapshot can be located — the caller must
// not emit a plan_update in that case.
func parseCodeBuddyTaskMetaTodos(meta map[string]any) ([]PlanEntry, bool) {
	if len(meta) == 0 {
		return nil, false
	}
	rawResp, ok := meta["codebuddy.ai/rawResponse"].(map[string]any)
	if !ok {
		return nil, false
	}

	// Prefer the `todos` array (CodeBuddy's flattened task view, content per
	// item), then fall back to `tasks` (raw task rows, subject per item).
	var items []any
	if todos, ok := rawResp["todos"].([]any); ok {
		items = todos
	} else if tasks, ok := rawResp["tasks"].([]any); ok {
		items = tasks
	}
	if items == nil {
		return nil, false
	}

	entries := make([]PlanEntry, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		// todos items: content; tasks items: subject. Tolerate both.
		content, _ := m["content"].(string)
		if content == "" {
			content, _ = m["subject"].(string)
		}
		if content == "" {
			continue
		}
		status, _ := m["status"].(string)
		entries = append(entries, PlanEntry{
			Content:  content,
			Priority: "medium", // CodeBuddy tasks have no priority field
			Status:   codebuddyTaskStatusToPlan(status),
		})
	}
	// Fail closed when items exist but every one was skipped (missing content/
	// subject): an unexpected shape should not wipe a real checklist with an
	// empty plan_update. Only a genuinely empty todos array means "no tasks".
	if len(items) > 0 && len(entries) == 0 {
		return nil, false
	}
	return entries, true
}

// codebuddyToolCallNameFromUpdate resolves the canonical CodeBuddy tool name
// from a ToolCallUpdate's _meta, mirroring parseCodeBuddyACPToolCallUpdate.
// The bridge only calls this for completed updates, so the title fallback
// mirrors the parse function's non-done path (tool.Name set from _meta or title).
func codebuddyToolCallNameFromUpdate(tcu acp.SessionToolCallUpdate) string {
	if name := extractMetaToolNameFlat(tcu.Meta); name != "" {
		return name
	}
	if tcu.Title != nil && *tcu.Title != "" {
		kind := acp.ToolKindExecute
		if tcu.Kind != nil {
			kind = *tcu.Kind
		}
		return extractToolName(*tcu.Title, kind, "codebuddy", string(tcu.ToolCallId))
	}
	return ""
}

// bridgeCodeBuddyPlanFromToolUpdate synthesizes a plan_update StreamEvent (and
// caches the plan) from a terminal CodeBuddy Task* tool result. It is a no-op
// unless the update is a completed TaskCreate/TaskUpdate/TaskList result whose
// _meta carries a full todos snapshot.
//
// DEADLOCK SAFETY: runs on the SDK notification goroutine (same as the
// update.Plan branch in mapACPSessionUpdate). It only calls forwardACPEvent
// (non-blocking channel send) and conn.SetCachedPlanState — the exact
// code path the existing update.Plan branch already exercises safely on this
// goroutine. Task* terminal results only arrive while the agent is running a
// prompt (c.mu released before the Prompt RPC), never during a LoadSession/
// ResumeSession RPC window (replays are buffered upstream and never reach
// mapACPSessionUpdate), so acquiring c.mu here cannot deadlock.
//
// Returns the number of plan_update events forwarded (0 or 1).
func bridgeCodeBuddyPlanFromToolUpdate(ch chan<- StreamEvent, conn *ACPConn, tcu acp.SessionToolCallUpdate) int {
	if tcu.Status == nil || *tcu.Status != acp.ToolCallStatusCompleted {
		return 0
	}
	name := codebuddyToolCallNameFromUpdate(tcu)
	if !codebuddyBridgedTaskTools[name] {
		return 0
	}
	entries, ok := parseCodeBuddyTaskMetaTodos(tcu.Meta)
	if !ok {
		slog.Debug("codebuddy plan bridge: no todos snapshot in _meta", "tool", name, "tool_call_id", tcu.ToolCallId)
		return 0
	}
	plan := &PlanState{Entries: entries}
	forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: plan})
	if conn != nil {
		conn.SetCachedPlanState(plan)
	}
	slog.Debug("codebuddy plan bridge: plan_update from task tool", "tool", name, "entries", len(entries))
	return 1
}
