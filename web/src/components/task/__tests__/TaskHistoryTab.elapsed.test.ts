import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskHistoryTab from '../TaskHistoryTab.vue'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
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
    History: stub('History'),
    Trash2: stub('Trash2'),
  }
})

// A running execution that started 65s before "now"
const runningExecs = [
  { id: 'session-abc', startedAt: new Date(Date.now() - 65000).toISOString(), triggerType: 'auto', status: 'running' },
]

vi.mock('@/composables/useTaskHistory.ts', async () => {
  const { ref } = await import('vue')
  return {
    useTaskHistory: () => ({
      loading: false,
      loadingMore: false,
      hasMore: false,
      allExecutions: ref(runningExecs),
      isRunning: (exec: Record<string, unknown>) => exec.status === 'running',
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
  formatDuration: (ms: number) => `${Math.round(ms / 1000)}s`,
}))

describe('TaskHistoryTab running execution elapsed time', () => {
  it('renders a friendly elapsed duration for a running execution', () => {
    const wrapper = mount(TaskHistoryTab, {
      props: { task: { id: 1 } },
    })
    const durationEl = wrapper.find('.exec-duration')
    expect(durationEl.exists()).toBe(true)
    // 65s elapsed, allow ±1s for test runtime drift
    expect(durationEl.text()).toMatch(/^6[45]s$/)
    wrapper.unmount()
  })
})
