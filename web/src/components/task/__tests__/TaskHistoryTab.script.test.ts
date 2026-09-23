import { describe, it, expect, vi, afterEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import TaskHistoryTab from '../TaskHistoryTab.vue'

// ── Mocks ──
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

// The executions fed to the component are swapped per test so one file can
// cover both the `skipped` completed row and the script-phase running row.
const executions = ref<any[]>([])
const mockCancelExecution = vi.fn()

vi.mock('@/composables/useTaskHistory.ts', async () => {
  const { ref } = await import('vue')
  return {
    useTaskHistory: () => ({
      loading: false,
      loadingMore: false,
      hasMore: false,
      allExecutions: executions,
      isRunning: (exec: Record<string, unknown>) => exec.status === 'running',
      // Mirrors the real helper: only the backend's `phase: "script"` marker
      // identifies the pre-AI phase.
      isScriptPhase: (exec: Record<string, unknown>) => exec.phase === 'script',
      isJustCompleted: () => false,
      loadExecutions: vi.fn().mockResolvedValue(undefined),
      loadMoreExecutions: vi.fn(),
      loadRunningStatus: vi.fn().mockResolvedValue(undefined),
      cancelExecution: mockCancelExecution,
      deleteExecution: vi.fn(),
      deleteAllExecutions: vi.fn(),
      openDetail: vi.fn(),
      isUnreadDisplay: () => false,
      onTaskChange: vi.fn(),
    }),
  }
})

let wrapper: ReturnType<typeof mount> | null = null

function mountWith(execs: any[]) {
  executions.value = execs
  wrapper = mount(TaskHistoryTab, { props: { task: { id: 2 } } })
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  mockCancelExecution.mockClear()
})

describe('TaskHistoryTab skipped execution', () => {
  it('shows the skipped badge instead of "no text output"', () => {
    // A skip produced no AI output by design, so the row must say so rather
    // than implying a failed/empty run.
    const w = mountWith([
      {
        id: 20,
        status: 'skipped',
        createdAt: '2026-08-23T02:00:00Z',
        sessionId: '',
        content: '',
        isUnread: false,
        triggerType: 'auto',
      },
    ])

    expect(w.find('.exec-status-badge.skipped').exists()).toBe(true)
    expect(w.find('.exec-status-badge.skipped').text()).toBe('task.exec.statusSkipped')
    // The empty-summary copy is the skip-specific one.
    expect(w.find('.exec-summary').text()).toBe('task.exec.skippedHint')
  })

  it('does not badge a completed run as skipped', () => {
    const w = mountWith([
      {
        id: 21,
        status: 'completed',
        createdAt: '2026-08-23T02:00:00Z',
        sessionId: 's-21',
        content: '',
        isUnread: false,
        triggerType: 'auto',
      },
    ])

    expect(w.find('.exec-status-badge.skipped').exists()).toBe(false)
    expect(w.find('.exec-summary').text()).toBe('task.exec.noTextOutput')
  })
})

describe('TaskHistoryTab script-phase running row', () => {
  it('labels the pre-AI script row distinctly and still offers cancel', () => {
    // The backend keys a script-phase run by "script-<taskID>" and emits no
    // task_update for it, so this row only ever arrives via the polling sync.
    // It must be labelled as preparing (not "running") and remain cancellable.
    const w = mountWith([
      {
        id: 'script-2',
        startedAt: '2026-08-23T02:00:00Z',
        triggerType: 'auto',
        status: 'running',
        phase: 'script',
      },
    ])

    const badge = w.find('.exec-status-badge.running')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('task.exec.statusScriptPhase')

    const cancel = w.find('.cancel-exec-btn')
    expect(cancel.exists()).toBe(true)
    cancel.trigger('click')
    // cancelExecution receives the running map key, which is what the backend
    // looks the execution up by.
    expect(mockCancelExecution).toHaveBeenCalledWith('script-2')
  })

  it('labels an AI-phase running row as running', () => {
    const w = mountWith([
      {
        id: 'session-abc',
        startedAt: '2026-08-23T02:00:00Z',
        triggerType: 'auto',
        status: 'running',
        phase: 'ai',
      },
    ])

    expect(w.find('.exec-status-badge.running').text()).toBe('task.exec.running')
  })
})
