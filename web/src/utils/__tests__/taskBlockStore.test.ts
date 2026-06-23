import { describe, expect, it, vi, beforeEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// taskBlockStore tests
// Tests ISS-013: fetchBatchTaskData should NOT mark all tasks
// as deleted on network error — only on 404/not-found-in-response
// ────────────────────────────────────────────────────────────

// Mock API helper
const mockApiGet = vi.fn()
vi.mock('@/utils/api.ts', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))

// Import after mocks
import { createTaskBlockStore } from '@/utils/taskBlockStore.ts'

beforeEach(() => {
  mockApiGet.mockReset()
})

// ── Tests ──

describe('taskBlockStore', () => {
  describe('fetchBatchData', () => {
    it('marks all entries as loading before fetch', async () => {
      const store = createTaskBlockStore()

      // Return a task list
      mockApiGet.mockResolvedValue({ tasks: [{ id: 1, name: 'Task 1' }] })

      const promise = store.fetchBatchData([
        { key: 'k1', taskId: 1 },
      ])

      // While loading, entries should be in loading state
      expect(store.blocks.k1?.loading).toBe(true)

      await promise
    })

    it('loads task data on success via apiGet', async () => {
      const store = createTaskBlockStore()

      mockApiGet.mockResolvedValue({ tasks: [{ id: 1, name: 'Task 1' }] })

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
      ])

      expect(store.blocks.k1.task).toEqual({ id: 1, name: 'Task 1' })
      expect(store.blocks.k1.loading).toBe(false)
      expect(store.blocks.k1.deleted).toBe(false)
    })

    it('marks tasks as deleted when not found in response', async () => {
      const store = createTaskBlockStore()

      // Server returns an empty task list — t1 is not found
      mockApiGet.mockResolvedValue({ tasks: [] })

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
      ])

      expect(store.blocks.k1.deleted).toBe(true)
      expect(store.blocks.k1.loading).toBe(false)
    })

    it('does NOT mark as deleted on network error', async () => {
      const store = createTaskBlockStore()

      mockApiGet.mockRejectedValue(new Error('Network error'))

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
      ])

      // ISS-013 fix: network error should NOT set deleted=true
      expect(store.blocks.k1.deleted).toBe(false)
      expect(store.blocks.k1.loading).toBe(false)
      expect(store.blocks.k1.error).toBe(true)
    })

    it('handles partial matches (some found, some not)', async () => {
      const store = createTaskBlockStore()

      mockApiGet.mockResolvedValue({
        tasks: [{ id: 1, name: 'Task 1' }],
      })

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
        { key: 'k2', taskId: 2 },
      ])

      expect(store.blocks.k1.task).toEqual({ id: 1, name: 'Task 1' })
      expect(store.blocks.k1.deleted).toBe(false)

      expect(store.blocks.k2.task).toBeNull()
      expect(store.blocks.k2.deleted).toBe(true)
    })

    it('handles empty task list response', async () => {
      const store = createTaskBlockStore()

      mockApiGet.mockResolvedValue({ tasks: [] })

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
      ])

      expect(store.blocks.k1.deleted).toBe(true)
    })

    it('skips entries that are already loaded or loading', async () => {
      const store = createTaskBlockStore()

      // Pre-load k1
      store.blocks.k1 = { taskId: 1, task: { id: 1 }, loading: false, deleted: false }

      mockApiGet.mockResolvedValue({ tasks: [] })

      await store.fetchBatchData([
        { key: 'k1', taskId: 1 },
        { key: 'k2', taskId: 2 },
      ])

      // k1 should remain unchanged (not re-fetched)
      expect(store.blocks.k1.task).toEqual({ id: 1 })
      // k2 was fetched but not found
      expect(store.blocks.k2.deleted).toBe(true)
    })
  })
})
