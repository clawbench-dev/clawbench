import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// useTaskTab composable tests
// Tests completion detection (runningCount drops to 0),
// notification triggers, taskJustCompleted state, dedup logic
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
import { useTaskTab, onTaskEvent } from '@/composables/useTaskTab.ts'
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
})

// ── Helper ──

function mockTasksResponse(tasks: any[] = [], hasUnread = false) {
  mockFetch.mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ tasks, hasUnread }),
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

      // Should not throw
      await loadTasks()
      expect(store.state.tasks).toEqual([])
    })
  })

  describe('completion detection', () => {
    it('detects task completion when runningCount drops to 0', async () => {
      const { loadTasks } = useTaskTab()

      // First poll: task is running
      mockTasksResponse([makeTask({ runningCount: 1, runCount: 0 })])
      await loadTasks()
      expect(store.state.taskRunning).toBe(true)

      // Second poll: task completed (runningCount = 0)
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      // Completion effects should fire
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
      expect(store.state.taskJustCompleted).toBe(true)
    })

    it('shows browser notification on completion', async () => {
      const { loadTasks } = useTaskTab()

      // First: running
      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      // Second: completed
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

      // First: running
      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      // Second: completed
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
  })

  describe('dedup — no double notification', () => {
    it('does not re-notify on subsequent polls after completion', async () => {
      const { loadTasks } = useTaskTab()

      // First: running
      mockTasksResponse([makeTask({ runningCount: 1 })])
      await loadTasks()

      // Second: completed — should notify
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      // Third: still not running — should NOT re-notify
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
    })

    it('re-notifies if task starts running again then completes again', async () => {
      const { loadTasks } = useTaskTab()

      // First execution: running → completed
      mockTasksResponse([makeTask({ runningCount: 1, runCount: 0 })])
      await loadTasks()
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 1 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)

      // Second execution: running → completed
      mockTasksResponse([makeTask({ runningCount: 1, runCount: 1 })])
      await loadTasks()
      mockTasksResponse([makeTask({ runningCount: 0, runCount: 2 })])
      await loadTasks()
      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
    })
  })

  describe('multiple tasks', () => {
    it('notifies for each task that completes', async () => {
      const { loadTasks } = useTaskTab()

      // Both running
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 1 }),
        makeTask({ id: 2, runningCount: 1 }),
      ])
      await loadTasks()

      // Only task 1 completes
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 1 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(1)
      expect(mockShowBrowserNotification).toHaveBeenCalledWith(
        'Test Task',
        expect.objectContaining({ tag: 'task-completed-1' }),
      )

      // Task 2 also completes
      mockTasksResponse([
        makeTask({ id: 1, runningCount: 0, runCount: 1 }),
        makeTask({ id: 2, runningCount: 0, runCount: 1 }),
      ])
      await loadTasks()

      expect(mockPlayNotificationSound).toHaveBeenCalledTimes(2)
      expect(mockShowBrowserNotification).toHaveBeenCalledWith(
        'Test Task',
        expect.objectContaining({ tag: 'task-completed-2' }),
      )
    })
  })

  describe('refreshExecDetail — null content guard', () => {
    it('preserves existing content when API returns null content', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      // Must set selectedTaskId first (navigateToTaskSettings sets it)
      navigateToTaskSettings(1)

      // Open an execution detail with valid content
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

      // API returns the same execution but with null content
      // (LEFT JOIN no match in chat_history)
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          executions: [{
            id: 100,
            sessionId: 'session-100',
            status: 'completed',
            content: null,        // ← null from LEFT JOIN
            summary: 'New summary',
            createdAt: '2026-01-01T00:00:00Z',
          }],
        }),
      })

      await refreshExecDetail()

      // Content should be preserved, not overwritten with null
      expect(selectedExecData.value.content).toBe(execData.content)
      // Other fields should be updated
      expect(selectedExecData.value.summary).toBe('New summary')
    })

    it('updates content when API returns non-null content', async () => {
      const { navigateToTaskSettings, openExecDetail, refreshExecDetail, selectedExecData } = useTaskTab()

      navigateToTaskSettings(1)
      openExecDetail('100', { id: 100, content: 'old', status: 'completed' })
      expect(selectedExecData.value.content).toBe('old')

      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          executions: [{
            id: 100,
            content: 'new content',
            status: 'completed',
          }],
        }),
      })

      await refreshExecDetail()

      expect(selectedExecData.value.content).toBe('new content')
    })

    it('does nothing when selectedTaskId or selectedExecId is null', async () => {
      const { navigateToList, refreshExecDetail } = useTaskTab()
      // Reset navigation state so selectedTaskId and selectedExecId are null
      navigateToList()

      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ executions: [] }),
      })

      await refreshExecDetail()

      expect(mockFetch).not.toHaveBeenCalled()
    })
  })

  describe('loadTasks — abort', () => {
    it('aborts previous in-flight request when loadTasks is called again', async () => {
      const { loadTasks } = useTaskTab()

      // First call: return a promise that doesn't resolve immediately
      let resolveFirst: (v: any) => void
      mockFetch.mockReturnValue(new Promise(r => { resolveFirst = r }))

      const firstCall = loadTasks()

      // Second call should abort the first
      mockTasksResponse([])
      const secondCall = loadTasks()

      // Resolve the first fetch (it should be aborted)
      resolveFirst!({ ok: true, json: () => Promise.resolve({ tasks: [], hasUnread: false }) })

      await Promise.allSettled([firstCall, secondCall])

      // The second call should pass with signal
      expect(mockFetch).toHaveBeenLastCalledWith('/api/tasks', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    })

    it('ignores AbortError from superseded requests', async () => {
      const { loadTasks } = useTaskTab()

      // First call aborts with AbortError
      mockFetch.mockRejectedValueOnce({ name: 'AbortError' })
      // Second call succeeds
      mockTasksResponse([])

      // Should not throw on AbortError
      const p1 = loadTasks()
      const p2 = loadTasks()
      await Promise.allSettled([p1, p2])

      // Store should reflect the second (successful) call
      expect(store.state.tasks).toEqual([])
    })
  })

  describe('onTaskEvent', () => {
    it('debounces rapid events into a single loadTasks call', async () => {
      vi.useFakeTimers()
      useTaskTab() // ensure module state is initialized
      mockTasksResponse([])

      // Fire 3 rapid events (onTaskEvent is a standalone export)
      onTaskEvent({ task_id: '1', status: 'completed' })
      onTaskEvent({ task_id: '2', status: 'completed' })
      onTaskEvent({ task_id: '3', status: 'completed' })

      // Before debounce fires, loadTasks should not have been called yet
      // (beyond any previous calls in beforeEach)
      const callCountBefore = mockFetch.mock.calls.length

      // Advance past 200ms debounce
      vi.advanceTimersByTime(250)

      // Only one additional loadTasks call should have been made
      expect(mockFetch.mock.calls.length).toBe(callCountBefore + 1)

      vi.useRealTimers()
    })
  })

  describe('polling', () => {
    it('startTaskPolling starts interval-based polling', () => {
      vi.useFakeTimers()
      const { startTaskPolling, stopTaskPolling } = useTaskTab()
      mockTasksResponse([])

      startTaskPolling()
      expect(mockFetch).toHaveBeenCalledTimes(1) // immediate load

      vi.advanceTimersByTime(2000)
      expect(mockFetch).toHaveBeenCalledTimes(2) // first interval

      vi.advanceTimersByTime(2000)
      expect(mockFetch).toHaveBeenCalledTimes(3) // second interval

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
      expect(mockFetch).toHaveBeenCalledTimes(1) // only the initial load

      vi.useRealTimers()
    })
  })

  describe('navigation', () => {
    it('navigateToTaskSettings sets selectedTaskId and currentView', () => {
      const { navigateToTaskSettings, selectedTaskId, currentView } = useTaskTab()
      navigateToTaskSettings(42)
      expect(selectedTaskId.value).toBe(42)
      expect(currentView.value).toBe('settings')
    })

    it('navigateToTaskHistory sets currentView to history and clears unread', async () => {
      const { navigateToTaskHistory, currentView, selectedTaskId } = useTaskTab()
      // Set up a task with unread count
      store.state.tasks = [{ id: 1, unreadCount: 2, name: 'Task 1' }]
      mockFetch.mockResolvedValue({ ok: true })

      navigateToTaskHistory(1)
      expect(currentView.value).toBe('history')
      expect(selectedTaskId.value).toBe(1)
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

    it('goBack closes exec detail first', () => {
      const { navigateToTaskSettings, openExecDetail, goBack, execDetailOpen } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })
      expect(execDetailOpen.value).toBe(true)

      goBack()
      expect(execDetailOpen.value).toBe(false)
    })

    it('goBack closes form first', () => {
      const { openCreateForm, goBack, formViewOpen } = useTaskTab()
      openCreateForm()
      expect(formViewOpen.value).toBe(true)

      goBack()
      expect(formViewOpen.value).toBe(false)
    })

    it('navigateToList resets all navigation state', () => {
      const { navigateToTaskSettings, navigateToList, currentView, selectedTaskId, formViewOpen, openCreateForm } = useTaskTab()
      navigateToTaskSettings(5)
      openCreateForm()

      navigateToList()
      expect(currentView.value).toBe('list')
      expect(selectedTaskId.value).toBeNull()
      expect(formViewOpen.value).toBe(false)
    })

    it('openExecDetail sets exec state', () => {
      const { navigateToTaskSettings, openExecDetail, execDetailOpen, selectedExecId, selectedExecData } = useTaskTab()
      navigateToTaskSettings(5)

      openExecDetail('exec-1', { id: 'exec-1', status: 'running' })
      expect(execDetailOpen.value).toBe(true)
      expect(selectedExecId.value).toBe('exec-1')
      expect(selectedExecData.value).toEqual({ id: 'exec-1', status: 'running' })
    })

    it('closeExecDetail clears exec state', () => {
      const { navigateToTaskSettings, openExecDetail, closeExecDetail, execDetailOpen, selectedExecId } = useTaskTab()
      navigateToTaskSettings(5)
      openExecDetail('exec-1', { id: 'exec-1' })

      closeExecDetail()
      expect(execDetailOpen.value).toBe(false)
      expect(selectedExecId.value).toBeNull()
    })

    it('openCreateForm and openEditForm set form mode', () => {
      const { openCreateForm, openEditForm, formMode, formViewOpen } = useTaskTab()

      openCreateForm()
      expect(formMode.value).toBe('create')
      expect(formViewOpen.value).toBe(true)

      openEditForm()
      expect(formMode.value).toBe('edit')
    })

    it('closeForm closes the form', () => {
      const { openCreateForm, closeForm, formViewOpen } = useTaskTab()
      openCreateForm()
      expect(formViewOpen.value).toBe(true)

      closeForm()
      expect(formViewOpen.value).toBe(false)
    })
  })

  describe('resetTaskTabState', () => {
    it('resets all navigation state', async () => {
      const { resetTaskTabState } = await import('@/composables/useTaskTab.ts')
      const { currentView, selectedTaskId, formViewOpen, execDetailOpen } = useTaskTab()

      // Set some state first
      const { navigateToTaskSettings } = useTaskTab()
      navigateToTaskSettings(10)

      resetTaskTabState()
      expect(currentView.value).toBe('list')
      expect(selectedTaskId.value).toBeNull()
      expect(formViewOpen.value).toBe(false)
      expect(execDetailOpen.value).toBe(false)
    })
  })

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

      // Should call fetch for tasks with unreadCount > 0
      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({ method: 'PUT' }))
      expect(mockFetch).toHaveBeenCalledWith('/api/tasks/3', expect.objectContaining({ method: 'PUT' }))
      expect(store.state.taskUnreadCount).toBe(0)
    })

    it('skips when no unread tasks', async () => {
      const { markAllTasksRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 0 }]
      mockFetch.mockResolvedValue({ ok: true })

      await markAllTasksRead()
      expect(mockFetch).not.toHaveBeenCalled()
    })
  })

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
      // Task 1's unreadCount should be cleared
      expect(store.state.tasks[0].unreadCount).toBe(0)
      // taskUnreadCount should be re-derived (only task 2's 3 remains)
      expect(store.state.taskUnreadCount).toBe(3)
    })

    it('skips when task has no unreadCount', async () => {
      const { markTaskRead } = useTaskTab()
      store.state.tasks = [{ id: 1, unreadCount: 0 }]
      mockFetch.mockResolvedValue({ ok: true })

      await markTaskRead(1)
      expect(mockFetch).not.toHaveBeenCalled()
    })
  })

  describe('onTaskEvent', () => {
    it('ignores undefined data', () => {
      onTaskEvent(undefined)
      // Should not throw
    })
  })
})
