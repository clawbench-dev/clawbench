# Plan Progress Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the ACP Plan progress panel that displays agent execution strategy above the chat action bar, with stepped timeline in expanded state and chip with pulse indicator in collapsed state.

**Architecture:** Backend adds `PlanState`/`PlanEntry` types to `StreamEvent`, maps ACP `SessionUpdate.Plan` to `plan_update` events, and transports them via SSE. Frontend adds `usePlanProgress` composable for state management (following the `useSessionIdentity` singleton pattern), `PlanPanel.vue` component for rendering, and integrates into `ChatPanelContent.vue`. E2E test uses Playwright to verify panel appears, updates, and collapses.

**Tech Stack:** Go (backend event mapping), Vue 3 + TypeScript (frontend), Vitest (unit), Playwright (e2e)

---

### Task 1: Backend — Add PlanState types to StreamEvent

**Files:**
- Modify: `internal/ai/interface.go:139-155` (add PlanState, PlanEntry types + Plan field to StreamEvent)

**Step 1: Write the failing test**

Add test to `internal/ai/acp_test.go`:

```go
func TestMapACPSessionUpdate_PlanUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	entries := []acp.PlanEntry{
		{Content: "Analyze codebase", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
		{Content: "Refactor components", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
		{Content: "Write tests", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusPending},
	}
	update := acp.SessionUpdate{Plan: &acp.Plan{Entries: entries}}

	mapACPSessionUpdate(update, ch, ctx)

	require.Len(t, ch, 1)
	event := <-ch
	assert.Equal(t, "plan_update", event.Type)
	require.NotNil(t, event.Plan)
	require.Len(t, event.Plan.Entries, 3)

	assert.Equal(t, "Analyze codebase", event.Plan.Entries[0].Content)
	assert.Equal(t, "high", event.Plan.Entries[0].Priority)
	assert.Equal(t, "completed", event.Plan.Entries[0].Status)

	assert.Equal(t, "Refactor components", event.Plan.Entries[1].Content)
	assert.Equal(t, "in_progress", event.Plan.Entries[1].Status)

	assert.Equal(t, "Write tests", event.Plan.Entries[2].Content)
	assert.Equal(t, "medium", event.Plan.Entries[2].Priority)
	assert.Equal(t, "pending", event.Plan.Entries[2].Status)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/ai/... -run TestMapACPSessionUpdate_PlanUpdate -v`
Expected: FAIL — `event.Plan` undefined, type `PlanState` doesn't exist

**Step 3: Add PlanState and PlanEntry types to interface.go**

In `internal/ai/interface.go`, add after `AvailableCommandInfo` struct (around line 124):

```go
// PlanEntry represents a single entry in the agent's execution plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"` // "high", "medium", "low"
	Status   string `json:"status"`   // "pending", "in_progress", "completed"
}

// PlanState carries the agent's execution plan entries.
// Populated from ACP SessionUpdate.Plan events (replace semantics).
type PlanState struct {
	Entries []PlanEntry `json:"entries"`
}
```

Add `Plan` field to `StreamEvent` struct (after `ThinkingEffort`):

```go
	Plan          *PlanState           // Plan entries (Type=plan_update)
```

Update the `Type` comment to include `"plan_update"`:

```go
	Type string // "content", "thinking", "metadata", "done", "error", "tool_use", "tool_result", "raw_output", "resume_split", "queue_drain", "queue_update", "session_capture", "mode_update", "config_update", "commands_update", "thinking_effort_update", "plan_update"
```

**Step 4: Map ACP Plan to StreamEvent in acp_events.go**

In `internal/ai/acp_events.go`, replace the `case update.Plan != nil:` block:

```go
	case update.Plan != nil:
		entries := make([]PlanEntry, 0, len(update.Plan.Entries))
		for _, e := range update.Plan.Entries {
			entries = append(entries, PlanEntry{
				Content:  e.Content,
				Priority: string(e.Priority),
				Status:   string(e.Status),
			})
		}
		forwardACPEvent(ch, StreamEvent{
			Type: "plan_update",
			Plan: &PlanState{Entries: entries},
		})
```

**Step 5: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/ai/... -run TestMapACPSessionUpdate_PlanUpdate -v`
Expected: PASS

**Step 6: Run all AI package tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/ai/... -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/ai/interface.go internal/ai/acp_events.go internal/ai/acp_test.go
git commit -m "feat: add PlanState type and map ACP plan events to plan_update StreamEvent"
```

---

### Task 2: Backend — Add plan_update SSE transport in chat_stream.go

**Files:**
- Modify: `internal/handler/chat_stream.go:225-230` (add plan_update case)
- Modify: `internal/handler/chat_stream_test.go` (add test)

**Step 1: Write the failing test**

Add test to `internal/handler/chat_stream_test.go`:

```go
func TestStreamEndpoint_PlanUpdate(t *testing.T) {
	w, _, ch := setupStreamTest(t)

	ch <- ai.StreamEvent{
		Type: "plan_update",
		Plan: &ai.PlanState{
			Entries: []ai.PlanEntry{
				{Content: "Analyze code", Priority: "high", Status: "completed"},
				{Content: "Refactor", Priority: "high", Status: "in_progress"},
				{Content: "Write tests", Priority: "medium", Status: "pending"},
			},
		},
	}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)

	body := w.Body.String()
	require.Contains(t, body, "event: plan_update")
	require.Contains(t, body, `"content":"Analyze code"`)
	require.Contains(t, body, `"priority":"high"`)
	require.Contains(t, body, `"status":"completed"`)
	require.Contains(t, body, `"status":"in_progress"`)
	require.Contains(t, body, `"status":"pending"`)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/handler/... -run TestStreamEndpoint_PlanUpdate -v`
Expected: FAIL — `plan_update` event not emitted in SSE output

**Step 3: Add plan_update case to chat_stream.go**

In `internal/handler/chat_stream.go`, add after the `thinking_effort_update` case (line 229):

```go
			case "plan_update":
				if event.Plan != nil {
					data, _ := json.Marshal(event.Plan)
					fmt.Fprintf(w, "event: plan_update\ndata: %s\n\n", data)
				}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/handler/... -run TestStreamEndpoint_PlanUpdate -v`
Expected: PASS

**Step 5: Run all handler tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/handler/... -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/handler/chat_stream.go internal/handler/chat_stream_test.go
git commit -m "feat: transport plan_update events via SSE"
```

---

### Task 3: Frontend — Add usePlanProgress composable

**Files:**
- Create: `web/src/composables/usePlanProgress.ts`
- Create: `web/src/composables/__tests__/usePlanProgress.test.ts`

**Step 1: Write the composable**

Create `web/src/composables/usePlanProgress.ts`:

```typescript
import { ref, computed } from 'vue'

export interface PlanEntry {
  content: string
  priority: 'high' | 'medium' | 'low'
  status: 'pending' | 'in_progress' | 'completed'
}

// Module-level singleton — plan state is session-scoped but
// must be accessible from ChatPanelContent (rendering) and
// useChatStream (SSE event handling) without prop drilling.
const planEntries = ref<PlanEntry[]>([])
const planCollapsed = ref(false)
const planHasUpdate = ref(false)

/** Update plan entries from SSE plan_update event (ACP replace semantics). */
export function updatePlanEntries(entries: PlanEntry[]) {
  planEntries.value = entries
  // Signal new change when collapsed
  if (planCollapsed.value && entries.length > 0) {
    planHasUpdate.value = true
  }
}

/** Clear all plan state (called on session switch). */
export function clearPlanState() {
  planEntries.value = []
  planCollapsed.value = false
  planHasUpdate.value = false
}

/** Toggle collapse state and clear update indicator when expanding. */
export function togglePlanCollapse() {
  planCollapsed.value = !planCollapsed.value
  if (!planCollapsed.value) {
    planHasUpdate.value = false
  }
}

/** Manually set collapsed state (used when user collapses). */
export function setPlanCollapsed(collapsed: boolean) {
  planCollapsed.value = collapsed
  if (!collapsed) {
    planHasUpdate.value = false
  }
}

export function usePlanProgress() {
  const hasPlan = computed(() => planEntries.value.length > 0)
  const inProgressEntry = computed(() =>
    planEntries.value.find(e => e.status === 'in_progress')
  )
  const completedCount = computed(() =>
    planEntries.value.filter(e => e.status === 'completed').length
  )
  const totalCount = computed(() => planEntries.value.length)

  return {
    planEntries,
    planCollapsed,
    planHasUpdate,
    hasPlan,
    inProgressEntry,
    completedCount,
    totalCount,
    togglePlanCollapse,
    setPlanCollapsed,
    updatePlanEntries,
    clearPlanState,
  }
}
```

**Step 2: Write the tests**

Create `web/src/composables/__tests__/usePlanProgress.test.ts`:

```typescript
import { describe, it, expect, beforeEach } from 'vitest'
import { usePlanProgress, clearPlanState, updatePlanEntries, setPlanCollapsed } from '../usePlanProgress'

describe('usePlanProgress', () => {
  beforeEach(() => {
    clearPlanState()
  })

  it('hasPlan is false when no entries', () => {
    const { hasPlan, planEntries } = usePlanProgress()
    expect(hasPlan.value).toBe(false)
    expect(planEntries.value).toEqual([])
  })

  it('hasPlan is true after updatePlanEntries', () => {
    const { hasPlan } = usePlanProgress()
    updatePlanEntries([
      { content: 'Task 1', priority: 'high', status: 'pending' },
    ])
    expect(hasPlan.value).toBe(true)
  })

  it('replaces entries on update (ACP replace semantics)', () => {
    const { planEntries } = usePlanProgress()
    updatePlanEntries([
      { content: 'Task 1', priority: 'high', status: 'pending' },
    ])
    updatePlanEntries([
      { content: 'Task A', priority: 'high', status: 'completed' },
      { content: 'Task B', priority: 'medium', status: 'in_progress' },
    ])
    expect(planEntries.value).toHaveLength(2)
    expect(planEntries.value[0].content).toBe('Task A')
    expect(planEntries.value[1].content).toBe('Task B')
  })

  it('inProgressEntry returns first in_progress entry', () => {
    const { inProgressEntry } = usePlanProgress()
    updatePlanEntries([
      { content: 'Done', priority: 'high', status: 'completed' },
      { content: 'Working', priority: 'medium', status: 'in_progress' },
      { content: 'Later', priority: 'low', status: 'pending' },
    ])
    expect(inProgressEntry.value?.content).toBe('Working')
  })

  it('inProgressEntry is undefined when nothing in progress', () => {
    const { inProgressEntry } = usePlanProgress()
    updatePlanEntries([
      { content: 'Done', priority: 'high', status: 'completed' },
    ])
    expect(inProgressEntry.value).toBeUndefined()
  })

  it('completedCount and totalCount work correctly', () => {
    const { completedCount, totalCount } = usePlanProgress()
    updatePlanEntries([
      { content: 'A', priority: 'high', status: 'completed' },
      { content: 'B', priority: 'medium', status: 'in_progress' },
      { content: 'C', priority: 'low', status: 'pending' },
    ])
    expect(completedCount.value).toBe(1)
    expect(totalCount.value).toBe(3)
  })

  it('clearPlanState resets everything', () => {
    updatePlanEntries([
      { content: 'Task', priority: 'high', status: 'pending' },
    ])
    clearPlanState()
    const { hasPlan, planCollapsed, planHasUpdate } = usePlanProgress()
    expect(hasPlan.value).toBe(false)
    expect(planCollapsed.value).toBe(false)
    expect(planHasUpdate.value).toBe(false)
  })

  it('planHasUpdate is set when collapsed and entries update', () => {
    setPlanCollapsed(true)
    updatePlanEntries([
      { content: 'Task', priority: 'high', status: 'in_progress' },
    ])
    const { planHasUpdate } = usePlanProgress()
    expect(planHasUpdate.value).toBe(true)
  })

  it('planHasUpdate is not set when expanded and entries update', () => {
    setPlanCollapsed(false)
    updatePlanEntries([
      { content: 'Task', priority: 'high', status: 'in_progress' },
    ])
    const { planHasUpdate } = usePlanProgress()
    expect(planHasUpdate.value).toBe(false)
  })

  it('expanding clears planHasUpdate', () => {
    setPlanCollapsed(true)
    updatePlanEntries([
      { content: 'Task', priority: 'high', status: 'in_progress' },
    ])
    setPlanCollapsed(false)
    const { planHasUpdate } = usePlanProgress()
    expect(planHasUpdate.value).toBe(false)
  })

  it('togglePlanCollapse flips state and clears update on expand', () => {
    const { togglePlanCollapse, planCollapsed, planHasUpdate } = usePlanProgress()
    setPlanCollapsed(true)
    updatePlanEntries([
      { content: 'Task', priority: 'high', status: 'in_progress' },
    ])
    expect(planHasUpdate.value).toBe(true)
    togglePlanCollapse() // now expanded
    expect(planCollapsed.value).toBe(false)
    expect(planHasUpdate.value).toBe(false)
  })
})
```

**Step 3: Run tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration/web && npx vitest run src/composables/__tests__/usePlanProgress.test.ts`
Expected: All PASS

**Step 4: Commit**

```bash
git add web/src/composables/usePlanProgress.ts web/src/composables/__tests__/usePlanProgress.test.ts
git commit -m "feat: add usePlanProgress composable for ACP plan state"
```

---

### Task 4: Frontend — Add plan_update SSE handler in useChatStream

**Files:**
- Modify: `web/src/composables/useChatStream.ts:553-560` (add plan_update event listener)
- Modify: `web/src/composables/__tests__/useChatStream.test.ts` (add test)

**Step 1: Add plan_update event listener**

In `web/src/composables/useChatStream.ts`, after the `commands_update` listener (around line 560), add:

```typescript
			eventSource.addEventListener('plan_update', (e) => {
				if (!guard()) return
				let data: any
				try { data = JSON.parse(e.data) } catch { console.warn('SSE plan_update: invalid JSON, skipping'); return }
				if (Array.isArray(data.entries)) {
					updatePlanEntries(data.entries)
				}
			})
```

Add the import at the top of the file:

```typescript
import { updatePlanEntries } from '@/composables/usePlanProgress'
```

**Step 2: Add test for plan_update SSE event**

In `web/src/composables/__tests__/useChatStream.test.ts`, add a test case:

```typescript
  it('should handle plan_update event', async () => {
    const { startStream } = useChatStream(mockOptions)
    await startStream('test-session')

    const listener = mockEventSource.addEventListener.mock.calls.find(
      (call: any[]) => call[0] === 'plan_update'
    )
    expect(listener).toBeDefined()

    const handler = listener![1]
    handler({ data: JSON.stringify({
      entries: [
        { content: 'Analyze code', priority: 'high', status: 'completed' },
        { content: 'Refactor', priority: 'high', status: 'in_progress' },
        { content: 'Test', priority: 'medium', status: 'pending' },
      ]
    })})

    // Verify updatePlanEntries was called — import and check singleton state
    const { planEntries, hasPlan } = await import('@/composables/usePlanProgress').then(m => m.usePlanProgress())
    expect(hasPlan.value).toBe(true)
    expect(planEntries.value).toHaveLength(3)
    expect(planEntries.value[1].content).toBe('Refactor')
  })
```

**Step 3: Run tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration/web && npx vitest run src/composables/__tests__/useChatStream.test.ts`
Expected: All PASS (including new test)

**Step 4: Commit**

```bash
git add web/src/composables/useChatStream.ts web/src/composables/__tests__/useChatStream.test.ts
git commit -m "feat: handle plan_update SSE events in useChatStream"
```

---

### Task 5: Frontend — Create PlanPanel.vue component

**Files:**
- Create: `web/src/components/chat/PlanPanel.vue`

**Step 1: Create the component**

Create `web/src/components/chat/PlanPanel.vue` with the stepped timeline (expanded) and chip (collapsed) states:

```vue
<template>
  <div v-if="entries.length > 0" class="plan-panel">
    <!-- Collapsed: chip with current task -->
    <div v-if="collapsed" class="plan-chip" :class="{ 'plan-chip--updated': hasUpdate }" @click="$emit('toggle-collapse')">
      <span class="plan-chip__pulse"></span>
      <span class="plan-chip__text">{{ chipText }}</span>
      <span class="plan-chip__toggle">▼</span>
    </div>

    <!-- Expanded: stepped timeline -->
    <div v-else class="plan-expanded">
      <div class="plan-header">
        <span class="plan-header__title">{{ t('chat.plan.title') }}</span>
        <button class="plan-header__toggle" @click="$emit('toggle-collapse')">▲</button>
      </div>
      <div class="plan-timeline">
        <div
          v-for="(entry, idx) in entries"
          :key="idx"
          class="plan-entry"
          :class="`plan-entry--${entry.priority} plan-entry--${entry.status}`"
        >
          <div class="plan-entry__line" :class="{ 'plan-entry__line--last': idx === entries.length - 1 }">
            <div class="plan-entry__node">
              <span v-if="entry.status === 'completed'" class="plan-entry__check">✓</span>
              <span v-else-if="entry.status === 'in_progress'" class="plan-entry__dot"></span>
              <span v-else class="plan-entry__circle"></span>
            </div>
          </div>
          <div class="plan-entry__content" :class="{ 'plan-entry__content--done': entry.status === 'completed' }">
            {{ entry.content }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/composables/useLocale'
import type { PlanEntry } from '@/composables/usePlanProgress'

const { t } = useI18n()

const props = defineProps<{
  entries: PlanEntry[]
  collapsed: boolean
  hasUpdate: boolean
}>()

defineEmits<{
  'toggle-collapse': []
}>()

const chipText = computed(() => {
  const inProgress = props.entries.find(e => e.status === 'in_progress')
  if (inProgress) return inProgress.content
  const completed = props.entries.filter(e => e.status === 'completed').length
  return `${completed}/${props.entries.length} 完成`
})
</script>

<style scoped>
.plan-panel {
  margin: 0 12px 6px;
}

/* ── Collapsed chip ── */
.plan-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border-radius: 8px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border);
  font-size: 12px;
  color: var(--color-text-2);
  cursor: pointer;
  transition: border-color 0.3s, box-shadow 0.3s;
}
.plan-chip--updated {
  border-color: var(--color-purple, #8b5cf6);
  box-shadow: 0 0 6px rgba(139, 92, 246, 0.3);
  animation: plan-chip-glow 0.5s ease-out;
}
@keyframes plan-chip-glow {
  0% { box-shadow: 0 0 12px rgba(139, 92, 246, 0.6); }
  100% { box-shadow: 0 0 6px rgba(139, 92, 246, 0.3); }
}
.plan-chip__pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-purple, #8b5cf6);
  animation: pulse 1.5s ease-in-out infinite;
  flex-shrink: 0;
}
@keyframes pulse {
  0%, 100% { opacity: 0.4; transform: scale(0.8); }
  50% { opacity: 1; transform: scale(1.2); }
}
.plan-chip__text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.plan-chip__toggle {
  color: var(--color-text-3);
  font-size: 10px;
  flex-shrink: 0;
}

/* ── Expanded panel ── */
.plan-expanded {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border);
  border-radius: 10px;
  padding: 10px 12px;
}
.plan-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.plan-header__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-2);
}
.plan-header__toggle {
  background: none;
  border: none;
  color: var(--color-text-3);
  font-size: 10px;
  cursor: pointer;
  padding: 2px;
}

/* ── Timeline ── */
.plan-timeline {
  display: flex;
  flex-direction: column;
}
.plan-entry {
  display: flex;
  gap: 10px;
  min-height: 28px;
}

/* Vertical line segment */
.plan-entry__line {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 16px;
  flex-shrink: 0;
  position: relative;
}
.plan-entry__line::after {
  content: '';
  width: 2px;
  flex: 1;
  min-height: 12px;
}
.plan-entry__line--last::after {
  display: none;
}

/* Priority colors on line segments */
.plan-entry--high .plan-entry__line::after { background: #ef4444; }
.plan-entry--medium .plan-entry__line::after { background: #f97316; }
.plan-entry--low .plan-entry__line::after { background: #9ca3af; }

/* Dashed for non-completed, solid for completed */
.plan-entry--pending .plan-entry__line::after,
.plan-entry--in_progress .plan-entry__line::after {
  border-left: 2px dashed currentColor;
  background: none;
  color: inherit;
}
.plan-entry--high.plan-entry--pending .plan-entry__line::after,
.plan-entry--high.plan-entry--in_progress .plan-entry__line::after { color: #ef4444; }
.plan-entry--medium.plan-entry--pending .plan-entry__line::after,
.plan-entry--medium.plan-entry--in_progress .plan-entry__line::after { color: #f97316; }
.plan-entry--low.plan-entry--pending .plan-entry__line::after,
.plan-entry--low.plan-entry--in_progress .plan-entry__line::after { color: #9ca3af; }

.plan-entry--completed .plan-entry__line::after {
  background: #22c55e;
}

/* Node (status icon) */
.plan-entry__node {
  width: 16px;
  height: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font-size: 12px;
  border-radius: 50%;
}
.plan-entry--high .plan-entry__node { border-color: #ef4444; color: #ef4444; }
.plan-entry--medium .plan-entry__node { border-color: #f97316; color: #f97316; }
.plan-entry--low .plan-entry__node { border-color: #9ca3af; color: #9ca3af; }

.plan-entry__check {
  color: #22c55e;
  font-weight: bold;
  animation: check-in 0.3s ease-out;
}
@keyframes check-in {
  0% { transform: scale(0); opacity: 0; }
  50% { transform: scale(1.3); }
  100% { transform: scale(1); opacity: 1; }
}
.plan-entry__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: pulse 1.5s ease-in-out infinite;
}
.plan-entry__circle {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  border: 1.5px solid currentColor;
}

/* Entry text */
.plan-entry__content {
  font-size: 13px;
  color: var(--color-text-1);
  padding-top: 1px;
  line-height: 1.4;
}
.plan-entry__content--done {
  color: var(--color-text-3);
  text-decoration: line-through;
}
</style>
```

**Step 2: Verify the component compiles**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration/web && npx vue-tsc --noEmit 2>&1 | head -20`
Expected: No errors related to PlanPanel.vue

**Step 3: Commit**

```bash
git add web/src/components/chat/PlanPanel.vue
git commit -m "feat: add PlanPanel component with stepped timeline and collapsed chip"
```

---

### Task 6: Frontend — Integrate PlanPanel into ChatPanelContent

**Files:**
- Modify: `web/src/components/chat/ChatPanelContent.vue`

**Step 1: Add PlanPanel to ChatPanelContent**

In the template, add `PlanPanel` above `ChatInputBar`:

```html
      <PlanPanel
        v-if="hasPlan"
        :entries="planEntries"
        :collapsed="planCollapsed"
        :has-update="planHasUpdate"
        @toggle-collapse="togglePlanCollapse"
      />
```

In the script, add imports and composable usage:

```typescript
import PlanPanel from './PlanPanel.vue'
import { usePlanProgress } from '@/composables/usePlanProgress'
```

Destructure in the setup:

```typescript
const { planEntries, planCollapsed, planHasUpdate, hasPlan, togglePlanCollapse, clearPlanState } = usePlanProgress()
```

In the session switch logic (where `clearModeState()` and `clearCommandState()` are called), also call:

```typescript
clearPlanState()
```

**Step 2: Verify compilation**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration/web && npx vue-tsc --noEmit 2>&1 | head -20`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/components/chat/ChatPanelContent.vue
git commit -m "feat: integrate PlanPanel into ChatPanelContent above input bar"
```

---

### Task 7: Frontend — Add i18n key for plan title

**Files:**
- Modify: `web/src/locales/zh-CN.ts` (or equivalent i18n file)
- Modify: `web/src/locales/en-US.ts` (or equivalent)

**Step 1: Find the i18n files and add keys**

Search for existing i18n files, then add:

```typescript
// zh-CN
'chat.plan.title': '执行计划',

// en-US
'chat.plan.title': 'Plan',
```

**Step 2: Commit**

```bash
git add web/src/locales/
git commit -m "feat: add i18n keys for plan panel"
```

---

### Task 8: E2E Test — Playwright test for PlanPanel

**Files:**
- Create: `e2e/plan-panel.spec.ts` (or the project's e2e directory)

**Step 1: Write the E2E test**

The test needs a running server with an ACP mock agent that emits plan updates. Use the existing `acp-mock` binary or modify it to emit plan events.

Create the Playwright test:

```typescript
import { test, expect } from '@playwright/test'

test.describe('Plan Panel', () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the app and ensure we're on the chat panel
    await page.goto('/')
    // Wait for app to load
    await page.waitForSelector('.chat-panel-content', { timeout: 10000 })
  })

  test('plan panel is hidden when no plan data', async ({ page }) => {
    // No plan panel should be visible initially
    await expect(page.locator('.plan-panel')).toBeHidden()
  })

  test('plan panel appears after plan_update event', async ({ page }) => {
    // Send a message to the ACP mock agent that triggers a plan response
    // (This requires the acp-mock to be configured to emit plan updates)
    const input = page.locator('textarea')
    await input.fill('create a plan for me')
    await input.press('Enter')

    // Wait for plan panel to appear
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })

    // Verify stepped timeline is visible
    await expect(page.locator('.plan-expanded')).toBeVisible()
    await expect(page.locator('.plan-entry')).toHaveCount(3, { timeout: 10000 })
  })

  test('plan panel collapses on toggle click', async ({ page }) => {
    // First get plan data
    const input = page.locator('textarea')
    await input.fill('create a plan for me')
    await input.press('Enter')
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })

    // Click the collapse toggle
    await page.locator('.plan-header__toggle').click()

    // Should now show chip
    await expect(page.locator('.plan-chip')).toBeVisible()
    await expect(page.locator('.plan-expanded')).toBeHidden()
  })

  test('collapsed chip shows current in-progress task', async ({ page }) => {
    // Get plan data and collapse
    const input = page.locator('textarea')
    await input.fill('create a plan for me')
    await input.press('Enter')
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })
    await page.locator('.plan-header__toggle').click()

    // Chip text should contain in-progress task or completion count
    const chipText = page.locator('.plan-chip__text')
    await expect(chipText).not.toBeEmpty()
  })

  test('clicking collapsed chip expands the panel', async ({ page }) => {
    // Get plan data and collapse
    const input = page.locator('textarea')
    await input.fill('create a plan for me')
    await input.press('Enter')
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })
    await page.locator('.plan-header__toggle').click()
    await expect(page.locator('.plan-chip')).toBeVisible()

    // Click chip to expand
    await page.locator('.plan-chip').click()
    await expect(page.locator('.plan-expanded')).toBeVisible()
    await expect(page.locator('.plan-chip')).toBeHidden()
  })

  test('plan panel clears on session switch', async ({ page }) => {
    // Get plan data
    const input = page.locator('textarea')
    await input.fill('create a plan for me')
    await input.press('Enter')
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })

    // Create a new session (via session list button)
    await page.locator('.chat-action-group button:nth-child(2)').click() // + button

    // Plan panel should be hidden in new session
    await expect(page.locator('.plan-panel')).toBeHidden()
  })
})
```

**Step 2: Ensure acp-mock emits plan updates**

Check if `cmd/acp-mock/main.go` needs modification to emit plan events during its `simulateTurn`. If it doesn't emit plan events, add plan emission:

In the mock's turn simulation, add before content chunks:

```go
// Emit plan at the start of the turn
planEntries := []acp.PlanEntry{
    {Content: "Analyze the request", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
    {Content: "Generate response", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
    {Content: "Verify output", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusPending},
}
// Send via session update notification...
```

**Step 3: Run E2E test**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && npx playwright test e2e/plan-panel.spec.ts`
Expected: All PASS (may need server running with acp-mock)

**Step 4: Commit**

```bash
git add e2e/plan-panel.spec.ts cmd/acp-mock/main.go
git commit -m "feat: add E2E test for PlanPanel and plan event emission in acp-mock"
```

---

### Task 9: Final verification

**Step 1: Run all Go tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && go test ./internal/ai/... ./internal/handler/... -v`

**Step 2: Run all frontend tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration/web && npx vitest run`

**Step 3: Build the project**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && ./build.sh`

**Step 4: Verify no regressions — run coverage checks**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/acp-integration && ./scripts/check-go-coverage.sh && ./scripts/check-frontend-coverage.sh`

**Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address any issues from final verification"
```
