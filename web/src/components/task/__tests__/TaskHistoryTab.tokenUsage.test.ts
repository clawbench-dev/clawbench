import { describe, it, expect, vi, afterEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import TaskHistoryTab from '../TaskHistoryTab.vue'

// ── Mocks ──
// The i18n stub keeps the params so the tooltip can be asserted without
// pulling in the real locale files.
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}|${params.input}/${params.output}` : key,
  }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key, locale: { value: 'en' } } },
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({
    name,
    props: { size: Number },
    template: `<svg :data-icon="'${name}'" />`,
  })
  return {
    Square: stub('Square'),
    Loader2: stub('Loader2'),
    LoaderCircle: stub('LoaderCircle'),
    History: stub('History'),
    Trash2: stub('Trash2'),
    Zap: stub('Zap'),
  }
})

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({ onEvent: vi.fn(() => vi.fn()) }),
}))

vi.mock('@/utils/format.ts', () => ({
  formatDuration: (ms: number) => `${ms}ms`,
  formatDateTime: (date: string) => `T:${date}`,
}))

const executions = ref<any[]>([])

vi.mock('@/composables/useTaskHistory.ts', async () => {
  const { ref } = await import('vue')
  return {
    useTaskHistory: () => ({
      loading: false,
      loadingMore: false,
      hasMore: false,
      allExecutions: executions,
      isRunning: () => false,
      isScriptPhase: () => false,
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

let wrapper: ReturnType<typeof mount> | null = null

function mountWith(metadata: Record<string, unknown>) {
  executions.value = [
    {
      id: 30,
      status: 'completed',
      createdAt: '2026-08-23T02:00:00Z',
      sessionId: 's-30',
      preview: 'done',
      isUnread: false,
      triggerType: 'auto',
      metadata,
    },
  ]
  wrapper = mount(TaskHistoryTab, { props: { task: { id: 2 } } })
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('TaskHistoryTab token usage chip', () => {
  it('compacts large counts instead of printing them in full', () => {
    // A long run reports six-figure counts; spelled out they crowd the model
    // name out of the row, which is why the chip goes through the shared
    // formatter (999.5 -> "1.0K", 999_950 -> "1.0M").
    const w = mountWith({ inputTokens: 123_456, outputTokens: 678 })
    const chips = w.findAll('.exec-meta-tag')

    expect(chips).toHaveLength(1)
    expect(chips[0].text()).toBe('123.5K↑ 678↓')
  })

  it('keeps the exact counts in the tooltip', () => {
    // Compacting rounds, so the hover text is the only place the real numbers
    // survive — and it must be thousands-separated like the stats page.
    const w = mountWith({ inputTokens: 123_456, outputTokens: 678 })

    expect(w.find('.exec-meta-tag').attributes('title')).toBe(
      'chat.message.tokenUsed|123,456/678',
    )
  })

  it('shows only the direction that has a count', () => {
    const w = mountWith({ inputTokens: 0, outputTokens: 2500 })

    expect(w.find('.exec-meta-tag').text()).toBe('2.5K↓')
  })

  it('renders no chip when the run reported no tokens', () => {
    const w = mountWith({ model: 'some-model' })

    expect(w.findAll('.exec-meta-tag').map(c => c.text())).toEqual(['some-model'])
  })
})
