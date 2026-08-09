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

import { useQuickCommands, type QuickCommand } from '@/composables/useQuickCommands'

function makeCommand(overrides: Partial<QuickCommand> = {}): QuickCommand {
  return {
    id: 1,
    label: 'ls',
    command: 'ls -la',
    hidden: false,
    auto_execute: false,
    sort_order: 0,
    ...overrides,
  }
}

beforeEach(() => {
  mockApiGet.mockReset()
  mockApiPost.mockReset()
  mockApiPut.mockReset()
  mockApiDelete.mockReset()
})

describe('useQuickCommands', () => {
  describe('visibleCommands', () => {
    it('filters out hidden commands', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'ls', command: 'ls', hidden: false }),
        makeCommand({ id: 2, label: 'cd', command: 'cd ~', hidden: true }),
        makeCommand({ id: 3, label: 'pwd', command: 'pwd', hidden: false }),
      ])

      const { fetchCommands, visibleCommands } = useQuickCommands()
      await fetchCommands(true)

      expect(visibleCommands.value).toHaveLength(2)
      expect(visibleCommands.value.map((c: QuickCommand) => c.label)).toEqual(['ls', 'pwd'])
    })

    it('returns all commands when none are hidden', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'ls', command: 'ls' }),
        makeCommand({ id: 2, label: 'pwd', command: 'pwd' }),
      ])

      const { fetchCommands, visibleCommands } = useQuickCommands()
      await fetchCommands(true)

      expect(visibleCommands.value).toHaveLength(2)
    })

    it('returns empty when all are hidden', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'ls', command: 'ls', hidden: true }),
      ])

      const { fetchCommands, visibleCommands } = useQuickCommands()
      await fetchCommands(true)

      expect(visibleCommands.value).toHaveLength(0)
    })
  })

  describe('autoExecCommand', () => {
    it('returns the first command with auto_execute=true', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'ls', command: 'ls', auto_execute: false }),
        makeCommand({ id: 2, label: 'cd', command: 'cd ~', auto_execute: true }),
        makeCommand({ id: 3, label: 'pwd', command: 'pwd', auto_execute: true }),
      ])

      const { fetchCommands, autoExecCommand } = useQuickCommands()
      await fetchCommands(true)

      expect(autoExecCommand.value).not.toBeNull()
      expect(autoExecCommand.value!.id).toBe(2)
    })

    it('returns null when no command has auto_execute', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'ls', command: 'ls', auto_execute: false }),
      ])

      const { fetchCommands, autoExecCommand } = useQuickCommands()
      await fetchCommands(true)

      expect(autoExecCommand.value).toBeNull()
    })

    it('returns null for empty commands list', async () => {
      mockApiGet.mockResolvedValue([])

      const { fetchCommands, autoExecCommand } = useQuickCommands()
      await fetchCommands(true)

      expect(autoExecCommand.value).toBeNull()
    })

    it('prefers project-scoped auto-execute over global', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'global', command: 'g', auto_execute: true, project_only: false }),
        makeCommand({ id: 2, label: 'scoped', command: 's', auto_execute: true, project_only: true }),
      ])

      const { fetchCommands, autoExecCommand } = useQuickCommands()
      await fetchCommands(true)

      expect(autoExecCommand.value!.id).toBe(2)
    })

    it('falls back to global auto-execute when no project-scoped one exists', async () => {
      mockApiGet.mockResolvedValue([
        makeCommand({ id: 1, label: 'global', command: 'g', auto_execute: true, project_only: false }),
      ])

      const { fetchCommands, autoExecCommand } = useQuickCommands()
      await fetchCommands(true)

      expect(autoExecCommand.value!.id).toBe(1)
    })
  })

  describe('addCommand', () => {
    it('calls addItem with the correct API prefix', async () => {
      mockApiPost.mockResolvedValue({ id: 3 })
      mockApiGet.mockResolvedValue([])

      const { addCommand } = useQuickCommands()
      const result = await addCommand({ label: 'grep', command: 'grep -r "test"', hidden: false, auto_execute: false })

      expect(result).toBe(true)
      expect(mockApiPost).toHaveBeenCalledWith('/api/terminal/quick-commands', expect.objectContaining({
        label: 'grep',
        command: 'grep -r "test"',
      }))
    })

    it('returns false on API error', async () => {
      mockApiPost.mockRejectedValue(new Error('Server error'))

      const { addCommand } = useQuickCommands()
      const result = await addCommand({ label: 'grep', command: 'grep', hidden: false, auto_execute: false })

      expect(result).toBe(false)
    })
  })

  describe('updateCommand', () => {
    it('calls updateItem with the correct endpoint', async () => {
      mockApiPut.mockResolvedValue({ success: true })
      mockApiGet.mockResolvedValue([])

      const { updateCommand } = useQuickCommands()
      const result = await updateCommand(1, { label: 'ls -la' })

      expect(result).toBe(true)
      expect(mockApiPut).toHaveBeenCalledWith('/api/terminal/quick-commands/1', expect.objectContaining({
        label: 'ls -la',
      }))
    })
  })

  describe('deleteCommand', () => {
    it('calls deleteItem with the correct endpoint', async () => {
      mockApiDelete.mockResolvedValue({ success: true })
      mockApiGet.mockResolvedValue([])

      const { deleteCommand } = useQuickCommands()
      const result = await deleteCommand(1)

      expect(result).toBe(true)
      expect(mockApiDelete).toHaveBeenCalledWith('/api/terminal/quick-commands/1')
    })
  })

  describe('reorderCommands', () => {
    it('calls reorderItems with the correct endpoint', async () => {
      mockApiGet.mockResolvedValue([makeCommand({ id: 1 }), makeCommand({ id: 2 })])
      mockApiPut.mockResolvedValue({ success: true })

      const { fetchCommands, reorderCommands } = useQuickCommands()
      await fetchCommands(true)

      await reorderCommands([2, 1])

      expect(mockApiPut).toHaveBeenCalledWith('/api/terminal/quick-commands/reorder', { ids: [2, 1] })
    })
  })

  describe('singleton behavior', () => {
    it('shares state across multiple instances', async () => {
      mockApiGet.mockResolvedValue([makeCommand({ id: 1, label: 'ls' })])

      const instance1 = useQuickCommands()
      const instance2 = useQuickCommands()

      await instance1.fetchCommands(true)

      // Both should see the same data
      expect(instance2.commands.value).toHaveLength(1)
    })
  })
})
