import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import TaskDetailPage from '../TaskDetailPage.vue'

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
vi.mock('@/components/task/TaskHistoryTab.vue', () => ({
  default: { name: 'TaskHistoryTab', template: '<div class="history-stub" />' },
}))

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
