import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskListPage from '../TaskListPage.vue'
import { resetForgeBindingState } from '@/composables/useForgeBinding'

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
    GitBranch: stub('GitBranch'),
  }
})

// The list resolves the project's forge binding once to label every event row.
const { mockFetchForgeBinding } = vi.hoisted(() => ({
  mockFetchForgeBinding: vi.fn(),
}))
vi.mock('@/utils/forgeApi', () => ({
  fetchForgeBinding: mockFetchForgeBinding,
}))

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

// mount() runs onMounted(refresh), which awaits a 600ms timer that only exists
// to keep the refresh spinner visible. Left pending it outlives the test and
// surfaces as an "Async Leaks" report, so drain it here: fake timers stop it
// from ever reaching the real clock, and runAllTimersAsync resolves it so the
// promise chain settles before the test ends.
async function mountWith(tasks: unknown[]) {
  mockStore.state.tasks = tasks
  vi.useFakeTimers()
  const wrapper = mount(TaskListPage)
  await vi.runAllTimersAsync()
  vi.useRealTimers()
  return wrapper
}

describe('TaskListPage — trigger-type distinction', () => {
  beforeEach(() => {
    mockStore.state.tasks = []
    mockStore.state.taskUnreadCount = 0
    mockFetchForgeBinding.mockReset()
    // The binding lives in a module-level singleton that survives between
    // tests, and it caches for a few seconds. Drop it so each test starts from
    // a clean lookup — a real project switch does the same via App.vue.
    resetForgeBindingState()
    mockFetchForgeBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' },
    })
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('badges a cron task and an event task differently', async () => {
    const wrapper = await mountWith([
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
  it('treats an absent triggerMode as cron', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: undefined })])

    const badge = wrapper.find('.task-trigger-badge')
    expect(badge.classes()).toContain('is-cron')
    expect(badge.classes()).not.toContain('is-event')
  })

  // The regression this guards: a Clock was rendered unconditionally on the
  // summary line, so an event task — which has no schedule at all — appeared to
  // have one.
  it('never shows a clock icon on an event task', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    const summary = wrapper.find('.task-item-next')
    expect(summary.findComponent({ name: 'GitBranch' }).exists()).toBe(true)
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(false)
  })

  // Every event task watches its project's binding, so the row names that
  // repository instead of repeating the trigger type already shown by the badge.
  it('shows the project-bound repository on an event task', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    await vi.waitFor(() => {
      expect(wrapper.find('.task-item-repo').text()).toBe('acme/widgets')
    })
  })

  // An unbound project can still host an event task (creation is allowed with a
  // warning), but it will never fire. The row must say so rather than showing a
  // stale or fabricated repository.
  it('shows the unbound label when the project has no binding', async () => {
    mockFetchForgeBinding.mockResolvedValue({ binding: null })
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    await vi.waitFor(() => {
      expect(wrapper.find('.task-item-repo').text()).toBe('task.form.eventRepoUnbound')
    })
  })

  // Regression: the row rendered `boundRepoLabel || unbound`, and the label
  // started at '', so an in-flight lookup claimed the project was unbound.
  it('does not claim the project is unbound while the lookup is in flight', async () => {
    let release: (v: unknown) => void = () => {}
    mockFetchForgeBinding.mockReturnValue(new Promise(resolve => { release = resolve }))

    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    const row = wrapper.find('.task-item-repo')
    expect(row.text()).not.toBe('task.form.eventRepoUnbound')
    expect(row.text()).toBe('common.loading')

    release({ binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' } })
    await vi.waitFor(() => {
      expect(wrapper.find('.task-item-repo').text()).toBe('acme/widgets')
    })
  })

  it('shows the next-run time, with a clock, on a cron task', async () => {
    const wrapper = await mountWith([
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
  it('keeps the clock for a cron task with no next run', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'cron', nextRunAt: undefined })])

    const summary = wrapper.find('.task-item-next')
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(true)
    expect(summary.findComponent({ name: 'Zap' }).exists()).toBe(false)
  })

  // The schedule/repeat line is cron-only. An event task has no schedule, and
  // its repeat mode is inert (the backend never exhausts it), so rendering
  // "不限次数" for one states something that is not true.
  it('omits the schedule and repeat line on an event task', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    expect(wrapper.find('.task-item-meta').exists()).toBe(false)
    expect(wrapper.find('.meta-item.repeat').exists()).toBe(false)
  })

  it('keeps the schedule and repeat line on a cron task', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'cron', cronExpr: '0 2 * * *' })])

    const meta = wrapper.find('.task-item-meta')
    expect(meta.exists()).toBe(true)
    expect(meta.text()).toContain('cron:0 2 * * *')
    expect(meta.text()).toContain('repeat')
  })

  // The regression this guards: the subscription list was rendered into the
  // shared meta line, where .cron span's max-width truncated a long
  // subscription mid-word and pushed the neighbouring repeat label far to the
  // right — so event rows never lined up with cron rows. The detail now lives
  // in the task overview, and nothing subscription-related may return here.
  it('does not render the event subscription in the list row', async () => {
    const wrapper = await mountWith([
      makeTask({
        id: 1,
        triggerMode: 'event',
        eventTypes: 'pr.opened,issue.opened,issue.closed,pr.merged',
      }),
    ])

    const row = wrapper.find('.task-item')
    expect(row.text()).not.toContain('issue')
    expect(row.text()).not.toContain('opened')
    // The repository is the only trigger-related detail the row carries.
    expect(row.find('.task-item-repo').exists()).toBe(true)
  })
})
