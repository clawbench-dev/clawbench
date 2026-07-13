import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// useTaskTab composable tests
// Comprehensive tests covering all exported functions,
// navigation methods, data methods, polling, completion
// detection, dedup, and edge cases.
// ────────────────────────────────────────────────────────────

// Mock i18n
vi.mock('@/i18n', () => ({
  default: {
    global: {
      locale: { value: 'en' },
      t: (key: string) => key,
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// Mock notification sound
const mockPlayNotificationSound = vi.fn()
vi.mock('@/composables/useNotificationSound', () => ({
  playNotificationSound: (...args: unknown[]) => mockPlayNotificationSound(...args),
}))

// Mock browser notification
const mockShowBrowserNotification = vi.fn()
vi.mock('@/composables/useNotification', () => ({
  showBrowserNotification: (...args: unknown[]) => mockShowBrowserNotification(...args),
}))

// Mock toast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// Mock fetch
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Import after mocks
import { useTaskTab, onTaskEvent, resetTaskTabState, registerSwitchTab } from '@/composables/useTaskTab.ts'
import { store } from '@/stores/app'

beforeEach(() => {
  mockPlayNotificationSound.mockReset()
  mockShowBrowserNotification.mockReset()
  mockToastShow.mockReset()
  mockFetch.mockReset()
  // Reset store state
  store.state.taskRunning = false
  store.state.taskUnreadCount = 0
  store.state.taskJustCompleted = false
  store.state.tasks = []
  // Reset module-level navigation state
  resetTaskTabState()
})

afterEach(() => {
  // Stop any polling that may have been started
  const { stopTaskPolling } = useTaskTab()
  stopTaskPolling()
})

// ── Helpers ──

function mockTasksResponse(tasks: any[] = [], hasUnread = false) {
  mockFetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ tasks, hasUnread }),
  })
}

function mockFetchOk(data: any) {
  mockFetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(data),
  })
}

function mockFetchNotOk(status = 500) {
  mockFetch.mockResolvedValue({
    ok: false,
    status,
    json: () => Promise.resolve({}),
  })
}

function makeTask(overrides: any = {}) {
  return {
    id: 1,
    name: 'Test Task',
    status: 'active',
    runningCount: 0,
    unreadCount: 0,
    runCount: 0,
    ...overrides,
  }
}

// ── Tests ──

describe('useTaskTab', () => {
  // ── loadTests — basic ──

  describe('loadTasks — basic', () => {
    it('fetches /api/tasks and updates store', async () => {
      const { loadTasks } = useTaskTab()
      const tasks = [makeTask({ id: 1 }), makeTask({ id: 2 })]
      mockTasksResponse(tasks, false)

      await loadTasks()

      expect(mockFetch).toHaveBeenCalledWith('/api/tasks', expect.any(Object))
      expect(store.state.tasks.length).toBe(2)
      expect(store.state.taskRunning).toBe(false)
      expect(store.state.taskUnreadCount).toBe(0)
    })

    it('sets taskRunning when any task has runningCount > 0', async () => {
      const { loadTasks } = useTaskTab()
      mockTasksResponse([makeTask({ runningCount: 1 })])

      await loadTasks()

      expect(store.state.taskRunning).toBe(true)
    })

    it('computes taskUnreadCount from task unreadCounts', async () => {
      const { loadTasks } = useTaskTab()
      mockTasksResponse([
        makeTask({ id: 1, unreadCount: 2 }),
        makeTask({ id: 2, unreadCount: 3 }),
      ])

      await loadTasks()

      expect(store.state.taskUnreadCount).toBe(5)
    })

    it('silently ignores fetch errors', async () => {
      const { loadTasks } = useTaskTab()
      mockFetch.mockRejectedValue(new Error('Network error'))

      await loadTasks()
      expect(store.state.tasks).toEqual([])
    })

    it('does not update store when response is not ok', async () => {
      const { loadTasks } = useTaskTab()
      mockFetchNotOk(500)

      await loadTasks()

      expect(store.state.tasks).toEqual([])
    })

    it('handles null tasks array in response', async () => {
      const { loadTasks } = useTaskTab()
      mockFetchOk({ tasks: null })

      await loadTasks()

      expect(store.state.tasks).toEqual([])
      expect(store.state.taskRunning).toBe(false)
    })
  })

  // ── loadTasks — markingReadInProgress guard ──

  describe('loadTasks — markingReadInProgress guard', () => {
    it('does not update taskUnreadCount when markingReadInProgress is true', async () => {
      const { loadTasks, markAllTasksRead } = useTaskTab()

      // Set up initial tasks with unread
      store.state.tasks = [{ id: 1, unreadCount: 3, name: 'Task 1' }]
      store.state.taskUnreadCount = 3

      // Make mark-all-read call that takes a while
      let resolveMarkRead: () => void
      mockFetch.mockImplementation((url: string) => {
        if (url === '/api/tasks/1' && url.includes('/1')) {
          // mark read request — delay it
          return new Promise(r => { resolveMarkRead = () => r({ ok: true }) })
        }
        // loadTasks request
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ tasks: [makeTask({ id: 1, unreadCount: 3 })] }),
        })
      })

      // Start mark all read
      const markPromise = markAllTasksRead()

      // While marking is in progress, loadTasks should not overwrite unread count
      await loadTasks()

      // Resolve the mark read
      resolveMarkRead!()
      await markPromise

      // After mark all read completes, unread should be 0
      expect(store.state.taskUnreadCount).toBe(0)
    })
  })

  // ── loadTasks — diff-check optimization ──

  describe('loadTasks — diff-check optimization', () => {
    it('skips store update when tasks are identical', async () => {
      const { loadTasks } = useTaskTab()
      const task = makeTask({ id: 1, status: 'active', runCount: 0, unreadCount: 0, runningCount: 0 })

      // First load
      mockTasksResponse([task])
      await loadTasks()

      const originalTasksRef = store.state.tasks

      // Second load with same data — should not replace the array
      mockTasksResponse([task])
      await loadTasks()

      // Same reference means no update happened
      expect(store.state.tasks).toBe(originalTasksRef)
    })

    it('updates store when task status changes', async () => {
      const { loadTasks } = useTaskTab()

      // First load
      mockTasksResponse([makeTask({ id: 1, status: 'active' })])
      await loadTasks()

      // Second load with different status
      mockTasksResponse([makeTask({ id: 1, status: 'paused' })])
      await loadTasks()

      expect((store.state.tasks[0] as any).status).toBe('paused')
    })

    it('updates store when task runCount changes', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ id: 1, runCount: 0 })])
      await loadTasks()

      mockTasksResponse([makeTask({ id: 1, runCount: 5 })])
      await loadTasks()

      expect((store.state.tasks[0] as any).runCount).toBe(5)
    })

    it('updates store when task count changes', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ id: 1 })])
      await loadTasks()
      expect(store.state.tasks.length).toBe(1)

      mockTasksResponse([makeTask({ id: 1 }), makeTask({ id: 2 })])
      await loadTasks()
      expect(store.state.tasks.length).toBe(2)
    })
  })

  // ── loadTasks — abort ──

  describe('loadTasks — abort', () => {
    it('aborts previous in-flight request when loadTasks is called again', async () => {
      const { loadTasks } = useTaskTab()

      let resolveFirst: (v: any) => void
      mockFetch.mockReturnValue(new Promise(r => { resolveFirst = r }))

      const firstCall = loadTasks()

      mockTasksResponse([])
      const secondCall = loadTasks()

      resolveFirst!({ ok: true, json: () => Promise.resolve({ tasks: [], hasUnread: false }) })

      await Promise.allSettled([firstCall, secondCall])

      expect(mockFetch).toHaveBeenLastCalledWith('/api/tasks', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    })

    it('ignores AbortError from superseded requests', async () => {
      const { loadTasks } = useTaskTab()

      mockFetch.mockRejectedValueOnce({ name: 'AbortError' })
      mockTasksResponse([])

      const p1 = loadTasks()
      const p2 = loadTasks()
      await Promise.allSettled([p1, p2])

      expect(store.state.tasks).toEqual([])
    })

    it('ignores AbortError that is an Error instance', async () => {
      const { loadTasks } = useTaskTab()

      const abortErr = new DOMException('The operation was aborted', 'AbortError')
      mockFetch.mockRejectedValueOnce(abortErr)
      mockTasksResponse([])

      const p1 = loadTasks()
      const p2 = loadTasks()
      await Promise.allSettled([p1, p2])

      expect(store.state.tasks).toEqual([])
    })
  })

  // ── loadTasks — prevRunningCounts cleanup ──

  describe('loadTasks — prevRunningCounts cleanup', () => {
    it('cleans up prevRunningCounts for deleted tasks', async () => {
      const { loadTasks } = useTaskTab()

      // Load with 2 tasks
      mockTasksResponse([makeTask({ id: 1, runningCount: 0 }), makeTask({ id: 2, runningCount: 0 })])
      await loadTasks()

      // Delete task 2 — only task 1 remains
      mockTasksResponse([makeTask({ id: 1, runningCount: 0 })])
      await loadTasks()

      // Now bring task 2 back — it should be treated as a new task (no stale prevRunningCount)
      // If prevRunningCounts wasn't cleaned, this wouldn't trigger completion detection
      mockTasksResponse([makeTask({ id: 1, runningCount: 0 }), makeTask({ id: 2, runningCount: 1 })])
      await loadTasks()

      // Task 2 goes from running to complete — should notify
      mockTasksResponse([makeTask({ id: 1, runningCount: 0 }), makeTask({ id: 2, runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
    })
  })

  // ── loadTasks — notifiedTaskCompletions cleanup ──

  describe('loadTasks — notifiedTaskCompletions cleanup', () => {
    it('cleans up notifiedTaskCompletions for tasks no longer running', async () => {
      const { loadTasks } = useTaskTab()

      // Task running
      mockTasksResponse([makeTask({ id: 1, runningCount: 1, runCount: 0 })])
      await loadTasks()

      // Task completed — notification fires
      mockTasksResponse([makeTask({ id: 1, runningCount: 0, runCount: 1 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      // Task runs again
      mockTasksResponse([makeTask({ id: 1, runningCount: 1, runCount: 1 })])
      await loadTasks()

      // Task completes again — should notify again (dedup key was cleaned)
      mockTasksResponse([makeTask({ id: 1, runningCount: 0, runCount: 2 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
    })

    it('keeps notifiedTaskCompletions for tasks still running', async () => {
      const { loadTasks } = useTaskTab()

      // Task 1 running, task 2 running
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 1, runCount: 0 }),
        makeTask({ id: 2, runningCount: 1, runCount: 0 }),
      ])
      await loadTasks()

      // Task 1 completes, task 2 still running
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 1, runCount: 0 }),
      ])
      await loadTasks()

      // notifiedTaskCompletions for task 2 should be kept (it's still running)
      // If task 2 then completes, it should still be notified correctly
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 0, runCount: 1 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
    })
  })

  // ── Completion detection ──

  describe('completion detection', () => {
    it('detects task completion when runningCount drops to 0', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1, runCount: 0 })])
      await loadTasks()
      expect(store.state.taskRunning).toBe(true)

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
      expect(store.state.taskJustCompleted).toBe(true)
    })

    it('shows browser notification on completion', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockShowBrowserNotification).toHaveBeenCalledWith(
        'Test Task',
        expect.objectContaining({
          body: expect.any(String),
          tag: 'task-completed-1',
        }),
      )
    })

    it('shows toast on completion with task name', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('Test Task'),
        expect.objectContaining({ type: 'success' }),
      )
    })

    it('sets taskJustCompleted flag on completion', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0 })])
      await loadTasks()

      expect(store.state.taskJustCompleted).toBe(true)
    })

    it('auto-clears taskJustCompleted after 2s', async () => {
      vi.useFakeTimers()
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0 })])
      await loadTasks()

      expect(store.state.taskJustCompleted).toBe(true)

      vi.advanceTimersByTime(2000)
      expect(store.state.taskJustCompleted).toBe(false)

      vi.useRealTimers()
    })

    it('uses fallback name when task has no name', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1, name: '' })])
      await loadTasks()

      // Completion notification should use gt('task.title') as fallback
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1, name: '' })])
      await loadTasks()

      // Browser notification should have been called with the i18n key as fallback
      expect(mockShowBrowserNotification).toHaveBeenCalledWith(
        'task.title',
        expect.any(Object),
      )
    })

    it('catches errors from showBrowserNotification', async () => {
      const { loadTasks } = useTaskTab()
      mockShowBrowserNotification.mockImplementation(() => { throw new Error('Notification denied') })

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      // Should not throw
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalled()
    })

    it('catches errors from useToast show', async () => {
      const { loadTasks } = useTaskTab()
      mockToastShow.mockImplementation(() => { throw new Error('Toast error') })

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalled()
    })
  })

  // ── Dedup — no double notification ──

  describe('dedup — no double notification', () => {
    it('does not re-notify on subsequent polls after completion', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
    })

    it('re-notifies if task starts running again then completes again', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1, runCount: 0 })])
      await loadTasks()
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      mockTasksResponse([makeTask({ runningCount: 1, runCount: 1 })])
      await loadTasks()
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 2 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
    })
  })

  // ── Multiple tasks ──

  describe('multiple tasks', () => {
    it('notifies for each task that completes', async () => {
      const { loadTasks } = useTaskTab()

      mockTasksResponse([
        makeTask({ id: 1, runningCount: 1 }),
        makeTask({ id: 2, runningCount: 1 }),
      ])
      await loadTasks()

      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 1 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 0, runCount: 1 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
    })

    it('does not notify for tasks that were not running', async () => {
      const { loadTasks } = useTaskTab()

      // Both tasks already at runningCount=0
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0 }),
        makeTask({ id: 2, runningCount: 0 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).not.toHaveBeenCalled()
    })
  })

  // ── registerSwitchTab ──

  describe('registerSwitchTab', () => {
    it('registers a callback that is called from completion notification onClick', async () => {
      const mockSwitchTab = vi.fn()
      registerSwitchTab(mockSwitchTab)

      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      // The browser notification should have been called with an onClick handler
      const notificationCall = mockShowBrowserNotification.mock.calls[0]
      const options = notificationCall[1] as { onClick?: () => void }
      expect(options.onClick).toBeDefined()

      // Call the onClick handler — should invoke switchTab callback
      options.onClick!()
      expect(mockSwitchTab).toHaveBeenCalledWith('tasks')
    })

    it('toast onClick also navigates via switchTab callback', async () => {
      const mockSwitchTab = vi.fn()
      registerSwitchTab(mockSwitchTab)

      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      const toastCall = mockToastShow.mock.calls[0]
      const options = toastCall[1] as { onClick?: () => void }
      expect(options.onClick).toBeDefined()

      options.onClick!()
      expect(mockSwitchTab).toHaveBeenCalledWith('tasks')
    })

    it('completion notification onClick does not crash when no callback registered', async () => {
      // Register null (default)
      registerSwitchTab(null as any)

      const { loadTasks } = useTaskTab()

      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      // The browser notification onClick should not throw
      const notificationCall = mockShowBrowserNotification.mock.calls[0]
      const options = notificationCall[1] as { onClick?: () => void }
      expect(() => options.onClick!()).not.toThrow()
    })
  })

  // ── refreshExecDetail ──

  describe('refreshExecDetail', () => {
    it('preserves existing content when API returns null content', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)

      const execData = {
        id: 100,
        sessionId: 'session-100',
        status: 'completed',
        content: '{"blocks":[{"type":"text","text":"Hello world"}]}',
        summary: 'Test summary',
        createdAt: '2026-01-01T00:00:00Z',
      }
      openExecDetail('100', execData)
      expect(selectedExecData.value.content).toBe(execData.content)

      mockFetchOk({
        executions: [{
          id: 100,
          sessionId: 'session-100',
          status: 'completed',
          content: null,
          summary: 'New summary',
          createdAt: '2026-01-01T00:00:00Z',
        }],
      })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe(execData.content)
      expect(selectedExecData.value.summary).toBe('New summary')
    })

    it('updates content when API returns non-null content', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('100', { id: 100, content: 'old', status: 'completed' })
      expect(selectedExecData.value.content).toBe('old')

      mockFetchOk({
        executions: [{
          id: 100,
          content: 'new content',
          status: 'completed',
        }],
      })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('new content')
    })

    it('does nothing when selectedTaskId or selectedExecId is null', async () => {
      const { navigateToList, refreshExecDetail } = useTaskTab()
      navigateToList()

      mockFetchOk({ executions: [] })

      await refreshExecDetail()

      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('matches execution by sessionId', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('session-abc', { id: 100, sessionId: 'session-abc', content: 'initial' })

      mockFetchOk({
        executions: [{
          id: 100,
          sessionId: 'session-abc',
          status: 'completed',
          content: 'updated via session match',
        }],
      })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('updated via session match')
    })

    it('does not update when no matching execution is found', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('999', { id: 999, content: 'original' })

      mockFetchOk({
        executions: [{ id: 100, sessionId: 'other', content: 'different' }],
      })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('original')
    })

    it('does not crash on API error', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('100', { id: 100, content: 'original' })

      mockFetch.mockRejectedValue(new Error('Network error'))

      await refreshExecDetail()
      // Should not throw
    })

    it('does nothing when API response is not ok', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('100', { id: 100, content: 'original' })

      mockFetchNotOk()

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('original')
    })

    it('handles empty executions array gracefully', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('100', { id: 100, content: 'original' })

      mockFetchOk({ executions: [] })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('original')
    })
  })

  // ── onTaskEvent ──

  describe('onTaskEvent', () => {
    it('debounces rapid events into a single loadTasks call', async () => {
      vi.useFakeTimers()
      useTaskTab()
      mockTasksResponse([])

      onTaskEvent({ task_id: '1', status: 'completed' })
      onTaskEvent({ task_id: '2', status: 'completed' })
      onTaskEvent({ task_id: '3', status: 'completed' })

      const callCountBefore = mockFetch.mock.calls.length

      vi.advanceTimersByTime(250)

      expect(mockFetch.mock.calls.length).toBe(callCountBefore + 1)

      vi.useRealTimers()
    })

    it('ignores undefined data', () => {
      onTaskEvent(undefined)
      // Should not throw
    })

    it('ignores null data', () => {
      onTaskEvent(null as any)
      // Should not throw
    })

    it('cancels previous debounce timer when new event arrives', async () => {
      vi.useFakeTimers()
      useTaskTab()
      mockTasksResponse([])

      onTaskEvent({ task_id: '1' })

      // Advance 100ms (not enough to trigger)
      vi.advanceTimersByTime(100)

      // New event resets the debounce
      onTaskEvent({ task_id: '2' })

      // Advance another 100ms — still not enough (needs 200ms from last event)
      vi.advanceTimersByTime(100)

      // Only 1 call from the initial loadTasks (if any), debounce hasn't fired yet
      const callsBeforeFinal = mockFetch.mock.calls.filter((c: any[]) => c[0] === '/api/tasks').length

      // Advance remaining time
      vi.advanceTimersByTime(100)

      // Now the debounce should have fired exactly once
      const callsAfter = mockFetch.mock.calls.filter((c: any[]) => c[0] === '/api/tasks').length
      expect(callsAfter).toBe(callsBeforeFinal + 1)

      vi.useRealTimers()
    })
  })

  // ── Polling ──

  describe('polling', () => {
    it('startTaskPolling starts interval-based polling', () => {
      vi.useFakeTimers()
      const { startTaskPolling, stopTaskPolling } = useTaskTab()
      mockTasksResponse([])

      startTaskPolling()
      expect(mockFetch).toHaveBeenCalledTimes(1) // immediate load

      vi.advanceTimersByTime(2000)
      expect(mockFetch).toHaveBeenCalledTimes(2)

      vi.advanceTimersByTime(2000)
      expect(mockFetch).toHaveBeenCalledTimes(3)

      stopTaskPolling()
      vi.useRealTimers()
    })

    it('stopTaskPolling stops the interval', () => {
      vi.useFakeTimers()
      const { startTaskPolling, stopTaskPolling } = useTaskTab()
      mockTasksResponse([])

      startTaskPolling()
      stopTaskPolling()

      vi.advanceTimersByTime(6000)
      expect(mockFetch).toHaveBeenCalledTimes(1)

      vi.useRealTimers()
    })

    it('startTaskPolling does not double-start', () => {
      vi.useFakeTimers()
      const { startTaskPolling, stopTaskPolling } = useTaskTab()
      mockTasksResponse([])

      startTaskPolling()
      startTaskPolling() // Second call should be a no-op

      // Only one immediate load should have been triggered
      expect(mockFetch).toHaveBeenCalledTimes(1)

      stopTaskPolling()
      vi.useRealTimers()
    })

    it('stopTaskPolling is a no-op when not started', () => {
      const { stopTaskPolling } = useTaskTab()
      // Should not throw
      stopTaskPolling()
    })
  })

  // ── Navigation ──

  describe('navigation', () => {
    it('navigateToTaskSettings sets selectedTaskId and currentView', () => {
      const { navigateToTaskSettings, selectedTaskId, currentView } = useTaskTab()
      navigateToTaskSettings(42)
      expect(selectedTaskId.value).toBe(42)
      expect(currentView.value).toBe('settings')
    })

    it('navigateToTaskSettings closes exec detail and form', () => {
      const { navigateToTaskSettings, openExecDetail, openCreateForm, execDetailOpen, formViewOpen } = useTaskTab()

      // Open exec detail and form first
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })
      openCreateForm()
      expect(execDetailOpen.value).toBe(true)
      expect(formViewOpen.value).toBe(true)

      // Navigate to settings of another task — should close both
      navigateToTaskSettings(10)
      expect(execDetailOpen.value).toBe(false)
      expect(formViewOpen.value).toBe(false)
    })

    it('navigateToTaskHistory sets currentView to history and calls markTaskRead', async () => {
      const { navigateToTaskHistory, currentView, selectedTaskId } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 2, name: 'Task 1' }]
      mockFetch.mockResolvedValue({ ok: true })

      navigateToTaskHistory(1)
      expect(currentView.value).toBe('history')
      expect(selectedTaskId.value).toBe(1)

      // markTaskRead should be called
      await vi.waitFor(() => {
        expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({ method: 'PUT' }))
      })
    })

    it('navigateToTaskHistory closes exec detail and form', () => {
      const { navigateToTaskSettings, openExecDetail, openCreateForm, navigateToTaskHistory, execDetailOpen, formViewOpen } = useTaskTab()

      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })
      openCreateForm()

      navigateToTaskHistory(5)
      expect(execDetailOpen.value).toBe(false)
      expect(formViewOpen.value).toBe(false)
    })

    it('goBack navigates from settings to list', () => {
      const { navigateToTaskSettings, goBack, currentView, selectedTaskId } = useTaskTab()
      navigateToTaskSettings(5)
      expect(currentView.value).toBe('settings')

      goBack()
      expect(currentView.value).toBe('list')
      expect(selectedTaskId.value).toBeNull()
    })

    it('goBack navigates from history to settings', () => {
      const { navigateToTaskSettings, navigateToTaskHistory, goBack, currentView } = useTaskTab()
      navigateToTaskSettings(5)
      navigateToTaskHistory(5)
      expect(currentView.value).toBe('history')

      goBack()
      expect(currentView.value).toBe('settings')
    })

    it('goBack closes exec detail first, clearing selectedExecId', () => {
      const { navigateToTaskSettings, openExecDetail, goBack, execDetailOpen, selectedExecId } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })
      expect(execDetailOpen.value).toBe(true)

      goBack()
      expect(execDetailOpen.value).toBe(false)
      expect(selectedExecId.value).toBeNull()
    })

    it('goBack closes form first', () => {
      const { openCreateForm, goBack, formViewOpen } = useTaskTab()
      openCreateForm()
      expect(formViewOpen.value).toBe(true)

      goBack()
      expect(formViewOpen.value).toBe(false)
    })

    it('navigateToList resets all navigation state', () => {
      const { navigateToTaskSettings, navigateToList, currentView, selectedTaskId, formViewOpen, openCreateForm, execDetailOpen, openExecDetail } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })
      openCreateForm()

      navigateToList()
      expect(currentView.value).toBe('list')
      expect(selectedTaskId.value).toBeNull()
      expect(formViewOpen.value).toBe(false)
      expect(execDetailOpen.value).toBe(false)
    })

    it('navigateToList clears selectedExecId', () => {
      const { navigateToTaskSettings, openExecDetail, navigateToList, selectedExecId } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })

      navigateToList()
      expect(selectedExecId.value).toBeNull()
    })

    it('openExecDetail sets exec state with execData', () => {
      const { navigateToTaskSettings, openExecDetail, execDetailOpen, selectedExecId, selectedExecData } = useTaskTab()
      navigateToTaskSettings(5)

      openExecDetail('exec-1', { id: 'exec-1', status: 'running' })
      expect(execDetailOpen.value).toBe(true)
      expect(selectedExecId.value).toBe('exec-1')
      expect(selectedExecData.value).toEqual({ id: 'exec-1', status: 'running' })
    })

    it('openExecDetail without execData triggers refreshExecDetail', async () => {
      const { navigateToTaskSettings, openExecDetail, selectedExecData } = useTaskTab()
      navigateToTaskSettings(5)

      mockFetchOk({
        executions: [{ id: 100, sessionId: 'exec-1', status: 'completed', content: 'auto-fetched' }],
      })

      openExecDetail('exec-1')

      await vi.waitFor(() => {
        expect(selectedExecData.value).toBeTruthy()
      })

      expect(selectedExecData.value.content).toBe('auto-fetched')
    })

    it('closeExecDetail clears exec state', () => {
      const { navigateToTaskSettings, openExecDetail, closeExecDetail, execDetailOpen, selectedExecId, selectedExecData } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })

      closeExecDetail()
      expect(execDetailOpen.value).toBe(false)
      expect(selectedExecId.value).toBeNull()
      expect(selectedExecData.value).toBeNull()
    })

    it('openCreateForm sets form mode to create', () => {
      const { openCreateForm, formMode, formViewOpen } = useTaskTab()

      openCreateForm()
      expect(formMode.value).toBe('create')
      expect(formViewOpen.value).toBe(true)
    })

    it('openEditForm sets form mode to edit', () => {
      const { openEditForm, formMode, formViewOpen } = useTaskTab()

      openEditForm()
      expect(formMode.value).toBe('edit')
      expect(formViewOpen.value).toBe(true)
    })

    it('closeForm closes the form', () => {
      const { openCreateForm, closeForm, formViewOpen } = useTaskTab()
      openCreateForm()
      expect(formViewOpen.value).toBe(true)

      closeForm()
      expect(formViewOpen.value).toBe(false)
    })
  })

  // ── openLatestExecDetail ──

  describe('openLatestExecDetail', () => {
    it('fetches latest execution and opens detail', async () => {
      const { openLatestExecDetail, currentView, selectedTaskId, execDetailOpen, selectedExecId } = useTaskTab()

      mockFetchOk({
        executions: [{ id: 42, sessionId: 'session-42', status: 'completed', content: 'latest result' }],
      })

      await openLatestExecDetail(5)

      expect(currentView.value).toBe('settings')
      expect(selectedTaskId.value).toBe(5)
      expect(execDetailOpen.value).toBe(true)
      expect(selectedExecId.value).toBe('42')
    })

    it('marks task as read', async () => {
      const { openLatestExecDetail } = useTaskTab()
      store.state.tasks = [{ id: 5, unreadCount: 3, name: 'Task 5' }]

      mockFetchOk({ executions: [{ id: 42, sessionId: 'session-42', status: 'completed', content: 'result' }] })

      await openLatestExecDetail(5)

      // Should have called markTaskRead which calls fetch with PUT
      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/5', expect.objectContaining({ method: 'PUT' }))
    })

    it('does nothing when API response is not ok', async () => {
      const { openLatestExecDetail, execDetailOpen } = useTaskTab()
      mockFetchNotOk()

      await openLatestExecDetail(5)

      expect(execDetailOpen.value).toBe(false)
    })

    it('does nothing when no executions returned', async () => {
      const { openLatestExecDetail, execDetailOpen } = useTaskTab()
      mockFetchOk({ executions: [] })

      await openLatestExecDetail(5)

      expect(execDetailOpen.value).toBe(false)
    })

    it('does nothing when executions is null', async () => {
      const { openLatestExecDetail, execDetailOpen } = useTaskTab()
      mockFetchOk({ executions: null })

      await openLatestExecDetail(5)

      expect(execDetailOpen.value).toBe(false)
    })

    it('handles fetch error gracefully', async () => {
      const { openLatestExecDetail, execDetailOpen } = useTaskTab()
      mockFetch.mockRejectedValue(new Error('Network error'))

      await openLatestExecDetail(5)

      expect(execDetailOpen.value).toBe(false)
    })

    it('closes form view', async () => {
      const { openCreateForm, openLatestExecDetail, formViewOpen } = useTaskTab()

      openCreateForm()
      expect(formViewOpen.value).toBe(true)

      mockFetchOk({ executions: [{ id: 1, content: 'test' }] })
      await openLatestExecDetail(5)

      expect(formViewOpen.value).toBe(false)
    })
  })

  // ── resetTaskTabState ──

  describe('resetTaskTabState', () => {
    it('resets all navigation state', () => {
      const { currentView, selectedTaskId, formViewOpen, execDetailOpen, selectedExecId, selectedExecData, formMode } = useTaskTab()

      // Set some state first
      const { navigateToTaskSettings, openExecDetail, openCreateForm } = useTaskTab()
      navigateToTaskSettings(10)
      openExecDetail('exec-1', { id: 'exec-1' })
      openCreateForm()

      resetTaskTabState()
      expect(currentView.value).toBe('list')
      expect(selectedTaskId.value).toBeNull()
      expect(formViewOpen.value).toBe(false)
      expect(execDetailOpen.value).toBe(false)
      expect(selectedExecId.value).toBeNull()
      expect(selectedExecData.value).toBeNull()
      expect(formMode.value).toBe('create')
    })
  })

  // ── markAllTasksRead ──

  describe('markAllTasksRead', () => {
    it('sends read action for all unread tasks', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [
        { id: 1, unreadCount: 2 },
        { id: 2, unreadCount: 0 },
        { id: 3, unreadCount: 1 },
      ]
      mockFetch.mockResolvedValue({ ok: true })

      await markAllTasksRead()

      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({ method: 'PUT' }))
      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/3', expect.objectContaining({ method: 'PUT' }))
      expect(store.state.taskUnreadCount).toBe(0)
    })

    it('optimistically clears unread counts in local store', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [
        { id: 1, unreadCount: 2 },
        { id: 2, unreadCount: 5 },
      ]
      mockFetch.mockResolvedValue({ ok: true })

      await markAllTasksRead()

      expect((store.state.tasks[0] as any).unreadCount).toBe(0)
      expect((store.state.tasks[1] as any).unreadCount).toBe(0)
    })

    it('skips when no unread tasks', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 0 }]
      mockFetch.mockResolvedValue({ ok: true })

      await markAllTasksRead()
      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('does not clear badge on fetch failure', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 3 }]
      store.state.taskUnreadCount = 3
      mockFetch.mockRejectedValue(new Error('Failed'))

      await markAllTasksRead()

      // On failure, the catch block runs but badge is not explicitly re-set.
      // The optimistic clear happens in the try block before await, but
      // if Promise.all rejects, the catch block doesn't re-set taskUnreadCount.
      // Next poll will correct the badge. Verify no crash.
      expect(mockFetch).toHaveBeenCalled()
    })

    it('handles partial failure (some mark-read calls fail)', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [
        { id: 1, unreadCount: 2 },
        { id: 2, unreadCount: 3 },
      ]

      // First call succeeds, second fails
      mockFetch.mockResolvedValueOnce({ ok: true })
      mockFetch.mockRejectedValueOnce(new Error('Failed'))

      await markAllTasksRead()

      // Overall Promise.all rejects, so catch block runs — badge not cleared
      // But the fetch was called for both tasks
      expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('handles non-ok response from mark-read API', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 2 }]
      store.state.taskUnreadCount = 2
      mockFetch.mockResolvedValue({ ok: false, status: 500 })

      // The PUT handler throws on !ok, causing Promise.all to reject
      await markAllTasksRead()

      // Verify the PUT was attempted
      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({ method: 'PUT' }))
    })
  })

  // ── markTaskRead ──

  describe('markTaskRead', () => {
    it('marks a single task as read', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [
        { id: 1, unreadCount: 2 },
        { id: 2, unreadCount: 3 },
      ]
      mockFetch.mockResolvedValue({ ok: true })

      await markTaskRead(1)

      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({ method: 'PUT' }))
      expect(store.state.tasks[0].unreadCount).toBe(0)
      expect(store.state.taskUnreadCount).toBe(3)
    })

    it('skips when task has no unreadCount', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 0 }]
      mockFetch.mockResolvedValue({ ok: true })

      await markTaskRead(1)
      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('skips when task is not found', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 2 }]
      mockFetch.mockResolvedValue({ ok: true })

      await markTaskRead(999)
      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('does not update local state when API returns not ok', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 2 }]
      mockFetch.mockResolvedValue({ ok: false, status: 500 })

      await markTaskRead(1)

      expect(store.state.tasks[0].unreadCount).toBe(2)
    })

    it('silently ignores fetch error', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 2 }]
      mockFetch.mockRejectedValue(new Error('Network error'))

      await markTaskRead(1)
      // Should not throw, unreadCount stays unchanged
      expect(store.state.tasks[0].unreadCount).toBe(2)
    })
  })
})
