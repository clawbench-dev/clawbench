import { describe, expect, it, vi, beforeEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// useTaskOverview composable tests
// Tests ISS-011 (raw fetch → apiPut/apiDelete) and ISS-014
// (toast on error, loadTasks only on success)
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

// Mock API helpers
const mockApiPut = vi.fn()
const mockApiDelete = vi.fn()
vi.mock('@/utils/api.ts', () => ({
  apiPut: (...args: unknown[]) => mockApiPut(...args),
  apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// Mock useTaskTab
const mockLoadTasks = vi.fn()
const mockNavigateToList = vi.fn()
vi.mock('@/composables/useTaskTab.ts', () => ({
  useTaskTab: () => ({ loadTasks: mockLoadTasks, navigateToList: mockNavigateToList }),
}))

// Mock useDialog
const mockDialogConfirm = vi.fn()
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

// Import after mocks
import { useTaskOverview } from '@/composables/useTaskOverview.ts'
import { ref } from 'vue'

beforeEach(() => {
  mockApiPut.mockReset()
  mockApiDelete.mockReset()
  mockToastShow.mockReset()
  mockLoadTasks.mockReset()
  mockNavigateToList.mockReset()
  mockDialogConfirm.mockReset()
})

// ── Helper ──

function createOverview(taskData: any = { id: 't1', status: 'active' }) {
  const task = ref(taskData)
  const deleted = vi.fn()
  const edit = vi.fn()
  const history = vi.fn()

  const actions = useTaskOverview({
    task,
    emit: { deleted, edit, history },
  })

  return { actions, task, deleted, edit, history }
}

// ── Tests ──

describe('useTaskOverview', () => {
  describe('triggerTask', () => {
    it('calls apiPut with action trigger', async () => {
      const { actions } = createOverview()
      mockApiPut.mockResolvedValue({})

      await actions.triggerTask()

      expect(mockApiPut).toHaveBeenCalledWith('/api/tasks/t1', { action: 'trigger' })
    })

    it('calls loadTasks on success', async () => {
      const { actions } = createOverview()
      mockApiPut.mockResolvedValue({})

      await actions.triggerTask()

      expect(mockLoadTasks).toHaveBeenCalled()
    })

    it('shows error toast and skips loadTasks on failure', async () => {
      const { actions } = createOverview()
      mockApiPut.mockRejectedValue(new Error('Network error'))

      await actions.triggerTask()

      expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'error' }))
      expect(mockLoadTasks).not.toHaveBeenCalled()
    })
  })

  describe('pauseTask', () => {
    it('calls apiPut with action pause', async () => {
      const { actions } = createOverview()
      mockApiPut.mockResolvedValue({})

      await actions.pauseTask()

      expect(mockApiPut).toHaveBeenCalledWith('/api/tasks/t1', { action: 'pause' })
    })

    it('shows error toast on failure', async () => {
      const { actions } = createOverview()
      mockApiPut.mockRejectedValue(new Error('Failed'))

      await actions.pauseTask()

      expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'error' }))
    })
  })

  describe('resumeTask', () => {
    it('calls apiPut with action resume', async () => {
      const { actions } = createOverview({ id: 't1', status: 'paused' })
      mockApiPut.mockResolvedValue({})

      await actions.resumeTask()

      expect(mockApiPut).toHaveBeenCalledWith('/api/tasks/t1', { action: 'resume' })
    })

    it('shows error toast on failure', async () => {
      const { actions } = createOverview({ id: 't1', status: 'paused' })
      mockApiPut.mockRejectedValue(new Error('Failed'))

      await actions.resumeTask()

      expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'error' }))
    })
  })

  describe('deleteTask', () => {
    it('calls apiDelete after confirmation, navigates to list, and emits deleted', async () => {
      const { actions, deleted } = createOverview()
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({})

      await actions.deleteTask()

      expect(mockApiDelete).toHaveBeenCalledWith('/api/tasks/t1')
      expect(mockNavigateToList).toHaveBeenCalled()
      expect(deleted).toHaveBeenCalled()
    })

    it('does not delete if confirmation denied', async () => {
      const { actions } = createOverview()
      mockDialogConfirm.mockResolvedValue(false)

      await actions.deleteTask()

      expect(mockApiDelete).not.toHaveBeenCalled()
    })

    it('shows error toast on delete failure', async () => {
      const { actions } = createOverview()
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockRejectedValue(new Error('Failed'))

      await actions.deleteTask()

      expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'error' }))
    })

    it('calls loadTasks on delete success', async () => {
      const { actions } = createOverview()
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({})

      await actions.deleteTask()

      expect(mockLoadTasks).toHaveBeenCalled()
    })

    it('navigates to list even if loadTasks fails after successful delete', async () => {
      const { actions, deleted } = createOverview()
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({})
      mockLoadTasks.mockRejectedValue(new Error('Network error'))

      await actions.deleteTask()

      expect(mockNavigateToList).toHaveBeenCalled()
      expect(deleted).toHaveBeenCalled()
    })
  })
})
