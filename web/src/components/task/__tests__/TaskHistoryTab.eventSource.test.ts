import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskHistoryTab from '../TaskHistoryTab.vue'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// The event-source label resolves through the app i18n instance, so a bare
// vue-i18n mock is not enough — importing @/i18n needs createI18n.
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
    template: `<svg :data-icon="'${name}'" />`,
  })
  return {
    Square: stub('Square'),
    History: stub('History'),
    Trash2: stub('Trash2'),
    Zap: stub('Zap'),
  }
})

// Two completed runs: one event-triggered (carries eventUrl), one manual.
const executions = [
  {
    id: 10,
    status: 'completed',
    createdAt: '2026-08-23T02:00:00Z',
    sessionId: 's-10',
    content: '',
    summary: 'Reviewed the PR',
    isUnread: false,
    triggerType: 'event',
    eventUrl: 'https://github.com/acme/widgets/pull/123',
    preview: 'Reviewed the PR',
  },
  {
    id: 11,
    status: 'completed',
    createdAt: '2026-08-22T02:00:00Z',
    sessionId: 's-11',
    content: '',
    summary: 'Manual run',
    isUnread: false,
    triggerType: 'manual',
    preview: 'Manual run',
  },
]

vi.mock('@/composables/useTaskHistory.ts', async () => {
  const { ref } = await import('vue')
  return {
    useTaskHistory: () => ({
      loading: false,
      loadingMore: false,
      hasMore: false,
      allExecutions: ref(executions),
      isRunning: () => false,
      isJustCompleted: () => false,
      loadExecutions: vi.fn().mockResolvedValue(undefined),
      loadMoreExecutions: vi.fn(),
      loadRunningStatus: vi.fn().mockResolvedValue(undefined),
      cancelExecution: vi.fn(),
      deleteExecution: vi.fn(),
      deleteAllExecutions: vi.fn(),
      openDetail: vi.fn(),
      isUnreadDisplay: () => false,
      onTaskChange: vi.fn(),
    }),
  }
})

vi.mock('@/utils/format.ts', () => ({
  formatDuration: (ms: number) => `${ms}ms`,
  formatDateTime: (date: string) => `T:${date}`,
}))

describe('TaskHistoryTab event source', () => {
  const mountTab = () =>
    mount(TaskHistoryTab, { props: { task: { id: 2 } } })

  it('labels an event-triggered run with its issue/PR', () => {
    const wrapper = mountTab()
    const source = wrapper.find('.exec-event-source')
    expect(source.exists()).toBe(true)
    expect(source.text()).toBe('acme/widgets PR #123')
    expect(source.attributes('title')).toBe('https://github.com/acme/widgets/pull/123')
  })

  it('renders exactly one source row for the single event-triggered run', () => {
    const wrapper = mountTab()
    expect(wrapper.findAll('.exec-event-source')).toHaveLength(1)
  })

  it('does not render a source row for a run without an event URL', () => {
    const wrapper = mountTab()
    // The manual run is the second row and must not carry the source chip.
    const rows = wrapper.findAll('.execution-item')
    expect(rows).toHaveLength(2)
    expect(rows[1].find('.exec-event-source').exists()).toBe(false)
  })
})
