# Plan Progress Panel Design

## Overview

Add an ACP Plan progress panel above the Chat Action Bar. The panel provides real-time visibility into the agent's execution strategy and task progress, driven by ACP `plan` / `plan_update` session events.

## Core Behavior

- **Hidden when empty** — When no plan data exists, the panel is not rendered at all (zero space).
- **Auto-appear on plan_update** — Panel becomes visible when the first `plan_update` event arrives, defaulting to expanded state.
- **Remember collapse preference** — Once the user manually collapses, subsequent plan updates keep it collapsed. The user must explicitly click to re-expand.
- **Session lifecycle** — Plan state is bound to the current session. Switching sessions or starting a new conversation clears the plan panel.
- **Reconnection recovery** — Plan state is not persisted to DB. On app restart, plan panel is empty. When resuming an ACP session (`--resume`), if the agent re-sends plan data via `NewSessionResponse` or subsequent `plan_update` events, the panel will populate from those events. If the agent doesn't re-emit plan data, the panel stays hidden — no manual recovery needed.

## Expanded State — Stepped Timeline Layout

```
  │ 🔴 Analyze the existing codebase         [completed: ✓]
  │ 🔴 Identify components for refactoring    [● in_progress]
  │ 🟠 Create unit tests for critical funcs   [○ pending]
  │ ⚪ Update documentation                    [○ pending]
  ▼
```

- **Left vertical line** connects entry nodes, forming a timeline/stepped flow.
- **Priority color coding** on line segments and node borders:
  - High → red (`#ef4444`)
  - Medium → orange (`#f97316`)
  - Low → gray (`#9ca3af`)
- **Status icons:**
  - `pending` → hollow circle (`○`)
  - `in_progress` → pulsing animated dot (`●`) with the line segment for that entry also pulsing
  - `completed` → checkmark (`✓`) with a brief green flash transition animation
- **Line segments:** completed entries get solid fill; pending/in-progress get dashed style.
- **Completion transition:** When an entry transitions from `in_progress` → `completed`:
  1. The pulsing dot morphs into a checkmark icon
  2. The node and line segment briefly flash green (`#22c55e`, 300ms ease-out)
  3. The line segment transitions from dashed to solid

## Collapsed State — Chip with Status

```
  ● 正在分析代码...                    [▼]
```

- **Compact single-line chip**, similar to the thinking chip style.
- **Left side:** Pulsing animation dot + current `in_progress` task name (truncated with ellipsis if too long).
- **When no `in_progress` entry:** Show progress summary instead, e.g. "3/5 完成".
- **New-change indicator:** When a plan update arrives while collapsed, the chip border briefly highlights (subtle glow animation, 500ms) to signal the change.
- **Right side:** Expand toggle button (`▼` / `▲`).

## Data Flow

### Backend

1. **StreamEvent extension** — Add `Plan` field to `StreamEvent` in `internal/ai/interface.go`:
   ```go
   type PlanState struct {
       Entries []PlanEntry `json:"entries"`
   }
   type PlanEntry struct {
       Content  string `json:"content"`
       Priority string `json:"priority"` // "high", "medium", "low"
       Status   string `json:"status"`   // "pending", "in_progress", "completed"
   }
   ```

2. **Event mapping** — In `internal/ai/acp_events.go`, map ACP `SessionUpdate.Plan` to `StreamEvent{Type: "plan_update", Plan: &PlanState{...}}` instead of skipping.

3. **SSE transport** — `plan_update` events flow through existing SSE channel, no new endpoint needed.

### Frontend

1. **Composable** — Add plan state management to `useChatRender` or a new `usePlanProgress` composable:
   - `planEntries: Ref<PlanEntry[]>` — reactive plan entries for current session
   - `planCollapsed: Ref<boolean>` — collapse preference (persisted per session)
   - `planHasUpdate: Ref<boolean>` — flag for new-change indicator in collapsed state
   - Listen for SSE `plan_update` events, replace entire entries array (ACP replace semantics)

2. **Component** — New `PlanPanel.vue` component:
   - Props: `entries`, `collapsed`, `hasUpdate`
   - Emits: `toggle-collapse`
   - Renders expanded stepped timeline or collapsed chip based on state
   - Handles completion transition animations via CSS transitions + `watch` on entry status changes

3. **Integration** — Place `PlanPanel` in `ChatPanelContent.vue`, above `ChatInputBar`, between the message list and the action bar. Pass plan state from composable.

## ACP Protocol Notes

- **v1 `plan` event** (stable): Sends flat `entries[]` list. Client MUST replace entire plan on each update.
- **v2 `plan_update`** (stable): Adds `id` field for multi-plan tracking. We'll use v1 flat list for initial implementation.
- **`PlanCapabilities`** (UNSTABLE): Not advertised in initial implementation; v1 `plan` works without capability negotiation.
- The Go SDK (`acp-go-sdk/types_gen.go`) already has `Plan`, `PlanEntry`, `PlanEntryPriority`, `PlanEntryStatus` types with full deserialization support.

## Scope Exclusions

- **v2 multi-plan tracking** — Out of scope for initial implementation. Single flat plan per session.
- **Plan editing** — Read-only display; no user interaction to modify plan entries.
- **Non-ACP backends** — Plan events only come from ACP agents. CLI backends don't emit plan data.
- **Persistence** — Plan state is in-memory only, not saved to DB or chat history. Recovery relies on ACP agent re-emitting plan data on session resume (see "Reconnection recovery" above).
