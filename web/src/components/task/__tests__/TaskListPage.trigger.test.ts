import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskListPage from '../TaskListPage.vue'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// The event-label helper (eventTypesSummary) resolves through the app i18n
// instance, so the module-level singleton has to exist for this suite.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
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
    expect(summary.findComponent({ name: 'Zap' }).exists()).toBe(true)
    expect(summary.findComponent({ name: 'Clock' }).exists()).toBe(false)
  })

  // The row states what fires the task. The repository used to be shown here,
  // but every event task in a project watches the same project binding, so the
  // repository said nothing about *this* task — the subscription does.
  it('lists the subscribed events on an event task', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    const row = wrapper.find('.task-item-events')
    expect(row.text()).toBe('task.form.eventKindPr · task.form.eventOpened')
  })

  it('renders every subscribed event, joined on one line', async () => {
    const wrapper = await mountWith([
      makeTask({ id: 1, triggerMode: 'event', eventTypes: 'issue.opened,pr.merged' }),
    ])

    const row = wrapper.find('.task-item-events')
    expect(row.text()).toBe(
      'task.form.eventKindIssue · task.form.eventOpened · ' +
      'task.form.eventKindPr · task.form.eventMerged',
    )
    // The full text must stay reachable when the cell truncates it.
    expect(row.attributes('title')).toBe(row.text())
  })

  // An event task is rejected at creation without a subscription, so this only
  // covers a row that predates the validation or was edited around it. The row
  // must not render an empty cell.
  it('falls back to a placeholder when no event is configured', async () => {
    const wrapper = await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: '' })])

    expect(wrapper.find('.task-item-events').text()).toBe('task.form.eventTypesNone')
  })

  // The list is a per-task view; it must not resolve the project binding at
  // all (it used to, to label each event row). Any lookup here would be a
  // regression to a redundant request.
  it('does not query the forge binding for the list', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    await mountWith([makeTask({ id: 1, triggerMode: 'event', eventTypes: 'pr.opened' })])

    const bindingCalls = fetchSpy.mock.calls.filter(c => String(c[0]).includes('/forge/binding'))
    expect(bindingCalls).toHaveLength(0)
    fetchSpy.mockRestore()
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

  // The regression this guards: the subscription list was once rendered into
  // the shared meta line, where .cron span's max-width truncated a long
  // subscription mid-word and pushed the neighbouring repeat label far to the
  // right — so event rows never lined up with cron rows. It now lives on its
  // own line, which is why the meta line stays cron-only.
  it('does not render the event subscription in the shared meta line', async () => {
    const wrapper = await mountWith([
      makeTask({
        id: 1,
        triggerMode: 'event',
        eventTypes: 'pr.opened,issue.opened,issue.closed,pr.merged',
      }),
    ])

    expect(wrapper.find('.task-item-meta').exists()).toBe(false)
    expect(wrapper.find('.task-item-events').exists()).toBe(true)
  })
})
