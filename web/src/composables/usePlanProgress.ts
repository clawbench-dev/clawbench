import { ref, computed } from 'vue'

// ───────────────────────────────────────────────────────────
// Module-level singleton state — shared across the whole app.
// Plan progress is globally needed (ChatPanelContent for
// rendering, useChatStream for SSE event handling) without
// prop drilling.
// ───────────────────────────────────────────────────────────

export interface PlanEntry {
  content: string
  priority: 'high' | 'medium' | 'low'
  status: 'pending' | 'in_progress' | 'completed'
}

const planEntries = ref<PlanEntry[]>([])
const planCollapsed = ref(false)
const planHasUpdate = ref(false)

/** Replace plan entries (ACP replace semantics). Sets planHasUpdate if collapsed. */
export function updatePlanEntries(entries: PlanEntry[]) {
  planEntries.value = entries
  if (planCollapsed.value && entries.length > 0) {
    planHasUpdate.value = true
  }
}

/** Reset all plan state — called on session switch or clear. */
export function clearPlanState() {
  planEntries.value = []
  planCollapsed.value = false
  planHasUpdate.value = false
}

/** Toggle collapsed state; clears planHasUpdate on expand. */
export function togglePlanCollapse() {
  planCollapsed.value = !planCollapsed.value
  if (!planCollapsed.value) {
    planHasUpdate.value = false
  }
}

/** Set collapsed state directly; clears planHasUpdate on expand. */
export function setPlanCollapsed(collapsed: boolean) {
  planCollapsed.value = collapsed
  if (!collapsed) {
    planHasUpdate.value = false
  }
}

// ───────────────────────────────────────────────────────────
// E2E test bridge — expose plan operations on window.__clawbench
// so Playwright/browser-automation can inject plan data.
// ───────────────────────────────────────────────────────────
if (typeof window !== 'undefined') {
  const bridge = (window as any).__clawbench || ((window as any).__clawbench = {})
  bridge.updatePlanEntries = updatePlanEntries
  bridge.clearPlanState = clearPlanState
  bridge.setPlanCollapsed = setPlanCollapsed
  bridge.togglePlanCollapse = togglePlanCollapse
}

// ───────────────────────────────────────────────────────────
// Public composable
// ───────────────────────────────────────────────────────────

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
