import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskListPage from '../TaskListPage.vue'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// Icons are stubbed with a stable component name so assertions can identify
// them via findComponent({ name }) — scoped-style attribute binding makes
// selector-based lookup on the rendered <svg> unreliable here.
vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({
    name,
    props: { size: Number },
    template: '<svg />',
  })
  return {
    Plus: stub('Plus'),
    CalendarX: stub('CalendarX'),
    Clock: stub('Clock'),
    Repeat: stub('Repeat'),
    CheckCheck: stub('CheckCheck'),
    Zap: stub('Zap'),
  }
})

vi.mock('@/composables/useTaskTab', () => ({
  useTaskTab: () => ({ loadTasks: vi.fn(), markAllTasksRead: vi.fn() }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    loadAgents: vi.fn(),
    getAgentBackend: () => 'acp',
    getAgentName: () => 'test-agent',
  }),
}))

const { mockStore } = vi.hoisted(() => ({
  mockStore: { state: { tasks: [] as unknown[], taskUnreadCount: 0 } },
}))
vi.mock('@/stores/app', () => ({ store: mockStore }))

vi.mock('@/utils/format', () => ({
  humanizeCron: (cron: string) => `cron:${cron}`,
  repeatLabel: () => 'repeat',
  statusLabel: (s: string) => `status:${s}`,
  formatDateTimeWithYear: (t: string) => `year:${t}`,
}))

vi.mock('@/utils/forgeEventLabels', () => ({
  eventTypesSummary: () => 'issue · opened',
}))

vi.mock('@/components/task/TaskBreadcrumb.vue', () => ({
  default: { name: 'TaskBreadcrumb', template: '<div />' },
}))
vi.mock('@/components/common/RefreshButton.vue', () => ({
  default: { name: 'RefreshButton', template: '<button />' },
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', template: '<span />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<span />' },
}))

function makeTask(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'A task',
    agentId: 'acp',
    status: 'active',
    cronExpr: '0 2 * * *',
    repeatMode: 'unlimited',
    maxRuns: 0,
    runCount: 0,
    runningCount: 0,
    unreadCount: 0,
    triggerMode: 'cron',
    ...overrides,
  }
}

function mountWith(tasks: unknown[]) {
  mockStore.state.tasks = tasks
  return mount(TaskListPage)
}

describe('TaskListPage — trigger-type distinction', () => {
  beforeEach(() => {
    mockStore.state.tasks = []
    mockStore.state.taskUnreadCount = 0
  })

  it('badges a cron task and an event task differently', () => {
    const wrapper = mountWith([
      makeTask({ id: 1, triggerMode: 'cron' }),
      makeTask({ id: 2, triggerMode: 'event', eventTypes: 'issue.opened' }),
    ])

    const badges = wrapper.findAll('.task-trigger-badge')
    expect(badges).toHaveLength(2)
    expect(badges[0].classes()).toContain('is-cron')
    expect(badges[1].classes()).toContain('is-event')
  })

  // An absent triggerMode is the cron default: the backend omits the field for
  // tasks created before the event mode existed.
  it('treats an absent triggerMode as cron', () => {
    const wrapper = mountWith([makeTask({ id: 1, triggerMode: undefined })])

    const badge = wrapper.find('.task-trigger-badge')
    expect(badge.classes()).toContain('is-cron')
    expect(badge.classes()).not.toContain('is-event')
  })

  // The regression this guards: a Clock was rendered unconditionally on the
  // summary line, so an event task — which has no schedule at all — appeared to
  // have one.
  it('never shows a clock icon on an event task', () => {
    const wrapper = mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    const summary = wrapper.find('.task-item-next')
    expect(summary.findComponent({ name: 'Zap' }).exists()).toBe(true)
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(false)
  })

  it('shows the next-run time, with a clock, on a cron task', () => {
    const wrapper = mountWith([
      makeTask({ id: 1, triggerMode: 'cron', nextRunAt: '2026-09-14T03:00:00Z' }),
    ])

    const summary = wrapper.find('.task-item-next')
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(true)
    // The mocked t() echoes the key, so the presence of the nextRun key proves
    // this branch rendered rather than the "no next run" fallback.
    expect(summary.text()).toContain('task.nextRun')
    expect(summary.text()).not.toContain('task.nextRunNone')
  })

  // A paused cron task has no next run; the row must still use the clock rather
  // than falling back to an event icon.
  it('keeps the clock for a cron task with no next run', () => {
    const wrapper = mountWith([makeTask({ id: 1, triggerMode: 'cron', nextRunAt: undefined })])

    const summary = wrapper.find('.task-item-next')
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(true)
    expect(summary.findComponent({ name: 'Zap' }).exists()).toBe(false)
  })
})
