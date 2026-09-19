import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
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

const mockLoadRunningStatus = vi.fn().mockResolvedValue(undefined)

vi.mock('@/composables/useTaskHistory.ts', async () => {
  const { ref } = await import('vue')
  return {
    useTaskHistory: () => ({
      loading: false,
      loadingMore: false,
      hasMore: false,
      allExecutions: ref([]),
      isRunning: () => false,
      isJustCompleted: () => false,
      loadExecutions: vi.fn().mockResolvedValue(undefined),
      loadMoreExecutions: vi.fn(),
      loadRunningStatus: mockLoadRunningStatus,
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

// A registry-aware fake: the component's unsubscribe must actually remove the
// handler, which is the contract the real useGlobalEvents provides.
const registry: Array<(event: string, data: unknown) => void> = []
const unsubscribeSpy = vi.fn()

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (h: (event: string, data: unknown) => void) => {
      registry.push(h)
      return () => {
        unsubscribeSpy()
        const i = registry.indexOf(h)
        if (i >= 0) registry.splice(i, 1)
      }
    },
  }),
}))

/** Deliver an event the way the real dispatcher would: via the registry. */
function dispatch(event: string, data: unknown) {
  for (const h of [...registry]) h(event, data)
}

let wrapper: ReturnType<typeof mount> | null = null

/** Mount and let the immediate watcher (and its mount-time sync) settle.
 *  The mount-time loadRunningStatus call is expected; callers that count calls
 *  must clear the spy after this returns. */
async function mountTab(taskId = 2) {
  wrapper = mount(TaskHistoryTab, { props: { task: { id: taskId } } })
  await nextTick()
  return wrapper
}

describe('TaskHistoryTab task_update subscription', () => {
  beforeEach(() => {
    registry.length = 0
    unsubscribeSpy.mockClear()
    mockLoadRunningStatus.mockClear()
    vi.useFakeTimers()
  })

  afterEach(() => {
    // Always unmount: a failing assertion must not leak the window
    // 'clawbench-reconnect' listener into the next test.
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
  })

  it('registers a handler and syncs running status for its own task', async () => {
    await mountTab(2)
    expect(registry).toHaveLength(1)
    mockLoadRunningStatus.mockClear() // drop the mount-time sync

    // Server sends task_id as a string; props.task.id is a number.
    dispatch('task_update', { task_id: '2', status: 'running' })
    vi.advanceTimersByTime(200)

    expect(mockLoadRunningStatus).toHaveBeenCalledTimes(1)
  })

  it('ignores updates for a different task', async () => {
    await mountTab(2)
    mockLoadRunningStatus.mockClear()

    dispatch('task_update', { task_id: '99', status: 'running' })
    vi.advanceTimersByTime(200)

    expect(mockLoadRunningStatus).not.toHaveBeenCalled()
  })

  it('ignores non-task_update events', async () => {
    await mountTab(2)
    mockLoadRunningStatus.mockClear()

    dispatch('session_update', { task_id: '2', status: 'running' })
    vi.advanceTimersByTime(200)

    expect(mockLoadRunningStatus).not.toHaveBeenCalled()
  })

  it('coalesces a burst of events into a single sync', async () => {
    await mountTab(2)
    mockLoadRunningStatus.mockClear()

    // A running → completed transition arrives back-to-back.
    dispatch('task_update', { task_id: '2', status: 'running' })
    dispatch('task_update', { task_id: '2', status: 'completed' })
    dispatch('task_update', { task_id: '2', status: 'completed' })
    vi.advanceTimersByTime(200)

    expect(mockLoadRunningStatus).toHaveBeenCalledTimes(1)
  })

  it('resyncs on WS reconnect', async () => {
    await mountTab(2)
    mockLoadRunningStatus.mockClear()

    // `running` events are not persisted for replay, so a reconnect must resync
    // from the authoritative endpoint.
    window.dispatchEvent(new CustomEvent('clawbench-reconnect'))
    vi.advanceTimersByTime(200)

    expect(mockLoadRunningStatus).toHaveBeenCalledTimes(1)
  })

  it('tears down the subscription and listeners on unmount', async () => {
    await mountTab(2)

    wrapper!.unmount()
    wrapper = null

    // The handler must be gone from the registry, so a late event cannot reach it.
    expect(unsubscribeSpy).toHaveBeenCalled()
    expect(registry).toHaveLength(0)

    // Neither a late event nor a reconnect may trigger a sync after unmount.
    mockLoadRunningStatus.mockClear()
    dispatch('task_update', { task_id: '2', status: 'running' })
    window.dispatchEvent(new CustomEvent('clawbench-reconnect'))
    vi.advanceTimersByTime(500)

    expect(mockLoadRunningStatus).not.toHaveBeenCalled()
  })
})
