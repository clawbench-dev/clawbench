/**
 * Status filter above the task list, and the per-device default that seeds it
 * (设置 → 任务 → 任务列表默认筛选).
 *
 * The rule the tests pin: the configured default applies only while the user
 * has not picked a filter in this view. A change made in settings must reach an
 * already-open list, but it must never undo a choice just made in the toolbar.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import TaskListPage from '../TaskListPage.vue'
import { localConfig } from '@/composables/useSettingsConfig'

// ── Mocks (same set as the other TaskListPage specs) ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// STORAGE_KEY is part of the real module's surface: the settings layer reads it
// at module load (migrateLegacyKeys).
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
  STORAGE_KEY: 'clawbench-locale',
}))

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
    Mail: stub('Mail'),
    MessageSquare: stub('MessageSquare'),
  }
})

vi.mock('@/composables/useTaskTab', () => ({
  useTaskTab: () => ({ loadTasks: vi.fn(), markAllTasksRead: vi.fn() }),
}))

vi.mock('@/composables/useAgents', () => ({
  registerIdentityUpdaters: vi.fn(),
  useAgents: () => ({
    loadAgents: vi.fn(),
    getAgentBackend: () => 'acp',
    getAgentName: () => 'test-agent',
    getAgentAvatar: () => '',
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

async function mountWith(tasks: unknown[]) {
  mockStore.state.tasks = tasks
  // The mount-time refresh awaits a 600ms spinner timer; fake it so the
  // promise settles here instead of leaking past the test.
  vi.useFakeTimers({ toFake: ['setTimeout'] })
  const wrapper = mount(TaskListPage)
  await vi.runAllTimersAsync()
  vi.useRealTimers()
  return wrapper
}

const TASKS = [
  makeTask({ id: 1, name: 'enabled one', status: 'active' }),
  makeTask({ id: 2, name: 'paused one', status: 'paused' }),
  makeTask({ id: 3, name: 'finished one', status: 'completed' }),
]

function visibleNames(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('.task-item-name').map(n => n.text())
}

describe('TaskListPage — status filter', () => {
  beforeEach(() => {
    mockStore.state.tasks = []
    mockStore.state.taskUnreadCount = 0
    localConfig.tasksDefaultFilter = 'all'
  })

  afterEach(() => {
    localConfig.tasksDefaultFilter = 'all'
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('renders the three chips and marks the current one', async () => {
    const wrapper = await mountWith(TASKS)
    const chips = wrapper.findAll('.task-filter-chip')
    expect(chips.map(c => c.text())).toEqual(['task.filter.all', 'task.filter.enabled', 'task.filter.paused'])
    expect(chips[0].classes()).toContain('active')
    expect(chips[0].attributes('aria-pressed')).toBe('true')
    wrapper.unmount()
  })

  it('opens on the configured default filter', async () => {
    localConfig.tasksDefaultFilter = 'paused'
    const wrapper = await mountWith(TASKS)

    expect(visibleNames(wrapper)).toEqual(['paused one'])
    expect(wrapper.find('[data-filter="paused"]').classes()).toContain('active')
    wrapper.unmount()
  })

  it('narrows the list to enabled / paused when a chip is clicked', async () => {
    const wrapper = await mountWith(TASKS)

    await wrapper.find('[data-filter="enabled"]').trigger('click')
    expect(visibleNames(wrapper)).toEqual(['enabled one'])

    await wrapper.find('[data-filter="paused"]').trigger('click')
    expect(visibleNames(wrapper)).toEqual(['paused one'])

    await wrapper.find('[data-filter="all"]').trigger('click')
    // A completed task is neither enabled nor paused, so it only appears here.
    expect(visibleNames(wrapper)).toEqual(['enabled one', 'paused one', 'finished one'])
    wrapper.unmount()
  })

  it('explains an all-filtered-out list and offers the way back', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, status: 'completed' })])

    await wrapper.find('[data-filter="enabled"]').trigger('click')
    expect(wrapper.findAll('.task-item')).toHaveLength(0)
    expect(wrapper.find('.task-filter-empty').text()).toContain('task.filter.empty')

    await wrapper.find('[data-action="task-filter-show-all"]').trigger('click')
    expect(wrapper.findAll('.task-item')).toHaveLength(1)
    wrapper.unmount()
  })

  it('follows a settings change while the user has not picked a filter', async () => {
    const wrapper = await mountWith(TASKS)
    expect(visibleNames(wrapper)).toHaveLength(3)

    localConfig.tasksDefaultFilter = 'enabled'
    await nextTick()

    expect(visibleNames(wrapper)).toEqual(['enabled one'])
    wrapper.unmount()
  })

  it('never overrides a filter the user picked in the toolbar', async () => {
    const wrapper = await mountWith(TASKS)

    await wrapper.find('[data-filter="paused"]').trigger('click')
    // The user's choice wins for the rest of this view, even if settings change.
    localConfig.tasksDefaultFilter = 'enabled'
    await nextTick()

    expect(visibleNames(wrapper)).toEqual(['paused one'])
    expect(wrapper.find('[data-filter="paused"]').classes()).toContain('active')
    wrapper.unmount()
  })

  it('hides the filter bar when there are no tasks at all', async () => {
    const wrapper = await mountWith([])
    expect(wrapper.find('.task-filter-bar').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    wrapper.unmount()
  })
})
