package ai

import (
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// OpenCode `todowrite` → plan_update bridge
// ---------------------------------------------------------------------------
//
// OpenCode does NOT emit ACP session/update.plan notifications (verified at the
// wire level — see opencode_acp_plan_probe_integration_test.go: the variant
// histogram contains zero `plan` / `plan_update` / `plan_removed` frames). It
// exposes its checklist exclusively through the `todowrite` tool, whose frames
// carry the FULL post-operation snapshot:
//
//	tool_call_update (status=in_progress):
//	    rawInput  = { todos: [{content, priority, status}, ...] }
//	tool_call_update (status=completed):
//	    rawOutput = { metadata: { todos: [{content, priority, status}, ...] }, output: "<json>" }
//
// Each todowrite invocation replaces the entire list (same full-snapshot
// semantics as the spec `plan` variant), and successive invocations walk the
// items pending → in_progress → completed. The terminal (completed) frame is
// the one bridged, so exactly one plan_update is emitted per invocation — the
// in_progress frame carries the same snapshot and would only duplicate it.
//
// Identification is by snapshot SHAPE, not by tool name: OpenCode retitles the
// terminal frame from "todowrite" to "N todos" (and drops the kind), so the
// resolved tool name is empty by the time it is terminal — the `todos` array
// under rawOutput.metadata / rawInput is the only reliable discriminator.
//
// This is deliberately looser than the CodeBuddy bridge (which additionally
// requires the tool name to be one of TaskCreate/TaskUpdate/TaskList): the name
// is unavailable here. The gate is therefore per-backend + shape-only, and the
// shape is specific enough in practice — the wire probe shows opencode's other
// tools (write/bash) carry `filepath`/`output` metadata with no `todos` array.
// A future opencode/MCP tool returning `metadata.todos` would be bridged; that
// is accepted because the payload then genuinely is a todo list.
//
// Without this bridge the frontend PlanPanel ("执行计划" checklist) stays empty
// for OpenCode sessions even while the agent maintains a todo list.

// isOpenCodePlanBackend reports whether the ACP backend surfaces its checklist
// through the `todowrite` tool rather than a native ACP plan notification.
// mimo is an OpenCode fork with the same tool surface.
func isOpenCodePlanBackend(backendID string) bool {
	return backendID == "opencode" || backendID == "mimo"
}

// opencodeTodoStatusToPlan maps an OpenCode todo status to the PlanEntry status
// vocabulary the frontend PlanPanel renders. OpenCode already uses the same
// three values; anything else (or missing) falls back to pending.
func opencodeTodoStatusToPlan(status string) string {
	switch status {
	case "in_progress", "completed":
		return status
	default:
		return "pending"
	}
}

// opencodeTodoPriorityToPlan maps an OpenCode todo priority to the PlanEntry
// priority vocabulary. OpenCode uses high/medium/low; unknown or missing falls
// back to medium (matching the CodeBuddy bridge's default).
func opencodeTodoPriorityToPlan(priority string) string {
	switch priority {
	case "high", "medium", "low":
		return priority
	default:
		return "medium"
	}
}

// opencodeTodoItems locates the todos array in a todowrite frame. The bridge
// only runs on terminal (completed) frames, which carry the APPLIED snapshot in
// rawOutput.metadata.todos — so that is preferred. rawInput.todos (the requested
// list, seen on the in_progress frame) is the fallback for a completed frame
// that omits rawOutput. Preferring rawOutput matters if a frame ever carries
// both: rawInput could be empty while rawOutput holds the real list, and
// preferring rawInput would then wipe a real checklist.
func opencodeTodoItems(rawInput, rawOutput any) ([]any, bool) {
	if m, ok := rawOutput.(map[string]any); ok {
		if meta, ok := m["metadata"].(map[string]any); ok {
			if arr, ok := meta["todos"].([]any); ok {
				return arr, true
			}
		}
	}
	if m, ok := rawInput.(map[string]any); ok {
		if arr, ok := m["todos"].([]any); ok {
			return arr, true
		}
	}
	return nil, false
}

// parseOpenCodeTodoSnapshot extracts PlanEntry items from an opencode todowrite
// tool frame. Returns (nil, false) when no todo snapshot can be located — the
// caller must not emit a plan_update in that case.
func parseOpenCodeTodoSnapshot(rawInput, rawOutput any) ([]PlanEntry, bool) {
	items, ok := opencodeTodoItems(rawInput, rawOutput)
	if !ok {
		return nil, false
	}

	entries := make([]PlanEntry, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		if content == "" {
			continue
		}
		status, _ := m["status"].(string)
		priority, _ := m["priority"].(string)
		entries = append(entries, PlanEntry{
			Content:  content,
			Priority: opencodeTodoPriorityToPlan(priority),
			Status:   opencodeTodoStatusToPlan(status),
		})
	}
	// Fail closed when items exist but every one was skipped (missing content):
	// an unexpected shape must not wipe a real checklist with an empty
	// plan_update. Only a genuinely empty todos array means "no tasks".
	if len(items) > 0 && len(entries) == 0 {
		return nil, false
	}
	return entries, true
}

// bridgeOpenCodePlanFromToolUpdate synthesizes a plan_update StreamEvent (and
// caches the plan) from a terminal opencode todowrite tool result. It is a
// no-op unless the update is a completed tool frame carrying a todos snapshot.
//
// DEADLOCK SAFETY: runs on the SDK notification goroutine (same as the
// update.Plan branch in mapACPSessionUpdate). It only calls forwardACPEvent
// (non-blocking channel send) and conn.SetCachedPlanState — the exact code path
// the existing update.Plan branch already exercises safely on this goroutine.
// Terminal tool results only arrive while the agent is running a prompt (c.mu
// released before the Prompt RPC), never during a LoadSession/ResumeSession RPC
// window, so acquiring c.mu here cannot deadlock.
//
// Returns the number of plan_update events forwarded (0 or 1).
func bridgeOpenCodePlanFromToolUpdate(ch chan<- StreamEvent, conn *ACPConn, tcu acp.SessionToolCallUpdate) int {
	if tcu.Status == nil || *tcu.Status != acp.ToolCallStatusCompleted {
		return 0
	}
	entries, ok := parseOpenCodeTodoSnapshot(tcu.RawInput, tcu.RawOutput)
	if !ok {
		return 0
	}
	plan := &PlanState{Entries: entries}
	forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: plan})
	if conn != nil {
		conn.SetCachedPlanState(plan)
	}
	slog.Debug("opencode plan bridge: plan_update from todowrite", "entries", len(entries))
	return 1
}
