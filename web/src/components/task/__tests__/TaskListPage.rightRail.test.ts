import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import TaskListPage from '../TaskListPage.vue'

// ── Mocks (same set as TaskListPage.trigger.test.ts) ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
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
  vi.useFakeTimers()
  const wrapper = mount(TaskListPage)
  await vi.runAllTimersAsync()
  vi.useRealTimers()
  return wrapper
}

/**
 * The card's right rail carries two things that belong to opposite edges: the
 * lifecycle pill at the top and the row actions at the bottom. Putting the
 * quote button in the middle column (where it used to be) left the lower-right
 * corner of every card empty while the button floated vertically centred next
 * to the pill — the row looked half-filled on tall cards.
 *
 * jsdom has no layout engine, so the geometry half of this is a source check
 * (same approach as notifFilterRow.css.test.ts); the DOM half proves the two
 * items actually live in the rail, which is what makes the CSS apply to them.
 */
function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlock(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    if (m[1].trim() === target) return m[2]
  }
  return null
}

const source = readWebFile('src/components/task/TaskListPage.vue')

describe('TaskListPage — right rail fills the card', () => {
  beforeEach(() => {
    mockStore.state.tasks = []
    mockStore.state.taskUnreadCount = 0
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('keeps both the status pill and the quote action in the rail', async () => {
    const wrapper = await mountWith([makeTask()])

    const rail = wrapper.find('.task-item-right')
    expect(rail.exists()).toBe(true)
    expect(rail.find('.task-item-status').exists()).toBe(true)
    expect(rail.find('.task-quote-btn').exists()).toBe(true)
  })

  it('puts the quote action last so it lands on the bottom edge', async () => {
    const wrapper = await mountWith([makeTask()])

    const rail = wrapper.find('.task-item-right')
    const children = rail.element.children
    expect(children).toHaveLength(2)
    expect(children[children.length - 1].classList.contains('task-quote-btn')).toBe(true)
  })

  it('stretches the rail and spreads its items to both edges', () => {
    const decls = baseRule(source, 'task-item-right')
    expect(decls, '.task-item-right rule must exist').not.toBeNull()
    // `stretch` makes the rail as tall as the left column; `space-between`
    // then pins the pill to the top and the action to the bottom. Dropping
    // either one puts the button back in the vertical middle.
    expect(decls).toMatch(/align-self:\s*stretch/)
    expect(decls).toMatch(/justify-content:\s*space-between/)
  })

  it('does not re-introduce a vertical offset that shortens the rail', () => {
    const decls = baseRule(source, 'task-item-right')!
    // A top margin on a stretched item pushes it below the card's top padding
    // and leaves the bottom edge short again.
    expect(decls).not.toMatch(/margin-top:/)
  })
})
