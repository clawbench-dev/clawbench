import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock API helpers
const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
const mockApiPut = vi.fn()
const mockApiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiPut: (...args: any[]) => mockApiPut(...args),
  apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

import { useCrudList, type CrudItem, _resetAllForTesting, resetAllCrudLists } from '@/composables/useCrudList'

interface TestItem extends CrudItem {
  id: number
  label: string
  command: string
  sort_order: number
}

function makeItem(overrides: Partial<TestItem> = {}): TestItem {
  return { id: 1, label: 'test', command: 'echo hi', sort_order: 0, ...overrides }
}

beforeEach(() => {
  mockApiGet.mockReset()
  mockApiPost.mockReset()
  mockApiPut.mockReset()
  mockApiDelete.mockReset()
  _resetAllForTesting()
})

describe('useCrudList', () => {
  describe('state isolation between different apiPrefixes', () => {
    it('items are independent — fetching one prefix does not overwrite another', async () => {
      const terminalCrud = useCrudList<TestItem>({ apiPrefix: '/api/terminal/quick-commands' })
      const chatCrud = useCrudList<TestItem>({ apiPrefix: '/api/chat/quick-send' })

      // Fetch terminal items
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'ls', command: 'ls' })])
      await terminalCrud.fetchItems(true)

      // Fetch chat items
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 10, label: 'hello', command: 'hi there' })])
      await chatCrud.fetchItems(true)

      // Each should have its own data
      expect(terminalCrud.items.value).toHaveLength(1)
      expect(terminalCrud.items.value[0].label).toBe('ls')

      expect(chatCrud.items.value).toHaveLength(1)
      expect(chatCrud.items.value[0].label).toBe('hello')
    })

    it('showEditDialog is independent per prefix', () => {
      const terminalCrud = useCrudList<TestItem>({ apiPrefix: '/api/terminal/quick-commands' })
      const chatCrud = useCrudList<TestItem>({ apiPrefix: '/api/chat/quick-send' })

      expect(terminalCrud.showEditDialog.value).toBe(false)
      expect(chatCrud.showEditDialog.value).toBe(false)

      // Open terminal edit dialog
      terminalCrud.showEditDialog.value = true

      // Terminal is open, chat is still closed
      expect(terminalCrud.showEditDialog.value).toBe(true)
      expect(chatCrud.showEditDialog.value).toBe(false)

      // Open chat edit dialog too
      chatCrud.showEditDialog.value = true

      // Both are open independently
      expect(terminalCrud.showEditDialog.value).toBe(true)
      expect(chatCrud.showEditDialog.value).toBe(true)

      // Close terminal — chat stays open
      terminalCrud.showEditDialog.value = false
      expect(terminalCrud.showEditDialog.value).toBe(false)
      expect(chatCrud.showEditDialog.value).toBe(true)
    })

    it('loaded flag is independent per prefix', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/a' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/b' })

      expect(crudA.loaded.value).toBe(false)
      expect(crudB.loaded.value).toBe(false)

      // Fetch only A
      mockApiGet.mockResolvedValueOnce([])
      await crudA.fetchItems(true)

      expect(crudA.loaded.value).toBe(true)
      expect(crudB.loaded.value).toBe(false)
    })

    it('addItem only refreshes the correct prefix', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/a' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/b' })

      // Initial fetch for both
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'a1' })])
      await crudA.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 2, label: 'b1' })])
      await crudB.fetchItems(true)

      // Add item to A — triggers re-fetch of A only
      mockApiPost.mockResolvedValueOnce({ id: 3 })
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'a1' }), makeItem({ id: 3, label: 'a2' })])
      const ok = await crudA.addItem({ label: 'a2', command: 'echo a2' })

      expect(ok).toBe(true)
      expect(crudA.items.value).toHaveLength(2)
      // B should be untouched
      expect(crudB.items.value).toHaveLength(1)
      expect(crudB.items.value[0].label).toBe('b1')
    })

    it('deleteItem only refreshes the correct prefix', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/a' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/b' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'a1' }), makeItem({ id: 2, label: 'a2' })])
      await crudA.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 3, label: 'b1' })])
      await crudB.fetchItems(true)

      // Delete from A
      mockApiDelete.mockResolvedValueOnce({ success: true })
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 2, label: 'a2' })])
      const ok = await crudA.deleteItem(1)

      expect(ok).toBe(true)
      expect(crudA.items.value).toHaveLength(1)
      expect(crudA.items.value[0].label).toBe('a2')
      // B untouched
      expect(crudB.items.value).toHaveLength(1)
      expect(crudB.items.value[0].label).toBe('b1')
    })

    it('updateItem only refreshes the correct prefix', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/a' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/b' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'a1' })])
      await crudA.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 2, label: 'b1' })])
      await crudB.fetchItems(true)

      // Update item in A
      mockApiPut.mockResolvedValueOnce({ success: true })
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'a1-updated' })])
      const ok = await crudA.updateItem(1, { label: 'a1-updated' })

      expect(ok).toBe(true)
      expect(crudA.items.value[0].label).toBe('a1-updated')
      // B untouched
      expect(crudB.items.value[0].label).toBe('b1')
    })

    it('reorderItems only affects the correct prefix', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/a' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/b' })

      mockApiGet.mockResolvedValueOnce([
        makeItem({ id: 1, label: 'a1', sort_order: 0 }),
        makeItem({ id: 2, label: 'a2', sort_order: 1 }),
      ])
      await crudA.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 3, label: 'b1' })])
      await crudB.fetchItems(true)

      // Reorder A
      mockApiPut.mockResolvedValueOnce({ success: true })
      const ok = await crudA.reorderItems([2, 1])

      expect(ok).toBe(true)
      // A was reordered optimistically
      expect(crudA.items.value[0].id).toBe(2)
      expect(crudA.items.value[1].id).toBe(1)
      // B untouched
      expect(crudB.items.value).toHaveLength(1)
      expect(crudB.items.value[0].label).toBe('b1')
    })
  })

  describe('singleton behavior within same apiPrefix', () => {
    it('shares items across multiple instances of the same prefix', async () => {
      const crud1 = useCrudList<TestItem>({ apiPrefix: '/api/shared' })
      const crud2 = useCrudList<TestItem>({ apiPrefix: '/api/shared' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'shared' })])
      await crud1.fetchItems(true)

      // Second instance should see the same data
      expect(crud2.items.value).toHaveLength(1)
      expect(crud2.items.value[0].label).toBe('shared')
    })

    it('shares showEditDialog across instances of the same prefix', () => {
      const crud1 = useCrudList<TestItem>({ apiPrefix: '/api/shared' })
      const crud2 = useCrudList<TestItem>({ apiPrefix: '/api/shared' })

      crud1.showEditDialog.value = true
      expect(crud2.showEditDialog.value).toBe(true)

      crud2.showEditDialog.value = false
      expect(crud1.showEditDialog.value).toBe(false)
    })
  })

  describe('fetchItems', () => {
    it('does not re-fetch when already loaded and not forced', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1 })])
      await crud.fetchItems() // force=false, first load
      expect(mockApiGet).toHaveBeenCalledTimes(1)

      // Second call without force should be skipped
      await crud.fetchItems()
      expect(mockApiGet).toHaveBeenCalledTimes(1)
    })

    it('re-fetches when forced', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1 })])
      await crud.fetchItems(true)
      expect(mockApiGet).toHaveBeenCalledTimes(1)

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1 }), makeItem({ id: 2 })])
      await crud.fetchItems(true)
      expect(mockApiGet).toHaveBeenCalledTimes(2)
      expect(crud.items.value).toHaveLength(2)
    })

    it('sets items to empty array when API returns null', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce(null)
      await crud.fetchItems(true)

      expect(crud.items.value).toEqual([])
    })

    it('silently handles fetch error', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockRejectedValueOnce(new Error('Network error'))
      await crud.fetchItems(true)

      // Should not throw, items remain empty, loaded stays false
      expect(crud.items.value).toEqual([])
      expect(crud.loaded.value).toBe(false)
    })
  })

  describe('addItem', () => {
    it('returns false on API error', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiPost.mockRejectedValueOnce(new Error('Server error'))
      const result = await crud.addItem({ label: 'x', command: 'y' })

      expect(result).toBe(false)
    })
  })

  describe('updateItem', () => {
    it('returns false on API error', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiPut.mockRejectedValueOnce(new Error('Server error'))
      const result = await crud.updateItem(1, { label: 'x' })

      expect(result).toBe(false)
    })
  })

  describe('deleteItem', () => {
    it('returns false on API error', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiDelete.mockRejectedValueOnce(new Error('Server error'))
      const result = await crud.deleteItem(1)

      expect(result).toBe(false)
    })
  })

  describe('reorderItems', () => {
    it('optimistically reorders items and confirms on success', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce([
        makeItem({ id: 1, label: 'a', sort_order: 0 }),
        makeItem({ id: 2, label: 'b', sort_order: 1 }),
        makeItem({ id: 3, label: 'c', sort_order: 2 }),
      ])
      await crud.fetchItems(true)

      mockApiPut.mockResolvedValueOnce({ success: true })
      const ok = await crud.reorderItems([3, 1, 2])

      expect(ok).toBe(true)
      expect(crud.items.value.map(i => i.id)).toEqual([3, 1, 2])
      expect(crud.items.value[0].sort_order).toBe(0)
      expect(crud.items.value[1].sort_order).toBe(1)
      expect(crud.items.value[2].sort_order).toBe(2)
    })

    it('rolls back on API error', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce([
        makeItem({ id: 1, label: 'a', sort_order: 0 }),
        makeItem({ id: 2, label: 'b', sort_order: 1 }),
      ])
      await crud.fetchItems(true)

      mockApiPut.mockRejectedValueOnce(new Error('Network error'))
      const ok = await crud.reorderItems([2, 1])

      expect(ok).toBe(false)
      // Should have rolled back to original order
      expect(crud.items.value.map(i => i.id)).toEqual([1, 2])
    })

    it('skips unknown IDs during optimistic reorder', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/test' })

      mockApiGet.mockResolvedValueOnce([
        makeItem({ id: 1, label: 'a', sort_order: 0 }),
        makeItem({ id: 2, label: 'b', sort_order: 1 }),
      ])
      await crud.fetchItems(true)

      mockApiPut.mockResolvedValueOnce({ success: true })
      // Include an ID that doesn't exist in the list
      const ok = await crud.reorderItems([2, 999, 1])

      expect(ok).toBe(true)
      // Unknown IDs are filtered out; known IDs are reordered
      expect(crud.items.value.map(i => i.id)).toEqual([2, 1])
    })
  })

  describe('resetAllCrudLists', () => {
    it('clears cached state so the next fetch refetches', async () => {
      const crud = useCrudList<TestItem>({ apiPrefix: '/api/reset-test' })

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1 })])
      await crud.fetchItems(true)
      expect(crud.loaded.value).toBe(true)
      expect(mockApiGet).toHaveBeenCalledTimes(1)

      resetAllCrudLists()

      const crud2 = useCrudList<TestItem>({ apiPrefix: '/api/reset-test' })
      expect(crud2.loaded.value).toBe(false)

      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1 }), makeItem({ id: 2 })])
      await crud2.fetchItems()
      expect(mockApiGet).toHaveBeenCalledTimes(2)
      expect(crud2.items.value).toHaveLength(2)
    })
  })

  describe('three-way isolation', () => {
    it('three different prefixes maintain fully independent state', async () => {
      const crudA = useCrudList<TestItem>({ apiPrefix: '/api/alpha' })
      const crudB = useCrudList<TestItem>({ apiPrefix: '/api/beta' })
      const crudC = useCrudList<TestItem>({ apiPrefix: '/api/gamma' })

      // Fetch all three with different data
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 1, label: 'alpha-1' })])
      await crudA.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 2, label: 'beta-1' }), makeItem({ id: 3, label: 'beta-2' })])
      await crudB.fetchItems(true)
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 4, label: 'gamma-1' })])
      await crudC.fetchItems(true)

      // Each has its own items
      expect(crudA.items.value).toHaveLength(1)
      expect(crudA.items.value[0].label).toBe('alpha-1')
      expect(crudB.items.value).toHaveLength(2)
      expect(crudC.items.value).toHaveLength(1)
      expect(crudC.items.value[0].label).toBe('gamma-1')

      // Edit dialogs are independent
      crudA.showEditDialog.value = true
      crudC.showEditDialog.value = true
      expect(crudA.showEditDialog.value).toBe(true)
      expect(crudB.showEditDialog.value).toBe(false)
      expect(crudC.showEditDialog.value).toBe(true)

      // Add item to B — A and C untouched
      mockApiPost.mockResolvedValueOnce({ id: 5 })
      mockApiGet.mockResolvedValueOnce([makeItem({ id: 2, label: 'beta-1' }), makeItem({ id: 3, label: 'beta-2' }), makeItem({ id: 5, label: 'beta-3' })])
      await crudB.addItem({ label: 'beta-3', command: 'echo' })

      expect(crudB.items.value).toHaveLength(3)
      expect(crudA.items.value).toHaveLength(1)
      expect(crudC.items.value).toHaveLength(1)
    })
  })
})
