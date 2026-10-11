import { describe, it, expect, vi, beforeEach } from 'vitest'
import { nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import TaskDetailPage from '../TaskDetailPage.vue'

// Controllable stub state, hoisted so the vi.mock factory below can close over
// it (vi.mock calls are hoisted above these imports). The page ORs the history
// tab's exposed hasAnyRunning into its Terminate condition, to cover the
// script phase which runningCount excludes.
const historyRunning = vi.hoisted(() => ({ value: false }))

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({
    name,
    props: { size: Number },
    template: `<svg :data-icon="${name}" />`,
  })
  return {
    History: stub('History'),
    Pencil: stub('Pencil'),
    Pause: stub('Pause'),
    Power: stub('Power'),
    Square: stub('Square'),
    Zap: stub('Zap'),
    Trash2: stub('Trash2'),
  }
})

vi.mock('@/composables/useTaskTab', () => ({
  useTaskTab: () => ({ loadTasks: vi.fn() }),
}))

// The action handlers are exercised in useTaskOverview.test.ts; here we only
// care whether the bar offers the manual-run button at all.
vi.mock('@/composables/useTaskOverview.ts', () => ({
  useTaskOverview: () => ({
    actionLoading: ref(false),
    triggerTask: vi.fn(),
    stopTask: vi.fn(),
    pauseTask: vi.fn(),
    resumeTask: vi.fn(),
    deleteTask: vi.fn(),
  }),
}))

vi.mock('@/components/task/TaskBreadcrumb.vue', () => ({
  default: { name: 'TaskBreadcrumb', template: '<div class="breadcrumb-stub" />' },
}))
vi.mock('@/components/common/RefreshButton.vue', () => ({
  default: { name: 'RefreshButton', props: { loading: Boolean, disabled: Boolean }, template: '<button class="refresh-stub" />' },
}))
vi.mock('@/components/task/TaskOverviewTab.vue', () => ({
  default: { name: 'TaskOverviewTab', template: '<div class="overview-stub" />' },
}))
// Controllable stub: the page ORs this component's exposed hasAnyRunning into
// its Terminate condition (to cover the script phase, which runningCount
// excludes). Tests set the value via historyRunning.value before mounting.
vi.mock('@/components/task/TaskHistoryTab.vue', async () => {
  const { computed } = await import('vue')
  return {
    default: {
      name: 'TaskHistoryTab',
      setup(_props: unknown, { expose }: { expose: (v: unknown) => void }) {
        expose({ hasAnyRunning: computed(() => historyRunning.value) })
        return () => null
      },
    },
  }
})

const baseTask = {
  id: 1,
  name: 'A task',
  agentId: 'acp',
  status: 'active',
  triggerMode: 'cron',
  cronExpr: '0 2 * * *',
  repeatMode: 'unlimited',
  maxRuns: 0,
  runCount: 0,
  runningCount: 0,
  unreadCount: 0,
  prompt: 'Do the thing.',
}

function mountWith(overrides: Record<string, unknown> = {}) {
  return mount(TaskDetailPage, { props: { task: { ...baseTask, ...overrides } } })
}

/** Text of every button in the fixed bottom action bar. */
function actionLabels(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper.findAll('.detail-actions button').map(b => b.text())
}

describe('TaskDetailPage manual-run button', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    historyRunning.value = false
  })

  it('offers the run button on an active cron task', () => {
    const labels = actionLabels(mountWith())
    expect(labels).toContain('task.run')
    expect(labels).toContain('task.pause')
  })

  // The regression this guards: a manual run has no event to inject, so the
  // prompt would execute with its {{TITLE}}/{{URL}} placeholders unsubstituted.
  // The backend rejects such a trigger, so the button must not be offered.
  it('hides the run button on an active event task but keeps disable/delete', () => {
    const labels = actionLabels(mountWith({ triggerMode: 'event', eventTypes: 'pr.opened' }))
    expect(labels).not.toContain('task.run')
    expect(labels).toContain('task.pause')
    expect(labels).toContain('task.delete')
  })

  it('hides the run button on a paused event task but keeps enable/delete', () => {
    const labels = actionLabels(mountWith({ status: 'paused', triggerMode: 'event', eventTypes: 'pr.opened' }))
    expect(labels).not.toContain('task.run')
    expect(labels).toContain('task.resume')
    expect(labels).toContain('task.delete')
  })

  // Tasks created before event mode existed carry no triggerMode; the backend
  // also treats an absent value as cron, so the button must stay.
  it('treats an absent triggerMode as cron and keeps the run button', () => {
    const labels = actionLabels(mountWith({ triggerMode: undefined }))
    expect(labels).toContain('task.run')
  })

  // A completed task has no actions other than edit/delete, in either mode.
  it('shows no run button on a completed task', () => {
    const labels = actionLabels(mountWith({ status: 'completed' }))
    expect(labels).not.toContain('task.run')
    expect(labels).toEqual(['common.edit', 'task.delete'])
  })
})

describe('TaskDetailPage terminate button', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    historyRunning.value = false
  })

  // While a run is in flight, the run button is replaced by a terminate button
  // (not merely disabled) — the conventional affordance, and the user's actual
  // intent when a task is running is to stop it.
  it('replaces the run button with terminate on an active task while running', () => {
    const labels = actionLabels(mountWith({ runningCount: 1 }))
    expect(labels).toContain('task.stop')
    expect(labels).not.toContain('task.run')
    // pause/delete stay available
    expect(labels).toContain('task.pause')
    expect(labels).toContain('task.delete')
  })

  it('replaces the run button with terminate on a paused task while running', () => {
    const labels = actionLabels(mountWith({ status: 'paused', runningCount: 1 }))
    expect(labels).toContain('task.stop')
    expect(labels).not.toContain('task.run')
    expect(labels).toContain('task.resume')
  })

  // An event task has no manual run, but it can still be running (an event
  // fired) — so it must offer terminate too.
  it('offers terminate for a running event task even though it has no run button', () => {
    const labels = actionLabels(mountWith({ triggerMode: 'event', eventTypes: 'pr.opened', runningCount: 1 }))
    expect(labels).toContain('task.stop')
    expect(labels).not.toContain('task.run')
  })

  it('shows the run button (not terminate) when nothing is running', () => {
    const labels = actionLabels(mountWith({ runningCount: 0 }))
    expect(labels).toContain('task.run')
    expect(labels).not.toContain('task.stop')
  })

  // RunningCount carries `omitempty`, so an idle task's payload has NO
  // runningCount key — the value is undefined, not 0. The check must treat
  // "absent" as not-running; a refactor to `=== 0` would pass the other tests
  // yet show terminate on every idle task in production.
  it('shows the run button when runningCount is absent (omitempty)', () => {
    const labels = actionLabels(mountWith({ runningCount: undefined }))
    expect(labels).toContain('task.run')
    expect(labels).not.toContain('task.stop')
  })

  // The pre-AI gating script runs before any session exists, so runningCount
  // (AI-phase-only) is 0 while it executes. The history tab's authoritative
  // running list DOES include it, so the bar must still offer terminate —
  // otherwise the user sees a Run button that only 409s (task already running).
  it('offers terminate during the script phase even though runningCount is 0', async () => {
    historyRunning.value = true
    const wrapper = mountWith({ runningCount: 0 })
    // The child ref binds during mount; the parent's computed re-evaluates a
    // tick later (first paint happens before the ref exists).
    await nextTick()
    const labels = actionLabels(wrapper)
    expect(labels).toContain('task.stop')
    expect(labels).not.toContain('task.run')
  })

  // Conversely, an idle task must not show terminate just because the history
  // tab is mounted (hasAnyRunning false).
  it('shows the run button when the history tab reports nothing running', () => {
    historyRunning.value = false
    const labels = actionLabels(mountWith({ runningCount: 0 }))
    expect(labels).toContain('task.run')
    expect(labels).not.toContain('task.stop')
  })
})
